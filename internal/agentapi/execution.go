package agentapi

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/horrible-gh/TimeWeaver/internal/store"
)

const executionBodyMax = 1 << 20
const environmentMax = 32768
const eventMessageMax = 16384
const claimTTL = 30 * time.Minute

type ExecutionStart struct {
	ExecutionID string    `json:"execution_grp_id"`
	ScheduleID  int64     `json:"schedule_id"`
	DetailID    string    `json:"detail_id"`
	Attempt     int64     `json:"attempt"`
	StartedAt   time.Time `json:"started_at"`
}
type ExecutionResult struct {
	ExecutionStart
	ManualID      *int64         `json:"manual_id"`
	ClaimToken    *string        `json:"claim_token"`
	FinishedAt    time.Time      `json:"finished_at"`
	ResultCode    int64          `json:"result_code"`
	ResultMessage *string        `json:"result_message"`
	Environment   map[string]any `json:"environment_info"`
}
type AgentEvent struct {
	Type        string         `json:"event_type"`
	Severity    string         `json:"severity"`
	OccurredAt  time.Time      `json:"occurred_at"`
	Message     *string        `json:"message"`
	Environment map[string]any `json:"environment_info"`
}
type AppliedTransition struct {
	Target string `json:"target"`
	ID     any    `json:"id"`
	Status string `json:"status"`
}
type ClaimData struct {
	ManualID    int64     `json:"manual_id"`
	Token       string    `json:"claim_token"`
	ExpiresAt   time.Time `json:"claim_expires_at"`
	ExecutionID *string   `json:"execution_grp_id"`
}
type ResultData struct {
	ExecutionID int64               `json:"execution_id"`
	Duplicate   bool                `json:"duplicate"`
	Transitions []AppliedTransition `json:"applied_transitions"`
}
type Execution struct{ db *store.Store }

func NewExecution(db *store.Store) (*Execution, error) {
	if db == nil || db.DB == nil || db.Dialect != "mysql" || db.ReadOnly() {
		return nil, errors.New("execution requires a writable MySQL/MariaDB store")
	}
	return &Execution{db: db}, nil
}
func executionError(code string) *APIError {
	switch code {
	case "not_found":
		return &APIError{404, code, "Execution target not found for this device", nil}
	case "already_claimed":
		return &APIError{409, code, "Manual run is already claimed", nil}
	case "claim_expired":
		return &APIError{410, code, "Manual-run claim is missing, invalid, or expired", nil}
	case "invalid_request":
		return &APIError{422, code, "Idempotency key was previously used with a different result", nil}
	default:
		return &APIError{503, "unavailable", "Temporarily unable to process the request", nil}
	}
}
func executionUUID(value string) ([]byte, string, error) {
	value = strings.TrimPrefix(value, "urn:uuid:")
	if len(value) == 38 && value[0] == '{' && value[37] == '}' {
		value = value[1:37]
	}
	if len(value) == 36 {
		if value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
			return nil, "", invalidRequest()
		}
		value = strings.ReplaceAll(value, "-", "")
	}
	if len(value) != 32 {
		return nil, "", invalidRequest()
	}
	b, err := hex.DecodeString(value)
	if err != nil {
		return nil, "", invalidRequest()
	}
	return b, fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
func validExecutionTime(value time.Time) bool {
	_, offset := value.Zone()
	return !value.IsZero() && offset == 0 && value.Year() >= 1 && value.Year() <= 9999
}
func validateStart(path string, r ExecutionStart) (ExecutionStart, []byte, []byte, error) {
	execID, canonical, err := executionUUID(r.ExecutionID)
	if err != nil {
		return r, nil, nil, err
	}
	pathID, _, err := executionUUID(path)
	if err != nil || !bytes.Equal(pathID, execID) || r.Attempt < 1 || !validExecutionTime(r.StartedAt) {
		return r, nil, nil, invalidRequest()
	}
	detailID, detailText, err := executionUUID(r.DetailID)
	if err != nil {
		return r, nil, nil, err
	}
	r.ExecutionID, r.DetailID = canonical, detailText
	r.StartedAt = utcSecond(r.StartedAt)
	return r, execID, detailID, nil
}
func validClaimToken(token string) bool {
	if len(token) != 64 {
		return false
	}
	for _, c := range token {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func (s *Execution) Claim(ctx context.Context, principal DeviceIdentity, manualID int64) (ClaimData, time.Time, error) {
	token, err := randomSecret("")
	if err != nil {
		return ClaimData{}, time.Time{}, err
	}
	result, err := s.claim(ctx, principal.DeviceID, manualID, token)
	if e := new(APIError); errors.As(err, &e) && (e.Code == "not_found" || e.Code == "already_claimed") {
		if e.Code == "not_found" {
			e.Message = fmt.Sprintf("Manual run %d not found for this device", manualID)
		} else {
			e.Message = fmt.Sprintf("Manual run %d is already claimed", manualID)
		}
	}
	return result.data, result.now, err
}
func (s *Execution) Start(ctx context.Context, principal DeviceIdentity, path string, r ExecutionStart) (time.Time, error) {
	r, execID, detailID, err := validateStart(path, r)
	if err != nil {
		return time.Time{}, err
	}
	return s.start(ctx, principal.DeviceID, r, execID, detailID)
}
func (s *Execution) Result(ctx context.Context, principal DeviceIdentity, path, key string, r ExecutionResult) (ResultData, time.Time, error) {
	if !validExecutionTime(r.FinishedAt) || r.StartedAt.After(r.FinishedAt) || (r.ManualID == nil) != (r.ClaimToken == nil) || (r.ClaimToken != nil && !validClaimToken(*r.ClaimToken)) || (r.ResultMessage != nil && !utf8.ValidString(*r.ResultMessage)) {
		return ResultData{}, time.Time{}, invalidRequest()
	}
	start, execID, detailID, err := validateStart(path, r.ExecutionStart)
	if err != nil {
		return ResultData{}, time.Time{}, err
	}
	if key != fmt.Sprintf("%s:%s:%d", start.ExecutionID, start.DetailID, start.Attempt) {
		return ResultData{}, time.Time{}, invalidRequest()
	}
	// Bound the normalized contract too, not only the HTTP bytes. In particular
	// escaped control characters may expand when a caller supplies typed values.
	raw, err := json.Marshal(r)
	if err == nil {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if err = decoder.Decode(&value); err == nil {
			raw, err = appendEnvironment(nil, value)
		}
	}
	if err != nil || len(raw) > executionBodyMax {
		return ResultData{}, time.Time{}, invalidRequest()
	}
	env, err := canonicalEnvironment(r.Environment)
	if err != nil {
		return ResultData{}, time.Time{}, err
	}
	r.ExecutionStart, r.FinishedAt = start, utcSecond(r.FinishedAt)
	result, err := s.result(ctx, principal.DeviceID, r, execID, detailID, env)
	return result.data, result.now, err
}
func (s *Execution) Event(ctx context.Context, principal DeviceIdentity, r AgentEvent) (time.Time, error) {
	switch r.Type {
	case "startup_error", "sync_error", "degraded", "recovered", "outbox_backlog":
	default:
		return time.Time{}, invalidRequest()
	}
	switch r.Severity {
	case "info", "warning", "error":
	default:
		return time.Time{}, invalidRequest()
	}
	if !validExecutionTime(r.OccurredAt) || (r.Message != nil && (!utf8.ValidString(*r.Message) || len(*r.Message) > eventMessageMax)) {
		return time.Time{}, invalidRequest()
	}
	env, err := canonicalEnvironment(r.Environment)
	if err != nil {
		return time.Time{}, err
	}
	r.OccurredAt = utcSecond(r.OccurredAt)
	return s.event(ctx, principal.DeviceID, r, env)
}

// Unlike snapshots, environment_info accepts nested JSON, finite floats and
// arbitrarily large integers. Preserve Python's compact, sorted json.dumps
// representation so cross-server replay compares equal in the TEXT column.
func canonicalEnvironment(value map[string]any) (*string, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, invalidRequest()
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var v any
	if err = decoder.Decode(&v); err != nil {
		return nil, invalidRequest()
	}
	raw, err = appendEnvironment(nil, v)
	if err != nil || len(raw) > environmentMax {
		return nil, invalidRequest()
	}
	out := string(raw)
	return &out, nil
}
func appendEnvironment(dst []byte, v any) ([]byte, error) {
	switch x := v.(type) {
	case json.Number:
		if !strings.ContainsAny(string(x), ".eE") {
			n, ok := new(big.Int).SetString(string(x), 10)
			if !ok {
				return nil, invalidRequest()
			}
			return append(dst, n.String()...), nil
		}
		n, err := strconv.ParseFloat(string(x), 64)
		if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
			return nil, invalidRequest()
		}
		scientific := strconv.FormatFloat(n, 'e', -1, 64)
		at := strings.LastIndex(scientific, "e")
		exponent, _ := strconv.Atoi(scientific[at+1:])
		if exponent >= -4 && exponent < 16 {
			plain := strconv.FormatFloat(n, 'f', -1, 64)
			if !strings.Contains(plain, ".") {
				plain += ".0"
			}
			return append(dst, plain...), nil
		}
		return append(dst, scientific...), nil
	case []any:
		dst = append(dst, '[')
		for i, value := range x {
			if i > 0 {
				dst = append(dst, ',')
			}
			var err error
			dst, err = appendEnvironment(dst, value)
			if err != nil {
				return nil, err
			}
		}
		return append(dst, ']'), nil
	case map[string]any:
		keys := make([]string, 0, len(x))
		for key := range x {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		dst = append(dst, '{')
		for i, key := range keys {
			if i > 0 {
				dst = append(dst, ',')
			}
			var err error
			dst, err = appendCanonical(dst, key)
			if err != nil {
				return nil, err
			}
			dst = append(dst, ':')
			dst, err = appendEnvironment(dst, x[key])
			if err != nil {
				return nil, err
			}
		}
		return append(dst, '}'), nil
	default:
		return appendCanonical(dst, v)
	}
}
