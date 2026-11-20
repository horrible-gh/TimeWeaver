package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/horrible-gh/TimeWeaver/internal/access"
	"github.com/horrible-gh/TimeWeaver/internal/config"
	"github.com/horrible-gh/TimeWeaver/internal/dashboard"
	"github.com/horrible-gh/TimeWeaver/internal/store"
	"github.com/horrible-gh/TimeWeaver/internal/webui"
)

// This test executes only the pinned agent binary built by CI, on loopback,
// with random disposable DBs and commands that write inside t.TempDir().
func serverDatabase(t *testing.T) (*store.Store, *store.Store) {
	db, probe, _ := serverDatabaseWithConfig(t)
	return db, probe
}

// The browser restart test reopens independent pools over this same disposable
// database, so persistence cannot accidentally depend on a service or connection.
func serverDatabaseWithConfig(t *testing.T) (*store.Store, *store.Store, config.Database) {
	t.Helper()
	if sqliteFixtureEnabled() {
		return sqliteServerDatabaseWithConfig(t)
	}
	if os.Getenv("TIMEWEAVER_TEST_MYSQL_ALLOW_CREATE") != "1" {
		t.Skip("requires explicit disposable DB permission")
	}
	host := os.Getenv("TIMEWEAVER_TEST_MYSQL_HOST")
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.IsLoopback() {
		t.Fatal("loopback database required")
	}
	cfg := mysql.NewConfig()
	cfg.User = "root"
	cfg.Passwd = os.Getenv("TIMEWEAVER_TEST_MYSQL_PASSWORD")
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(host, "3306")
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	admin := sql.OpenDB(connector)
	sum := sha256.Sum256([]byte(t.TempDir()))
	name := "tw_surface_" + hex.EncodeToString(sum[:12])
	if _, err = admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4"); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec("DROP DATABASE `" + name + "`"); admin.Close() })
	c := config.Database{Type: "mysql", Host: host, Port: 3306, User: "root", Password: cfg.Passwd, Name: name}
	db, err := store.Open(context.Background(), c, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err = db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	probe, err := store.Open(context.Background(), c, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { probe.Close() })
	return db, probe, c
}

func TestGoAgentEndToEnd(t *testing.T) {
	binary := os.Getenv("TW_AGENT_BINARY")
	if binary == "" {
		t.Skip("requires CI-built pinned Go agent binary")
	}
	if runtime.GOOS != "linux" {
		t.Skip("native process integration currently runs on Linux")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(binary)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatal("agent binary unavailable")
	}
	db, probe := serverDatabase(t)
	settings := config.Settings{Context: "/timeweaver_svr", SecretKey: "surface-test-only-0123456789abcdef0123456789", HTTP: config.DefaultHTTPSettings(), IdleTimeout: 30 * time.Minute}
	settings.HTTP.Capacity = 1000
	settings.HTTP.Refill = 100
	ui, err := webui.Bundled(settings.Context)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err = db.RepairCriticalSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = access.Initialize(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	// Run the unchanged device protocol assertions through the full browser
	// composition, including the human session routes on this same listener.
	app, err := NewBrowserSurface(context.Background(), db, probe, settings, ui, logger, access.HTTPOptions{Origin: "http://" + ln.Addr().String()})
	if err != nil {
		ln.Close()
		t.Fatal(err)
	}
	if _, err = NewAgentSurface(db, db, settings, ui, logger); err == nil {
		t.Fatal("shared health/business pool accepted")
	}
	var lostAck atomic.Bool
	var manualID atomic.Int64
	var manualDeliveries, unchangedSnapshots, snapshotReads atomic.Int64
	transport := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/results") {
			raw, e := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if e != nil {
				t.Error("cannot read test request")
				w.WriteHeader(500)
				return
			}
			r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(raw))
			var data struct {
				ManualID *int64 `json:"manual_id"`
			}
			_ = json.Unmarshal(raw, &data)
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, r)
			if data.ManualID != nil && *data.ManualID == manualID.Load() {
				manualDeliveries.Add(1)
				// Deliberately lose the first successful ACK AFTER the result committed.
				if rec.Code == 200 && lostAck.CompareAndSwap(false, true) {
					w.Header().Set("Retry-After", "1")
					jsonReply(w, r, 503, map[string]any{"detail": map[string]string{"code": "unavailable", "message": "test transport interruption"}})
					return
				}
			}
			for k, values := range rec.Header() {
				for _, v := range values {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(rec.Code)
			_, _ = w.Write(rec.Body.Bytes())
			return
		}
		if strings.HasSuffix(r.URL.Path, "/snapshot") {
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, r)
			if rec.Code == 200 || rec.Code == 304 {
				snapshotReads.Add(1)
			}
			if rec.Code == 304 {
				unchangedSnapshots.Add(1)
			}
			for k, values := range rec.Header() {
				for _, v := range values {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(rec.Code)
			_, _ = w.Write(rec.Body.Bytes())
			return
		}
		app.ServeHTTP(w, r)
	})
	root, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serve(root, ln, transport, app.sweeper, 10*time.Second, logger, func() { app.draining.Store(true) })
	}()
	// Registered before subprocess cleanup, so agents drain while the server lives.
	t.Cleanup(func() {
		cancel()
		select {
		case e := <-done:
			if e != nil {
				t.Error(e)
			}
		case <-time.After(15 * time.Second):
			t.Error("server did not stop")
		}
	})
	base := "http://" + ln.Addr().String()
	client := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for path, kind := range map[string]string{"/login": "text/html", "/dashboard/tasks": "text/html", "/favicon.ico?v=tw-clock-1": "image/", settings.Context + "/api/agent/v1/health": "application/json"} {
		resp, e := client.Get(base + path)
		if e != nil {
			t.Fatal(e)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), kind) {
			t.Fatalf("unified route %s: %d", path, resp.StatusCode)
		}
		if strings.Contains(kind, "html") && !bytes.Contains(body, []byte("/favicon.ico?v=tw-clock-1")) {
			t.Fatal("built UI has no branded favicon")
		}
	}
	for path, want := range map[string]int{settings.Context + "/api/agent/v1/snapshot": 401, settings.Context + "/dashboard/tasks": 404, settings.Context + "/api/agent/v1/no-such-route": 404} {
		resp, e := client.Get(base + path)
		if e != nil {
			t.Fatal(e)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatal(path, resp.StatusCode)
		}
	}
	dir := t.TempDir()
	credentialPath, statusPath := filepath.Join(dir, "credential.json"), filepath.Join(dir, "status.json")
	configuration := map[string]any{"device": "surface-agent", "timezone": "UTC", "api": map[string]any{
		"base_url": base + settings.Context, "credential_path": credentialPath, "status_path": statusPath,
		"enrollment_token_env": "TW_SURFACE_ENROLLMENT", "heartbeat_interval": 0.2, "snapshot_sync_interval": 0.2,
		"read_timeout": 2, "connect_timeout": 1, "shutdown_grace": 5, "manual_dispatch_delay": 0.1,
		"retry_initial_delay": 0.1, "retry_max_delay": 1, "retry_jitter_ratio": 0,
		"outbox_capacity": 100, "outbox_high_watermark": 80, "outbox_low_watermark": 50, "outbox_sender_workers": 1}}
	raw, _ := json.Marshal(configuration)
	configPath := filepath.Join(dir, "config.json")
	if err = os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	// The real agent now consumes material created by the human management
	// service, instead of an independently seeded enrollment table row.
	if _, err = db.RepairCriticalSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = fixtureExec(t, db, "UPDATE users SET password=? WHERE user_id='timeweaver'", deviceTestHash); err != nil {
		t.Fatal(err)
	}
	if _, err = access.Initialize(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	human, err := access.New(context.Background(), db, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	principal, session, err := human.Login(context.Background(), "timeweaver", deviceTestPassword, "127.0.0.1", "")
	if err != nil {
		t.Fatal(err)
	}
	issue, err := dashboard.New(human).IssueEnrollment(context.Background(), session, principal.CSRF, dashboard.EnrollmentInput{GroupID: gid(0), Name: dname("surface-agent"), TTLHours: 24})
	if err != nil {
		t.Fatal(err)
	}
	token := issue.Token
	env := []string{}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "TW_SURFACE_ENROLLMENT=") {
			env = append(env, entry)
		}
	}
	enrollCtx, enrollCancel := context.WithTimeout(context.Background(), 15*time.Second)
	enroll := exec.CommandContext(enrollCtx, binary, "enroll", "--config", configPath)
	enroll.Dir = dir
	enroll.Env = append(append([]string{}, env...), "TW_SURFACE_ENROLLMENT="+token)
	output, e := enroll.CombinedOutput()
	enrollCancel()
	if e != nil {
		t.Fatalf("real agent enrollment failed: %v", e)
	}
	if bytes.Contains(output, []byte(token)) {
		t.Fatal("enrollment output disclosed token")
	}
	var deviceID int64
	if err = fixtureRow(t, db, "SELECT device_id FROM devices WHERE device_name='surface-agent'").Scan(&deviceID); err != nil {
		t.Fatal(err)
	}
	// Build the schedules, paired tasks and manual requests through the human
	// management service before the real agent consumes them.
	management := dashboard.New(human)
	var reservationID int64
	due := time.Now().UTC().Add(10 * time.Second)
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	for i, filename := range []string{"manual.txt", "scheduled.txt", "reserved.txt"} {
		input := dashboard.ScheduleInput{GroupID: 0, DeviceID: deviceID, Name: filename, Status: "active", CronInput: dashboard.CronInput{Year: "2000"}}
		if i == 1 {
			input.CronInput = dashboard.CronInput{Year: due.Format("2006"), Month: due.Format("1"), Day: due.Format("2"), Hour: due.Format("15"), Minute: due.Format("4"), Second: due.Format("5")}
		}
		sid, e := management.SaveSchedule(context.Background(), session, principal.CSRF, input, true)
		if e != nil {
			t.Fatal(e)
		}
		command := "printf 'executed\\n' >> " + quote(filepath.Join(dir, filename))
		tid, e := management.SaveTask(context.Background(), session, principal.CSRF, dashboard.TaskInput{ScheduleID: sid, Name: "test-write", Status: "active", Type: "command", Command: &command, Sequence: 1}, true)
		if e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			ids, err := management.QueueManual(context.Background(), session, principal.CSRF, dashboard.ManualInput{DetailID: tid, Immediate: true})
			if err != nil || len(ids) != 1 {
				t.Fatalf("human manual request: %v, %v", ids, err)
			}
			manualID.Store(ids[0])
		} else if i == 2 {
			// The reservation and all early-execution assertions use the database
			// clock, independently of the runner's timezone or wall-clock offset.
			var databaseNow time.Time
			if err := fixtureRow(t, db, "SELECT UTC_TIMESTAMP()").Scan(&databaseNow); err != nil {
				t.Fatal(err)
			}
			ids, err := management.QueueManual(context.Background(), session, principal.CSRF, dashboard.ManualInput{DetailID: tid, ScheduleDatetime: databaseNow.UTC().Add(15 * time.Second).Format(time.RFC3339)})
			if err != nil || len(ids) != 1 {
				t.Fatalf("human reserved request: %v, %v", ids, err)
			}
			reservationID = ids[0]
		}
	}
	wait := func(label string, condition func() bool) {
		t.Helper()
		deadline := time.Now().Add(35 * time.Second)
		for time.Now().Before(deadline) {
			if condition() {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("timed out: " + label)
	}
	count := func(query string) int64 {
		t.Helper()
		var n int64
		if err := fixtureRow(t, db, query).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	healthyEmpty := func() bool {
		b, e := os.ReadFile(statusPath)
		if e != nil {
			return false
		}
		var v struct {
			State   string `json:"state"`
			Outbox  int    `json:"outbox_size"`
			Allowed bool   `json:"execution_allowed"`
		}
		return json.Unmarshal(b, &v) == nil && v.State == "HEALTHY" && v.Allowed && v.Outbox == 0
	}
	startAgent := func() func() {
		t.Helper()
		logFile, e := os.OpenFile(filepath.Join(dir, fmt.Sprintf("agent-%d.log", time.Now().UnixNano())), os.O_CREATE|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		cmd := exec.Command(binary, "run", "--config", configPath)
		cmd.Dir = dir
		cmd.Env = env
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		if e = cmd.Start(); e != nil {
			logFile.Close()
			t.Fatal(e)
		}
		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
		stopped := false
		stop := func() {
			if stopped {
				return
			}
			stopped = true
			_ = cmd.Process.Signal(os.Interrupt)
			select {
			case e := <-exited:
				if e != nil {
					t.Errorf("agent process failed: %v", e)
				}
			case <-time.After(10 * time.Second):
				_ = cmd.Process.Kill()
				<-exited
				t.Error("agent graceful stop timed out")
			}
			logFile.Close()
		}
		t.Cleanup(stop)
		return stop
	}
	stop := startAgent()
	observedBeforeDue := false
	wait("reservation becomes due without an early claim or execution", func() bool {
		var now, reserved time.Time
		var status string
		var results, running int64
		if err := fixtureRow(t, db, `SELECT UTC_TIMESTAMP(),me.schedule_datetime,me.status,
 (SELECT COUNT(*) FROM execution_log el WHERE el.manual_id=me.manual_id),
 (SELECT COUNT(*) FROM execution_running er WHERE er.detail_id=me.detail_id)
 FROM manual_execution me WHERE me.manual_id=?`, reservationID).Scan(&now, &reserved, &status, &results, &running); err != nil {
			t.Fatal(err)
		}
		if !now.Before(reserved) {
			return true
		}
		if status != "wait" || results != 0 || running != 0 {
			t.Fatalf("reservation executed before database due time: status=%s results=%d running=%d", status, results, running)
		}
		if snapshotReads.Load() >= 2 {
			observedBeforeDue = true
		}
		return false
	})
	if !observedBeforeDue {
		t.Fatal("reservation was not observed before due time after real agent snapshot polling")
	}
	wait("scheduled/immediate/reserved results and outbox drain", func() bool {
		return count("SELECT COUNT(*) FROM execution_log WHERE result_code=0") == 3 && lostAck.Load() && manualDeliveries.Load() >= 2 && unchangedSnapshots.Load() > 0 && healthyEmpty()
	})
	stop()
	if count("SELECT COUNT(*) FROM execution_log") != 3 || count("SELECT COUNT(*) FROM execution_running") != 0 || count(fmt.Sprintf("SELECT COUNT(*) FROM manual_execution WHERE manual_id IN (%d,%d) AND status='done'", manualID.Load(), reservationID)) != 2 {
		t.Fatal("duplicate execution or incomplete transition")
	}
	if count(fmt.Sprintf("SELECT COUNT(*) FROM execution_log el JOIN manual_execution me ON me.manual_id=el.manual_id WHERE me.manual_id=%d AND el.start_time>=me.schedule_datetime", reservationID)) != 1 {
		t.Fatal("reserved execution did not start at or after the stored UTC due time")
	}
	history, err := management.ExecutionHistory(context.Background(), session, "", "")
	if err != nil || len(history) != 3 {
		t.Fatalf("real agent results missing from human history: %d, %v", len(history), err)
	}
	for _, name := range []string{"manual.txt", "scheduled.txt", "reserved.txt"} {
		b, e := os.ReadFile(filepath.Join(dir, name))
		if e != nil || string(b) != "executed\n" {
			t.Fatal("task was lost or executed twice", name)
		}
	}
	before := count("SELECT MAX(credential_id) FROM agent_device_credential")
	// Restart from the real on-disk credential, without another enrollment secret.
	_ = os.Remove(statusPath)
	stopAgain := startAgent()
	wait("credential-based restart", func() bool {
		return count("SELECT MAX(credential_id) FROM agent_device_credential") > before && healthyEmpty()
	})
	stopAgain()
	if count("SELECT COUNT(*) FROM execution_log") != 3 {
		t.Fatal("restart re-executed a completed job")
	}
	if count("SELECT COUNT(*) FROM agent_enrollment_token WHERE used_at IS NOT NULL") != 1 {
		t.Fatal("unexpected re-enrollment")
	}
	t.Log("PASS: real pinned agent with human-created enrollment, schedules, tasks and immediate/reserved requests; no early reservation claim, scheduled/manual commands, human history, lost-ACK retry, single result persistence, credential restart, unified UI/favicon and graceful shutdown")
}
