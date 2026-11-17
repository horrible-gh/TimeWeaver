package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/access"
	"github.com/horrible-gh/TimeWeaver/internal/agentapi"
	"github.com/horrible-gh/TimeWeaver/internal/config"
	"github.com/horrible-gh/TimeWeaver/internal/dashboard"
	"github.com/horrible-gh/TimeWeaver/internal/store"
)

const deviceTestPassword = "migration-test-password"
const deviceTestHash = "$pbkdf2-sha256$29000$Y29tcGF0aWJpbGl0eS1zYWx0$8W/0pwrIBoQnZcgvWBYWEQ0taGg5MkmakNBL8BT02/A"

type deviceSession struct{ token, csrf string }
type deviceFixture struct {
	db, probe *store.Store
	auth      *access.Service
	dashboard *dashboard.Service
	identity  *agentapi.Identity
	sessions  map[string]deviceSession
	settings  config.Settings
}

func deviceFixtureFor(t *testing.T) *deviceFixture {
	t.Helper()
	db, probe := serverDatabase(t)
	ctx := context.Background()
	for _, q := range []string{"INSERT INTO `groups`(group_id,group_name,status) VALUES(7,'Seven','active'),(8,'Eight','active'),(9,'Disabled','inactive')", "INSERT INTO devices(group_id,device_name) VALUES(7,'seven'),(8,'eight')"} {
		if _, err := db.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.DB.Exec("UPDATE users SET password=? WHERE user_id='timeweaver'", deviceTestHash); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"viewer", "operator", "editor", "global-editor"} {
		group := 7
		if id == "global-editor" {
			group = 0
		}
		if _, err := db.DB.Exec("INSERT INTO users(user_id,name,password,role,group_id) VALUES(?,?,?,'user',?)", id, id, deviceTestHash, group); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.RepairCriticalSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := access.Initialize(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"viewer", "operator"} {
		if _, err := db.DB.Exec("UPDATE tw_group_member SET role=? WHERE user_id=?", role, role); err != nil {
			t.Fatal(err)
		}
	}
	auth, err := access.New(ctx, db, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	settings := config.Settings{Context: "/timeweaver_svr", SecretKey: "device-management-test-0123456789abcdef0123456789", IdleTimeout: 30 * time.Minute, HTTP: config.DefaultHTTPSettings()}
	identity, err := agentapi.NewIdentity(db, settings)
	if err != nil {
		t.Fatal(err)
	}
	f := &deviceFixture{db, probe, auth, dashboard.New(auth), identity, map[string]deviceSession{}, settings}
	for _, id := range []string{"timeweaver", "viewer", "operator", "editor", "global-editor"} {
		p, token, err := auth.Login(ctx, id, deviceTestPassword, "127.0.0.1", "")
		if err != nil {
			t.Fatal(err)
		}
		f.sessions[id] = deviceSession{token, p.CSRF}
	}
	return f
}
func errorCode(err error) string {
	var a *access.Error
	if errors.As(err, &a) {
		return a.Code
	}
	var b *agentapi.APIError
	if errors.As(err, &b) {
		return b.Code
	}
	return fmt.Sprint(err)
}
func expectCode(t *testing.T, err error, code string) {
	t.Helper()
	if errorCode(err) != code {
		t.Fatalf("wanted %s, got %v", code, err)
	}
}
func gid(id int64) *int64    { return &id }
func dname(s string) *string { return &s }
func TestMySQLDeviceMutationScopesAndPreservation(t *testing.T) {
	f := deviceFixtureFor(t)
	ctx := context.Background()
	editor := f.sessions["editor"]
	admin := f.sessions["timeweaver"]
	for _, role := range []string{"viewer", "operator"} {
		p := f.sessions[role]
		_, err := f.dashboard.SaveDevice(ctx, p.token, p.csrf, dashboard.DeviceInput{GroupID: gid(7), Name: "denied", Status: "active"}, true)
		expectCode(t, err, "forbidden")
	}
	_, err := f.dashboard.SaveDevice(ctx, editor.token, editor.csrf, dashboard.DeviceInput{GroupID: gid(8), Name: "denied", Status: "active"}, true)
	expectCode(t, err, "forbidden")
	_, err = f.dashboard.SaveDevice(ctx, editor.token, "wrong", dashboard.DeviceInput{GroupID: gid(7), Name: "denied", Status: "active"}, true)
	expectCode(t, err, "csrf_failed")
	id, err := f.dashboard.SaveDevice(ctx, editor.token, editor.csrf, dashboard.DeviceInput{GroupID: gid(7), Name: " 한글-日本語 ", Status: "active", Creator: "forged"}, true)
	if err != nil {
		t.Fatal(err)
	}
	var actor, name string
	if err = f.db.DB.QueryRow("SELECT creator,device_name FROM devices WHERE device_id=?", id).Scan(&actor, &name); err != nil || actor != "editor" || name != "한글-日本語" {
		t.Fatal("server actor/name", err)
	}
	_, err = f.dashboard.SaveDevice(ctx, editor.token, editor.csrf, dashboard.DeviceInput{ID: id, GroupID: gid(8), Name: name, Status: "active"}, false)
	expectCode(t, err, "device_group_changed")
	var other int64
	if err = f.db.DB.QueryRow("SELECT device_id FROM devices WHERE group_id=8").Scan(&other); err != nil {
		t.Fatal(err)
	}
	_, err = f.dashboard.SaveDevice(ctx, editor.token, editor.csrf, dashboard.DeviceInput{ID: other, GroupID: gid(7), Name: "forged", Status: "inactive"}, false)
	expectCode(t, err, "forbidden")
	expectCode(t, f.dashboard.DeleteDevice(ctx, editor.token, editor.csrf, other), "forbidden")
	_, err = f.dashboard.SaveDevice(ctx, admin.token, admin.csrf, dashboard.DeviceInput{ID: -1, Name: "sentinel", Status: "active"}, false)
	expectCode(t, err, "invalid_request")
	_, err = f.dashboard.SaveDevice(ctx, editor.token, editor.csrf, dashboard.DeviceInput{GroupID: gid(7), Name: name, Status: "active"}, true)
	expectCode(t, err, "device_name_conflict")
	_, err = f.dashboard.SaveDevice(ctx, admin.token, admin.csrf, dashboard.DeviceInput{GroupID: gid(8), Name: name, Status: "active"}, true)
	if err != nil {
		t.Fatal("same name in another group", err)
	}
	_, err = f.dashboard.SaveDevice(ctx, admin.token, admin.csrf, dashboard.DeviceInput{GroupID: gid(9), Name: "inactive", Status: "active"}, true)
	expectCode(t, err, "group_inactive")
	rows, err := f.dashboard.Devices(ctx, f.sessions["viewer"].token)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range rows {
		if d.GroupID != 7 || d.CanEdit {
			t.Fatal("viewer scope/edit hint")
		}
	}
	if _, err = f.dashboard.SaveDevice(ctx, editor.token, editor.csrf, dashboard.DeviceInput{ID: id, Name: "renamed", Status: "inactive", Modifier: "forged"}, false); err != nil {
		t.Fatal(err)
	}
	if err = f.db.DB.QueryRow("SELECT modifier FROM devices WHERE device_id=?", id).Scan(&actor); err != nil || actor != "editor" {
		t.Fatal("modifier", err)
	}
	if err = f.dashboard.DeleteDevice(ctx, editor.token, editor.csrf, id); err != nil {
		t.Fatal(err)
	}
	// Each linked record independently prevents physical deletion.
	for _, kind := range []string{"schedule", "credential", "event", "used_enrollment", "unused_enrollment"} {
		id, err = f.dashboard.SaveDevice(ctx, editor.token, editor.csrf, dashboard.DeviceInput{GroupID: gid(7), Name: kind, Status: "active"}, true)
		if err != nil {
			t.Fatal(err)
		}
		switch kind {
		case "schedule":
			_, err = f.db.DB.Exec("INSERT INTO schedule_group(name,target_device) VALUES('linked',?)", id)
		case "credential":
			_, err = f.db.DB.Exec("INSERT INTO agent_device_credential(device_id,token_hash,expires_at) VALUES(?,UNHEX(REPEAT('ab',32)),UTC_TIMESTAMP()+INTERVAL 1 DAY)", id)
		case "event":
			_, err = f.db.DB.Exec("INSERT INTO agent_event(device_id,event_type,severity,occurred_at) VALUES(?,'recovered','info',UTC_TIMESTAMP())", id)
		default:
			var issue dashboard.EnrollmentIssue
			issue, err = f.dashboard.IssueEnrollment(ctx, admin.token, admin.csrf, dashboard.EnrollmentInput{GroupID: gid(7), Name: dname(kind), TTLHours: 1})
			if err == nil && kind == "used_enrollment" {
				_, err = f.db.DB.Exec("UPDATE agent_enrollment_token SET used_at=UTC_TIMESTAMP(),used_by_device_id=? WHERE enrollment_id=UNHEX(REPLACE(?,'-',''))", id, issue.ID)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		expectCode(t, f.dashboard.DeleteDevice(ctx, editor.token, editor.csrf, id), "device_in_use")
	}
	// A removed grant is checked again; cached/UI permissions cannot authorize.
	if _, err = f.db.DB.Exec("DELETE FROM tw_group_member WHERE user_id='editor'"); err != nil {
		t.Fatal(err)
	}
	_, err = f.dashboard.SaveDevice(ctx, editor.token, editor.csrf, dashboard.DeviceInput{GroupID: gid(7), Name: "revoked", Status: "active"}, true)
	expectCode(t, err, "forbidden")
}
func TestMySQLEnrollmentLifecycleAndAuditRollback(t *testing.T) {
	f := deviceFixtureFor(t)
	ctx := context.Background()
	admin := f.sessions["timeweaver"]
	for _, role := range []string{"viewer", "operator", "editor", "global-editor"} {
		p := f.sessions[role]
		_, err := f.dashboard.IssueEnrollment(ctx, p.token, p.csrf, dashboard.EnrollmentInput{GroupID: gid(7), TTLHours: 1})
		expectCode(t, err, "admin_required")
		_, err = f.dashboard.Enrollments(ctx, p.token, nil)
		expectCode(t, err, "admin_required")
	}
	for _, in := range []dashboard.EnrollmentInput{{GroupID: gid(7), TTLHours: 0}, {GroupID: gid(7), TTLHours: 169}, {TTLHours: 1}, {GroupID: gid(-1), TTLHours: 1}} {
		_, err := f.dashboard.IssueEnrollment(ctx, admin.token, admin.csrf, in)
		expectCode(t, err, "invalid_request")
	}
	_, err := f.dashboard.IssueEnrollment(ctx, admin.token, admin.csrf, dashboard.EnrollmentInput{GroupID: gid(9), TTLHours: 1})
	expectCode(t, err, "group_inactive")
	issue, err := f.dashboard.IssueEnrollment(ctx, admin.token, admin.csrf, dashboard.EnrollmentInput{GroupID: gid(7), Name: dname("日本語-agent"), TTLHours: 24})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(issue.Token, "enr_") || len(issue.Token) != 68 {
		t.Fatal("raw token contract")
	}
	var hash []byte
	var hours int
	if err = f.db.DB.QueryRow("SELECT token_hash,TIMESTAMPDIFF(HOUR,created_at,expires_at) FROM agent_enrollment_token WHERE enrollment_id=UNHEX(REPLACE(?,'-',''))", issue.ID).Scan(&hash, &hours); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte(issue.Token))
	if !bytes.Equal(want[:], hash) || hours != 24 {
		t.Fatal("digest/TTL contract")
	}
	items, err := f.dashboard.Enrollments(ctx, admin.token, gid(7))
	if err != nil || len(items) != 1 || items[0].Status != "unused" {
		t.Fatal(items, err)
	}
	raw, _ := json.Marshal(items)
	if bytes.Contains(raw, []byte(issue.Token)) || bytes.Contains(raw, []byte("token_hash")) {
		t.Fatal("secret leaked in list")
	}
	_, err = f.identity.Enroll(ctx, issue.Token, "mismatch", nil)
	expectCode(t, err, "enrollment_token_invalid")
	data, err := f.identity.Enroll(ctx, issue.Token, "日本語-agent", nil)
	if err != nil {
		t.Fatal(err)
	}
	items, err = f.dashboard.Enrollments(ctx, admin.token, nil)
	if err != nil || len(items) != 1 || items[0].Status != "used" || items[0].UsedBy == nil || *items[0].UsedBy != data.DeviceID {
		t.Fatal("consumption not recorded", err)
	}
	_, err = f.dashboard.RevokeEnrollment(ctx, admin.token, admin.csrf, issue.ID)
	expectCode(t, err, "already_used")
	_, err = f.identity.Enroll(ctx, issue.Token, "日本語-agent", nil)
	expectCode(t, err, "enrollment_token_invalid")
	// Disable blocks the existing credential; re-enable preserves legacy pause semantics.
	editor := f.sessions["editor"]
	_, err = f.dashboard.SaveDevice(ctx, editor.token, editor.csrf, dashboard.DeviceInput{ID: data.DeviceID, Name: "日本語-agent", Status: "inactive"}, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.identity.VerifyAccess(ctx, data.AccessToken)
	expectCode(t, err, "device_revoked")
	_, err = f.identity.Rotate(ctx, data.RefreshToken)
	expectCode(t, err, "device_inactive")
	_, err = f.dashboard.SaveDevice(ctx, editor.token, editor.csrf, dashboard.DeviceInput{ID: data.DeviceID, Name: "日本語-agent", Status: "active"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.identity.VerifyAccess(ctx, data.AccessToken); err != nil {
		t.Fatal(err)
	}
	// Reissue reuses the device identity and revokes the previous credential.
	second, err := f.dashboard.IssueEnrollment(ctx, admin.token, admin.csrf, dashboard.EnrollmentInput{GroupID: gid(7), Name: issue.Name, TTLHours: 1})
	if err != nil {
		t.Fatal(err)
	}
	next, err := f.identity.Enroll(ctx, second.Token, *issue.Name, nil)
	if err != nil || next.DeviceID != data.DeviceID {
		t.Fatal("reissue", err)
	}
	_, err = f.identity.VerifyAccess(ctx, data.AccessToken)
	expectCode(t, err, "device_revoked")
	free, err := f.dashboard.IssueEnrollment(ctx, admin.token, admin.csrf, dashboard.EnrollmentInput{GroupID: gid(0), Name: dname("  "), TTLHours: 1})
	if err != nil || free.Name != nil {
		t.Fatal(err)
	}
	revoked, err := f.dashboard.RevokeEnrollment(ctx, admin.token, admin.csrf, free.ID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.dashboard.RevokeEnrollment(ctx, admin.token, admin.csrf, free.ID)
	if err != nil || !again.Revoked.Equal(revoked.Revoked) {
		t.Fatal("idempotent revoke", err)
	}
	_, err = f.identity.Enroll(ctx, free.Token, "any-name", nil)
	expectCode(t, err, "enrollment_token_invalid")
	_, err = f.dashboard.RevokeEnrollment(ctx, admin.token, admin.csrf, "00000000-0000-4000-8000-000000000001")
	expectCode(t, err, "not_found")
	expired, err := f.dashboard.IssueEnrollment(ctx, admin.token, admin.csrf, dashboard.EnrollmentInput{GroupID: gid(8), TTLHours: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.DB.Exec("UPDATE agent_enrollment_token SET expires_at=UTC_TIMESTAMP()-INTERVAL 1 SECOND WHERE enrollment_id=UNHEX(REPLACE(?,'-',''))", expired.ID); err != nil {
		t.Fatal(err)
	}
	items, err = f.dashboard.Enrollments(ctx, admin.token, gid(8))
	if err != nil || len(items) != 1 || items[0].Status != "expired" {
		t.Fatal(err)
	}
	manualID, err := f.dashboard.SaveDevice(ctx, admin.token, admin.csrf, dashboard.DeviceInput{GroupID: gid(7), Name: "delete-rollback", Status: "active"}, true)
	if err != nil {
		t.Fatal(err)
	}
	// An unavailable audit table must roll back every mutation, not just log a warning.
	var before int
	f.db.DB.QueryRow("SELECT COUNT(*) FROM agent_enrollment_token").Scan(&before)
	if _, err = f.db.DB.Exec("RENAME TABLE tw_audit TO tw_audit_test_offline"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.db.DB.Exec("RENAME TABLE tw_audit_test_offline TO tw_audit") })
	failed, err := f.dashboard.IssueEnrollment(ctx, admin.token, admin.csrf, dashboard.EnrollmentInput{GroupID: gid(7), TTLHours: 1})
	expectCode(t, err, "unavailable")
	if failed.Token != "" {
		t.Fatal("uncommitted secret escaped")
	}
	var after int
	f.db.DB.QueryRow("SELECT COUNT(*) FROM agent_enrollment_token").Scan(&after)
	if before != after {
		t.Fatal("failed token persisted")
	}
	_, err = f.dashboard.SaveDevice(ctx, admin.token, admin.csrf, dashboard.DeviceInput{GroupID: gid(7), Name: "rollback", Status: "active"}, true)
	expectCode(t, err, "unavailable")
	f.db.DB.QueryRow("SELECT COUNT(*) FROM devices WHERE device_name='rollback'").Scan(&after)
	if after != 0 {
		t.Fatal("failed create persisted")
	}
	_, err = f.dashboard.SaveDevice(ctx, admin.token, admin.csrf, dashboard.DeviceInput{ID: next.DeviceID, Name: "rollback-update", Status: "inactive"}, false)
	expectCode(t, err, "unavailable")
	var storedName string
	f.db.DB.QueryRow("SELECT device_name FROM devices WHERE device_id=?", next.DeviceID).Scan(&storedName)
	if storedName != *issue.Name {
		t.Fatal("failed update persisted")
	}
	_, err = f.dashboard.RevokeEnrollment(ctx, admin.token, admin.csrf, expired.ID)
	expectCode(t, err, "unavailable")
	expectCode(t, f.dashboard.DeleteDevice(ctx, admin.token, admin.csrf, manualID), "unavailable")
	f.db.DB.QueryRow("SELECT COUNT(*) FROM devices WHERE device_id=?", manualID).Scan(&after)
	if after != 1 {
		t.Fatal("failed delete persisted")
	}
	var timestamp sql.NullTime
	f.db.DB.QueryRow("SELECT revoked_at FROM agent_enrollment_token WHERE enrollment_id=UNHEX(REPLACE(?,'-',''))", expired.ID).Scan(&timestamp)
	if timestamp.Valid {
		t.Fatal("failed revoke persisted")
	}
}
func TestMySQLEnrollmentConcurrentConsumeOrRevoke(t *testing.T) {
	f := deviceFixtureFor(t)
	ctx := context.Background()
	p := f.sessions["timeweaver"]
	for round := 0; round < 8; round++ {
		name := fmt.Sprintf("race-%d", round)
		issue, err := f.dashboard.IssueEnrollment(ctx, p.token, p.csrf, dashboard.EnrollmentInput{GroupID: gid(7), Name: &name, TTLHours: 1})
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		var consumeErr, revokeErr error
		wg.Add(2)
		go func() { defer wg.Done(); <-start; _, consumeErr = f.identity.Enroll(ctx, issue.Token, name, nil) }()
		go func() {
			defer wg.Done()
			<-start
			_, revokeErr = f.dashboard.RevokeEnrollment(ctx, p.token, p.csrf, issue.ID)
		}()
		close(start)
		wg.Wait()
		if consumeErr == nil {
			expectCode(t, revokeErr, "already_used")
		} else {
			expectCode(t, consumeErr, "enrollment_token_invalid")
			if revokeErr != nil {
				t.Fatal(revokeErr)
			}
		}
	}
}
func TestMySQLDeviceHTTPBoundaries(t *testing.T) {
	f := deviceFixtureFor(t)
	guard, err := access.NewHTTP(f.auth, f.settings.Context, access.HTTPOptions{Origin: "http://127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	h := dashboard.NewHTTP(f.auth, guard, f.settings.Context)
	admin := f.sessions["timeweaver"]
	call := func(method, path, body string, p deviceSession, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://127.0.0.1"+f.settings.Context+path, strings.NewReader(body))
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Origin", "http://127.0.0.1")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", p.csrf)
		if p.token != "" {
			req.AddCookie(&http.Cookie{Name: "tw_session", Value: p.token})
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	for _, tc := range []struct {
		method, path, body string
		p                  deviceSession
		headers            map[string]string
		status             int
	}{
		{"GET", "/dashboard/devices/get_devices", "", deviceSession{}, nil, 401},
		{"POST", "/dashboard/devices/insert_device", `{"group_id":7,"device_name":"new","status":"active"}`, admin, map[string]string{"Origin": "https://evil.invalid"}, 403},
		{"POST", "/dashboard/devices/insert_device", `{"group_id":7,"device_name":"new","status":"active"}`, admin, map[string]string{"X-CSRF-Token": "wrong"}, 403},
		{"POST", "/dashboard/devices/insert_device", `{"group_id":7,"device_name":"new","status":"active"}`, f.sessions["viewer"], nil, 403},
		{"GET", "/dashboard/devices/get_devices?group_id=8", "", f.sessions["viewer"], map[string]string{"Authorization": "Bearer forged-agent"}, 401},
		{"GET", "/dashboard/agent-enrollment-tokens", "", f.sessions["editor"], nil, 403},
		{"GET", "/dashboard/agent-enrollment-tokens?group_id=-1", "", admin, nil, 422},
		{"GET", "/dashboard/agent-enrollment-tokens?group_id=7&group_id=8", "", admin, nil, 422},
		{"POST", "/dashboard/agent-enrollment-tokens", `{"group_id":7,"ttl_hours":0}`, admin, nil, 422},
		{"POST", "/dashboard/agent-enrollment-tokens", `{"group_id":7,"token":"forged"}`, admin, nil, 422},
		{"PUT", "/dashboard/agent-enrollment-tokens", `{}`, admin, nil, 405},
		{"DELETE", "/dashboard/agent-enrollment-tokens/not-a-uuid", "", admin, nil, 422},
		{"DELETE", "/dashboard/devices/remove_device/-1", "", admin, nil, 422},
		{"GET", "/dashboard/missing", "", admin, nil, 404},
	} {
		rec := call(tc.method, tc.path, tc.body, tc.p, tc.headers)
		if rec.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
	rec := call("POST", "/dashboard/agent-enrollment-tokens/", `{"group_id":7,"device_name":"http-agent"}`, admin, nil)
	if rec.Code != 201 || rec.Header().Get("Location") != "" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var response struct {
		Version string                    `json:"schema_version"`
		Data    dashboard.EnrollmentIssue `json:"data"`
	}
	if err = json.Unmarshal(rec.Body.Bytes(), &response); err != nil || response.Version != "1" {
		t.Fatal(err)
	}
	list := call("GET", "/dashboard/agent-enrollment-tokens/", "", admin, nil)
	if list.Code != 200 || strings.Contains(list.Body.String(), response.Data.Token) || strings.Contains(list.Body.String(), "token_hash") {
		t.Fatal("HTTP list disclosed secret")
	}
	devices := call("GET", "/dashboard/devices/get_devices?group_id=8", "", f.sessions["viewer"], nil)
	if devices.Code != 200 || strings.Contains(devices.Body.String(), "eight") {
		t.Fatal("forged scope enlarged visibility")
	}
}
