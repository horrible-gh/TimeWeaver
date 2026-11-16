package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/horrible-gh/TimeWeaver/internal/config"
	_ "modernc.org/sqlite"
)

type Store struct {
	DB       *sql.DB
	Dialect  string
	readOnly bool
}

// Open never runs migrations. Read-only mode cannot create a missing SQLite file
// and sets the database's own write protection, not just a Go boolean.
func Open(ctx context.Context, c config.Database, readOnly bool) (*Store, error) {
	dialect, err := dialectName(c.Type)
	if err != nil {
		return nil, err
	}
	var db *sql.DB
	if dialect == "mysql" {
		cfg := mysql.NewConfig()
		cfg.User = c.User
		cfg.Passwd = c.Password
		cfg.Net = "tcp"
		cfg.Addr = net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
		cfg.DBName = c.Name
		cfg.ParseTime = true
		cfg.Loc = time.UTC
		cfg.Timeout = 5 * time.Second
		cfg.ReadTimeout = 30 * time.Second
		cfg.WriteTimeout = 30 * time.Second
		cfg.MultiStatements = false
		cfg.Collation = "utf8mb4_unicode_ci"
		cfg.Params = map[string]string{}
		connector, e := mysql.NewConnector(cfg)
		if e != nil {
			return nil, dbError("configure database", e)
		}
		if readOnly {
			db = sql.OpenDB(readOnlyConnector{connector})
		} else {
			db = sql.OpenDB(connector)
		}
	} else {
		filename, e := filepath.Abs(c.Path)
		if e != nil || c.Path == "" {
			return nil, errors.New("invalid SQLite path")
		}
		if readOnly {
			info, e := os.Stat(filename)
			if e != nil || !info.Mode().IsRegular() {
				return nil, errors.New("SQLite database does not exist or is not a regular file; read-only check created nothing")
			}
		}
		uriPath := filepath.ToSlash(filename)
		if !strings.HasPrefix(uriPath, "/") {
			uriPath = "/" + uriPath
		}
		uri := url.URL{Scheme: "file", Path: uriPath}
		query := url.Values{"_pragma": {"busy_timeout(5000)", "foreign_keys(1)"}}
		if readOnly {
			query.Set("mode", "ro")
			query.Add("_pragma", "query_only(1)")
		} else {
			query.Set("mode", "rwc")
			query.Add("_pragma", "journal_mode(WAL)")
			query.Set("_txlock", "immediate")
		}
		uri.RawQuery = query.Encode()
		db, err = sql.Open("sqlite", uri.String())
		if err != nil {
			return nil, dbError("open database", err)
		}
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(5 * time.Minute)
	if dialect == "sqlite" {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	}
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, dbError("connect database", err)
	}
	return &Store{db, dialect, readOnly}, nil
}
func (s *Store) Close() error { return s.DB.Close() }

// Driver diagnostics retain machine codes but not query text, bound values,
// credentials, connection strings or server-supplied error messages.
func dbError(operation string, err error) error {
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%s: %w", operation, context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w", operation, context.DeadlineExceeded)
	}
	var my *mysql.MySQLError
	if errors.As(err, &my) {
		return fmt.Errorf("%s: MySQL error %d", operation, my.Number)
	}
	var sq interface{ Code() int }
	if errors.As(err, &sq) {
		return fmt.Errorf("%s: SQLite error %d", operation, sq.Code())
	}
	return fmt.Errorf("%s failed; check database settings and availability", operation)
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func tableExists(ctx context.Context, q queryer, dialect, name string) (bool, error) {
	query := "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?"
	if dialect == "sqlite" {
		query = "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?"
	}
	var count int
	if err := q.QueryRowContext(ctx, query, name).Scan(&count); err != nil {
		return false, dbError("inspect schema", err)
	}
	return count > 0, nil
}

// This SQL syntax is shared by MySQL and MariaDB. Run it for every new pooled
// connection so a reconnect cannot silently remove read-only protection.
type readOnlyConnector struct{ driver.Connector }

func (c readOnlyConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	executor, ok := conn.(driver.ExecerContext)
	if !ok {
		conn.Close()
		return nil, errors.New("driver cannot initialize read-only connection")
	}
	if _, err = executor.ExecContext(ctx, "SET SESSION TRANSACTION READ ONLY", nil); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}
