package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"hearth/internal/auth"
	"hearth/internal/perms"
	"hearth/internal/servers"
	"hearth/internal/settings"
	"hearth/internal/sftpd"
	"hearth/internal/store"
)

// SFTPInfo describes the built-in SFTP server for the UI.
type SFTPInfo struct {
	Enabled     bool
	Port        int
	Fingerprint string
	mu          sync.Mutex
	err         error
}

// SetSFTP records the state of the SFTP server (called by the app).
func (a *API) SetSFTP(enabled bool, port int, fingerprint string, err error) {
	a.sftp.mu.Lock()
	defer a.sftp.mu.Unlock()
	a.sftp.Enabled, a.sftp.Port, a.sftp.Fingerprint, a.sftp.err = enabled, port, fingerprint, err
}

// SFTPAuth returns the authenticator for the SFTP server.
func (a *API) SFTPAuth() sftpd.Authenticator { return sftpAuth{a} }

type sftpAuth struct{ a *API }

var errSFTPDenied = errors.New("denied")

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func tokenOf(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

// access resolves what a user may do on a server over SFTP.
func (s sftpAuth) access(ctx context.Context, u *store.User, serverID, method, token string) (*sftpd.Access, error) {
	if u.Disabled {
		return nil, errSFTPDenied
	}
	inst := s.a.Servers.Get(serverID)
	if inst == nil {
		return nil, errSFTPDenied
	}
	srv := inst.Server()
	ps := s.a.permsFor(ctx, u, srv)
	if ps == nil || !ps.Has(perms.FilesSFTP) || !ps.Has(perms.FilesRead) {
		return nil, errSFTPDenied
	}
	if srv.Suspended && !u.IsAdmin {
		return nil, errSFTPDenied
	}
	return &sftpd.Access{
		ServerID: srv.ID, ServerName: srv.Name, Root: inst.Dir(), UserID: u.ID, Username: u.Username, Method: method,
		Read: true, Write: ps.Has(perms.FilesWrite), Delete: ps.Has(perms.FilesDelete), Token: token,
		Free: func() int64 { return freeBytes(inst) },
	}, nil
}

func freeBytes(inst *servers.Instance) int64 {
	lim := inst.Server().DiskLimitMB
	if lim <= 0 {
		return -1
	}
	return int64(lim)<<20 - inst.DiskBytes()
}

func (s sftpAuth) loginAudit(u *store.User, serverID, action, ip string, details map[string]any) {
	e := &store.AuditEntry{ServerID: serverID, Action: action, UserID: u.ID, Username: u.Username, IP: ip}
	if details != nil {
		e.Details = mustJSON(details)
	}
	s.a.Store.AddAudit(context.Background(), e)
}

func (s sftpAuth) Password(ctx context.Context, user, server, password, ip string) (*sftpd.Access, error) {
	a := s.a
	name := strings.ToLower(strings.TrimSpace(user))
	if !a.loginIP.Allow(ip) || a.loginUser.Blocked(name) {
		return nil, errSFTPDenied
	}
	u, err := a.Store.GetUserByName(ctx, name)
	if err != nil {
		auth.CheckDummy(password)
		a.loginUser.Allow(name)
		return nil, errSFTPDenied
	}
	if !auth.CheckPassword(u.PasswordHash, password) {
		a.loginUser.Allow(name)
		s.loginAudit(u, server, "files.sftp_login_failed", ip, nil)
		return nil, errSFTPDenied
	}
	if u.TOTPEnabled {
		// the password alone would skip the second factor
		s.loginAudit(u, server, "files.sftp_login_failed", ip, map[string]any{"reason": "2fa"})
		return nil, errSFTPDenied
	}
	acc, err := s.access(ctx, u, server, "password", tokenOf(u.PasswordHash))
	if err != nil {
		if s.a.Servers.Get(server) != nil {
			s.loginAudit(u, server, "files.sftp_login_failed", ip, map[string]any{"reason": "perm"})
		}
		return nil, err
	}
	a.loginUser.Reset(name)
	return acc, nil
}

func (s sftpAuth) PublicKey(ctx context.Context, user, server string, key ssh.PublicKey, ip string) (*sftpd.Access, error) {
	a := s.a
	name := strings.ToLower(strings.TrimSpace(user))
	if a.loginUser.Blocked(name) {
		return nil, errSFTPDenied
	}
	u, err := a.Store.GetUserByName(ctx, name)
	if err != nil {
		return nil, errSFTPDenied
	}
	k, err := a.Store.FindSSHKey(ctx, u.ID, ssh.FingerprintSHA256(key))
	if err != nil {
		return nil, errSFTPDenied
	}
	// the client may only be asking whether the key would be accepted; the
	// login is recorded in Opened, after it proved to hold the private key
	return s.access(ctx, u, server, "publickey", strconv.FormatInt(k.ID, 10))
}

func (s sftpAuth) Opened(acc *sftpd.Access, ip string) {
	details := map[string]any{"method": acc.Method}
	if acc.Method == "publickey" {
		if id, err := strconv.ParseInt(acc.Token, 10, 64); err == nil {
			s.a.Store.TouchSSHKey(context.Background(), id)
		}
	}
	e := &store.AuditEntry{ServerID: acc.ServerID, Action: "files.sftp_login", UserID: acc.UserID, Username: acc.Username, IP: ip, Details: mustJSON(details)}
	s.a.Store.AddAudit(context.Background(), e)
}

func (s sftpAuth) Recheck(ctx context.Context, acc *sftpd.Access) (*sftpd.Access, error) {
	u, err := s.a.Store.GetUser(ctx, acc.UserID)
	if err != nil {
		return nil, errSFTPDenied
	}
	switch acc.Method {
	case "password":
		if u.TOTPEnabled || tokenOf(u.PasswordHash) != acc.Token {
			return nil, errSFTPDenied
		}
	case "publickey":
		id, _ := strconv.ParseInt(acc.Token, 10, 64)
		keys, err := s.a.Store.ListSSHKeys(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		found := false
		for _, k := range keys {
			found = found || k.ID == id
		}
		if !found {
			return nil, errSFTPDenied
		}
	}
	return s.access(ctx, u, acc.ServerID, acc.Method, acc.Token)
}

func (s sftpAuth) Closed(acc *sftpd.Access, ip string, st sftpd.Stats, d time.Duration) {
	if acc == nil {
		return
	}
	if inst := s.a.Servers.Get(acc.ServerID); inst != nil && (st.BytesIn > 0 || st.Deleted > 0) {
		inst.RefreshDisk()
	}
	if st.Uploaded+st.Deleted+st.Renamed+st.Created == 0 {
		return // only reading: the login entry is enough
	}
	e := &store.AuditEntry{ServerID: acc.ServerID, Action: "files.sftp_session", UserID: acc.UserID, Username: acc.Username, IP: ip,
		Details: mustJSON(map[string]any{"uploaded": st.Uploaded, "deleted": st.Deleted, "renamed": st.Renamed, "created": st.Created,
			"bytesIn": st.BytesIn, "minutes": int(d.Minutes())})}
	s.a.Store.AddAudit(context.Background(), e)
}

// sftpHost is the host users connect to: the configured one, else the host of
// the panel address.
func (a *API) sftpHost(r *http.Request) (string, int) {
	a.sftp.mu.Lock()
	port := a.sftp.Port
	a.sftp.mu.Unlock()
	if h := a.Settings.Get(settings.SFTPHost); h != "" {
		if host, p, err := net.SplitHostPort(h); err == nil {
			if n, err := strconv.Atoi(p); err == nil {
				return host, n
			}
		}
		return strings.Trim(h, "[]"), port
	}
	host := a.PublicURL(r)
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.Trim(host, "[]"), port
}

// GET /api/servers/{id}/sftp
func (a *API) serverSFTP(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	a.sftp.mu.Lock()
	enabled, fp, lerr := a.sftp.Enabled, a.sftp.Fingerprint, a.sftp.err
	a.sftp.mu.Unlock()
	host, port := a.sftpHost(r)
	keys, _ := a.Store.ListSSHKeys(r.Context(), u.ID)
	out := map[string]any{
		"enabled": enabled && lerr == nil, "host": host, "port": port, "username": u.Username + "." + inst.ID(),
		"fingerprint": fp, "passwordLogin": !u.TOTPEnabled, "keys": len(keys),
		"write": ps.Has(perms.FilesWrite), "delete": ps.Has(perms.FilesDelete),
	}
	if lerr != nil {
		out["error"] = lerr.Error()
	}
	ok(w, out)
	return nil
}

// ---- SSH keys ----

// GET /api/account/ssh-keys
func (a *API) listSSHKeys(w http.ResponseWriter, r *http.Request, u *store.User) error {
	list, err := a.Store.ListSSHKeys(r.Context(), u.ID)
	if err != nil {
		return err
	}
	ok(w, list)
	return nil
}

// POST /api/account/ssh-keys {name, publicKey}
func (a *API) addSSHKey(w http.ResponseWriter, r *http.Request, u *store.User) error {
	var req struct {
		Name      string `json:"name"`
		PublicKey string `json:"publicKey"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	line := strings.TrimSpace(req.PublicKey)
	if line == "" || len(line) > 16<<10 {
		return badRequest("Bitte einen öffentlichen Schlüssel einfügen (Inhalt der .pub-Datei)")
	}
	if strings.Contains(line, "PRIVATE KEY") {
		return badRequest("Das ist ein privater Schlüssel. Bitte nur den öffentlichen Schlüssel (.pub) einfügen und den privaten geheim halten.")
	}
	key, comment, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return badRequest("Unbekanntes Schlüsselformat. Erwartet wird eine Zeile wie „ssh-ed25519 AAAA… name“.")
	}
	if ck, ok := key.(ssh.CryptoPublicKey); ok {
		if rk, ok := ck.CryptoPublicKey().(interface{ Size() int }); ok && key.Type() == ssh.KeyAlgoRSA && rk.Size()*8 < 2048 {
			return badRequest("RSA-Schlüssel brauchen mindestens 2048 Bit")
		}
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = strings.TrimSpace(comment)
	}
	if name == "" {
		name = key.Type()
	}
	if len(name) > 60 {
		name = name[:60]
	}
	k := &store.SSHKey{UserID: u.ID, Name: name, Fingerprint: ssh.FingerprintSHA256(key),
		PublicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))}
	if err := a.Store.CreateSSHKey(r.Context(), k); err != nil {
		if store.IsUniqueViolation(err) {
			return badRequest("Dieser Schlüssel ist schon hinterlegt")
		}
		return err
	}
	a.audit(r, "", "account.sshkey_added", map[string]any{"name": k.Name, "fingerprint": k.Fingerprint})
	ok(w, k)
	return nil
}

// DELETE /api/account/ssh-keys/{kid}
func (a *API) deleteSSHKey(w http.ResponseWriter, r *http.Request, u *store.User) error {
	id, _ := strconv.ParseInt(r.PathValue("kid"), 10, 64)
	if err := a.Store.DeleteSSHKey(r.Context(), u.ID, id); err != nil {
		return err
	}
	a.audit(r, "", "account.sshkey_removed", map[string]any{"id": id})
	ok(w, nil)
	return nil
}
