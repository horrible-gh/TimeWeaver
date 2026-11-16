// Package webui serves the compiled Vue application without a Node runtime.
package webui

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/horrible-gh/TimeWeaver/internal/config"
)

//go:embed assets
var embedded embed.FS

var assetTag = regexp.MustCompile(`(?i)<(?:script|link)\b[^>]*\b(?:src|href)\s*=\s*(?:"([^"]+)"|'([^']+)'|([^\s>]+))`)
var appRoot = regexp.MustCompile(`(?i)\bid\s*=\s*(?:"app"|'app'|app)(?:\s|>)`)
var hashedName = regexp.MustCompile(`\.[0-9a-fA-F]{8,64}\.(?:js|css|png|jpg|jpeg|svg|webp|woff2?|ttf)$`)

type Handler struct {
	assets           fs.FS
	context          string
	login, dashboard []byte
}

func Bundled(context string) (*Handler, error) {
	assets, err := fs.Sub(embedded, "assets/dist")
	if err != nil {
		return nil, errors.New("embedded UI is missing; run the Go build script")
	}
	return New(assets, context)
}

// New validates both entry points and their local script/style references before
// serving anything. A placeholder or a partial frontend build must fail closed.
func New(assets fs.FS, context string) (*Handler, error) {
	if err := config.ValidateContext(context); err != nil {
		return nil, err
	}
	h := &Handler{assets: assets, context: context}
	for _, entry := range []struct {
		name   string
		target *[]byte
	}{{"index.html", &h.login}, {"dashboard/index.html", &h.dashboard}} {
		raw, err := fs.ReadFile(assets, entry.name)
		if err != nil {
			return nil, fmt.Errorf("embedded UI entry %s is missing; run the Go build script", entry.name)
		}
		if !utf8.Valid(raw) || !appRoot.Match(raw) {
			return nil, fmt.Errorf("invalid Vue entry: %s", entry.name)
		}
		text := string(raw)
		lower := strings.ToLower(text)
		if strings.Count(lower, "</head>") != 1 || strings.Contains(lower, "tw-api-context") {
			return nil, fmt.Errorf("invalid UI head: %s", entry.name)
		}
		localScript := false
		for _, match := range assetTag.FindAllStringSubmatch(text, -1) {
			ref := match[1]
			if ref == "" {
				ref = match[2]
			}
			if ref == "" {
				ref = match[3]
			}
			parsed, e := url.Parse(html.UnescapeString(ref))
			if e != nil {
				return nil, fmt.Errorf("invalid asset reference in %s", entry.name)
			}
			if parsed.IsAbs() || parsed.Host != "" {
				continue
			} // Existing public font/icon URLs remain unchanged.
			assetPath := parsed.Path
			if !strings.HasPrefix(assetPath, "/") {
				assetPath = path.Join("/", path.Dir(entry.name), assetPath)
			}
			if !safePath(assetPath) {
				return nil, fmt.Errorf("unsafe asset reference in %s", entry.name)
			}
			item, e := fs.Stat(assets, strings.TrimPrefix(assetPath, "/"))
			if e != nil || !item.Mode().IsRegular() {
				return nil, fmt.Errorf("missing local UI asset: %s", assetPath)
			}
			if strings.HasPrefix(strings.ToLower(match[0]), "<script") && path.Ext(assetPath) == ".js" {
				localScript = true
			}
		}
		if !localScript {
			return nil, fmt.Errorf("Vue bundle not found in %s", entry.name)
		}
		position := strings.Index(lower, "</head>")
		*entry.target = []byte(text[:position] + `<meta name="tw-api-context" content="` + html.EscapeString(context) + `">` + text[position:])
	}
	return h, nil
}

func safePath(value string) bool {
	if !strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\\x00\r\n") {
		return false
	}
	if value == "/" {
		return true
	}
	trimmed := strings.TrimSuffix(value, "/")
	if path.Clean(value) != trimmed {
		return false
	}
	for _, part := range strings.Split(trimmed, "/")[1:] {
		if part == "" || strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	p := r.URL.Path
	// API misses must never receive index.html or a redirect from the SPA fallback.
	if p == h.context || strings.HasPrefix(p, h.context+"/") {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNotFound)
		if r.Method != http.MethodHead {
			_ = json.NewEncoder(w).Encode(map[string]any{"detail": map[string]string{"code": "not_found", "message": "API route not found"}})
		}
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !safePath(p) {
		http.NotFound(w, r)
		return
	}
	switch p {
	case "/", "/index.html", "/login", "/login/":
		h.serve(w, r, "index.html", h.login, false)
		return
	case "/dashboard", "/dashboard/", "/dashboard/index.html":
		h.serve(w, r, "dashboard.html", h.dashboard, false)
		return
	}
	if strings.HasPrefix(p, "/dashboard/") && path.Ext(strings.TrimSuffix(p, "/")) == "" {
		h.serve(w, r, "dashboard.html", h.dashboard, false)
		return
	}
	switch strings.ToLower(path.Ext(p)) {
	case ".js", ".css", ".png", ".jpg", ".jpeg", ".svg", ".webp", ".ico", ".woff", ".woff2", ".ttf":
	default:
		http.NotFound(w, r)
		return
	}
	raw, err := fs.ReadFile(h.assets, strings.TrimPrefix(p, "/"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h.serve(w, r, path.Base(p), raw, hashedName.MatchString(path.Base(p)))
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request, name string, raw []byte, immutable bool) {
	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	if immutable {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	hash := sha256.Sum256(raw)
	w.Header().Set("ETag", `"`+hex.EncodeToString(hash[:])+`"`)
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(raw))
}
