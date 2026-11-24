package agentapi

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var snapshotRevision = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func validHeartbeat(r HeartbeatRequest) bool {
	if !utf8.ValidString(r.AgentVersion) || utf8.RuneCountInString(r.AgentVersion) < 1 || utf8.RuneCountInString(r.AgentVersion) > 50 {
		return false
	}
	if r.AppliedRevision != nil && !snapshotRevision.MatchString(*r.AppliedRevision) {
		return false
	}
	switch r.State {
	case "BOOTSTRAP", "HEALTHY", "DEGRADED", "HALTED":
		return true
	}
	return false
}

// RuntimeHTTP shares identity verification, admission slots and device rate
// buckets with IdentityHTTP. It starts no listener and exposes no admin routes.
type RuntimeHTTP struct {
	auth    *IdentityHTTP
	runtime *Runtime
}

func NewRuntimeHTTP(auth *IdentityHTTP, runtime *Runtime) (*RuntimeHTTP, error) {
	if auth == nil || runtime == nil || runtime.db == nil || auth.identity == nil || auth.identity.db != runtime.db {
		return nil, errors.New("runtime and identity must share a store")
	}
	return &RuntimeHTTP{auth: auth, runtime: runtime}, nil
}
func (h *RuntimeHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	heartbeat := path == h.auth.base+"/heartbeat"
	snapshot := path == h.auth.base+"/snapshot"
	if !heartbeat && !snapshot {
		writeFailure(w, &APIError{404, "not_found", "Not found", nil})
		return
	}
	method := "GET"
	if heartbeat {
		method = "POST"
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		writeFailure(w, &APIError{405, "method_not_allowed", "Method not allowed", nil})
		return
	}
	principal, err := h.auth.Authenticate(r)
	if err != nil {
		writeFailure(w, err)
		return
	}
	cap := r.Header.Get("X-TW-Agent-Capabilities")
	if !utf8.ValidString(cap) || utf8.RuneCountInString(cap) > 1024 || (heartbeat && utf8.RuneCountInString(r.Header.Get("X-TW-Agent-Version")) > 50) {
		writeFailure(w, invalidRequest())
		return
	}
	var body HeartbeatRequest
	if heartbeat {
		if err = decodeBody(w, r, &body); err != nil {
			writeFailure(w, err)
			return
		}
		if !validHeartbeat(body) {
			writeFailure(w, invalidRequest())
			return
		}
	} else if values, present := r.Header["X-Tw-Schema-Version"]; present {
		value := ""
		if len(values) > 0 {
			value = strings.TrimSpace(values[0])
		}
		version, e := strconv.ParseInt(value, 10, 64)
		if e != nil || version < 1 {
			writeFailure(w, &APIError{422, "schema_mismatch", "Schema version is no longer supported, minimum is 1", nil})
			return
		}
	}
	if !h.auth.acquire() {
		writeFailure(w, &APIError{503, "unavailable", "Service is busy", nil})
		return
	}
	defer h.auth.release()
	if heartbeat {
		data, e := h.runtime.Heartbeat(r.Context(), principal, body, cap)
		if e != nil {
			writeFailure(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"schema_version": "1", "server_time": data.ServerTime, "data": data})
		return
	}
	result, err := h.runtime.Snapshot(r.Context(), principal)
	if err != nil {
		writeFailure(w, err)
		return
	}
	// Must precede the matching ETag path: a downgraded agent cannot reuse a
	// previously cached plan containing recursive housekeeping.
	if (needsRecursive(result.Data) && !supportsRecursive(cap)) || (needsDeleteEmpty(result.Data) && !supportsDeleteEmpty(cap)) {
		writeFailure(w, &APIError{409, "agent_upgrade_required", "Update TimeWeaver-Agent to use the configured housekeeping options", nil})
		return
	}
	w.Header().Set("ETag", result.ETag)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Header.Get("If-None-Match") == result.ETag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, 200, map[string]any{"schema_version": "1", "server_time": result.Data.GeneratedAt, "data": result.Data})
}
