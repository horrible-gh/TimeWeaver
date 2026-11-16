package agentapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

type ExecutionHTTP struct {
	auth      *IdentityHTTP
	execution *Execution
}

func NewExecutionHTTP(auth *IdentityHTTP, execution *Execution) (*ExecutionHTTP, error) {
	if auth == nil || auth.identity == nil || execution == nil || execution.db == nil || auth.identity.db != execution.db {
		return nil, errors.New("execution and identity must share a store")
	}
	return &ExecutionHTTP{auth, execution}, nil
}
func (h *ExecutionHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	if !strings.HasPrefix(path, h.auth.base+"/") {
		writeFailure(w, executionError("not_found"))
		return
	}
	parts := strings.Split(strings.TrimPrefix(path, h.auth.base+"/"), "/")
	kind, id := "", ""
	switch {
	case len(parts) == 1 && parts[0] == "events":
		kind = "event"
	case len(parts) == 3 && parts[0] == "manual-runs" && parts[2] == "claim" && parts[1] != "":
		kind, id = "claim", parts[1]
	case len(parts) == 3 && parts[0] == "executions" && (parts[2] == "start" || parts[2] == "results") && parts[1] != "":
		kind, id = parts[2], parts[1]
	default:
		writeFailure(w, executionError("not_found"))
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeFailure(w, &APIError{405, "method_not_allowed", "Method not allowed", nil})
		return
	}
	principal, err := h.auth.Authenticate(r)
	if err != nil {
		writeFailure(w, err)
		return
	}
	var manualID int64
	var start ExecutionStart
	var result ExecutionResult
	var event AgentEvent
	required := []string{"execution_grp_id", "schedule_id", "detail_id", "attempt", "started_at"}
	switch kind {
	case "claim":
		manualID, err = strconv.ParseInt(id, 10, 64)
		if err != nil {
			err = invalidRequest()
		}
	case "start":
		err = decodeExecutionBody(w, r, &start, required)
	case "results":
		err = decodeExecutionBody(w, r, &result, append(required, "finished_at", "result_code"))
	case "event":
		err = decodeExecutionBody(w, r, &event, []string{"event_type", "severity", "occurred_at"})
	}
	if err != nil {
		writeFailure(w, err)
		return
	}
	if !h.auth.acquire() {
		writeFailure(w, &APIError{503, "unavailable", "Service is busy", nil})
		return
	}
	defer h.auth.release()
	switch kind {
	case "claim":
		data, now, e := h.execution.Claim(r.Context(), principal, manualID)
		if e != nil {
			writeFailure(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"schema_version": "1", "server_time": now, "data": data})
	case "start":
		now, e := h.execution.Start(r.Context(), principal, id, start)
		if e != nil {
			writeFailure(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"schema_version": "1", "server_time": now, "data": map[string]bool{"accepted": true}})
	case "results":
		data, now, e := h.execution.Result(r.Context(), principal, id, r.Header.Get("Idempotency-Key"), result)
		if e != nil {
			writeFailure(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"schema_version": "1", "server_time": now, "data": data})
	case "event":
		now, e := h.execution.Event(r.Context(), principal, event)
		if e != nil {
			writeFailure(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"schema_version": "1", "server_time": now, "data": map[string]bool{"accepted": true}})
	}
}
func decodeExecutionBody(w http.ResponseWriter, r *http.Request, dst any, required []string) error {
	if raw := r.Header.Get("Content-Type"); raw != "" {
		media, _, err := mime.ParseMediaType(raw)
		if err != nil || (media != "application/json" && !strings.HasSuffix(media, "+json")) {
			return invalidRequest()
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, executionBodyMax)
	defer r.Body.Close()
	raw, err := io.ReadAll(r.Body)
	if err != nil || !utf8.Valid(raw) {
		return invalidRequest()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return invalidRequest()
	}
	for _, key := range required {
		value, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return invalidRequest()
		}
	}
	// Pydantic's existing integer coercion includes numeric strings and integral
	// JSON floats. Avoid float64 so identifiers/attempts never lose precision.
	for _, key := range []string{"schedule_id", "attempt", "manual_id", "result_code"} {
		value, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			continue
		}
		n, e := executionInt(value)
		if e != nil {
			return invalidRequest()
		}
		fields[key] = json.RawMessage(strconv.FormatInt(n, 10))
	}
	raw, err = json.Marshal(fields)
	if err != nil {
		return invalidRequest()
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(dst) != nil {
		return invalidRequest()
	}
	return nil
}
func executionInt(raw []byte) (int64, error) {
	var v any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&v) != nil {
		return 0, invalidRequest()
	}
	var text string
	switch x := v.(type) {
	case json.Number:
		text = string(x)
	case string:
		text = strings.TrimSpace(x)
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	default:
		return 0, invalidRequest()
	}
	if len(text) == 0 || len(text) > 128 || strings.ContainsAny(text, "/xXpP_") {
		return 0, invalidRequest()
	}
	// Bound exponent work before math/big to avoid attacker-controlled allocation.
	if i := strings.IndexAny(text, "eE"); i >= 0 {
		e, err := strconv.ParseInt(text[i+1:], 10, 16)
		if err != nil || e < -128 || e > 128 {
			return 0, invalidRequest()
		}
	}
	n, ok := new(big.Rat).SetString(text)
	if !ok || !n.IsInt() || !n.Num().IsInt64() {
		return 0, invalidRequest()
	}
	return n.Num().Int64(), nil
}
