package store

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestEnumParser(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want []string
	}{
		{"enum('null','zip')", []string{"null", "zip"}},
		{"ENUM('comma,value','it''s','back\\\\slash','line\\nfeed')", []string{"comma,value", "it's", "back\\slash", "line\nfeed"}},
	} {
		got, err := parseEnum(tt.in)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Fatal(tt.in, got, err)
		}
	}
	for _, v := range []string{"varchar(10)", "enum()", "enum('a',)", "enum('a)", "enum('a';drop)", "enum(a)"} {
		if _, err := parseEnum(v); err == nil {
			t.Fatal("invalid definition accepted", v)
		}
	}
	for _, v := range []string{"", "a'b", "a\\b", "\x00\n\r", "한글,中文"} {
		got, err := parseEnum("enum(" + mysqlLiteral(v) + ")")
		if err != nil || len(got) != 1 || got[0] != v {
			t.Fatal("literal roundtrip", err)
		}
	}
}
func TestMySQLSchemaGuardPreservesDataAndExtraEnums(t *testing.T) {
	s := mysqlTestStore(t)
	ctx := context.Background()
	if _, err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := s.CriticalSchema(ctx)
	if err != nil || len(before.Issues) != 0 {
		t.Fatal(before, err)
	}
	for _, q := range []string{
		"ALTER TABLE task_detail MODIFY archive_type ENUM('zip','custom,archive') NULL DEFAULT 'custom,archive' COMMENT 'retained'",
		"ALTER TABLE schedule_detail MODIFY status ENUM('active','inactive','error','custom') NOT NULL DEFAULT 'custom'",
		"UPDATE `groups` SET group_name='wrong' WHERE group_id=0",
		"INSERT INTO schedule_group(schedule_id,name) VALUES (17,'retained')",
		"INSERT INTO schedule_detail(detail_id,schedule_id,schedule_name,status) VALUES (UNHEX(REPEAT('11',16)),17,'retained','custom')",
		"INSERT INTO task_detail(detail_id,archive_type) VALUES (UNHEX(REPEAT('11',16)),'custom,archive')",
	} {
		if _, err = s.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	status, err := s.CriticalSchema(ctx)
	if err != nil || len(status.Issues) != 3 {
		t.Fatal(status, err)
	}
	repaired, err := s.RepairCriticalSchema(ctx)
	if err != nil || len(repaired.Repaired) != 3 || len(repaired.Issues) != 0 {
		t.Fatal(repaired, err)
	}
	c, err := readEnumColumn(ctx, s.DB, criticalEnums[0])
	if err != nil || c.comment != "retained" || !strings.Contains(c.def.String, "custom,archive") {
		t.Fatal(c, err)
	}
	values, err := parseEnum(c.kind)
	if err != nil || !reflect.DeepEqual(values, []string{"zip", "custom,archive", "null"}) {
		t.Fatal(values, err)
	}
	var archive, statusValue string
	if err = s.DB.QueryRow("SELECT archive_type FROM task_detail WHERE detail_id=UNHEX(REPEAT('11',16))").Scan(&archive); err != nil || archive != "custom,archive" {
		t.Fatal(archive, err)
	}
	if err = s.DB.QueryRow("SELECT status FROM schedule_detail WHERE detail_id=UNHEX(REPEAT('11',16))").Scan(&statusValue); err != nil || statusValue != "custom" {
		t.Fatal(statusValue, err)
	}
	again, err := s.RepairCriticalSchema(ctx)
	if err != nil || len(again.Repaired) != 0 {
		t.Fatal(again, err)
	}
	// A separate connection cannot inherit the repair session's sql_mode.
	var mode string
	if err = s.DB.QueryRow("SELECT @@SESSION.sql_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(mode, "NO_AUTO_VALUE_ON_ZERO") {
		t.Fatal("repair leaked session mode")
	}
	// Unknown types cause refusal BEFORE another (otherwise repairable) change.
	if _, err = s.DB.Exec("ALTER TABLE schedule_detail MODIFY status VARCHAR(40)"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("UPDATE `groups` SET group_name='do-not-overwrite' WHERE group_id=0"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RepairCriticalSchema(ctx); err == nil {
		t.Fatal("unknown schema repaired")
	}
	var name string
	if err = s.DB.QueryRow("SELECT group_name FROM `groups` WHERE group_id=0").Scan(&name); err != nil || name != "do-not-overwrite" {
		t.Fatal(name, err)
	}
}
func TestMySQLSchemaGuardRestoresMissingZero(t *testing.T) {
	s := mysqlTestStore(t)
	ctx := context.Background()
	if _, err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	// Build an isolated fixture by moving all references before deleting group 0.
	for _, q := range []string{"INSERT INTO `groups`(group_id,group_name) VALUES(99,'fixture')", "UPDATE users SET group_id=99", "UPDATE devices SET group_id=99", "UPDATE schedule_group SET group_id=99", "DELETE FROM `groups` WHERE group_id=0"} {
		if _, err := s.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	r, err := s.RepairCriticalSchema(ctx)
	if err != nil || !reflect.DeepEqual(r.Repaired, []string{"groups.0"}) {
		t.Fatal(r, err)
	}
	var name string
	if err = s.DB.QueryRow("SELECT group_name FROM `groups` WHERE group_id=0").Scan(&name); err != nil || name != "Unknown" {
		t.Fatal(name, err)
	}
}
