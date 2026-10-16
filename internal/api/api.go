// Package api implements the HTTP JSON API and WebSocket endpoint of the panel.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"hearth/internal/auth"
	"hearth/internal/backup"
	"hearth/internal/config"
	"hearth/internal/events"
	"hearth/internal/javart"
	"hearth/internal/mapr"
	"hearth/internal/minecraft"
	"hearth/internal/notify"
	"hearth/internal/perms"
	"hearth/internal/sched"
	"hearth/internal/servers"
	"hearth/internal/settings"
	"hearth/internal/store"
)

const sessionCookie = "hearth_session"
const sessionTTL = 30 * 24 * time.Hour

type API struct {
	Cfg      *config.Config
	Store    *store.Store
	Settings *settings.Service
	Servers  *servers.Manager
	Backups  *backup.Service
	Sched    *sched.Scheduler
	Notify   *notify.Notifier
	Bus      *events.Bus
	Java     *javart.Manager
	MC       *minecraft.Game
	Maps     *mapr.Service
	Log      *slog.Logger
	// Static serves the embedded web UI; PublicPage renders /s/{slug}.
	Static http.Handler
	Index  func(w http.ResponseWriter, r *http.Request, page string, meta string)

	loginIP   *auth.Limiter
	loginUser *auth.Limiter
	twoFA     *auth.Limiter
	touchMu   sync.Mutex
	touched   map[string]time.Time
	started   time.Time
	sftp      SFTPInfo
	packMu    sync.Mutex // one modpack build at a time
}

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxSession
	ctxAPIKey
)

func (a *API) Init() {
	a.loginIP = auth.NewLimiter(20, 5*time.Minute)
	a.loginUser = auth.NewLimiter(10, 15*time.Minute)
	a.twoFA = auth.NewLimiter(6, 5*time.Minute)
	a.touched = map[string]time.Time{}
	a.started = time.Now()
}

// ---- JSON helpers ----

type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string { return e.Message }

func errStatus(status int, msg string) error { return &apiError{status, msg} }

var (
	errForbidden    = errStatus(http.StatusForbidden, "Dafür fehlt dir die Berechtigung")
	errNotFound     = errStatus(http.StatusNotFound, "Nicht gefunden")
	errUnauthorized = errStatus(http.StatusUnauthorized, "Bitte melde dich an")
)

func badRequest(msg string) error { return errStatus(http.StatusBadRequest, msg) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if v == nil {
		w.Write([]byte("{}"))
		return
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func ok(w http.ResponseWriter, v any) { writeJSON(w, http.StatusOK, v) }

func (a *API) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apiError
	switch {
	case errors.As(err, &ae):
		writeJSON(w, ae.Status, map[string]string{"error": ae.Message})
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Nicht gefunden"})
	case errors.Is(err, servers.ErrNotRunning), errors.Is(err, servers.ErrAlreadyRunning), errors.Is(err, servers.ErrBusy),
		errors.Is(err, servers.ErrNotInstalled), errors.Is(err, servers.ErrSuspended), errors.Is(err, mapr.ErrRunning):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		// most internal errors carry user-readable (German) messages
		a.Log.Debug("request failed", "path", r.URL.Path, "err", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
}

func readJSON(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 4<<20)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		if err == io.EOF {
			return badRequest("Leere Anfrage")
		}
		return badRequest("Ungültige Anfrage: " + err.Error())
	}
	return nil
}

func queryInt(r *http.Request, key string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(key)); err == nil {
		return v
	}
	return def
}

// ---- request context ----

func userFrom(r *http.Request) *store.User {
	u, _ := r.Context().Value(ctxUser).(*store.User)
	return u
}

func sessionFrom(r *http.Request) *store.Session {
	s, _ := r.Context().Value(ctxSession).(*store.Session)
	return s
}

func (a *API) clientIP(r *http.Request) string {
	if a.Cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.Split(xff, ",")[0])
		}
		if xr := r.Header.Get("X-Real-Ip"); xr != "" {
			return xr
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (a *API) isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return a.Cfg.TrustProxy && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// audit records an action of the current user.
func (a *API) audit(r *http.Request, serverID, action string, details map[string]any) {
	e := &store.AuditEntry{ServerID: serverID, Action: action, IP: a.clientIP(r)}
	if u := userFrom(r); u != nil {
		e.UserID, e.Username = u.ID, u.Username
	}
	if details != nil {
		e.Details, _ = json.Marshal(details)
	}
	a.Store.AddAudit(context.Background(), e)
}

// SystemAudit records an action performed by the panel itself.
func (a *API) SystemAudit(serverID, action string, details map[string]any) {
	e := &store.AuditEntry{ServerID: serverID, Action: action, Username: "System"}
	if details != nil {
		e.Details, _ = json.Marshal(details)
	}
	a.Store.AddAudit(context.Background(), e)
}

// ---- middleware ----

type statusWriter struct {
	http.ResponseWriter
	status int
	size   int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	n, err := w.ResponseWriter.Write(b)
	w.size += n
	return n, err
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (a *API) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		defer func() {
			if rec := recover(); rec != nil {
				a.Log.Error("panic", "path", r.URL.Path, "err", rec, "stack", string(debug.Stack()))
				if sw.status == 0 {
					writeJSON(sw, 500, map[string]string{"error": "Interner Fehler"})
				}
			}
			if strings.HasPrefix(r.URL.Path, "/api/") && !strings.Contains(r.URL.Path, "/tiles/") && !strings.HasPrefix(r.URL.Path, "/api/heads/") {
				a.Log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", sw.status, "dur", time.Since(start).Round(time.Millisecond))
			}
		}()
		h := sw.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		if !strings.HasPrefix(r.URL.Path, "/s/") && !strings.HasPrefix(r.URL.Path, "/api/public/") {
			h.Set("X-Frame-Options", "SAMEORIGIN")
		}
		if a.isHTTPS(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		r = a.authenticate(r)
		// CSRF protection: state changing requests from the browser must
		// carry a custom header (cross-site forms cannot set it).
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions &&
			strings.HasPrefix(r.URL.Path, "/api/") && r.Context().Value(ctxAPIKey) == nil {
			if r.Header.Get("X-Hearth") == "" {
				writeJSON(sw, http.StatusForbidden, map[string]string{"error": "CSRF-Schutz: Header fehlt"})
				return
			}
		}
		next.ServeHTTP(sw, r)
	})
}

// authenticate attaches the user of a session cookie or API key to the request.
func (a *API) authenticate(r *http.Request) *http.Request {
	ctx := r.Context()
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		tok := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		k, err := a.Store.GetAPIKeyByHash(ctx, auth.HashToken(tok))
		if err != nil || (k.ExpiresAt > 0 && k.ExpiresAt < time.Now().Unix()) {
			return r
		}
		u, err := a.Store.GetUser(ctx, k.UserID)
		if err != nil || u.Disabled {
			return r
		}
		a.Store.TouchAPIKey(ctx, k.ID)
		ctx = context.WithValue(ctx, ctxUser, u)
		ctx = context.WithValue(ctx, ctxAPIKey, k)
		return r.WithContext(ctx)
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return r
	}
	id := auth.HashToken(c.Value)
	sess, err := a.Store.GetSession(ctx, id)
	if err != nil || sess.ExpiresAt < time.Now().Unix() {
		return r
	}
	u, err := a.Store.GetUser(ctx, sess.UserID)
	if err != nil || u.Disabled {
		return r
	}
	// sliding expiry, written at most once per minute per session
	a.touchMu.Lock()
	last := a.touched[id]
	if time.Since(last) > time.Minute {
		a.touched[id] = time.Now()
		a.touchMu.Unlock()
		if !sess.Pending2FA {
			a.Store.TouchSession(ctx, id, time.Now().Add(sessionTTL).Unix(), a.clientIP(r))
		}
	} else {
		a.touchMu.Unlock()
	}
	ctx = context.WithValue(ctx, ctxSession, sess)
	ctx = context.WithValue(ctx, ctxUser, u)
	return r.WithContext(ctx)
}

type userHandler func(w http.ResponseWriter, r *http.Request, u *store.User) error

// authed requires a fully authenticated user.
func (a *API) authed(h userHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFrom(r)
		if u == nil {
			a.fail(w, r, errUnauthorized)
			return
		}
		if s := sessionFrom(r); s != nil && s.Pending2FA {
			a.fail(w, r, errStatus(http.StatusUnauthorized, "Zwei-Faktor-Bestätigung ausstehend"))
			return
		}
		if err := h(w, r, u); err != nil {
			a.fail(w, r, err)
		}
	}
}

func (a *API) admin(h userHandler) http.HandlerFunc {
	return a.authed(func(w http.ResponseWriter, r *http.Request, u *store.User) error {
		if !u.IsAdmin {
			return errForbidden
		}
		return h(w, r, u)
	})
}

type serverHandler func(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error

// permsFor resolves the permissions of a user on a server (nil = no access).
func (a *API) permsFor(ctx context.Context, u *store.User, srv *store.Server) *perms.Set {
	if u.IsAdmin {
		return perms.Full(srv.OwnerID == u.ID)
	}
	if srv.OwnerID == u.ID {
		return perms.Full(true)
	}
	m, err := a.Store.GetMember(ctx, srv.ID, u.ID)
	if err != nil {
		return nil
	}
	return perms.FromList(m.Permissions)
}

// srv loads the server {id} and checks perm ("" = any access).
func (a *API) srv(perm string, h serverHandler) http.HandlerFunc {
	return a.authed(func(w http.ResponseWriter, r *http.Request, u *store.User) error {
		inst := a.Servers.Get(r.PathValue("id"))
		if inst == nil {
			return errNotFound
		}
		ps := a.permsFor(r.Context(), u, inst.Server())
		if ps == nil {
			return errNotFound
		}
		if perm != "" && !ps.Has(perm) {
			return errForbidden
		}
		return h(w, r, u, inst, ps)
	})
}

// ---- sessions ----

func (a *API) createSession(w http.ResponseWriter, r *http.Request, u *store.User, pending bool) error {
	tok := auth.RandomToken(32)
	now := time.Now()
	ttl := sessionTTL
	if pending {
		ttl = 10 * time.Minute
	}
	sess := &store.Session{
		ID: auth.HashToken(tok), UserID: u.ID, Pending2FA: pending, CreatedAt: now.Unix(),
		ExpiresAt: now.Add(ttl).Unix(), LastSeenAt: now.Unix(), IP: a.clientIP(r),
		UserAgent: truncate(r.UserAgent(), 300),
	}
	if err := a.Store.CreateSession(r.Context(), sess); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: tok, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: a.isHTTPS(r), Expires: now.Add(sessionTTL),
	})
	return nil
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, MaxAge: -1, SameSite: http.SameSiteLaxMode})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// PublicURL returns the configured external URL or derives it from the request.
func (a *API) PublicURL(r *http.Request) string {
	if u := strings.TrimRight(a.Settings.Get(settings.PanelURL), "/"); u != "" {
		return u
	}
	scheme := "http"
	if a.isHTTPS(r) {
		scheme = "https"
	}
	host := r.Host
	if a.Cfg.TrustProxy && r.Header.Get("X-Forwarded-Host") != "" {
		host = r.Header.Get("X-Forwarded-Host")
	}
	return scheme + "://" + host
}

func fmtBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	default:
		return fmt.Sprintf("%d KB", b>>10)
	}
}
