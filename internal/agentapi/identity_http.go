package agentapi

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/horrible-gh/TimeWeaver/internal/config"
)

// HTTPOptions is supplied by server composition. No listener is started here.
type HTTPOptions struct {
	Capacity       int
	Refill         float64
	Concurrency    int
	TrustedProxies []netip.Prefix
}
type bucket struct {
	tokens float64
	at     time.Time
}
type IdentityHTTP struct {
	identity *Identity
	base     string
	options  HTTPOptions
	slots    chan struct{}
	mu       sync.Mutex
	buckets  map[string]bucket
}

func NewIdentityHTTP(identity *Identity, context string, options HTTPOptions) (*IdentityHTTP, error) {
	if identity == nil {
		return nil, errors.New("identity service is required")
	}
	if err := config.ValidateContext(context); err != nil {
		return nil, err
	}
	if options.Capacity <= 0 || options.Capacity > 100000 || options.Refill <= 0 || math.IsNaN(options.Refill) || math.IsInf(options.Refill, 0) || options.Concurrency <= 0 || options.Concurrency > 4096 {
		return nil, errors.New("invalid identity HTTP limits")
	}
	options.TrustedProxies = append([]netip.Prefix(nil), options.TrustedProxies...)
	return &IdentityHTTP{identity: identity, base: context + "/api/agent/v1", options: options, slots: make(chan struct{}, options.Concurrency), buckets: map[string]bucket{}}, nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func writeFailure(w http.ResponseWriter, err error) {
	var e *APIError
	if !errors.As(err, &e) {
		e = failure("unavailable")
	}
	detail := map[string]any{"code": e.Code, "message": e.Message}
	for k, v := range e.Details {
		detail[k] = v
	}
	if e.Status == 429 || e.Status == 503 {
		retry := 5
		if n, ok := detail["retry_after"].(int); ok {
			retry = n
		}
		w.Header().Set("Retry-After", strconv.Itoa(retry))
	}
	if e.Status == 401 {
		w.Header().Set("WWW-Authenticate", "Bearer")
	}
	writeJSON(w, e.Status, map[string]any{"detail": detail})
}
func invalidRequest() *APIError {
	return &APIError{Status: 422, Code: "invalid_request", Message: "Request validation failed"}
}
func decodeBody(w http.ResponseWriter, r *http.Request, v any) error {
	if raw := r.Header.Get("Content-Type"); raw != "" {
		media, _, err := mime.ParseMediaType(raw)
		if err != nil || (media != "application/json" && !strings.HasSuffix(media, "+json")) {
			return invalidRequest()
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(v); err != nil {
		return invalidRequest()
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return invalidRequest()
	}
	return nil
}
func validText(value *string, min, max int) bool {
	return value != nil && utf8.ValidString(*value) && utf8.RuneCountInString(*value) >= min && utf8.RuneCountInString(*value) <= max
}
func (h *IdentityHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Dispatch directly. ServeMux path cleaning and slash redirects are not part
	// of the agent protocol (the Go agent explicitly refuses redirects).
	path := strings.TrimSuffix(r.URL.Path, "/")
	enroll := path == h.base+"/enroll"
	refresh := path == h.base+"/token" || path == h.base+"/token/refresh"
	if !enroll && !refresh {
		writeFailure(w, &APIError{404, "not_found", "Not found", nil})
		return
	}
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		writeFailure(w, &APIError{405, "method_not_allowed", "Method not allowed", nil})
		return
	}
	var token, name string
	var version *string
	if enroll {
		var body struct {
			Token   *string `json:"enrollment_token"`
			Name    *string `json:"device_name"`
			Version *string `json:"agent_version"`
		}
		if err := decodeBody(w, r, &body); err != nil {
			writeFailure(w, err)
			return
		}
		if !validText(body.Token, 5, 256) || !validText(body.Name, 1, 255) || (body.Version != nil && !validText(body.Version, 0, 50)) {
			writeFailure(w, invalidRequest())
			return
		}
		token = *body.Token
		name = strings.TrimSpace(*body.Name)
		version = body.Version
		if name == "" {
			writeFailure(w, invalidRequest())
			return
		}
	} else {
		var body struct {
			Token *string `json:"refresh_token"`
		}
		if err := decodeBody(w, r, &body); err != nil {
			writeFailure(w, err)
			return
		}
		if !validText(body.Token, 5, 256) {
			writeFailure(w, invalidRequest())
			return
		}
		token = *body.Token
	}
	endpoint := "refresh"
	if enroll {
		endpoint = "enroll"
	}
	if err := h.limit("unauthenticated:"+endpoint+":"+h.address(r), "Request rate exceeded"); err != nil {
		writeFailure(w, err)
		return
	}
	if !h.acquire() {
		writeFailure(w, &APIError{503, "unavailable", "Service is busy", nil})
		return
	}
	defer h.release()
	var data TokenData
	var err error
	if enroll {
		data, err = h.identity.Enroll(r.Context(), token, name, version)
	} else {
		data, err = h.identity.Rotate(r.Context(), token)
	}
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"schema_version": "1", "server_time": time.Now().UTC(), "data": data})
}

// Authenticate is used by protected agent handlers, never by browser sessions.
func (h *IdentityHTTP) Authenticate(r *http.Request) (DeviceIdentity, error) {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return DeviceIdentity{}, &APIError{401, "invalid_token", "Agent bearer token required", nil}
	}
	if !h.acquire() {
		return DeviceIdentity{}, &APIError{503, "unavailable", "Service is busy", nil}
	}
	defer h.release()
	v, err := h.identity.VerifyAccess(r.Context(), parts[1])
	if err != nil {
		return v, err
	}
	err = h.limit("device:"+strconv.FormatInt(v.DeviceID, 10), "Device request rate exceeded")
	return v, err
}
func (h *IdentityHTTP) acquire() bool {
	select {
	case h.slots <- struct{}{}:
		return true
	default:
		return false
	}
}
func (h *IdentityHTTP) release() { <-h.slots }
func (h *IdentityHTTP) address(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown"
	}
	ip = ip.Unmap()
	for _, prefix := range h.options.TrustedProxies {
		if prefix.Contains(ip) {
			first := strings.TrimSpace(strings.SplitN(r.Header.Get("X-Forwarded-For"), ",", 2)[0])
			if forwarded, err := netip.ParseAddr(first); err == nil {
				return forwarded.Unmap().String()
			}
			break
		}
	}
	return ip.String()
}
func (h *IdentityHTTP) limit(key, message string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	b, exists := h.buckets[key]
	capacity := float64(h.options.Capacity)
	if !exists {
		if len(h.buckets) >= 10000 {
			for k, v := range h.buckets {
				if now.Sub(v.at).Seconds()*h.options.Refill >= capacity*2 {
					delete(h.buckets, k)
				}
			}
		}
		// Bound memory without evicting a live attacker's exhausted bucket.
		if len(h.buckets) >= 10000 {
			return &APIError{429, "rate_limited", message, map[string]any{"retry_after": 5}}
		}
		b = bucket{capacity, now}
	}
	b.tokens = math.Min(capacity, b.tokens+math.Max(0, now.Sub(b.at).Seconds())*h.options.Refill)
	b.at = now
	if b.tokens < 1 {
		h.buckets[key] = b
		retry := int(math.Min(86400, math.Ceil((1-b.tokens)/h.options.Refill)))
		if retry < 1 {
			retry = 1
		}
		return &APIError{429, "rate_limited", message, map[string]any{"retry_after": retry}}
	}
	b.tokens--
	h.buckets[key] = b
	return nil
}
