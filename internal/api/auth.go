package api

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"hearth/internal/auth"
	"hearth/internal/perms"
	"hearth/internal/settings"
	"hearth/internal/store"
	"hearth/internal/version"
)

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,32}$`)

func validUsername(n string) error {
	if !usernameRe.MatchString(n) {
		return badRequest("Benutzername: 3–32 Zeichen, nur Buchstaben, Zahlen, _ . -")
	}
	return nil
}

// meView is the user object returned to the UI.
type meView struct {
	*store.User
	ServerCount int `json:"serverCount"`
}

func (a *API) panelInfo() map[string]any {
	return map[string]any{
		"name":      a.Settings.Get(settings.PanelName),
		"version":   version.Version,
		"publicUrl": a.Settings.Get(settings.PanelURL),
	}
}

// GET /api/setup
func (a *API) setupStatus(w http.ResponseWriter, r *http.Request) {
	n, err := a.Store.CountUsers(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	ok(w, map[string]any{"needsSetup": n == 0, "panel": a.panelInfo()})
}

// POST /api/setup creates the first administrator. Requires the setup code
// printed to the log on first start.
func (a *API) setup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token     string `json:"token"`
		Username  string `json:"username"`
		Email     string `json:"email"`
		Password  string `json:"password"`
		PanelName string `json:"panelName"`
		PanelURL  string `json:"panelUrl"`
	}
	if err := readJSON(r, &req); err != nil {
		a.fail(w, r, err)
		return
	}
	if !a.loginIP.Allow("setup:" + a.clientIP(r)) {
		a.fail(w, r, errStatus(429, "Zu viele Versuche, bitte warte kurz"))
		return
	}
	n, _ := a.Store.CountUsers(r.Context())
	if n > 0 {
		a.fail(w, r, errStatus(http.StatusConflict, "Das Panel ist bereits eingerichtet"))
		return
	}
	want := a.Settings.Get(settings.SetupToken)
	if want == "" || subtle.ConstantTimeCompare([]byte(auth.HashToken(strings.TrimSpace(req.Token))), []byte(want)) != 1 {
		a.fail(w, r, errStatus(http.StatusForbidden, "Der Setup-Code ist falsch. Du findest ihn in der Konsole bzw. im Log des Panels."))
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if err := validUsername(req.Username); err != nil {
		a.fail(w, r, err)
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		a.fail(w, r, badRequest(err.Error()))
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	u := &store.User{Username: req.Username, Email: strings.TrimSpace(req.Email), PasswordHash: hash, IsAdmin: true, CanCreateServers: true}
	if err := a.Store.CreateUser(r.Context(), u); err != nil {
		a.fail(w, r, err)
		return
	}
	ctx := r.Context()
	if name := strings.TrimSpace(req.PanelName); name != "" {
		a.Settings.Set(ctx, settings.PanelName, truncate(name, 60))
	}
	if url := strings.TrimRight(strings.TrimSpace(req.PanelURL), "/"); url != "" && (strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")) {
		a.Settings.Set(ctx, settings.PanelURL, url)
	}
	if u.Email != "" {
		a.Settings.Set(ctx, settings.Contact, u.Email)
	}
	a.Settings.Delete(ctx, settings.SetupToken)
	if err := a.createSession(w, r, u, false); err != nil {
		a.fail(w, r, err)
		return
	}
	a.Store.TouchLogin(ctx, u.ID)
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, u))
	a.audit(r, "", "panel.setup", nil)
	ok(w, map[string]any{"user": u})
}

// POST /api/auth/login
func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		a.fail(w, r, err)
		return
	}
	ip := a.clientIP(r)
	name := strings.ToLower(strings.TrimSpace(req.Username))
	if !a.loginIP.Allow(ip) || a.loginUser.Blocked(name) {
		a.fail(w, r, errStatus(429, "Zu viele Anmeldeversuche. Bitte warte ein paar Minuten."))
		return
	}
	u, err := a.Store.GetUserByName(r.Context(), name)
	if err != nil {
		auth.CheckDummy(req.Password)
		a.loginUser.Allow(name)
		a.Store.AddAudit(r.Context(), &store.AuditEntry{Action: "auth.login_failed", Username: truncate(name, 40), IP: ip})
		a.fail(w, r, errStatus(http.StatusUnauthorized, "Benutzername oder Passwort falsch"))
		return
	}
	if !auth.CheckPassword(u.PasswordHash, req.Password) {
		a.loginUser.Allow(name)
		a.Store.AddAudit(r.Context(), &store.AuditEntry{Action: "auth.login_failed", UserID: u.ID, Username: u.Username, IP: ip})
		a.fail(w, r, errStatus(http.StatusUnauthorized, "Benutzername oder Passwort falsch"))
		return
	}
	if u.Disabled {
		a.fail(w, r, errStatus(http.StatusForbidden, "Dieses Konto ist deaktiviert"))
		return
	}
	a.loginUser.Reset(name)
	if err := a.createSession(w, r, u, u.TOTPEnabled); err != nil {
		a.fail(w, r, err)
		return
	}
	if u.TOTPEnabled {
		ok(w, map[string]any{"twoFactor": true})
		return
	}
	a.Store.TouchLogin(r.Context(), u.ID)
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, u))
	a.audit(r, "", "auth.login", nil)
	ok(w, map[string]any{"user": u})
}

// POST /api/auth/2fa completes a login with a TOTP or recovery code.
func (a *API) login2FA(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	u := userFrom(r)
	if sess == nil || u == nil || !sess.Pending2FA {
		a.fail(w, r, errUnauthorized)
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := readJSON(r, &req); err != nil {
		a.fail(w, r, err)
		return
	}
	if !a.twoFA.Allow(sess.ID) {
		a.Store.DeleteSession(r.Context(), sess.ID)
		clearSessionCookie(w)
		a.fail(w, r, errStatus(429, "Zu viele Fehlversuche – bitte erneut anmelden"))
		return
	}
	code := strings.TrimSpace(req.Code)
	valid := auth.VerifyTOTP(u.TOTPSecret, code, strconv.FormatInt(u.ID, 10))
	if !valid && len(code) > 6 {
		// recovery code (single use)
		h := auth.HashToken(strings.ToLower(code))
		for i, rc := range u.RecoveryCodes {
			if subtle.ConstantTimeCompare([]byte(rc), []byte(h)) == 1 {
				valid = true
				remaining := append(append([]string{}, u.RecoveryCodes[:i]...), u.RecoveryCodes[i+1:]...)
				a.Store.SetRecoveryCodes(r.Context(), u.ID, remaining)
				break
			}
		}
	}
	if !valid {
		a.fail(w, r, errStatus(http.StatusUnauthorized, "Code ungültig"))
		return
	}
	a.Store.CompleteSession2FA(r.Context(), sess.ID)
	a.Store.TouchSession(r.Context(), sess.ID, time.Now().Add(sessionTTL).Unix(), a.clientIP(r))
	a.Store.TouchLogin(r.Context(), u.ID)
	a.audit(r, "", "auth.login", map[string]any{"twoFactor": true})
	ok(w, map[string]any{"user": u})
}

// POST /api/auth/logout
func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if s := sessionFrom(r); s != nil {
		a.Store.DeleteSession(r.Context(), s.ID)
	}
	clearSessionCookie(w)
	ok(w, nil)
}

// GET /api/auth/me
func (a *API) me(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u == nil {
		ok(w, map[string]any{"user": nil, "panel": a.panelInfo()})
		return
	}
	if s := sessionFrom(r); s != nil && s.Pending2FA {
		ok(w, map[string]any{"user": nil, "twoFactor": true, "panel": a.panelInfo()})
		return
	}
	ok(w, map[string]any{"user": u, "panel": a.panelInfo()})
}

// ---- invites ----

func (a *API) loadInvite(r *http.Request) (*store.Invite, error) {
	inv, err := a.Store.GetInviteByHash(r.Context(), auth.HashToken(r.PathValue("token")))
	if err != nil {
		return nil, errStatus(http.StatusNotFound, "Einladung nicht gefunden")
	}
	if inv.UsedAt > 0 {
		return nil, errStatus(http.StatusGone, "Diese Einladung wurde bereits verwendet")
	}
	if inv.ExpiresAt < time.Now().Unix() {
		return nil, errStatus(http.StatusGone, "Diese Einladung ist abgelaufen")
	}
	return inv, nil
}

// GET /api/invites/{token}
func (a *API) inviteInfo(w http.ResponseWriter, r *http.Request) {
	inv, err := a.loadInvite(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	out := map[string]any{"valid": true, "expiresAt": inv.ExpiresAt, "panel": a.panelInfo()}
	if inv.ServerID != "" {
		if inst := a.Servers.Get(inv.ServerID); inst != nil {
			out["serverName"] = inst.Server().Name
		}
	}
	ok(w, out)
}

// POST /api/invites/{token} registers a new account.
func (a *API) inviteAccept(w http.ResponseWriter, r *http.Request) {
	inv, err := a.loadInvite(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if !a.loginIP.Allow("invite:" + a.clientIP(r)) {
		a.fail(w, r, errStatus(429, "Zu viele Versuche"))
		return
	}
	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		a.fail(w, r, err)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if err := validUsername(req.Username); err != nil {
		a.fail(w, r, err)
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		a.fail(w, r, badRequest(err.Error()))
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	u := &store.User{Username: req.Username, Email: strings.TrimSpace(req.Email), PasswordHash: hash,
		IsAdmin: inv.IsAdmin, CanCreateServers: inv.CanCreateServers}
	ctx := r.Context()
	err = a.Store.Tx(ctx, func(tx *sql.Tx) error {
		t := time.Now().Unix()
		res, err := tx.ExecContext(ctx, `INSERT INTO users (username, email, password_hash, is_admin, can_create_servers, prefs, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, '{}', ?, ?)`, u.Username, u.Email, u.PasswordHash, boolInt(u.IsAdmin), boolInt(u.CanCreateServers), t, t)
		if err != nil {
			return err
		}
		u.ID, _ = res.LastInsertId()
		u.CreatedAt, u.UpdatedAt = t, t
		if err := a.Store.UseInvite(ctx, tx, inv.ID, u.ID); err != nil {
			return errStatus(http.StatusGone, "Diese Einladung wurde bereits verwendet")
		}
		if inv.ServerID != "" && a.Servers.Get(inv.ServerID) != nil {
			return a.Store.UpsertMember(ctx, tx, inv.ServerID, u.ID, perms.Sanitize(inv.ServerPerms), inv.CreatedBy)
		}
		return nil
	})
	if err != nil {
		if store.IsUniqueViolation(err) {
			err = badRequest("Dieser Benutzername ist bereits vergeben")
		}
		a.fail(w, r, err)
		return
	}
	u.Prefs = json.RawMessage("{}")
	if err := a.createSession(w, r, u, false); err != nil {
		a.fail(w, r, err)
		return
	}
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, u))
	a.audit(r, inv.ServerID, "user.register", map[string]any{"invite": inv.ID})
	ok(w, map[string]any{"user": u})
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---- account ----

// PUT /api/account
func (a *API) updateAccount(w http.ResponseWriter, r *http.Request, u *store.User) error {
	var req struct {
		Email *string          `json:"email"`
		Prefs *json.RawMessage `json:"prefs"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	if req.Email != nil {
		u.Email = truncate(strings.TrimSpace(*req.Email), 200)
	}
	if req.Prefs != nil {
		if len(*req.Prefs) > 8192 || !json.Valid(*req.Prefs) {
			return badRequest("Ungültige Einstellungen")
		}
		u.Prefs = *req.Prefs
	}
	if err := a.Store.UpdateUser(r.Context(), u); err != nil {
		return err
	}
	ok(w, map[string]any{"user": u})
	return nil
}

// PUT /api/account/password
func (a *API) changePassword(w http.ResponseWriter, r *http.Request, u *store.User) error {
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	if !auth.CheckPassword(u.PasswordHash, req.Current) {
		return badRequest("Aktuelles Passwort ist falsch")
	}
	if err := auth.ValidatePassword(req.New); err != nil {
		return badRequest(err.Error())
	}
	hash, err := auth.HashPassword(req.New)
	if err != nil {
		return err
	}
	if err := a.Store.SetPassword(r.Context(), u.ID, hash); err != nil {
		return err
	}
	keep := ""
	if s := sessionFrom(r); s != nil {
		keep = s.ID
	}
	a.Store.DeleteUserSessions(r.Context(), u.ID, keep)
	a.audit(r, "", "account.password", nil)
	ok(w, nil)
	return nil
}

// POST /api/account/2fa/setup
func (a *API) twoFASetup(w http.ResponseWriter, r *http.Request, u *store.User) error {
	if u.TOTPEnabled {
		return badRequest("Zwei-Faktor-Authentifizierung ist bereits aktiv")
	}
	secret := auth.NewTOTPSecret()
	if err := a.Store.SetTOTP(r.Context(), u.ID, secret, false, nil); err != nil {
		return err
	}
	ok(w, map[string]any{"secret": secret, "url": auth.TOTPURL(a.Settings.Get(settings.PanelName), u.Username, secret)})
	return nil
}

// POST /api/account/2fa/enable
func (a *API) twoFAEnable(w http.ResponseWriter, r *http.Request, u *store.User) error {
	var req struct {
		Code string `json:"code"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	if u.TOTPEnabled || u.TOTPSecret == "" {
		return badRequest("Bitte zuerst die Einrichtung starten")
	}
	if !auth.VerifyTOTP(u.TOTPSecret, req.Code, strconv.FormatInt(u.ID, 10)) {
		return badRequest("Code ungültig – stimmt die Uhrzeit auf deinem Gerät?")
	}
	codes, hashes := auth.NewRecoveryCodes(10)
	if err := a.Store.SetTOTP(r.Context(), u.ID, u.TOTPSecret, true, hashes); err != nil {
		return err
	}
	a.audit(r, "", "account.2fa_enabled", nil)
	ok(w, map[string]any{"recoveryCodes": codes})
	return nil
}

// POST /api/account/2fa/disable
func (a *API) twoFADisable(w http.ResponseWriter, r *http.Request, u *store.User) error {
	var req struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	if !auth.CheckPassword(u.PasswordHash, req.Password) {
		return badRequest("Passwort ist falsch")
	}
	if err := a.Store.SetTOTP(r.Context(), u.ID, "", false, nil); err != nil {
		return err
	}
	a.audit(r, "", "account.2fa_disabled", nil)
	ok(w, nil)
	return nil
}

// GET /api/account/sessions
func (a *API) listSessions(w http.ResponseWriter, r *http.Request, u *store.User) error {
	list, err := a.Store.ListSessions(r.Context(), u.ID)
	if err != nil {
		return err
	}
	cur := ""
	if s := sessionFrom(r); s != nil {
		cur = s.ID
	}
	type view struct {
		*store.Session
		Current bool   `json:"current"`
		Key     string `json:"key"`
	}
	out := []view{}
	for _, s := range list {
		// the session id is a token hash; expose only a prefix as handle
		out = append(out, view{Session: s, Current: s.ID == cur, Key: s.ID[:16]})
	}
	for i := range out {
		cp := *out[i].Session
		cp.ID = ""
		out[i].Session = &cp
	}
	ok(w, out)
	return nil
}

// DELETE /api/account/sessions/{key}
func (a *API) deleteSession(w http.ResponseWriter, r *http.Request, u *store.User) error {
	key := r.PathValue("key")
	list, err := a.Store.ListSessions(r.Context(), u.ID)
	if err != nil {
		return err
	}
	for _, s := range list {
		if strings.HasPrefix(s.ID, key) && len(key) >= 16 {
			a.Store.DeleteSession(r.Context(), s.ID)
			ok(w, nil)
			return nil
		}
	}
	return errNotFound
}

// GET /api/account/keys
func (a *API) listKeys(w http.ResponseWriter, r *http.Request, u *store.User) error {
	list, err := a.Store.ListAPIKeys(r.Context(), u.ID)
	if err != nil {
		return err
	}
	ok(w, list)
	return nil
}

// POST /api/account/keys
func (a *API) createKey(w http.ResponseWriter, r *http.Request, u *store.User) error {
	var req struct {
		Name string `json:"name"`
		Days int    `json:"days"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 60 {
		return badRequest("Bitte einen Namen (max. 60 Zeichen) angeben")
	}
	tok := "hth_" + auth.RandomToken(30)
	k := &store.APIKey{UserID: u.ID, Name: req.Name, Prefix: tok[:10], TokenHash: auth.HashToken(tok)}
	if req.Days > 0 {
		k.ExpiresAt = time.Now().Add(time.Duration(req.Days) * 24 * time.Hour).Unix()
	}
	if err := a.Store.CreateAPIKey(r.Context(), k); err != nil {
		return err
	}
	a.audit(r, "", "account.apikey_created", map[string]any{"name": k.Name})
	ok(w, map[string]any{"key": k, "token": tok})
	return nil
}

// DELETE /api/account/keys/{kid}
func (a *API) deleteKey(w http.ResponseWriter, r *http.Request, u *store.User) error {
	id, _ := strconv.ParseInt(r.PathValue("kid"), 10, 64)
	if err := a.Store.DeleteAPIKey(r.Context(), u.ID, id); err != nil {
		return err
	}
	ok(w, nil)
	return nil
}
