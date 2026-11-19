package dashboard

import (
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/store"
)

var sqliteBusinessQueries, sqliteBusinessQueryError = store.Queries("sqlite")

func businessCatalog(dialect string) (store.QueryCatalog, error) {
	if dialect == "sqlite" {
		return sqliteBusinessQueries, sqliteBusinessQueryError
	}
	return businessQueries, businessQueryError
}

func (s *Service) query(key string) (string, error) {
	catalog, err := businessCatalog(s.access.Dialect())
	if err != nil {
		return "", fail(503, "unavailable")
	}
	q, err := catalog.Get(key)
	if err != nil {
		return "", fail(503, "unavailable")
	}
	return q, nil
}

func (s *Service) lock() string              { return store.LockClause(s.access.Dialect(), false) }
func (s *Service) nowSQL() string            { return store.NowExpr(s.access.Dialect()) }
func (s *Service) timeValue(t time.Time) any { return store.TimeValue(s.access.Dialect(), t) }

// Aggregate expressions in SQLite do not carry the source DATETIME declaration.
// Normalize only known date columns; user-supplied text is never guessed to be a date.
func jsonDateColumn(name string, value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	switch name {
	case "lastest_start_time", "created_at", "modified_at", "start_time", "end_time":
		var stamp time.Time
		if err := store.ScanTime(&stamp).Scan(value); err != nil {
			return nil, dbError(err)
		}
		return stamp.UTC().Format(time.RFC3339Nano), nil
	default:
		return value, nil
	}
}
