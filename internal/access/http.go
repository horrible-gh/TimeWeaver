package access

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/config"
)

const cookieName = "tw_session"

type HTTPOptions struct {
	Origin         string
	TrustedProxies []netip.Prefix
}
type HTTP struct {
	service          *Service
	base             string
	origin           *url.URL
	trusted          []netip.Prefix
	slots, passwords chan struct{}
}

func NewHTTP(service *Service, base string, options HTTPOptions) (*HTTP, error) {
	if service == nil {
		return nil, errors.New("access service is required")
	}
	if err := config.ValidateContext(base); err != nil {
		return nil, err
	}
	u, err := url.Parse(options.Origin)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("PUBLIC_ORIGIN must be an exact HTTP(S) origin")
	}
	if u.Scheme == "http" && !localHost(u.Hostname()) {
		return nil, errors.New("remote human login requires HTTPS")
	}
	for _, p := range options.TrustedProxies {
		if !p.IsValid() {
			return nil, errors.New("invalid trusted proxy")
		}
	}
	u.Path = ""
	return &HTTP{service, base, u, append([]netip.Prefix(nil), options.TrustedProxies...), make(chan struct{}, 32), make(chan struct{}, 4)}, nil
}
func localHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}
func remoteIP(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, _ := netip.ParseAddr(host)
	return ip.Unmap()
}
func (h *HTTP) trustedIP(ip netip.Addr) bool {
	for _, p := range h.trusted {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}
func (h *HTTP) peer(r *http.Request) string {
	ip := remoteIP(r)
	if !ip.IsValid() {
		return "unknown"
	}
	if !h.trustedIP(ip) {
		return ip.String()
	}
	values := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	if len(values) > 32 {
		return ip.String()
	}
	for i := len(values) - 1; i >= 0 && h.trustedIP(ip); i-- {
		next, err := netip.ParseAddr(strings.TrimSpace(values[i]))
		if err != nil {
			return remoteIP(r).String()
		}
		ip = next.Unmap()
	}
	return ip.String()
}
func (h *HTTP) transport(r *http.Request) bool {
	if !strings.EqualFold(r.Host, h.origin.Host) {
		return false
	}
	if h.origin.Scheme == "https" {
		return r.TLS != nil || h.trustedIP(remoteIP(r)) && r.Header.Get("X-Forwarded-Proto") == "https"
	}
	// A reverse proxy forwarding remote users to loopback is NOT a local client.
	return remoteIP(r).IsLoopback() && localHost(h.origin.Hostname()) && r.Header.Get("Forwarded") == "" && r.Header.Get("X-Forwarded-For") == "" && r.Header.Get("X-Forwarded-Proto") == ""
}
func (h *HTTP) sameOrigin(r *http.Request, write bool) bool {
	site := r.Header.Get("Sec-Fetch-Site")
	if site != "" && site != "same-origin" && site != "none" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return !write
	}
	u, err := url.Parse(origin)
	return err == nil && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Path == "" && u.Scheme == h.origin.Scheme && strings.EqualFold(u.Host, h.origin.Host)
}
func (h *HTTP) setCookie(w http.ResponseWriter, token string, remove bool) {
	c := &http.Cookie{Name: cookieName, Value: token, Path: h.base + "/", HttpOnly: true, Secure: h.origin.Scheme == "https", SameSite: http.SameSiteStrictMode}
	if remove {
		c.MaxAge = -1
		c.Expires = time.Unix(1, 0)
	}
	http.SetCookie(w, c)
}
func tokenFrom(r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return c.Value
}
func jsonReply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, err error) {
	var e *Error
	if !errors.As(err, &e) {
		e = unavailable()
	}
	if e.Status == 429 {
		w.Header().Set("Retry-After", "300")
	} else if e.Status == 503 {
		w.Header().Set("Retry-After", "5")
	}
	jsonReply(w, e.Status, map[string]any{"detail": map[string]string{"code": e.Code, "message": "Request could not be completed"}})
}
func (h *HTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimSuffix(r.URL.Path, "/")
	if r.URL.RawPath != "" || strings.Contains(r.URL.Path, "//") {
		writeError(w, failure(404, "not_found"))
		return
	}
	wanted := "POST"
	switch p {
	case h.base + "/login", h.base + "/logout", h.base + "/auth/password":
	case h.base + "/auth/me", h.base + "/auth/methods":
		wanted = "GET"
	default:
		writeError(w, failure(404, "not_found"))
		return
	}
	if r.Method != wanted {
		w.Header().Set("Allow", wanted)
		writeError(w, failure(405, "method_not_allowed"))
		return
	}
	if !h.transport(r) {
		writeError(w, failure(403, "secure_origin_required"))
		return
	}
	if !h.sameOrigin(r, wanted == "POST") {
		writeError(w, failure(403, "origin_rejected"))
		return
	}
	// A human endpoint never accepts a device or legacy JWT, even alongside a cookie.
	if r.Header.Get("Authorization") != "" {
		writeError(w, failure(401, "unauthenticated"))
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		writeError(w, unavailable())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	if p == h.base+"/auth/methods" {
		jsonReply(w, 200, map[string]any{"methods": []string{"local"}})
		return
	}
	if p == h.base+"/login" || p == h.base+"/auth/password" {
		select {
		case h.passwords <- struct{}{}:
			defer func() { <-h.passwords }()
		default:
			writeError(w, unavailable())
			return
		}
	}
	switch p {
	case h.base + "/login":
		content, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || content != "application/x-www-form-urlencoded" {
			writeError(w, failure(415, "unsupported_media_type"))
			return
		}
		b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
		if err != nil {
			writeError(w, failure(413, "body_too_large"))
			return
		}
		form, err := url.ParseQuery(string(b))
		if err != nil || len(form["username"]) != 1 || len(form["password"]) != 1 || len(form["method"]) > 1 || (form.Get("method") != "" && form.Get("method") != "local") {
			writeError(w, failure(422, "invalid_request"))
			return
		}
		principal, token, err := h.service.Login(ctx, form.Get("username"), form.Get("password"), h.peer(r), tokenFrom(r))
		if err != nil {
			writeError(w, err)
			return
		}
		h.setCookie(w, token, false)
		jsonReply(w, 200, principal)
	case h.base + "/auth/me":
		principal, err := h.service.Session(ctx, tokenFrom(r), true)
		if err != nil {
			writeError(w, err)
			return
		}
		jsonReply(w, 200, principal)
	case h.base + "/logout":
		if err := h.service.Logout(ctx, tokenFrom(r), r.Header.Get("X-CSRF-Token")); err != nil {
			writeError(w, err)
			return
		}
		h.setCookie(w, "", true)
		jsonReply(w, 200, map[string]bool{"ok": true})
	case h.base + "/auth/password":
		content, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || content != "application/json" {
			writeError(w, failure(415, "unsupported_media_type"))
			return
		}
		var body struct {
			Current *string `json:"current_password"`
			Next    *string `json:"new_password"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		dec.DisallowUnknownFields()
		if err = dec.Decode(&body); err != nil || body.Current == nil || body.Next == nil {
			writeError(w, failure(422, "invalid_request"))
			return
		}
		var extra any
		if dec.Decode(&extra) != io.EOF {
			writeError(w, failure(422, "invalid_request"))
			return
		}
		if err = h.service.ChangePassword(ctx, tokenFrom(r), r.Header.Get("X-CSRF-Token"), *body.Current, *body.Next); err != nil {
			writeError(w, err)
			return
		}
		h.setCookie(w, "", true)
		jsonReply(w, 200, map[string]bool{"reauthenticate": true})
	}
}
