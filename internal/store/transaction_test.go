package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/horrible-gh/TimeWeaver/internal/config"
)

func sqliteAdmissionStores(t *testing.T) (*Store, *Store) {
	t.Helper()
	cfg := config.Database{Type: "sqlite3", Path: filepath.Join(t.TempDir(), "admission.sqlite3")}
	first, err := Open(context.Background(), cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { first.Close() })
	if _, err = first.DB.Exec("CREATE TABLE admitted(value INTEGER NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	second, err := Open(context.Background(), cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { second.Close() })
	return first, second
}

type observedSQLiteBegin struct {
	db      *sql.DB
	wantCtx context.Context
	options *sql.TxOptions
	busy    chan struct{}
	once    sync.Once
	calls   atomic.Int64
}

func (b *observedSQLiteBegin) BeginTx(ctx context.Context, options *sql.TxOptions) (*sql.Tx, error) {
	b.calls.Add(1)
	if ctx != b.wantCtx || options != b.options {
		return nil, errors.New("writer admission replaced the caller context or options")
	}
	tx, err := b.db.BeginTx(ctx, options)
	if IsBusy(err) && b.busy != nil {
		b.once.Do(func() { close(b.busy) })
	}
	return tx, err
}

func TestSQLiteWriteAdmissionWaitsForWriterAndKeepsCallerContext(t *testing.T) {
	first, second := sqliteAdmissionStores(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	holder, err := first.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback()
	options := &sql.TxOptions{Isolation: sql.LevelReadCommitted}
	begin := &observedSQLiteBegin{db: second.DB, wantCtx: ctx, options: options, busy: make(chan struct{})}
	type result struct {
		tx  *sql.Tx
		err error
	}
	done := make(chan result, 1)
	go func() {
		tx, err := BeginWrite(ctx, begin, "sqlite", options)
		done <- result{tx, err}
	}()
	// Wait until SQLite has actually exhausted one native busy timeout. This
	// cannot pass because a slow test goroutine started after the writer left.
	select {
	case <-begin.busy:
	case <-ctx.Done():
		t.Fatal("writer admission never attempted the held database")
	}
	if err = holder.Rollback(); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil || got.tx == nil {
		t.Fatal("writer was not admitted after lock release", got.err)
	}
	defer got.tx.Rollback()
	if begin.calls.Load() < 2 {
		t.Fatal("the real busy BEGIN was not retried")
	}
	// An admission-only context canceled on return would roll this transaction
	// back here. The original caller context must remain attached to it.
	if _, err = got.tx.ExecContext(ctx, "INSERT INTO admitted(value) VALUES(1)"); err != nil {
		t.Fatal("admitted transaction lost its caller context", err)
	}
	if err = got.tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = first.DB.QueryRow("SELECT COUNT(*) FROM admitted").Scan(&count); err != nil || count != 1 {
		t.Fatal("admitted write was lost or replayed", count, err)
	}
}

func TestSQLiteWriteAdmissionCancellationDoesNotStartTransaction(t *testing.T) {
	first, second := sqliteAdmissionStores(t)
	holder, err := first.DB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	tx, err := BeginWrite(ctx, second.DB, "sqlite", nil)
	if tx != nil {
		tx.Rollback()
		t.Fatal("canceled writer obtained a transaction")
	}
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatal("writer admission did not honor cancellation", err, time.Since(start))
	}
	if err = holder.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = first.DB.QueryRow("SELECT COUNT(*) FROM admitted").Scan(&count); err != nil || count != 0 {
		t.Fatal("canceled writer changed data", count, err)
	}
}

type failedBegin struct {
	err   error
	calls int
}

func (b *failedBegin) BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error) {
	b.calls++
	return nil, b.err
}

func TestWriteAdmissionDoesNotRetryMySQLOrUnrelatedErrors(t *testing.T) {
	for _, tc := range []struct {
		name, dialect string
		err           error
	}{
		{"mysql-deadlock", "mysql", &mysql.MySQLError{Number: 1213}},
		{"sqlite-unrelated", "sqlite", errors.New("unrelated database failure")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			begin := &failedBegin{err: tc.err}
			tx, err := BeginWrite(context.Background(), begin, tc.dialect, nil)
			if tx != nil || err != tc.err || begin.calls != 1 {
				t.Fatal("writer admission retried an unrelated begin failure", begin.calls, err)
			}
		})
	}
}
