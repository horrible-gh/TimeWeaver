package agentapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/config"
	"github.com/horrible-gh/TimeWeaver/internal/store"
)

// Separate pools on one real file exercise SQLite's cross-instance locking;
// a single-connection pool or an in-memory DB would hide enrollment/claim races.
func sqliteAgentStores(t *testing.T) (*store.Store, *store.Store, config.Database) {
	t.Helper()
	c := config.Database{Type: "sqlite", Path: filepath.Join(t.TempDir(), "agents.sqlite")}
	first, err := store.Open(context.Background(), c, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { first.Close() })
	if _, err = first.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = first.RepairCriticalSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, err := store.Open(context.Background(), c, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { second.Close() })
	return first, second, c
}

func TestSQLiteIdentityEnrollmentAndRefreshAcrossInstances(t *testing.T) {
	first, second, _ := sqliteAgentStores(t)
	services := []*Identity{identityService(t, first, ""), identityService(t, second, "")}
	ctx := context.Background()
	raw := enrollment(t, services[0], strptr("sqlite-device"), 0)
	type response struct {
		data TokenData
		err  error
	}
	race := func(enroll bool, raw string) TokenData {
		t.Helper()
		start := make(chan struct{})
		results := make(chan response, 8)
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			service := services[i%len(services)]
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				var v TokenData
				var err error
				if enroll {
					v, err = service.Enroll(ctx, raw, "sqlite-device", strptr("sqlite-v1"))
				} else {
					v, err = service.Rotate(ctx, raw)
				}
				results <- response{v, err}
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		wins := 0
		var winner TokenData
		for result := range results {
			if result.err == nil {
				wins++
				winner = result.data
			} else if enroll {
				requireCode(t, result.err, 403, "enrollment_token_invalid")
			} else {
				requireCode(t, result.err, 401, "invalid_token")
			}
		}
		if wins != 1 {
			t.Fatalf("one-time operation committed %d winners", wins)
		}
		return winner
	}
	current := race(true, raw)
	if current.DeviceName != "sqlite-device" || current.DeviceID <= 0 {
		t.Fatal("enrollment lost device identity", current.DeviceID, current.DeviceName)
	}
	deviceID := current.DeviceID
	for round := 0; round < 3; round++ {
		previous := current
		current = race(false, previous.RefreshToken)
		for _, service := range services {
			_, err := service.VerifyAccess(ctx, previous.AccessToken)
			requireCode(t, err, 403, "device_revoked")
			principal, err := service.VerifyAccess(ctx, current.AccessToken)
			if err != nil || principal.DeviceID != deviceID || principal.DeviceName != "sqlite-device" {
				t.Fatal("rotated credential is not shared by server instances", principal, err)
			}
		}
	}
	var devices, live int
	if err := first.DB.QueryRow("SELECT COUNT(*) FROM devices WHERE device_name='sqlite-device'").Scan(&devices); err != nil || devices != 1 {
		t.Fatal("enrollment duplicated the device", devices, err)
	}
	if err := second.DB.QueryRow("SELECT COUNT(*) FROM agent_device_credential WHERE revoked_at IS NULL").Scan(&live); err != nil || live != 1 {
		t.Fatal("rotation left multiple live credentials", live, err)
	}
	// HTTP dispatch and token persistence use the same SQLite-backed identity.
	body, _ := json.Marshal(map[string]string{"refresh_token": current.RefreshToken})
	w := post(httpService(t, services[1]), "/timeweaver_svr/api/agent/v1/token/refresh/", string(body))
	if w.Code != 200 || w.Header().Get("Location") != "" || !strings.Contains(w.Body.String(), `"schema_version":"1"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestSQLiteIdentityFailureRollsBackCredentialsAndEnrollment(t *testing.T) {
	first, second, _ := sqliteAgentStores(t)
	s := identityService(t, first, "")
	other := identityService(t, second, "")
	ctx := context.Background()
	raw := enrollment(t, s, strptr("sqlite-rollback"), 0)
	_, err := s.Enroll(ctx, raw, "wrong-name", nil)
	requireCode(t, err, 403, "enrollment_token_invalid")
	if _, err = first.DB.Exec(`CREATE TRIGGER reject_agent_credential BEFORE INSERT ON agent_device_credential BEGIN SELECT RAISE(ABORT,'fixture credential storage unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	_, err = s.Enroll(ctx, raw, "sqlite-rollback", nil)
	requireCode(t, err, 503, "unavailable")
	var count int
	var used sql.NullTime
	if err = first.DB.QueryRow("SELECT COUNT(*) FROM devices WHERE device_name='sqlite-rollback'").Scan(&count); err != nil || count != 0 {
		t.Fatal("failed enrollment left a device", count, err)
	}
	if err = first.DB.QueryRow("SELECT used_at FROM agent_enrollment_token WHERE token_hash=?", digest(raw)).Scan(store.ScanNullTime(&used)); err != nil || used.Valid {
		t.Fatal("failed enrollment consumed its token", used, err)
	}
	if _, err = first.DB.Exec("DROP TRIGGER reject_agent_credential"); err != nil {
		t.Fatal(err)
	}
	current, err := other.Enroll(ctx, raw, "sqlite-rollback", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = first.DB.Exec(`CREATE TRIGGER reject_agent_credential BEFORE INSERT ON agent_device_credential BEGIN SELECT RAISE(ABORT,'fixture credential storage unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	_, err = s.Rotate(ctx, current.RefreshToken)
	requireCode(t, err, 503, "unavailable")
	if _, err = other.VerifyAccess(ctx, current.AccessToken); err != nil {
		t.Fatal("failed rotation revoked the working credential", err)
	}
	if _, err = first.DB.Exec("DROP TRIGGER reject_agent_credential"); err != nil {
		t.Fatal(err)
	}
	if _, err = other.Rotate(ctx, current.RefreshToken); err != nil {
		t.Fatal("rolled-back refresh token could not be retried", err)
	}
	// Legacy RFC3339 expiry text is compared as an instant, not lexically.
	expired := enrollment(t, s, nil, 0)
	if _, err = first.DB.Exec("UPDATE agent_enrollment_token SET expires_at=? WHERE token_hash=?", time.Now().Add(-time.Hour).In(time.FixedZone("offset", 9*3600)).Format(time.RFC3339), digest(expired)); err != nil {
		t.Fatal(err)
	}
	_, err = s.Enroll(ctx, expired, "expired", nil)
	requireCode(t, err, 403, "enrollment_token_invalid")
}

func TestSQLiteRuntimeConsistentReadOnlySnapshotAndHTTP(t *testing.T) {
	first, second, _ := sqliteAgentStores(t)
	f := newRuntimeFixtureOn(t, first)
	ctx := context.Background()
	id := identityService(t, second, "")
	private, err := id.Enroll(ctx, enrollment(t, id, nil, 0), "other-device", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = second.DB.Exec("INSERT INTO schedule_group(schedule_id,name,target_device) VALUES(99,'private-schedule',?)", private.DeviceID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_, err = f.runtime.Heartbeat(ctx, f.principal, HeartbeatRequest{AgentVersion: "sqlite-v2", State: "HEALTHY"}, recursiveHousekeep)
		if err != nil {
			t.Fatal("repeat heartbeat failed", err)
		}
	}
	var heartbeat time.Time
	var version, capabilities string
	if err = second.DB.QueryRow("SELECT last_heartbeat_at,version,agent_capabilities FROM devices WHERE device_id=?", f.principal.DeviceID).Scan(store.ScanTime(&heartbeat), &version, &capabilities); err != nil || heartbeat.IsZero() || version != "sqlite-v2" || capabilities != recursiveHousekeep {
		t.Fatal("heartbeat was not persisted", err)
	}
	read, err := f.runtime.beginSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer read.close()
	before, err := f.runtime.loadSnapshot(ctx, read.conn, f.principal.DeviceID)
	if err != nil || len(before.Schedules) != 1 || before.Schedules[0].ID != 12 || len(before.Schedules[0].Details) != 1 {
		t.Fatal("snapshot leaked another device or lost its task", before.Schedules, err)
	}
	writeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := second.DB.BeginTx(writeCtx, nil)
	if err != nil {
		t.Fatal("snapshot blocked an independent WAL writer", err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(writeCtx, "UPDATE schedule_group SET name='new-group' WHERE schedule_id=12"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(writeCtx, "UPDATE schedule_detail SET schedule_name='new-detail' WHERE schedule_id=12"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	after, err := f.runtime.loadSnapshot(ctx, read.conn, f.principal.DeviceID)
	if err != nil || !reflect.DeepEqual(before.Schedules, after.Schedules) {
		t.Fatal("snapshot mixed committed versions", err)
	}
	if _, err = read.conn.ExecContext(ctx, "UPDATE devices SET version='must-not-write' WHERE device_id=?", f.principal.DeviceID); err == nil {
		t.Fatal("snapshot connection could write")
	}
	if err = read.commit(ctx); err != nil {
		t.Fatal(err)
	}
	read.close()
	current, err := f.runtime.Snapshot(ctx, f.principal)
	if err != nil || current.Data.Schedules[0].Name != "new-group" || *current.Data.Schedules[0].Details[0].Name != "new-detail" {
		t.Fatal("new snapshot did not advance", err)
	}
	if _, err = first.DB.Exec("UPDATE devices SET version='pool-reusable' WHERE device_id=?", f.principal.DeviceID); err != nil {
		t.Fatal("snapshot left query_only enabled in the writer pool", err)
	}
	canceled, stop := context.WithCancel(ctx)
	interrupted, err := f.runtime.beginSnapshot(canceled)
	if err != nil {
		stop()
		t.Fatal(err)
	}
	stop()
	if err = interrupted.commit(canceled); err == nil {
		interrupted.close()
		t.Fatal("cancelled snapshot committed")
	}
	interrupted.close()
	if _, err = first.DB.Exec("UPDATE devices SET version='after-cancel' WHERE device_id=?", f.principal.DeviceID); err != nil {
		t.Fatal("cancelled snapshot left an open or read-only transaction", err)
	}
	base := "/timeweaver_svr/api/agent/v1/snapshot"
	w := runtimeRequest(f.handler, "GET", base+"/", "", f.token.AccessToken, nil)
	if w.Code != 200 || w.Header().Get("ETag") == "" || strings.Contains(w.Body.String(), "private-schedule") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = runtimeRequest(f.handler, "GET", base, "", f.token.AccessToken, map[string]string{"If-None-Match": w.Header().Get("ETag")})
	if w.Code != 304 || w.Body.Len() != 0 {
		t.Fatal("unchanged SQLite snapshot did not honor ETag", w.Code, w.Body.String())
	}
	if _, err = first.DB.Exec("UPDATE task_detail SET task_type='housekeep',house_keep_recursive=1,destination_path='/fixture',house_keep_days=2"); err != nil {
		t.Fatal(err)
	}
	w = runtimeRequest(f.handler, "GET", base, "", f.token.AccessToken, nil)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "agent_upgrade_required") {
		t.Fatal("recursive capability gate missing", w.Code, w.Body.String())
	}
	w = runtimeRequest(f.handler, "GET", base, "", f.token.AccessToken, map[string]string{"X-TW-Agent-Capabilities": recursiveHousekeep})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err = first.DB.Exec("DELETE FROM task_detail"); err != nil {
		t.Fatal(err)
	}
	w = runtimeRequest(f.handler, "GET", base, "", f.token.AccessToken, nil)
	if w.Code != 500 || !strings.Contains(w.Body.String(), "server_error") {
		t.Fatal("incomplete task yielded a partial snapshot", w.Code, w.Body.String())
	}
	if _, err = first.DB.Exec("UPDATE devices SET status='inactive' WHERE device_id=?", f.principal.DeviceID); err != nil {
		t.Fatal(err)
	}
	w = runtimeRequest(f.handler, "GET", base, "", f.token.AccessToken, map[string]string{"If-None-Match": w.Header().Get("ETag")})
	if w.Code != 403 {
		t.Fatal("revocation was bypassed by conditional snapshot", w.Code)
	}
}

func TestSQLiteHealthUsesIndependentReadOnlyPool(t *testing.T) {
	first, _, c := sqliteAgentStores(t)
	probe, err := store.Open(context.Background(), c, true)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	h, err := NewHealthHTTP(probe, 200*time.Millisecond, 5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewHealthHTTP(first, time.Second, 5); err == nil {
		t.Fatal("writable health pool accepted")
	}
	if _, err = NewIdentity(probe, config.Settings{SecretKey: testSecret}); err == nil {
		t.Fatal("identity accepted a read-only store")
	}
	if _, err = NewRuntime(probe); err == nil {
		t.Fatal("runtime accepted a read-only store")
	}
	if _, err = NewExecution(probe); err == nil {
		t.Fatal("execution accepted a read-only store")
	}
	check := func(code int) {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
		if w.Code != code {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	busy, err := first.DB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Rollback()
	check(200) // Business writer/pool is occupied; the WAL read probe still works.
	if err = busy.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err = probe.DB.Exec("DELETE FROM migrations"); err == nil {
		t.Fatal("health connection can write")
	}
	if _, err = first.DB.Exec("DROP TABLE agent_event"); err != nil {
		t.Fatal(err)
	}
	check(503)
}

func sqliteDetail() []byte { return bytes.Repeat([]byte{0x11}, 16) }
