package store

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/horrible-gh/TimeWeaver/internal/config"
)

func TestMySQLReadOnlyAcrossConnections(t *testing.T) {
	rw := mysqlTestStore(t)
	ctx := context.Background()
	if _, err := rw.DB.Exec("CREATE TABLE retained (n INT)"); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := rw.DB.QueryRow("SELECT DATABASE()").Scan(&name); err != nil {
		t.Fatal(err)
	}
	ro, err := Open(ctx, config.Database{Type: "mysql", Host: os.Getenv("TIMEWEAVER_TEST_MYSQL_HOST"), Port: 3306, User: "root", Password: os.Getenv("TIMEWEAVER_TEST_MYSQL_PASSWORD"), Name: name}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	ro.DB.SetMaxIdleConns(0) // Force a fresh protected connection per request.
	for i := 0; i < 2; i++ {
		if _, err := ro.Status(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := ro.DB.Exec("INSERT INTO retained VALUES (1)"); err == nil {
			t.Fatal("read-only connection permitted DML")
		}
		if _, err := ro.DB.Exec("CREATE TABLE forbidden (n INT)"); err == nil {
			t.Fatal("read-only connection permitted DDL")
		}
	}
}

func TestMySQLUntrackedDataIsNotBootstrapped(t *testing.T) {
	s := mysqlTestStore(t)
	ctx := context.Background()
	if _, err := s.DB.Exec("CREATE TABLE existing_data (n INT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Migrate(ctx); err == nil || !strings.Contains(err.Error(), "no migration history") {
		t.Fatal("untracked database was not refused", err)
	}
	if exists, err := tableExists(ctx, s.DB, "mysql", "migrations"); err != nil || exists {
		t.Fatal("refused bootstrap wrote history", exists, err)
	}
}

func TestMySQLQueryPreparation(t *testing.T) {
	s := mysqlTestStore(t)
	ctx := context.Background()
	if _, err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	queries, err := Queries("mysql")
	if err != nil {
		t.Fatal(err)
	}
	for key, query := range queries {
		stmt, err := s.DB.PrepareContext(ctx, query)
		if err != nil {
			t.Fatalf("query %s cannot prepare: %v", key, err)
		}
		stmt.Close()
	}
}
