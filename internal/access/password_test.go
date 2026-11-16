package access

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

const testPassword = "migration-test-password"
const testHash = "$pbkdf2-sha256$29000$Y29tcGF0aWJpbGl0eS1zYWx0$8W/0pwrIBoQnZcgvWBYWEQ0taGg5MkmakNBL8BT02/A"

func TestPasslibPasswordCompatibility(t *testing.T) {
	if !checkPassword(testHash, testPassword) || checkPassword(testHash, "wrong") {
		t.Fatal("legacy PBKDF2 vector")
	}
	for _, bad := range []string{"!os", "", strings.Replace(testHash, "29000", "999999999", 1), strings.Replace(testHash, "29000", "029000", 1), strings.Replace(testHash, "pbkdf2-sha256", "sha256", 1), testHash + "="} {
		if checkPassword(bad, testPassword) {
			t.Fatal("malformed hash accepted")
		}
	}
	for _, bad := range []string{"", "short", strings.Repeat("x", 1025)} {
		if _, err := hashPassword(bad); err == nil {
			t.Fatal("password policy")
		}
	}
	plain := "새로운-비밀번호-일본語-123"
	encoded, err := hashPassword(plain)
	if err != nil || !checkPassword(encoded, plain) {
		t.Fatal("new password did not roundtrip", err)
	}
	if !strings.HasPrefix(encoded, "$pbkdf2-sha256$600000$") {
		t.Fatal("work factor")
	}
	if os.Getenv("TIMEWEAVER_ACCESS_INTEROP") == "1" {
		// Test-only stdin carries the password/hash; no values are put in logs.
		cmd := exec.Command("python", "-c", `import sys; from passlib.hash import pbkdf2_sha256; h,p=sys.stdin.read().splitlines(); assert pbkdf2_sha256.verify(p,h); print("PASS: passlib verifies Go password hash")`)
		cmd.Stdin = strings.NewReader(encoded + "\n" + plain + "\n")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal("passlib interop failed", err)
		}
		t.Log(string(out))
	}
}
func TestGroupPolicyScopes(t *testing.T) {
	u := User{Role: "user", status: "active", Groups: []Membership{{7, "viewer"}, {8, "operator"}}}
	if !u.Can(7, "viewer") || u.Can(7, "operator") || !u.Can(8, "operator") || u.Can(8, "editor") || u.Can(9, "viewer") || u.Can(0, "viewer") {
		t.Fatal("scope escalation")
	}
	filter, args, err := u.GroupFilter("d.group_id", "operator")
	if err != nil || filter != "d.group_id IN (?)" || len(args) != 1 || args[0] != int64(8) {
		t.Fatal(filter, args, err)
	}
	u.Groups = append(u.Groups, Membership{0, "editor"})
	if !u.Can(999, "editor") {
		t.Fatal("explicit global grant lost")
	}
	u.MustChangePassword = true
	u.Role = "admin"
	if u.Can(1, "viewer") {
		t.Fatal("password gate bypass")
	}
	filter, args, err = u.GroupFilter("d.group_id", "viewer")
	if err != nil || filter != "1=0" || len(args) != 0 {
		t.Fatal("must-change scope leaked")
	}
	if _, _, err = u.GroupFilter("d.group_id;DROP TABLE users", "viewer"); err == nil {
		t.Fatal("SQL expression accepted")
	}
}
