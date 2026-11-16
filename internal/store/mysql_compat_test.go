package store

import (
	"strings"
	"testing"
)

func TestLegacyGroupsIdentifierOnly(t *testing.T) {
	for _, prefix := range []string{"CREATE TABLE ", "CREATE TABLE IF NOT EXISTS ", "INSERT IGNORE INTO ", "SELECT * FROM ", "UPDATE ", "LEFT JOIN ", "REFERENCES ", "ALTER TABLE ", "DROP TABLE "} {
		got, err := mysqlLegacySQL(prefix + "groups (group_id)")
		if err != nil || got != prefix+"`groups` (group_id)" {
			t.Fatal(got, err)
		}
	}
	text := "SELECT 'groups', 'FROM groups', group_id FROM groups /* FROM groups */ WHERE name='groups' -- groups\n"
	want := "SELECT 'groups', 'FROM groups', group_id FROM `groups` /* FROM groups */ WHERE name='groups' -- groups\n"
	if got, err := mysqlLegacySQL(text); err != nil || got != want {
		t.Fatal(got, err)
	}
	for _, unchanged := range []string{"SELECT * FROM `groups`", "SELECT * FROM user_groups", "SET @groups=1", "SELECT COUNT(*) OVER (ORDER BY id GROUPS BETWEEN 1 PRECEDING AND CURRENT ROW) FROM events"} {
		if got, err := mysqlLegacySQL(unchanged); err != nil || got != unchanged {
			t.Fatal(got, err)
		}
	}
	files, err := Migrations("mysql")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range files {
		if m.Name == "groups_001.sql" && !strings.Contains(m.Statements[0], "CREATE TABLE `groups`") {
			t.Fatal("migration execution was not adapted")
		}
	}
	q, err := Queries("mysql")
	if err != nil || !strings.Contains(q["groups.get_groups"], "FROM `groups`") {
		t.Fatal(q["groups.get_groups"], err)
	}
}
