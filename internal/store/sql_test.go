package store

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

func TestFrozenSQLAssets(t *testing.T) {
	count := 0
	err := fs.WalkDir(assets, "sql", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		bundled, err := assets.ReadFile(name)
		if err != nil {
			return err
		}
		original, err := os.ReadFile(filepath.Join("..", "..", "server", "res", filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		if !bytes.Equal(bundled, original) {
			t.Errorf("legacy SQL changed: %s", name)
		}
		count++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 54 {
		t.Fatalf("SQL asset inventory changed: %d", count)
	}
}
func TestCatalogAndQueries(t *testing.T) {
	for dialect, count := range map[string]int{"mysql": 31, "sqlite": 1} {
		files, err := Migrations(dialect)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != count {
			t.Fatalf("%s: %d files", dialect, len(files))
		}
		for i, m := range files {
			if len(m.SHA256) != 64 || len(m.Statements) == 0 {
				t.Fatal(m)
			}
			if i > 0 && files[i-1].Name >= m.Name {
				t.Fatal("not sorted")
			}
		}
	}
	q, err := Queries("mysql")
	if err != nil {
		t.Fatal(err)
	}
	if len(q) != 39 {
		t.Fatalf("query count %d", len(q))
	}
	if text, _ := q.Get("get_password"); !strings.Contains(text, "user_id = ?") {
		t.Fatal(text)
	}
	if text, _ := q.Get("tasks.insert_task"); !strings.Contains(text, "UNHEX(REPLACE(?, '-', ''))") {
		t.Fatal(text)
	}
	if _, err := q.Get("not-existing"); err == nil {
		t.Fatal("missing query accepted")
	}
	legacy, err := legacySQLiteMigrations()
	if err != nil || len(legacy) != 7 {
		t.Fatal("frozen SQLite predecessor inventory", len(legacy), err)
	}
	got := []string{}
	for _, migration := range legacy {
		got = append(got, migration.Name)
	}
	want := []string{"groups_001.sql", "groups_002.sql", "timeweaver_server_014.sql", "timeweaver_server_015.sql", "users_001.sql", "users_002.sql", "users_003_group_integrity.sql"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	sqliteQueries, err := Queries("sqlite3")
	if err != nil || len(sqliteQueries) != 39 {
		t.Fatal(len(sqliteQueries), err)
	}
	db := sqliteTestStore(t)
	if _, err = db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	for key := range q {
		statement, err := sqliteQueries.Get(key)
		if err != nil {
			t.Fatal(key, err)
		}
		prepared, err := db.DB.PrepareContext(context.Background(), statement)
		if err != nil {
			t.Fatalf("SQLite query %s: %v", key, err)
		}
		prepared.Close()
	}
}
func TestSQLSyntaxBoundaries(t *testing.T) {
	source := "-- header;\nSET @q='it''s; -- text %s'; /* ; */ PREPARE p FROM @q; SELECT `semi;column`, \"quoted;\", %s; # tail;"
	got, err := SplitSQL(source, "mysql")
	if err != nil || len(got) != 3 {
		t.Fatalf("%#v %v", got, err)
	}
	bound, err := BindSQL("SELECT %s, '%s', '%Y%m%d', `x%s`, \"%s\" -- %s\n, %s /* %s */", "mysql")
	if err != nil || bound != "SELECT ?, '%s', '%Y%m%d', `x%s`, \"%s\" -- %s\n, ? /* %s */" {
		t.Fatal(bound, err)
	}
	statements, err := SplitSQL("PRAGMA foreign_keys=OFF; BEGIN TRANSACTION; SELECT [x;y], 'a'';b'; COMMIT;", "sqlite")
	if err != nil || len(statements) != 4 {
		t.Fatal(statements, err)
	}
	if got, err := SplitSQL("-- comment only\n/* still empty */;", "mysql"); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	for _, bad := range []string{"SELECT 'unclosed", "SELECT /* unclosed", "DELIMITER $$", "CREATE TRIGGER x AFTER INSERT ON t BEGIN SELECT 1; END;", "/*!40101 SET @x=1 */;"} {
		if _, err := SplitSQL(bad, "mysql"); err == nil {
			t.Fatalf("unsafe SQL accepted: %s", bad)
		}
	}
	escaped, err := SplitSQL("SELECT 'a\\';b'; SELECT 2;", "mysql")
	if err != nil || len(escaped) != 2 {
		t.Fatal(escaped, err)
	}
}
func TestQueryFileReferenceValidation(t *testing.T) {
	for _, text := range []string{`{"q":"../outside.sql"}`, `{"q":"missing.sql"}`, `{"q":"SELECT 1; DROP TABLE x"}`, `{"q":42}`, `{"q.x":"SELECT 1"}`} {
		source := fstest.MapFS{"queries/index.json": {Data: []byte(text)}, "outside.sql": {Data: []byte("SELECT 1")}}
		if _, err := loadQueries(source, "queries", "index.json", "mysql"); err == nil {
			t.Fatal("invalid query accepted", text)
		}
	}
	source := fstest.MapFS{"q/index.json": {Data: []byte(`{"nested":{"q":"child.sql"}}`)}, "q/child.sql": {Data: []byte("SELECT %s")}}
	got, err := loadQueries(source, "q", "index.json", "mysql")
	if err != nil || !reflect.DeepEqual(got, QueryCatalog{"nested.q": "SELECT ?"}) {
		t.Fatal(got, err)
	}
}
