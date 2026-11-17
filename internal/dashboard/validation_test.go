package dashboard

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeviceAndEnrollmentBoundaries(t *testing.T) {
	for _, name := range []string{"", strings.Repeat("한", 256), "name\x00", "x\n", "\xff"} {
		if validName(name) {
			t.Fatalf("accepted invalid name length %d", len(name))
		}
	}
	for _, name := range []string{"my-device", "한글-日本語", strings.Repeat("한", 255)} {
		if !validName(name) {
			t.Fatal("rejected unicode device")
		}
	}
	for _, id := range []int64{-1, 2147483648} {
		if validID(id, true) {
			t.Fatal("invalid id accepted")
		}
	}
	if validID(0, false) || !validID(0, true) || !validID(2147483647, false) {
		t.Fatal("identifier bounds")
	}
	const id = "12345678-1234-4123-8123-123456789abc"
	b, err := parseUUID(strings.ToUpper(id))
	if err != nil || uuidString(b) != id {
		t.Fatal("UUID roundtrip", err)
	}
	for _, bad := range []string{"", strings.ReplaceAll(id, "-", ""), "not-a-uuid", id + "/child", "12345678-1234-4123-8123-123456789abg"} {
		if _, err = parseUUID(bad); err == nil {
			t.Fatal("accepted malformed uuid")
		}
	}
}
func TestDeviceJSONIsBoundedAndTyped(t *testing.T) {
	for _, raw := range []string{`{"group_id":"7"}`, `{"group_id":7,"role":"admin"}`, `{} {}`, `{"group_id":7.5}`, strings.Repeat(" ", 65537) + `{}`} {
		req := httptest.NewRequest("POST", "/", strings.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		var in DeviceInput
		if decode(httptest.NewRecorder(), req, &in) == nil {
			t.Fatal("invalid JSON was accepted")
		}
	}
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"group_id":7,"device_name":"日本語","status":"active","creator":"ignored"}`))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	var in DeviceInput
	if err := decode(httptest.NewRecorder(), req, &in); err != nil || in.GroupID == nil || *in.GroupID != 7 {
		t.Fatal(err)
	}
}
