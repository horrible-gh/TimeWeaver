// Package config reads settings without opening a database or changing files.
package config

import (
	"bufio"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Database struct {
	Type     string `json:"type"`
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	User     string `json:"-"`
	Password string `json:"-"`
	Name     string `json:"name,omitempty"`
	Schema   string `json:"schema,omitempty"`
	Path     string `json:"path,omitempty"`
}

type Settings struct {
	HTTP            HTTPSettings  `json:"http"`
	Context         string        `json:"context"`
	ListenHost      string        `json:"listen_host"`
	ListenPort      int           `json:"listen_port"`
	SecretKey       string        `json:"-"`
	AgentSecretKey  string        `json:"-"`
	Database        Database      `json:"database"`
	IdleTimeout     time.Duration `json:"-"`
	AbsoluteTimeout time.Duration `json:"-"`
	OSLoginEnabled  bool          `json:"os_login_enabled"`
}

var namePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var contextPattern = regexp.MustCompile(`^/[A-Za-z0-9._~-]+(/[A-Za-z0-9._~-]+)*$`)

// ValidateContext rejects paths that would shadow UI routes or need redirects.
func ValidateContext(value string) error {
	if !contextPattern.MatchString(value) {
		return errors.New("CONTEXT must be a nonempty absolute path without a trailing slash")
	}
	for _, segment := range strings.Split(value, "/")[1:] {
		if segment == "." || segment == ".." {
			return errors.New("CONTEXT cannot contain dot segments")
		}
	}
	for _, reserved := range []string{"/dashboard", "/login", "/logout", "/index.html", "/js", "/css", "/img", "/fonts", "/favicon.ico"} {
		if value == reserved || strings.HasPrefix(value, reserved+"/") || strings.HasPrefix(reserved, value+"/") {
			return errors.New("CONTEXT overlaps a web UI path")
		}
	}
	return nil
}

// Load uses process environment values before file values. Relative DB_PATH is
// anchored to the explicitly selected env file, not an accidental launch folder.
// Supported dotenv syntax is deliberately explicit; interpolation fails closed.
func Load(filename string, lookup func(string) (string, bool)) (Settings, error) {
	var result Settings
	raw, err := os.ReadFile(filename)
	if err != nil {
		return result, errors.New("cannot read environment file")
	}
	values, err := parseEnv(string(raw))
	if err != nil {
		return result, err
	}
	if lookup == nil {
		lookup = os.LookupEnv
	}
	get := func(key, fallback string) string {
		if value, ok := lookup(key); ok {
			return value
		}
		if value, ok := values[key]; ok {
			return value
		}
		return fallback
	}
	integer := func(key, fallback string, min, max int) (int, error) {
		value, e := strconv.Atoi(get(key, fallback))
		if e != nil || value < min || value > max {
			return 0, fmt.Errorf("%s must be an integer between %d and %d", key, min, max)
		}
		return value, nil
	}
	result.Context = get("CONTEXT", "/time_weaver")
	if err = ValidateContext(result.Context); err != nil {
		return Settings{}, err
	}
	result.ListenHost = get("LISTEN_HOST", "0.0.0.0")
	if result.ListenHost == "" || strings.ContainsAny(result.ListenHost, "/\\ \t\r\n") || (strings.Contains(result.ListenHost, ":") && net.ParseIP(result.ListenHost) == nil) {
		return Settings{}, errors.New("LISTEN_HOST must be a host name or unbracketed IP address")
	}
	if result.ListenPort, err = integer("LISTEN_PORT", "8000", 1, 65535); err != nil {
		return Settings{}, err
	}
	result.SecretKey = get("SECRET_KEY", "")
	result.AgentSecretKey = get("AGENT_SECRET_KEY", "")
	if err = validateSecret("SECRET_KEY", result.SecretKey, false); err != nil {
		return Settings{}, err
	}
	if err = validateSecret("AGENT_SECRET_KEY", result.AgentSecretKey, true); err != nil {
		return Settings{}, err
	}
	idle, err := integer("ACCESS_TOKEN_EXPIRE_MINUTES", "30", 1, 43200)
	if err != nil {
		return Settings{}, err
	}
	absolute, err := integer("SESSION_ABSOLUTE_HOURS", "12", 1, 8760)
	if err != nil {
		return Settings{}, err
	}
	result.IdleTimeout = time.Duration(idle) * time.Minute
	result.AbsoluteTimeout = time.Duration(absolute) * time.Hour
	if result.OSLoginEnabled, err = strconv.ParseBool(get("OS_LOGIN_ENABLED", "true")); err != nil {
		return Settings{}, errors.New("OS_LOGIN_ENABLED must be a boolean")
	}
	db := Database{Type: get("DB_TYPE", "sqlite3")}
	switch db.Type {
	case "mysql":
		db.Host, db.User, db.Password = get("DB_HOST", ""), get("DB_USER", ""), get("DB_PASSWORD", "")
		db.Name, db.Schema = get("DB_DATABASE", ""), get("DB_SCHEMA", "")
		if db.Host == "" || db.User == "" || db.Name == "" {
			return Settings{}, errors.New("MySQL requires DB_HOST, DB_USER and DB_DATABASE")
		}
		if db.Port, err = integer("DB_PORT", "3306", 1, 65535); err != nil {
			return Settings{}, err
		}
	case "sqlite", "sqlite3", "local":
		db.Type = "sqlite3"
		db.Path = get("DB_PATH", "./timeweaver.sqlite3")
		if db.Path == "" || db.Path == ":memory:" || strings.HasPrefix(db.Path, "file:") {
			return Settings{}, errors.New("DB_PATH must identify a persistent filesystem database")
		}
		if !filepath.IsAbs(db.Path) {
			db.Path = filepath.Join(filepath.Dir(filename), db.Path)
		}
		if db.Path, err = filepath.Abs(db.Path); err != nil {
			return Settings{}, errors.New("cannot resolve DB_PATH")
		}
	default:
		return Settings{}, errors.New("DB_TYPE must be mysql, sqlite, sqlite3 or local")
	}
	result.Database = db
	if result.HTTP, err = loadHTTP(get); err != nil {
		return Settings{}, err
	}
	return result, nil
}

func validateSecret(name, value string, optional bool) error {
	if optional && value == "" {
		return nil
	}
	if len([]byte(value)) < 32 || value == "change-me-for-local-development" {
		return fmt.Errorf("%s must be a non-example key of at least 32 bytes", name)
	}
	return nil
}

// AgentSigningKey preserves Python _agent_secret(), including the case where
// AGENT_SECRET_KEY accidentally duplicates SECRET_KEY. No key is logged.
func (s Settings) AgentSigningKey() []byte {
	if s.AgentSecretKey != "" && s.AgentSecretKey != s.SecretKey {
		return []byte(s.AgentSecretKey)
	}
	digest := sha256.Sum256([]byte("timeweaver-agent:" + s.SecretKey))
	return append([]byte(nil), digest[:]...)
}

func parseEnv(raw string) (map[string]string, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(strings.TrimPrefix(raw, "\ufeff")))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		text = strings.TrimPrefix(text, "export ")
		key, value, ok := strings.Cut(text, "=")
		key = strings.TrimSpace(key)
		if !ok || !namePattern.MatchString(key) {
			return nil, fmt.Errorf("invalid environment assignment at line %d", line)
		}
		if _, exists := values[key]; exists {
			return nil, fmt.Errorf("duplicate environment key at line %d", line)
		}
		decoded, err := envValue(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("invalid environment value at line %d: %s", line, err)
		}
		values[key] = decoded
	}
	if scanner.Err() != nil {
		return nil, errors.New("cannot parse environment file")
	}
	return values, nil
}

func envValue(value string) (string, error) {
	if strings.Contains(value, "${") {
		return "", errors.New("variable interpolation is unsupported; supply an explicit value")
	}
	if value == "" {
		return "", nil
	}
	if value[0] != '\'' && value[0] != '"' {
		for i := 1; i < len(value); i++ {
			if value[i] == '#' && (value[i-1] == ' ' || value[i-1] == '\t') {
				value = value[:i]
				break
			}
		}
		return strings.TrimSpace(value), nil
	}
	quote := value[0]
	for i := 1; i < len(value); i++ {
		if value[i] == '\\' {
			i++
			continue
		}
		if value[i] != quote {
			continue
		}
		tail := strings.TrimSpace(value[i+1:])
		if tail != "" && !strings.HasPrefix(tail, "#") {
			break
		}
		return decodeQuoted(value[1:i], quote), nil
	}
	return "", errors.New("unterminated quoted value or unexpected trailing text")
}

// python-dotenv decodes only its documented quote escapes, not Go's \x/\u
// escapes. Unknown escapes must remain literal, especially in credentials.
func decodeQuoted(value string, quote byte) string {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != '\\' || i+1 == len(value) {
			out.WriteByte(value[i])
			continue
		}
		next := value[i+1]
		decoded, known := next, next == '\\' || next == '\''
		if quote == '"' {
			switch next {
			case '"':
				known = true
			case 'a':
				decoded = '\a'
				known = true
			case 'b':
				decoded = '\b'
				known = true
			case 'f':
				decoded = '\f'
				known = true
			case 'n':
				decoded = '\n'
				known = true
			case 'r':
				decoded = '\r'
				known = true
			case 't':
				decoded = '\t'
				known = true
			case 'v':
				decoded = '\v'
				known = true
			}
		}
		if known {
			out.WriteByte(decoded)
			i++
		} else {
			out.WriteByte(value[i])
		}
	}
	return out.String()
}
