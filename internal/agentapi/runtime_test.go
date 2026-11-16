package agentapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func strptr(s string) *string { return &s }
func TestRuntimeCanonicalVector(t *testing.T) {
	raw, err := os.ReadFile("testdata/runtime-canonical.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Data      SnapshotData `json:"data"`
		Canonical string       `json:"canonical"`
		Digest    string       `json:"sha256"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	got, err := canonicalSnapshot(fixture.Data)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(got)
	if string(got) != fixture.Canonical || hex.EncodeToString(hash[:]) != fixture.Digest {
		t.Fatalf("Python canonical vector differs: %s", got)
	}
	fixture.Data.GeneratedAt = time.Now()
	fixture.Data.Revision = "ignored"
	second, err := canonicalSnapshot(fixture.Data)
	if err != nil || string(second) != string(got) {
		t.Fatal("transport timestamp/revision influenced content hash")
	}
}
func TestRuntimeTaskNormalization(t *testing.T) {
	days := int64(7)
	for _, kind := range []string{"command", "copy", "archive", "housekeep"} {
		task, err := normalizeTask(SnapshotTask{Type: kind, Command: strptr("echo 그대로"), ArchiveType: strptr("zip"), SourcePath: strptr("source"), DestinationPath: strptr("target"), DateFormat: strptr(" null "), TargetDateFormat: strptr("  "), DestinationDateFormat: strptr("%Y"), HouseKeepDays: &days, HouseKeepRecursive: true, ErrorOnMissingSource: true})
		if err != nil {
			t.Fatal(err)
		}
		if task.DateFormat != nil || task.TargetDateFormat != nil || *task.DestinationDateFormat != "%Y" || !task.ErrorOnMissingSource {
			t.Fatal("sentinel/date normalization changed")
		}
		if task.HouseKeepRecursive != (kind == "housekeep") {
			t.Fatal("unrelated task inherited recursive flag")
		}
		switch kind {
		case "command":
			if task.Command == nil || task.SourcePath != nil || task.ArchiveType != nil || task.DestinationPath != nil || task.HouseKeepDays != nil {
				t.Fatal(task)
			}
		case "copy":
			if task.Command != nil || task.ArchiveType != nil || task.HouseKeepDays != nil || task.SourcePath == nil {
				t.Fatal(task)
			}
		case "archive":
			if task.Command != nil || task.HouseKeepDays != nil || task.ArchiveType == nil {
				t.Fatal(task)
			}
		case "housekeep":
			if task.Command != nil || task.ArchiveType != nil || task.SourcePath != nil || task.HouseKeepDays == nil {
				t.Fatal(task)
			}
		}
	}
	if _, err := normalizeTask(SnapshotTask{Type: "unknown"}); err == nil {
		t.Fatal("inconsistent task accepted")
	}
	for _, value := range []string{"", "housekeep_recursive_v10", "xhousekeep_recursive_v1", "HOUSEKEEP_RECURSIVE_V1"} {
		if supportsRecursive(value) {
			t.Fatal("inexact capability accepted")
		}
	}
	if !supportsRecursive("other, housekeep_recursive_v1 ,other") {
		t.Fatal("capability list rejected")
	}
	for _, state := range []string{"BOOTSTRAP", "HEALTHY", "DEGRADED", "HALTED"} {
		if !validHeartbeat(HeartbeatRequest{AgentVersion: "v1", State: state}) {
			t.Fatal(state)
		}
	}
	for _, request := range []HeartbeatRequest{{AgentVersion: "v1"}, {AgentVersion: "v1", State: "HEALTHY", AppliedRevision: strptr("sha256:bad")}, {AgentVersion: strings.Repeat("한", 51), State: "HEALTHY"}} {
		if validHeartbeat(request) {
			t.Fatal("invalid heartbeat accepted")
		}
	}
}

type runtimeFixture struct {
	runtime   *Runtime
	auth      *IdentityHTTP
	handler   *RuntimeHTTP
	principal DeviceIdentity
	token     TokenData
}

func newRuntimeFixture(t *testing.T) runtimeFixture {
	t.Helper()
	db := identityStore(t)
	id := identityService(t, db, "")
	auth := httpService(t, id)
	token, err := id.Enroll(context.Background(), enrollment(t, id, nil, 0), "runtime-device", strptr("runtime-v1"))
	if err != nil {
		t.Fatal(err)
	}
	principal, err := id.VerifyAccess(context.Background(), token.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(db)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewRuntimeHTTP(auth, runtime)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`INSERT INTO schedule_group(schedule_id,name,target_device) VALUES(12,'runtime-schedule',?)`,
		`INSERT INTO schedule_detail(detail_id,schedule_id,schedule_name,sequence) VALUES(UNHEX('11111111111111111111111111111111'),12,'runtime-detail',10)`,
		`INSERT INTO task_detail(detail_id,command) VALUES(UNHEX('11111111111111111111111111111111'),'echo runtime')`,
	} {
		var args []any
		if strings.Contains(query, "?") {
			args = []any{principal.DeviceID}
		}
		if _, err = db.DB.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	return runtimeFixture{runtime, auth, handler, principal, token}
}
func runtimeRequest(h http.Handler, method, path, body, token string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.RemoteAddr = "127.0.0.1:1234"
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		request.Header.Set(k, v)
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	return response
}
func TestMySQLRuntimeHTTPBoundaries(t *testing.T) {
	f := newRuntimeFixture(t)
	ctx := context.Background()
	base := "/timeweaver_svr/api/agent/v1"
	request := func(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
		return runtimeRequest(f.handler, method, base+path, body, f.token.AccessToken, headers)
	}
	for _, body := range []string{`{}`, `null`, `{"agent_version":"v2","state":"unknown"}`, `{"agent_version":"v2","state":"HEALTHY","applied_revision":"bad"}`} {
		if w := request("POST", "/heartbeat", body, nil); w.Code != 422 {
			t.Fatalf("invalid body: %d %s", w.Code, w.Body)
		}
	}
	for i := 0; i < 3; i++ {
		w := request("POST", "/heartbeat/", `{"agent_version":"v2","state":"HEALTHY","applied_revision":null}`, map[string]string{"X-TW-Agent-Capabilities": "other, housekeep_recursive_v1", "X-TW-Schema-Version": "0"})
		if w.Code != 200 || w.Header().Get("Location") != "" {
			t.Fatalf("heartbeat: %d %s", w.Code, w.Body)
		}
	}
	var version, cap string
	var applied sql.NullString
	var beat time.Time
	if err := f.runtime.db.DB.QueryRow("SELECT version,agent_capabilities,applied_revision,last_heartbeat_at FROM devices WHERE device_id=?", f.principal.DeviceID).Scan(&version, &cap, &applied, &beat); err != nil {
		t.Fatal(err)
	}
	if version != "v2" || cap != recursiveHousekeep || applied.Valid || beat.IsZero() {
		t.Fatal("heartbeat not persisted")
	}
	if w := request("POST", "/heartbeat", `{"agent_version":"v2","state":"HALTED"}`, nil); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if err := f.runtime.db.DB.QueryRow("SELECT agent_capabilities FROM devices WHERE device_id=?", f.principal.DeviceID).Scan(&cap); err != nil || cap != "" {
		t.Fatal("old agent capability not cleared", err)
	}
	w := request("GET", "/snapshot/", "", nil)
	if w.Code != 200 || w.Header().Get("ETag") == "" || w.Header().Get("Location") != "" {
		t.Fatal(w.Code, w.Body)
	}
	etag := w.Header().Get("ETag")
	for _, p := range []string{"/snapshot", "/snapshot/"} {
		w = request("GET", p, "", map[string]string{"If-None-Match": etag})
		if w.Code != 304 || w.Body.Len() != 0 || w.Header().Get("ETag") != etag {
			t.Fatal("conditional snapshot", w.Code, w.Body)
		}
	}
	for _, v := range []string{"", "0", "-1", "garbage"} {
		w = request("GET", "/snapshot", "", map[string]string{"X-TW-Schema-Version": v})
		if w.Code != 422 || !strings.Contains(w.Body.String(), "schema_mismatch") {
			t.Fatal(w.Code, w.Body)
		}
	}
	w = request("POST", "/snapshot", "{}", nil)
	if w.Code != 405 || w.Header().Get("Allow") != "GET" {
		t.Fatal(w.Code, w.Body)
	}
	w = request("GET", "/snapshot//", "", nil)
	if w.Code != 404 || w.Header().Get("Location") != "" {
		t.Fatal("path cleanup redirected")
	}
	w = runtimeRequest(f.handler, "GET", base+"/snapshot", "", "", map[string]string{"If-None-Match": etag})
	if w.Code != 401 {
		t.Fatal("cache bypassed authentication")
	}
	if _, err := f.runtime.db.DB.Exec(`UPDATE task_detail SET task_type='housekeep',house_keep_recursive=1,destination_path='/tmp/runtime-test',house_keep_days=5`); err != nil {
		t.Fatal(err)
	}
	result, err := f.runtime.Snapshot(ctx, f.principal)
	if err != nil {
		t.Fatal(err)
	}
	w = request("GET", "/snapshot", "", map[string]string{"If-None-Match": result.ETag})
	if w.Code != 409 || !strings.Contains(w.Body.String(), "agent_upgrade_required") {
		t.Fatal("capability gate skipped before 304", w.Code, w.Body)
	}
	w = request("GET", "/snapshot", "", map[string]string{"If-None-Match": result.ETag, "X-TW-Agent-Capabilities": recursiveHousekeep})
	if w.Code != 304 {
		t.Fatal(w.Code, w.Body)
	}
	if _, err = f.runtime.db.DB.Exec(`DELETE FROM task_detail`); err != nil {
		t.Fatal(err)
	}
	w = request("GET", "/snapshot", "", nil)
	if w.Code != 500 || !strings.Contains(w.Body.String(), "server_error") {
		t.Fatal("partial orphaned snapshot returned", w.Code, w.Body)
	}
	if _, err = f.runtime.db.DB.Exec(`UPDATE devices SET status='inactive' WHERE device_id=?`, f.principal.DeviceID); err != nil {
		t.Fatal(err)
	}
	_, err = f.runtime.Heartbeat(ctx, f.principal, HeartbeatRequest{AgentVersion: "inactive", State: "HEALTHY"}, "")
	requireCode(t, err, 403, "device_revoked")
	w = request("GET", "/snapshot", "", map[string]string{"If-None-Match": result.ETag})
	if w.Code != 403 {
		t.Fatal("revoked device bypassed cache auth", w.Code, w.Body)
	}
}
func TestMySQLRuntimeRepeatableRead(t *testing.T) {
	f := newRuntimeFixture(t)
	ctx := context.Background()
	read, err := f.runtime.beginSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer read.close()
	before, err := f.runtime.loadSnapshot(ctx, read.conn, f.principal.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	// A concurrent writer must not be blocked by this read-only snapshot.
	writeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := f.runtime.db.DB.BeginTx(writeCtx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(writeCtx, `UPDATE schedule_group SET name='new-group' WHERE schedule_id=12`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(writeCtx, `UPDATE schedule_detail SET schedule_name='new-detail' WHERE schedule_id=12`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	after, err := f.runtime.loadSnapshot(ctx, read.conn, f.principal.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Schedules, after.Schedules) {
		t.Fatal("mixed committed versions inside a snapshot")
	}
	if _, err = read.conn.ExecContext(ctx, `UPDATE devices SET version='must-not-write' WHERE device_id=?`, f.principal.DeviceID); err == nil {
		t.Fatal("snapshot transaction allowed writes")
	}
	if err = read.commit(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := f.runtime.Snapshot(ctx, f.principal)
	if err != nil {
		t.Fatal(err)
	}
	if current.Data.Schedules[0].Name != "new-group" || *current.Data.Schedules[0].Details[0].Name != "new-detail" {
		t.Fatal("new snapshot did not advance")
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	_, err = f.runtime.Snapshot(canceled, f.principal)
	requireCode(t, err, 503, "unavailable")
}
func TestPythonRuntimeInterop(t *testing.T) {
	if os.Getenv("TIMEWEAVER_RUNTIME_INTEROP") != "1" {
		t.Skip("requires frozen Python runtime dependencies")
	}
	db := identityStore(t)
	identity := identityService(t, db, "")
	auth := httpService(t, identity)
	runtime, err := NewRuntime(db)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewRuntimeHTTP(auth, runtime)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimSuffix(r.URL.Path, "/")
		if strings.HasSuffix(path, "/enroll") || strings.HasSuffix(path, "/token") {
			auth.ServeHTTP(w, r)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	var database string
	if err = db.DB.QueryRow("SELECT DATABASE()").Scan(&database); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("python", filepath.Join("..", "..", "scripts", "test-go-runtime-compat.py"), server.URL, database)
	cmd.Env = append(os.Environ(), "TW_INTEROP_SECRET="+testSecret, "TW_INTEROP_AGENT_SECRET=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Python runtime interop failed: %v\n%s", err, out)
	}
	t.Log(strings.TrimSpace(string(out)))
}
