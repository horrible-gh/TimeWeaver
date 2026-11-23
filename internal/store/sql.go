// Package store owns SQL assets and database access during the Go migration.
package store

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// Legacy assets are byte-for-byte copies, checked against the frozen baseline.
//
//go:embed sql
var assets embed.FS

// Native SQLite resources are kept outside the byte-frozen legacy directory.
//
//go:embed sqlite
var sqliteAssets embed.FS

// Retired agent SQL is used only to identify existing MySQL history. These
// byte-for-byte snapshots come from 55b4c97a27aafa90a50845dd4728d7c59f33180a,
// immediately before 750d315 removed agent/res/time_weaver/sql/migration.
// They are deliberately outside the executable migration catalog above.
//
//go:embed mysql/legacy/*.sql
var mysqlLegacyAssets embed.FS

type Migration struct {
	Name       string   `json:"name"`
	SHA256     string   `json:"sha256"`
	Statements []string `json:"-"`
}

func dialectName(dialect string) (string, error) {
	switch dialect {
	case "mysql":
		return "mysql", nil
	case "sqlite", "sqlite3", "local":
		return "sqlite", nil
	default:
		return "", errors.New("unsupported database dialect")
	}
}

func Migrations(dialect string) ([]Migration, error) {
	dialect, err := dialectName(dialect)
	if err != nil {
		return nil, err
	}
	if dialect == "sqlite" {
		return readMigrationCatalog(sqliteAssets, "sqlite/migrations/*.sql", dialect)
	}
	return readMigrationCatalog(assets, "sql/migration/"+dialect+"/*.sql", dialect)
}

func legacySQLiteMigrations() ([]Migration, error) {
	return readMigrationCatalog(assets, "sql/migration/sqlite/*.sql", "sqlite")
}

func legacyMySQLMigrations() ([]Migration, error) {
	// task_detail_001.sql is intentionally absent: that basename still belongs
	// to a different, active server migration and must retain its current hash.
	return readMigrationCatalog(mysqlLegacyAssets, "mysql/legacy/*.sql", "mysql")
}

func readMigrationCatalog(source fs.FS, pattern, dialect string) ([]Migration, error) {
	names, err := fs.Glob(source, pattern)
	if err != nil || len(names) == 0 {
		return nil, errors.New("embedded migration catalog is missing")
	}
	sort.Strings(names)
	out := make([]Migration, 0, len(names))
	for _, name := range names {
		data, err := fs.ReadFile(source, name)
		if err != nil {
			return nil, err
		}
		statements, err := SplitSQL(string(data), dialect)
		if err != nil {
			return nil, fmt.Errorf("invalid migration %s: %w", path.Base(name), err)
		}
		if len(statements) == 0 {
			return nil, fmt.Errorf("empty migration %s", path.Base(name))
		}
		if dialect == "mysql" {
			for i, statement := range statements {
				statements[i], err = mysqlLegacySQL(statement)
				if err != nil {
					return nil, err
				}
			}
		}
		digest := sha256.Sum256(data)
		out = append(out, Migration{path.Base(name), hex.EncodeToString(digest[:]), statements})
	}
	return out, nil
}

// QueryCatalog keeps the legacy dotted JSON keys. It does not translate dialects.
type QueryCatalog map[string]string

func Queries(dialect string) (QueryCatalog, error) {
	dialect, err := dialectName(dialect)
	if err != nil {
		return nil, err
	}
	if dialect == "sqlite" {
		return loadQueries(sqliteAssets, "sqlite/queries", "time_weaver.json", dialect)
	}
	return loadQueries(assets, "sql/sqloader/"+dialect, "time_weaver.json", dialect)
}

func loadQueries(source fs.FS, root, filename, dialect string) (QueryCatalog, error) {
	data, err := fs.ReadFile(source, path.Join(root, filename))
	if err != nil {
		return nil, err
	}
	var object map[string]any
	if err = json.Unmarshal(data, &object); err != nil {
		return nil, errors.New("invalid query catalog JSON")
	}
	out := QueryCatalog{}
	var visit func(map[string]any, string) error
	visit = func(node map[string]any, prefix string) error {
		for key, value := range node {
			if key == "" || strings.Contains(key, ".") {
				return errors.New("invalid query key")
			}
			name := prefix + key
			if nested, ok := value.(map[string]any); ok {
				if err := visit(nested, name+"."); err != nil {
					return err
				}
				continue
			}
			text, ok := value.(string)
			if !ok {
				return fmt.Errorf("query %s is not a string", name)
			}
			if strings.HasSuffix(text, ".sql") {
				if !fs.ValidPath(text) || strings.Contains(text, "\\") {
					return errors.New("unsafe query file reference")
				}
				raw, err := fs.ReadFile(source, path.Join(root, text))
				if err != nil {
					return fmt.Errorf("missing query file for %s", name)
				}
				text = string(raw)
			}
			statements, err := SplitSQL(text, dialect)
			if err != nil || len(statements) != 1 {
				return fmt.Errorf("query %s must contain exactly one statement", name)
			}
			bound, err := BindSQL(statements[0], dialect)
			if err != nil {
				return err
			}
			out[name] = bound
		}
		return nil
	}
	if err = visit(object, ""); err != nil {
		return nil, err
	}
	return out, nil
}

func (q QueryCatalog) Get(name string) (string, error) {
	value, ok := q[name]
	if !ok {
		return "", fmt.Errorf("unknown SQL query %s", name)
	}
	return value, nil
}

// syntaxMask keeps offsets while hiding strings, quoted identifiers and comments.
// Unterminated constructs and executable MySQL comments fail closed.
func syntaxMask(text, dialect string) ([]byte, error) {
	mask := []byte(text)
	for i := 0; i < len(text); {
		start := i
		if text[i] == '\'' || text[i] == '"' || text[i] == '`' || (dialect == "sqlite" && text[i] == '[') {
			quote := text[i]
			end := quote
			if quote == '[' {
				end = ']'
			}
			i++
			closed := false
			for i < len(text) {
				if dialect == "mysql" && quote != '`' && text[i] == '\\' {
					i += 2
					continue
				}
				if text[i] == end {
					if i+1 < len(text) && text[i+1] == end {
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return nil, errors.New("unterminated SQL quote")
			}
		} else if (text[i] == '-' && i+1 < len(text) && text[i+1] == '-' && (dialect != "mysql" || i+2 == len(text) || text[i+2] <= ' ')) || (dialect == "mysql" && text[i] == '#') {
			for i < len(text) && text[i] != '\n' {
				i++
			}
		} else if text[i] == '/' && i+1 < len(text) && text[i+1] == '*' {
			if i+2 < len(text) && text[i+2] == '!' {
				return nil, errors.New("executable SQL comments are unsupported")
			}
			close := strings.Index(text[i+2:], "*/")
			if close < 0 {
				return nil, errors.New("unterminated SQL comment")
			}
			i += close + 4
		} else {
			i++
			continue
		}
		for j := start; j < i; j++ {
			if mask[j] != '\n' && mask[j] != '\r' {
				mask[j] = ' '
			}
		}
	}
	return mask, nil
}

func SplitSQL(text, dialect string) ([]string, error) {
	d, err := dialectName(dialect)
	if err != nil {
		return nil, err
	}
	mask, err := syntaxMask(text, d)
	if err != nil {
		return nil, err
	}
	out := []string{}
	start := 0
	add := func(end int) error {
		bare := strings.TrimSpace(string(mask[start:end]))
		if bare == "" {
			return nil
		}
		words := strings.FieldsFunc(strings.ToUpper(bare), func(r rune) bool { return !((r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') })
		for _, w := range words {
			if w == "DELIMITER" || w == "TRIGGER" || w == "PROCEDURE" || w == "FUNCTION" {
				return errors.New("compound SQL is unsupported")
			}
			if w == "BEGIN" && !strings.EqualFold(bare, "BEGIN") && !strings.EqualFold(bare, "BEGIN TRANSACTION") && !strings.EqualFold(bare, "BEGIN IMMEDIATE") {
				return errors.New("compound BEGIN is unsupported")
			}
		}
		out = append(out, strings.TrimSpace(text[start:end]))
		return nil
	}
	for i, ch := range mask {
		if ch == ';' {
			if err = add(i); err != nil {
				return nil, err
			}
			start = i + 1
		}
	}
	if err = add(len(text)); err != nil {
		return nil, err
	}
	return out, nil
}

// BindSQL converts only actual placeholders, never quoted %s/date formats/comments.
func BindSQL(text, dialect string) (string, error) {
	d, err := dialectName(dialect)
	if err != nil {
		return "", err
	}
	mask, err := syntaxMask(text, d)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for i := 0; i < len(text); i++ {
		if mask[i] == '%' && i+1 < len(text) && mask[i+1] == 's' {
			out.WriteByte('?')
			i++
			continue
		}
		out.WriteByte(text[i])
	}
	if d == "mysql" {
		return mysqlLegacySQL(out.String())
	}
	return out.String(), nil
}
