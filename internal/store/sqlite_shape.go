package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// The reference schema is compiled from the same embedded DDL that is executed
// on a fresh database. The private in-memory database never touches a user file.
type sqliteShape struct {
	ddl           string
	columns       []sqliteColumn
	pk            string
	checks        []string
	fks           []string
	indexes       []sqliteIndex
	autoIncrement bool
}
type sqliteIndex struct {
	name, columns, ddl, origin string
	unique, partial            bool
}

var sqliteReference = sync.OnceValues(func() (map[string]sqliteShape, error) {
	return loadSQLiteReference(false)
})
var sqliteCurrentReference = sync.OnceValues(func() (map[string]sqliteShape, error) {
	return loadSQLiteReference(true)
})

func loadSQLiteReference(current bool) (map[string]sqliteShape, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ddl, err := sqliteAssets.ReadFile("sqlite/migrations/" + sqliteFoundation)
	if err != nil {
		return nil, err
	}
	if _, err = db.Exec(string(ddl)); err != nil {
		return nil, err
	}
	if current {
		catalog, err := Migrations("sqlite")
		if err != nil {
			return nil, err
		}
		for _, migration := range catalog {
			if migration.Name == sqliteFoundation {
				continue
			}
			for _, statement := range migration.Statements {
				if _, err = db.Exec(statement); err != nil {
					return nil, err
				}
			}
		}
	}
	out := map[string]sqliteShape{}
	for name := range sqliteRequiredColumns {
		shape, err := sqliteTableShape(context.Background(), db, name)
		if err != nil {
			return nil, err
		}
		out[name] = shape
	}
	return out, nil
}

func sqliteTableShape(ctx context.Context, q queryer, table string) (sqliteShape, error) {
	var s sqliteShape
	if err := validIdentifier(table); err != nil {
		return s, err
	}
	err := q.QueryRowContext(ctx, "SELECT sql FROM sqlite_schema WHERE type='table' AND name=?", table).Scan(&s.ddl)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if s.columns, err = sqliteColumns(ctx, q, table); err != nil {
		return s, err
	}
	pk := map[int]string{}
	for _, c := range s.columns {
		if c.pk > 0 {
			pk[c.pk] = c.name
		}
	}
	for i := 1; i <= len(pk); i++ {
		if i > 1 {
			s.pk += ","
		}
		s.pk += pk[i]
	}
	if s.checks, err = sqliteChecks(s.ddl); err != nil {
		return s, err
	}
	mask, err := syntaxMask(s.ddl, "sqlite")
	if err != nil {
		return s, err
	}
	for _, word := range strings.FieldsFunc(strings.ToUpper(string(mask)), func(r rune) bool { return r > 127 || !isSQLWord(byte(r)) }) {
		if word == "AUTOINCREMENT" {
			s.autoIncrement = true
		}
	}
	rows, err := q.QueryContext(ctx, "PRAGMA foreign_key_list(`"+table+"`)")
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var id, seq int
		var parent, from, to, update, del, match string
		if err = rows.Scan(&id, &seq, &parent, &from, &to, &update, &del, &match); err != nil {
			rows.Close()
			return s, err
		}
		s.fks = append(s.fks, fmt.Sprintf("%d:%s:%s:%s:%s:%s:%s", seq, parent, from, to, update, del, match))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return s, err
	}
	sort.Strings(s.fks)
	rows, err = q.QueryContext(ctx, "PRAGMA index_list(`"+table+"`)")
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var index sqliteIndex
		var seq, unique, partial int
		if err = rows.Scan(&seq, &index.name, &unique, &index.origin, &partial); err != nil {
			rows.Close()
			return s, err
		}
		index.unique, index.partial = unique != 0, partial != 0
		s.indexes = append(s.indexes, index)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return s, err
	}
	for i := range s.indexes {
		index := &s.indexes[i]
		// Metadata is not trusted as an SQL identifier; table-valued pragmas bind
		// index names, including legacy names containing punctuation, as values.
		rows, err = q.QueryContext(ctx, "SELECT name,coll,desc FROM pragma_index_xinfo(?) WHERE key=1 ORDER BY seqno", index.name)
		if err != nil {
			return s, err
		}
		parts := []string{}
		for rows.Next() {
			var name sql.NullString
			var collation string
			var desc int
			if err = rows.Scan(&name, &collation, &desc); err != nil {
				rows.Close()
				return s, err
			}
			if !name.Valid || collation != "BINARY" {
				parts = append(parts, "<custom-expression-or-collation>")
			} else {
				parts = append(parts, name.String)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return s, err
		}
		index.columns = strings.Join(parts, ",")
		var ddl sql.NullString
		if err = q.QueryRowContext(ctx, "SELECT sql FROM sqlite_schema WHERE type='index' AND name=?", index.name).Scan(&ddl); err != nil {
			return s, err
		}
		index.ddl = ddl.String
	}
	return s, nil
}

func sqliteUniqueKeys(s sqliteShape) []string {
	keys := []string{}
	for _, index := range s.indexes {
		if index.unique && index.origin != "pk" {
			key := index.columns
			if index.partial {
				key += "<partial>"
			}
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// CHECK clauses are deliberately recognized, not ignored. An unknown legacy
// constraint can encode a site's policy and must never disappear in a rebuild.
func sqliteChecks(ddl string) ([]string, error) {
	var err error
	ddl, err = sqliteWithoutComments(ddl)
	if err != nil {
		return nil, err
	}
	checks := []string{}
	for i := 0; i < len(ddl); {
		if ddl[i] == '\'' || ddl[i] == '"' || ddl[i] == '`' || ddl[i] == '[' {
			i = sqliteQuotedEnd(ddl, i)
			continue
		}
		if isSQLWord(ddl[i]) {
			start := i
			for i < len(ddl) && isSQLWord(ddl[i]) {
				i++
			}
			word := strings.ToLower(ddl[start:i])
			switch word {
			case "collate", "generated", "strict", "without", "deferrable", "conflict":
				return nil, errors.New("custom SQLite schema policy requires review")
			case "check":
				for i < len(ddl) && strings.ContainsRune(" \t\r\n", rune(ddl[i])) {
					i++
				}
				if i == len(ddl) || ddl[i] != '(' {
					return nil, errors.New("invalid SQLite CHECK metadata")
				}
				start, depth := i+1, 1
				i++
				for i < len(ddl) && depth > 0 {
					if ddl[i] == '\'' || ddl[i] == '"' || ddl[i] == '`' || ddl[i] == '[' {
						i = sqliteQuotedEnd(ddl, i)
						continue
					}
					if ddl[i] == '(' {
						depth++
					}
					if ddl[i] == ')' {
						depth--
					}
					i++
				}
				if depth != 0 {
					return nil, errors.New("invalid SQLite CHECK metadata")
				}
				checks = append(checks, compactSQLiteCheck(ddl[start:i-1]))
			}
			continue
		}
		i++
	}
	sort.Strings(checks)
	return checks, nil
}

func isSQLWord(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
}

func sqliteWithoutComments(ddl string) (string, error) {
	if _, err := syntaxMask(ddl, "sqlite"); err != nil {
		return "", err
	}
	var out strings.Builder
	for i := 0; i < len(ddl); {
		if ddl[i] == '\'' || ddl[i] == '"' || ddl[i] == '`' || ddl[i] == '[' {
			end := sqliteQuotedEnd(ddl, i)
			out.WriteString(ddl[i:end])
			i = end
			continue
		}
		if i+1 < len(ddl) && ddl[i:i+2] == "--" {
			for i < len(ddl) && ddl[i] != '\n' {
				i++
			}
			out.WriteByte(' ')
			continue
		}
		if i+1 < len(ddl) && ddl[i:i+2] == "/*" {
			i += strings.Index(ddl[i+2:], "*/") + 4
			out.WriteByte(' ')
			continue
		}
		out.WriteByte(ddl[i])
		i++
	}
	return out.String(), nil
}
func sqliteQuotedEnd(text string, start int) int {
	end := text[start]
	if end == '[' {
		end = ']'
	}
	for i := start + 1; i < len(text); i++ {
		if text[i] == end {
			if i+1 < len(text) && text[i+1] == end {
				i++
				continue
			}
			return i + 1
		}
	}
	return len(text)
}
func compactSQLiteCheck(text string) string {
	var out strings.Builder
	for i := 0; i < len(text); {
		if text[i] == '\'' {
			end := sqliteQuotedEnd(text, i)
			out.WriteString(text[i:end])
			i = end
			continue
		}
		if text[i] == '"' || text[i] == '`' || text[i] == '[' {
			end := sqliteQuotedEnd(text, i)
			out.WriteString(strings.ToLower(text[i+1 : end-1]))
			i = end
			continue
		}
		if !strings.ContainsRune(" \t\r\n", rune(text[i])) {
			out.WriteString(strings.ToLower(text[i : i+1]))
		}
		i++
	}
	return out.String()
}

func exactSQLiteShape(got, want sqliteShape) bool {
	if got.pk != want.pk || got.autoIncrement != want.autoIncrement || !reflect.DeepEqual(sqlitePKKeys(got), sqlitePKKeys(want)) || !reflect.DeepEqual(got.checks, want.checks) || !reflect.DeepEqual(got.fks, want.fks) || !reflect.DeepEqual(sqliteUniqueKeys(got), sqliteUniqueKeys(want)) {
		return false
	}
	if len(got.columns) != len(want.columns) {
		return false
	}
	byName := map[string]sqliteColumn{}
	for _, c := range got.columns {
		byName[c.name] = c
	}
	for _, c := range want.columns {
		other, ok := byName[c.name]
		if !ok || other.hidden != 0 || strings.ToUpper(other.kind) != strings.ToUpper(c.kind) || other.notNull != c.notNull || other.pk != c.pk || other.def != c.def {
			return false
		}
	}
	return true
}

func sqlitePKKeys(shape sqliteShape) []string {
	keys := []string{}
	for _, index := range shape.indexes {
		if index.origin == "pk" {
			keys = append(keys, index.columns)
		}
	}
	sort.Strings(keys)
	return keys
}
