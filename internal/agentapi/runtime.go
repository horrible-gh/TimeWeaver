package agentapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/horrible-gh/TimeWeaver/internal/store"
)

const recursiveHousekeep = "housekeep_recursive_v1"

type CronData struct {
	Year      string `json:"year"`
	Month     string `json:"month"`
	DayOfWeek string `json:"day_of_week"`
	Day       string `json:"day"`
	Hour      string `json:"hour"`
	Minute    string `json:"minute"`
	Second    string `json:"second"`
}
type SnapshotTask struct {
	Type                  string  `json:"task_type"`
	Command               *string `json:"command"`
	ArchiveType           *string `json:"archive_type"`
	SourcePath            *string `json:"source_path"`
	ErrorOnMissingSource  bool    `json:"error_on_missing_source"`
	DestinationPath       *string `json:"destination_path"`
	DateFormat            *string `json:"date_format"`
	TargetDateFormat      *string `json:"target_date_format"`
	DestinationDateFormat *string `json:"destination_date_format"`
	HouseKeepDays         *int64  `json:"house_keep_days"`
	HouseKeepRecursive    bool    `json:"house_keep_recursive"`
}
type SnapshotDetail struct {
	ID           string       `json:"detail_id"`
	Name         *string      `json:"schedule_name"`
	Cron         CronData     `json:"cron"`
	ErrorStop    bool         `json:"is_error_stop"`
	Sequence     int64        `json:"sequence"`
	ExecSequence int64        `json:"exec_sequence"`
	RetryCount   int64        `json:"retry_count"`
	Task         SnapshotTask `json:"task"`
}
type SnapshotSchedule struct {
	ID        int64            `json:"schedule_id"`
	Name      string           `json:"name"`
	Cron      CronData         `json:"cron"`
	ErrorStop bool             `json:"is_error_stop"`
	Details   []SnapshotDetail `json:"details"`
}
type SnapshotDevice struct {
	ID      int64   `json:"device_id"`
	Name    string  `json:"device_name"`
	Status  string  `json:"status"`
	Version *string `json:"known_agent_version"`
}
type SnapshotManual struct {
	ID          int64     `json:"manual_id"`
	ScheduleID  int64     `json:"schedule_id"`
	DetailID    string    `json:"detail_id"`
	Status      string    `json:"status"`
	Immediate   bool      `json:"is_immediate"`
	ScheduledAt time.Time `json:"schedule_datetime"`
	Claimable   bool      `json:"claimable"`
}
type SnapshotData struct {
	Revision    string             `json:"revision"`
	GeneratedAt time.Time          `json:"generated_at"`
	Device      SnapshotDevice     `json:"device"`
	Schedules   []SnapshotSchedule `json:"schedules"`
	ManualRuns  []SnapshotManual   `json:"manual_runs"`
}
type SnapshotResult struct {
	Data SnapshotData
	ETag string
}
type HeartbeatRequest struct {
	AgentVersion    string  `json:"agent_version"`
	AppliedRevision *string `json:"applied_revision"`
	State           string  `json:"state"`
}
type HeartbeatData struct {
	DeviceID     int64     `json:"device_id"`
	DeviceStatus string    `json:"device_status"`
	ServerTime   time.Time `json:"server_time"`
}

// Runtime uses only the existing device-scoped schema; it never schedules tasks
// itself, creates a database, or changes a migration/credential record.
type Runtime struct{ db *store.Store }

func NewRuntime(db *store.Store) (*Runtime, error) {
	if db == nil || db.DB == nil || db.Dialect != "mysql" || db.ReadOnly() {
		return nil, errors.New("runtime requires a writable MySQL/MariaDB store")
	}
	return &Runtime{db: db}, nil
}
func runtimeFailure() *APIError {
	return &APIError{Status: 503, Code: "unavailable", Message: "Agent runtime storage unavailable"}
}
func inconsistentSnapshot() *APIError {
	return &APIError{Status: 500, Code: "server_error", Message: "Snapshot source data is inconsistent"}
}
func runtimeRevoked() *APIError {
	return &APIError{Status: 403, Code: "device_revoked", Message: "Device credential has been revoked"}
}
func utcSecond(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }
func supportsRecursive(value string) bool {
	for _, v := range strings.Split(value, ",") {
		if strings.TrimSpace(v) == recursiveHousekeep {
			return true
		}
	}
	return false
}
func normalizeTask(t SnapshotTask) (SnapshotTask, error) {
	for _, p := range []**string{&t.Command, &t.ArchiveType, &t.SourcePath, &t.DestinationPath, &t.DateFormat, &t.TargetDateFormat, &t.DestinationDateFormat} {
		if *p != nil {
			v := strings.ToLower(strings.TrimSpace(**p))
			if v == "" || v == "null" {
				*p = nil
			}
		}
	}
	switch t.Type {
	case "command":
		t.ArchiveType = nil
		t.SourcePath = nil
		t.DestinationPath = nil
		t.HouseKeepDays = nil
	case "copy":
		t.Command = nil
		t.ArchiveType = nil
		t.HouseKeepDays = nil
	case "archive":
		t.Command = nil
		t.HouseKeepDays = nil
	case "housekeep":
		t.Command = nil
		t.ArchiveType = nil
		t.SourcePath = nil
	default:
		return SnapshotTask{}, inconsistentSnapshot()
	}
	if t.Type != "housekeep" {
		t.HouseKeepRecursive = false
	}
	return t, nil
}
func needsRecursive(data SnapshotData) bool {
	for _, group := range data.Schedules {
		for _, detail := range group.Details {
			if detail.Task.Type == "housekeep" && detail.Task.HouseKeepRecursive {
				return true
			}
		}
	}
	return false
}
func (s *Runtime) Snapshot(ctx context.Context, principal DeviceIdentity) (SnapshotResult, error) {
	read, err := s.beginSnapshot(ctx)
	if err != nil {
		return SnapshotResult{}, err
	}
	defer read.close()
	data, err := s.loadSnapshot(ctx, read.conn, principal.DeviceID)
	if err != nil {
		return SnapshotResult{}, err
	}
	if err = read.commit(ctx); err != nil {
		return SnapshotResult{}, err
	}
	canonical, err := canonicalSnapshot(data)
	if err != nil {
		return SnapshotResult{}, inconsistentSnapshot()
	}
	sum := sha256.Sum256(canonical)
	digest := hex.EncodeToString(sum[:])
	data.Revision = "sha256:" + digest
	return SnapshotResult{Data: data, ETag: `W/"` + digest[:16] + `"`}, nil
}

// The frozen Python hashes only these three fields, with sorted object keys,
// compact separators, UTF-8 strings and second-resolution UTC timestamps.
// No timestamp/revision is included. A custom string writer is needed because
// encoding/json still escapes U+2028/U+2029 even with SetEscapeHTML(false).
// This is deliberately restricted to the snapshot's integer-only JSON domain.
func canonicalSnapshot(data SnapshotData) ([]byte, error) {
	raw, err := json.Marshal(map[string]any{"device": data.Device, "schedules": data.Schedules, "manual_runs": data.ManualRuns})
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err = decoder.Decode(&value); err != nil {
		return nil, err
	}
	return appendCanonical(nil, value)
}
func appendCanonical(dst []byte, v any) ([]byte, error) {
	switch x := v.(type) {
	case nil:
		return append(dst, "null"...), nil
	case bool:
		return strconv.AppendBool(dst, x), nil
	case json.Number:
		n, err := strconv.ParseInt(string(x), 10, 64)
		if err != nil {
			return nil, err
		}
		return strconv.AppendInt(dst, n, 10), nil
	case string:
		if !utf8.ValidString(x) {
			return nil, errors.New("invalid UTF-8")
		}
		dst = append(dst, '"')
		const hexDigits = "0123456789abcdef"
		for _, r := range x {
			switch r {
			case '"', '\\':
				dst = append(dst, '\\', byte(r))
			case '\b':
				dst = append(dst, `\b`...)
			case '\f':
				dst = append(dst, `\f`...)
			case '\n':
				dst = append(dst, `\n`...)
			case '\r':
				dst = append(dst, `\r`...)
			case '\t':
				dst = append(dst, `\t`...)
			default:
				if r < 0x20 {
					dst = append(dst, '\\', 'u', '0', '0', hexDigits[r>>4], hexDigits[r&15])
				} else {
					dst = utf8.AppendRune(dst, r)
				}
			}
		}
		return append(dst, '"'), nil
	case []any:
		dst = append(dst, '[')
		for i, item := range x {
			if i > 0 {
				dst = append(dst, ',')
			}
			var err error
			dst, err = appendCanonical(dst, item)
			if err != nil {
				return nil, err
			}
		}
		return append(dst, ']'), nil
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		dst = append(dst, '{')
		for i, k := range keys {
			if i > 0 {
				dst = append(dst, ',')
			}
			var err error
			dst, err = appendCanonical(dst, k)
			if err != nil {
				return nil, err
			}
			dst = append(dst, ':')
			dst, err = appendCanonical(dst, x[k])
			if err != nil {
				return nil, err
			}
		}
		return append(dst, '}'), nil
	default:
		return nil, errors.New("unsupported canonical value")
	}
}
