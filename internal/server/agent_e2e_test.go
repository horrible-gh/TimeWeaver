package server

import (
	"bytes"
	"context"
	"crypto/rand"
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
	"github.com/horrible-gh/TimeWeaver/internal/config"
	"github.com/horrible-gh/TimeWeaver/internal/store"
	"github.com/horrible-gh/TimeWeaver/internal/webui"
)

// This test executes only the pinned agent binary built by CI, on loopback,
// with random disposable DBs and commands that write inside t.TempDir().
func serverDatabase(t *testing.T) (*store.Store, *store.Store) {
	t.Helper()
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
	return db, probe
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
	settings := config.Settings{Context: "/timeweaver_svr", SecretKey: "surface-test-only-0123456789abcdef0123456789", HTTP: config.DefaultHTTPSettings()}
	settings.HTTP.Capacity = 1000
	settings.HTTP.Refill = 100
	ui, err := webui.Bundled(settings.Context)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app, err := NewAgentSurface(db, probe, settings, ui, logger)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewAgentSurface(db, db, settings, ui, logger); err == nil {
		t.Fatal("shared health/business pool accepted")
	}
	var lostAck atomic.Bool
	var manualDeliveries, unchangedSnapshots atomic.Int64
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
			if data.ManualID != nil && *data.ManualID == 41 {
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
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
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
	random := make([]byte, 32)
	if _, err = rand.Read(random); err != nil {
		t.Fatal(err)
	}
	token := "enr_" + hex.EncodeToString(random)
	digest := sha256.Sum256([]byte(token))
	if _, err = db.DB.Exec(`INSERT INTO agent_enrollment_token(enrollment_id,token_hash,device_name,group_id,expires_at) VALUES(?,?,'surface-agent',0,UTC_TIMESTAMP()+INTERVAL 1 DAY)`, random[:16], digest[:]); err != nil {
		t.Fatal(err)
	}
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
	if err = db.DB.QueryRow("SELECT device_id FROM devices WHERE device_name='surface-agent'").Scan(&deviceID); err != nil {
		t.Fatal(err)
	}
	execSQL := func(query string, args ...any) {
		t.Helper()
		if _, e := db.DB.Exec(query, args...); e != nil {
			t.Fatal(e)
		}
	}
	// The first group can run only manually; the second is one real clock trigger.
	due := time.Now().UTC().Add(10 * time.Second)
	execSQL(`INSERT INTO schedule_group(schedule_id,name,target_device,year) VALUES(12,'manual-only',?,'2000')`, deviceID)
	execSQL("INSERT INTO schedule_group(schedule_id,name,target_device,year,month,day,hour,minute,second) VALUES(13,'one-shot',?,?,?,?,?,?,?)", deviceID, due.Format("2006"), due.Format("1"), due.Format("2"), due.Format("15"), due.Format("4"), due.Format("5"))
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	for i, filename := range []string{"manual.txt", "scheduled.txt"} {
		id := strings.Repeat(fmt.Sprintf("%x", i+1), 32)
		execSQL(`INSERT INTO schedule_detail(detail_id,schedule_id,schedule_name,sequence) VALUES(UNHEX(?),?,'test-write',1)`, id, 12+i)
		command := "printf 'executed\\n' >> " + quote(filepath.Join(dir, filename))
		execSQL(`INSERT INTO task_detail(detail_id,task_type,command) VALUES(UNHEX(?),'command',?)`, id, command)
	}
	execSQL(`INSERT INTO manual_execution(manual_id,detail_id,is_immediate,status,schedule_datetime) VALUES(41,UNHEX(?),1,'wait',UTC_TIMESTAMP())`, strings.Repeat("1", 32))
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
		if err := db.DB.QueryRow(query).Scan(&n); err != nil {
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
	wait("scheduled/manual results and outbox drain", func() bool {
		return count("SELECT COUNT(*) FROM execution_log WHERE result_code=0") == 2 && lostAck.Load() && manualDeliveries.Load() >= 2 && unchangedSnapshots.Load() > 0 && healthyEmpty()
	})
	stop()
	if count("SELECT COUNT(*) FROM execution_log") != 2 || count("SELECT COUNT(*) FROM execution_running") != 0 || count("SELECT COUNT(*) FROM manual_execution WHERE manual_id=41 AND status='done'") != 1 {
		t.Fatal("duplicate execution or incomplete transition")
	}
	for _, name := range []string{"manual.txt", "scheduled.txt"} {
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
	if count("SELECT COUNT(*) FROM execution_log") != 2 {
		t.Fatal("restart re-executed a completed job")
	}
	if count("SELECT COUNT(*) FROM agent_enrollment_token WHERE used_at IS NOT NULL") != 1 {
		t.Fatal("unexpected re-enrollment")
	}
	t.Log("PASS: real pinned agent enrollment, heartbeat, scheduled/manual commands, lost-ACK retry, single result persistence, credential restart, unified UI/favicon and graceful shutdown")
}
