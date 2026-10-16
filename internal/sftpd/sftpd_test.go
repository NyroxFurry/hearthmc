package sftpd

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type fakeAuth struct {
	mu      sync.Mutex
	root    string
	clientK ssh.PublicKey
	read    bool
	write   bool
	del     bool
	free    int64
	revoked bool
	closed  []Stats
}

func (f *fakeAuth) access(user, server, method string) (*Access, error) {
	if user != "alice" || server != "srv1" {
		return nil, errors.New("unknown")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return &Access{ServerID: server, Root: f.root, UserID: 1, Username: user, Method: method, Read: f.read, Write: f.write, Delete: f.del,
		Free: func() int64 { f.mu.Lock(); defer f.mu.Unlock(); return f.free }}, nil
}

func (f *fakeAuth) Password(ctx context.Context, user, server, pw, ip string) (*Access, error) {
	if pw != "geheim" {
		return nil, errors.New("wrong password")
	}
	return f.access(user, server, "password")
}

func (f *fakeAuth) PublicKey(ctx context.Context, user, server string, key ssh.PublicKey, ip string) (*Access, error) {
	if f.clientK == nil || !bytes.Equal(key.Marshal(), f.clientK.Marshal()) {
		return nil, errors.New("unknown key")
	}
	return f.access(user, server, "publickey")
}

func (f *fakeAuth) Recheck(ctx context.Context, a *Access) (*Access, error) {
	f.mu.Lock()
	revoked := f.revoked
	f.mu.Unlock()
	if revoked {
		return nil, errors.New("revoked")
	}
	return f.access(a.Username, a.ServerID, a.Method)
}

func (f *fakeAuth) Opened(a *Access, ip string) {}

func (f *fakeAuth) Closed(a *Access, ip string, st Stats, d time.Duration) {
	f.mu.Lock()
	f.closed = append(f.closed, st)
	f.mu.Unlock()
}

func startServer(t *testing.T, auth *fakeAuth) string {
	t.Helper()
	signer, err := LoadHostKey(filepath.Join(t.TempDir(), "host_key"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Addr: "127.0.0.1:0", Signer: signer, Auth: auth, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := s.Listen(); err != nil {
		t.Fatal(err)
	}
	go s.Serve()
	t.Cleanup(func() { s.Close() })
	return s.ln.Addr().String()
}

func dial(addr, user string, methods ...ssh.AuthMethod) (*sftp.Client, *ssh.Client, error) {
	c, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{User: user, Auth: methods, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second})
	if err != nil {
		return nil, nil, err
	}
	sc, err := sftp.NewClient(c)
	if err != nil {
		c.Close()
		return nil, nil, err
	}
	return sc, c, nil
}

func TestSFTP(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("geheim"), 0o644)
	os.WriteFile(filepath.Join(root, "server.properties"), []byte("motd=Hallo\n"), 0o644)
	os.Symlink(outside, filepath.Join(root, "escape"))
	auth := &fakeAuth{root: root, read: true, write: true, del: true, free: -1}
	addr := startServer(t, auth)

	if _, _, err := dial(addr, "alice.srv1", ssh.Password("falsch")); err == nil {
		t.Fatal("login with a wrong password worked")
	}
	if _, _, err := dial(addr, "alice", ssh.Password("geheim")); err == nil {
		t.Fatal("login without server id worked")
	}
	c, conn, err := dial(addr, "alice.srv1", ssh.Password("geheim"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// shells are refused
	if s, err := conn.NewSession(); err == nil {
		if err := s.Run("id"); err == nil {
			t.Error("exec worked")
		}
		s.Close()
	}

	list, err := c.ReadDir("/")
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %v %v", list, err)
	}
	f, err := c.Create("/plugins/x.txt")
	if err == nil {
		f.Close()
		t.Fatal("created a file in a missing folder")
	}
	if err := c.Mkdir("/plugins"); err != nil {
		t.Fatal(err)
	}
	f, err = c.Create("/plugins/config.yml")
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("a: 1\n"))
	f.Close()
	if b, _ := os.ReadFile(filepath.Join(root, "plugins", "config.yml")); string(b) != "a: 1\n" {
		t.Fatalf("uploaded content %q", b)
	}
	rf, err := c.Open("/server.properties")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rf)
	rf.Close()
	if string(b) != "motd=Hallo\n" {
		t.Fatalf("read %q", b)
	}
	// escapes: ../ stays inside, symlinks out of the folder do not work
	if _, err := c.Stat("/../../../etc/passwd"); err == nil {
		t.Error("stat outside root")
	}
	if _, err := c.Open("/escape/secret.txt"); err == nil {
		t.Error("followed a symlink out of the server folder")
	}
	if err := c.Symlink("/etc/passwd", "/link"); err == nil {
		t.Error("symlink created")
	}
	// rename never overwrites, posix-rename does
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("A"), 0o644)
	os.WriteFile(filepath.Join(root, "b.txt"), []byte("B"), 0o644)
	if err := c.Rename("/a.txt", "/b.txt"); err == nil {
		t.Error("rename overwrote a file")
	}
	if err := c.PosixRename("/a.txt", "/b.txt"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "b.txt")); string(b) != "A" {
		t.Errorf("posix rename result %q", b)
	}
	if err := c.Chmod("/b.txt", 0o4755); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(filepath.Join(root, "b.txt")); st.Mode()&os.ModeSetuid != 0 || st.Mode().Perm() != 0o755 {
		t.Errorf("chmod mode %v", st.Mode())
	}
	if err := c.Remove("/plugins"); err == nil {
		t.Error("removed a folder with Remove")
	}
	if err := c.Remove("/plugins/config.yml"); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveDirectory("/plugins"); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove("/"); err == nil {
		t.Error("removed the root")
	}
	c.Close()
	conn.Close()

	// read-only member
	auth.mu.Lock()
	auth.write, auth.del = false, false
	auth.mu.Unlock()
	c, conn, err = dial(addr, "alice.srv1", ssh.Password("geheim"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Create("/new.txt"); err == nil {
		t.Error("read-only user created a file")
	}
	if err := c.Remove("/b.txt"); err == nil {
		t.Error("read-only user deleted a file")
	}
	if _, err := c.Stat("/b.txt"); err != nil {
		t.Errorf("read-only stat: %v", err)
	}
	c.Close()
	conn.Close()

	// disk limit
	auth.mu.Lock()
	auth.write, auth.free = true, 10
	auth.mu.Unlock()
	c, conn, err = dial(addr, "alice.srv1", ssh.Password("geheim"))
	if err != nil {
		t.Fatal(err)
	}
	f, err = c.Create("/big.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(make([]byte, 100)); err == nil {
		if err := f.Close(); err == nil {
			t.Error("write over the disk limit worked")
		}
	} else {
		f.Close()
	}
	c.Close()
	conn.Close()

	// public key login and revocation of a running session
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	sshPub, _ := ssh.NewPublicKey(pub)
	signer, _ := ssh.NewSignerFromKey(priv)
	auth.mu.Lock()
	auth.clientK, auth.free = sshPub, -1
	auth.mu.Unlock()
	old := recheckEvery
	recheckEvery = 50 * time.Millisecond
	defer func() { recheckEvery = old }()
	c, conn, err = dial(addr, "alice.srv1", ssh.PublicKeys(signer))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadDir("/"); err != nil {
		t.Fatal(err)
	}
	auth.mu.Lock()
	auth.revoked = true
	auth.mu.Unlock()
	time.Sleep(100 * time.Millisecond)
	if _, err := c.ReadDir("/"); err == nil {
		t.Error("revoked session still works")
	}
	c.Close()
	conn.Close()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		auth.mu.Lock()
		n := len(auth.closed)
		auth.mu.Unlock()
		if n >= 4 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	auth.mu.Lock()
	defer auth.mu.Unlock()
	if len(auth.closed) < 4 || auth.closed[0].Uploaded != 1 || auth.closed[0].Deleted != 2 || auth.closed[0].Downloaded != 1 {
		t.Errorf("session stats %+v", auth.closed)
	}
}

func TestSplitUser(t *testing.T) {
	for in, want := range map[string][2]string{"alice.srv1": {"alice", "srv1"}, "a.b.c.srv": {"a.b.c", "srv"}} {
		u, s, ok := SplitUser(in)
		if !ok || u != want[0] || s != want[1] {
			t.Errorf("%q: %q %q %v", in, u, s, ok)
		}
	}
	for _, in := range []string{"alice", ".srv", "alice.", ""} {
		if _, _, ok := SplitUser(in); ok {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestHostKeyStable(t *testing.T) {
	p := filepath.Join(t.TempDir(), "k")
	a, err := LoadHostKey(p)
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadHostKey(p)
	if err != nil {
		t.Fatal(err)
	}
	if Fingerprint(a) != Fingerprint(b) || Fingerprint(a) == "" {
		t.Fatal("host key changed")
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Errorf("host key mode %v", st.Mode())
	}
	_ = net.IPv4len
}
