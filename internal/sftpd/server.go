// Package sftpd is the built-in SFTP server. Users log in as
// "<username>.<server-id>" with their panel password or an SSH key and see
// the files of that one server, within their file permissions.
package sftpd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Access describes what a logged-in user may do on one server.
type Access struct {
	ServerID   string
	ServerName string
	Root       string // server directory
	UserID     int64
	Username   string
	Method     string // "password" or "publickey"
	Read       bool
	Write      bool
	Delete     bool
	// Free returns the bytes the server may still use (< 0 = no limit).
	Free func() int64
	// Token is opaque to the SFTP server; the authenticator uses it to see
	// whether the login is still valid (password changed, key removed).
	Token string
}

// Stats counts what happened in a session (for the activity log).
type Stats struct {
	Uploaded   int   `json:"uploaded"`
	Downloaded int   `json:"downloaded"`
	Deleted    int   `json:"deleted"`
	Renamed    int   `json:"renamed"`
	Created    int   `json:"created"`
	BytesIn    int64 `json:"bytesIn"`
	BytesOut   int64 `json:"bytesOut"`
}

// Authenticator checks logins; user and server are the two halves of the SSH
// user name. It is implemented by the API, which knows users, permissions and
// rate limits.
type Authenticator interface {
	Password(ctx context.Context, user, server, password, ip string) (*Access, error)
	PublicKey(ctx context.Context, user, server string, key ssh.PublicKey, ip string) (*Access, error)
	// Opened reports a successful login (after the handshake: a key that was
	// only offered, without proof of the private key, never gets here).
	Opened(a *Access, ip string)
	// Recheck returns the current access of an open session, so revoked
	// permissions or a locked server end running sessions.
	Recheck(ctx context.Context, a *Access) (*Access, error)
	// Closed reports the end of a session.
	Closed(a *Access, ip string, st Stats, d time.Duration)
}

const (
	maxConns      = 64
	maxConnsPerIP = 8
	idleTimeout   = 15 * time.Minute
)

// recheckEvery is how often open sessions ask the panel again (a variable
// for tests).
var recheckEvery = time.Minute

type Server struct {
	Addr   string
	Signer ssh.Signer
	Auth   Authenticator
	Log    *slog.Logger

	mu    sync.Mutex
	ln    net.Listener
	conns map[net.Conn]string // conn -> ip
	perIP map[string]int
	done  bool
}

// SplitUser splits "name.serverid" at the last dot.
func SplitUser(s string) (user, server string, ok bool) {
	i := strings.LastIndexByte(s, '.')
	if i <= 0 || i == len(s)-1 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

// ErrDenied is the error users see for every failed login.
var ErrDenied = errors.New("access denied")

type accessKey struct{}

func (s *Server) config(ip string) *ssh.ServerConfig {
	perm := func(a *Access, err error) (*ssh.Permissions, error) {
		if err != nil || a == nil {
			return nil, ErrDenied
		}
		return &ssh.Permissions{ExtraData: map[any]any{accessKey{}: a}}, nil
	}
	cfg := &ssh.ServerConfig{
		ServerVersion: "SSH-2.0-Hearth",
		MaxAuthTries:  6,
		PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			user, server, ok := SplitUser(c.User())
			if !ok {
				return nil, ErrDenied
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return perm(s.Auth.Password(ctx, user, server, string(pw), ip))
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			user, server, ok := SplitUser(c.User())
			if !ok {
				return nil, ErrDenied
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return perm(s.Auth.PublicKey(ctx, user, server, key, ip))
		},
	}
	cfg.AddHostKey(s.Signer)
	return cfg
}

// Listen opens the listening socket. Serve then accepts connections.
func (s *Server) Listen() error {
	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.ln, s.conns, s.perIP = ln, map[net.Conn]string{}, map[string]int{}
	s.mu.Unlock()
	return nil
}

// Serve accepts connections until Close is called.
func (s *Server) Serve() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			s.mu.Lock()
			done := s.done
			s.mu.Unlock()
			if done {
				return
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			s.Log.Warn("SFTP: accept", "err", err)
			time.Sleep(time.Second)
			continue
		}
		ip, _, _ := net.SplitHostPort(c.RemoteAddr().String())
		if !s.track(c, ip) {
			c.Close()
			continue
		}
		go func() {
			defer s.untrack(c)
			s.handle(c, ip)
		}()
	}
}

func (s *Server) track(c net.Conn, ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done || len(s.conns) >= maxConns || s.perIP[ip] >= maxConnsPerIP {
		return false
	}
	s.conns[c] = ip
	s.perIP[ip]++
	return true
}

func (s *Server) untrack(c net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ip, ok := s.conns[c]; ok {
		delete(s.conns, c)
		if s.perIP[ip]--; s.perIP[ip] <= 0 {
			delete(s.perIP, ip)
		}
	}
}

// Close stops listening and ends all sessions.
func (s *Server) Close() error {
	s.mu.Lock()
	s.done = true
	ln := s.ln
	conns := make([]net.Conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	for _, c := range conns {
		c.Close()
	}
	if ln != nil {
		return ln.Close()
	}
	return nil
}

// idleConn closes logged-in connections without traffic for a while.
type idleConn struct {
	net.Conn
	idle atomic.Bool
}

func (c *idleConn) Read(b []byte) (int, error) {
	if c.idle.Load() {
		c.Conn.SetReadDeadline(time.Now().Add(idleTimeout))
	}
	return c.Conn.Read(b)
}

func (s *Server) handle(nc net.Conn, ip string) {
	defer nc.Close()
	ic := &idleConn{Conn: nc}
	ic.SetDeadline(time.Now().Add(30 * time.Second)) // handshake and login
	sc, chans, reqs, err := ssh.NewServerConn(ic, s.config(ip))
	if err != nil {
		return
	}
	defer sc.Close()
	ic.SetDeadline(time.Time{})
	ic.idle.Store(true)
	access, _ := sc.Permissions.ExtraData[accessKey{}].(*Access)
	if access == nil {
		return
	}
	sess := &session{srv: s, conn: sc, access: access, checked: time.Now(), started: time.Now()}
	go ssh.DiscardRequests(reqs)
	s.Log.Info("SFTP-Anmeldung", "user", access.Username, "server", access.ServerID, "ip", ip, "method", access.Method)
	s.Auth.Opened(access, ip)
	var wg sync.WaitGroup
	for nch := range chans {
		if nch.ChannelType() != "session" {
			nch.Reject(ssh.UnknownChannelType, "only sessions are supported")
			continue
		}
		ch, creqs, err := nch.Accept()
		if err != nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sess.serveChannel(ch, creqs)
		}()
	}
	wg.Wait()
	s.Auth.Closed(sess.current(), ip, sess.stats(), time.Since(sess.started))
}

// serveChannel waits for the "sftp" subsystem request and runs the SFTP
// protocol on the channel. Shells and commands are refused.
func (sess *session) serveChannel(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	started := false
	for req := range reqs {
		switch {
		case req.Type == "subsystem" && !started && isSFTP(req.Payload):
			req.Reply(true, nil)
			started = true
			go func() {
				for r := range reqs {
					r.Reply(false, nil)
				}
			}()
			sess.runSFTP(ch)
			return
		case req.Type == "shell" || req.Type == "exec":
			fmt.Fprintln(ch.Stderr(), "Hearth bietet nur SFTP an, keine Shell.")
			req.Reply(false, nil)
		default:
			req.Reply(false, nil)
		}
	}
}

func isSFTP(payload []byte) bool {
	// string payload: uint32 length + "sftp"
	return len(payload) >= 4 && string(payload[4:]) == "sftp"
}

func (sess *session) runSFTP(ch ssh.Channel) {
	h, err := newHandler(sess)
	if err != nil {
		fmt.Fprintln(ch.Stderr(), "Serverordner nicht verfügbar")
		return
	}
	defer h.close()
	rs := sftp.NewRequestServer(ch, sftp.Handlers{FileGet: h, FilePut: h, FileCmd: h, FileList: h}, sftp.WithStartDirectory("/"))
	if err := rs.Serve(); err != nil && !errors.Is(err, io.EOF) {
		sess.srv.Log.Debug("SFTP-Sitzung beendet", "err", err)
	}
	rs.Close()
}
