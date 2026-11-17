package dashboard

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestScheduleCronAndFormValidation(t *testing.T) {
	for _, tc := range []struct {
		s        string
		min, max int
		aliases  string
		ok       bool
	}{
		{"*", 0, 59, "", true}, {"*/5", 0, 59, "", true}, {"5/10", 0, 59, "", true}, {"59/1", 0, 59, "", false}, {"60", 0, 59, "", false}, {"1-0", 0, 59, "", false}, {"0,5", 0, 59, "", true}, {"*/0", 0, 59, "", false}, {"?", 0, 59, "", false}, {" mon", 0, 6, "mon tue wed thu fri sat sun", false}, {"mon-fri", 0, 6, "mon tue wed thu fri sat sun", true}, {"sun", 0, 6, "mon tue wed thu fri sat sun", true},
	} {
		if got := validCron(tc.s, tc.min, tc.max, tc.aliases); got != tc.ok {
			t.Fatalf("%q: %v", tc.s, got)
		}
	}
	c := CronInput{}
	if err := c.normalize(); err != nil || c.Second != "*" {
		t.Fatal(err, c)
	}
	c.Minute = "0,15,30,45"
	if c.normalize() == nil {
		t.Fatal("would truncate legacy varchar(4)")
	}
	for _, raw := range []string{`"false"`, `"0"`, `0`, `false`, `null`} {
		var b FormBool
		if json.Unmarshal([]byte(raw), &b) != nil || b {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{`"yes"`, `2`, `{}`, `[]`} {
		var b FormBool
		if json.Unmarshal([]byte(raw), &b) == nil {
			t.Fatal(raw)
		}
	}
}
func TestTaskNormalizationAndBounds(t *testing.T) {
	command, unused := "echo ok", "old value"
	days := int64(4)
	in := TaskInput{ScheduleID: 1, Name: " one ", Status: "active", Type: "command", Command: &command, Archive: &unused, Source: &unused, Destination: &unused, KeepDays: &days, Recursive: true}
	if err := in.normalize(true); err != nil {
		t.Fatal(err)
	}
	if in.Name != "one" || in.Source != nil || in.Destination != nil || in.KeepDays != nil || in.Archive != nil || in.Recursive {
		t.Fatal("inapplicable fields survived", in)
	}
	in.Retry = 101
	if in.normalize(true) == nil {
		t.Fatal("unbounded retry")
	}
	in.Retry = 0
	in.Command = &unused
	unused = strings.Repeat("x", 16385)
	if in.normalize(true) == nil {
		t.Fatal("oversize command")
	}
	empty := "null"
	in.Command = &empty
	if in.normalize(true) == nil {
		t.Fatal("empty command")
	}
	in = TaskInput{ScheduleID: 1, Name: "hk", Status: "manual", Type: "housekeep", Destination: &command, KeepDays: &days, Recursive: true}
	if err := in.normalize(true); err != nil || !in.Recursive {
		t.Fatal(err)
	}
	if !recursiveCap("foo, housekeep_recursive_v1,bar") || recursiveCap("housekeep_recursive_v10") {
		t.Fatal("capability mismatch")
	}
}
