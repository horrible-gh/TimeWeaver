package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/horrible-gh/TimeWeaver/internal/config"
)

func TestSQLiteOpenExistingNeverCreatesButAllowsRuntimeWrites(t *testing.T) {
	ctx := context.Background()
	cfg := config.Database{Type: "sqlite3", Path: filepath.Join(t.TempDir(), "runtime #db.sqlite3")}
	if db, err := OpenExisting(ctx, cfg); err == nil {
		db.Close()
		t.Fatal("runtime pool opened a missing database")
	}
	if _, err := os.Stat(cfg.Path); !os.IsNotExist(err) {
		t.Fatal("runtime pool created a missing database")
	}
	db, err := Open(ctx, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("CREATE TABLE owned(value TEXT)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = OpenExisting(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.DB.Exec("INSERT INTO owned VALUES('runtime data')"); err != nil {
		t.Fatal("runtime pool cannot perform authorized writes", err)
	}
}
