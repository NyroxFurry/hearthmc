package sftpd

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// session is one logged-in SSH connection.
type session struct {
	srv     *Server
	conn    *ssh.ServerConn
	started time.Time

	mu      sync.Mutex
	access  *Access
	checked time.Time
	st      Stats
	written int64 // bytes written in this session (for the disk limit)
}

var (
	errIsDir = errors.New("ist ein Ordner")
	errQuota = errors.New("Das Speicherlimit des Servers ist erreicht")
	errRoot  = errors.New("der Serverordner selbst kann nicht geändert werden")
)

func (s *session) current() *Access {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.access
}

func (s *session) stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st
}

func (s *session) count(f func(*Stats)) {
	s.mu.Lock()
	f(&s.st)
	s.mu.Unlock()
}

// allow checks a permission. Every minute the access is checked again with
// the panel; when it is gone, the session ends.
func (s *session) allow(need func(*Access) bool) error {
	s.mu.Lock()
	a, due := s.access, time.Since(s.checked) > recheckEvery
	s.mu.Unlock()
	if due {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		na, err := s.srv.Auth.Recheck(ctx, a)
		cancel()
		if err != nil || na == nil {
			s.conn.Close()
			return sftp.ErrSSHFxPermissionDenied
		}
		s.mu.Lock()
		s.access, s.checked, a = na, time.Now(), na
		s.mu.Unlock()
	}
	if !need(a) {
		return sftp.ErrSSHFxPermissionDenied
	}
	return nil
}

func canRead(a *Access) bool   { return a.Read }
func canWrite(a *Access) bool  { return a.Write }
func canDelete(a *Access) bool { return a.Delete }

// reserve accounts n written bytes against the disk limit of the server.
func (s *session) reserve(n int64) error {
	a := s.current()
	if a.Free == nil {
		return nil
	}
	free := a.Free()
	if free < 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if free-s.written <= 0 || free-s.written < n {
		return errQuota
	}
	s.written += n
	return nil
}

// rel turns an SFTP path into a path inside the server directory ("." = the
// directory itself). os.Root rejects anything that escapes it, including
// symlinks pointing outside.
func rel(p string) (string, error) {
	if strings.ContainsRune(p, 0) {
		return "", os.ErrInvalid
	}
	c := strings.TrimPrefix(path.Clean("/"+strings.ReplaceAll(p, "\\", "/")), "/")
	if c == "" {
		return ".", nil
	}
	return filepath.FromSlash(c), nil
}

type handler struct {
	sess *session
	root *os.Root
}

func newHandler(sess *session) (*handler, error) {
	r, err := os.OpenRoot(sess.current().Root)
	if err != nil {
		return nil, err
	}
	return &handler{sess: sess, root: r}, nil
}

func (h *handler) close() { h.root.Close() }

// ---- reading ----

type readFile struct {
	f    *os.File
	sess *session
}

func (r *readFile) ReadAt(b []byte, off int64) (int, error) {
	n, err := r.f.ReadAt(b, off)
	r.sess.count(func(s *Stats) { s.BytesOut += int64(n) })
	return n, err
}

func (r *readFile) Close() error { return r.f.Close() }

func (h *handler) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	if err := h.sess.allow(canRead); err != nil {
		return nil, err
	}
	p, err := rel(r.Filepath)
	if err != nil {
		return nil, err
	}
	f, err := h.root.Open(p)
	if err != nil {
		return nil, err
	}
	if st, err := f.Stat(); err != nil || st.IsDir() {
		f.Close()
		return nil, errIsDir
	}
	h.sess.count(func(s *Stats) { s.Downloaded++ })
	return &readFile{f: f, sess: h.sess}, nil
}

// ---- writing ----

type writeFile struct {
	f     *os.File
	sess  *session
	wrote bool
}

func (w *writeFile) WriteAt(b []byte, off int64) (int, error) {
	if err := w.sess.reserve(int64(len(b))); err != nil {
		return 0, err
	}
	n, err := w.f.WriteAt(b, off)
	w.wrote = true
	w.sess.count(func(s *Stats) { s.BytesIn += int64(n) })
	return n, err
}

func (w *writeFile) ReadAt(b []byte, off int64) (int, error) { return w.f.ReadAt(b, off) }

func (w *writeFile) Close() error {
	if w.wrote {
		w.sess.count(func(s *Stats) { s.Uploaded++ })
	}
	return w.f.Close()
}

func (h *handler) open(r *sftp.Request, read bool) (*writeFile, error) {
	need := canWrite
	if read {
		need = func(a *Access) bool { return a.Write && a.Read }
	}
	if err := h.sess.allow(need); err != nil {
		return nil, err
	}
	p, err := rel(r.Filepath)
	if err != nil {
		return nil, err
	}
	if p == "." {
		return nil, errIsDir
	}
	if err := h.sess.reserve(0); err != nil {
		return nil, err
	}
	pf := r.Pflags()
	flags := os.O_WRONLY
	if read {
		flags = os.O_RDWR
	}
	if pf.Creat {
		flags |= os.O_CREATE
	}
	if pf.Trunc {
		flags |= os.O_TRUNC
	}
	if pf.Excl {
		flags |= os.O_EXCL
	}
	_, statErr := h.root.Lstat(p)
	f, err := h.root.OpenFile(p, flags, 0o644)
	if err != nil {
		return nil, err
	}
	if st, err := f.Stat(); err != nil || st.IsDir() {
		f.Close()
		return nil, errIsDir
	}
	if statErr != nil {
		h.sess.count(func(s *Stats) { s.Created++ })
	}
	return &writeFile{f: f, sess: h.sess}, nil
}

func (h *handler) Filewrite(r *sftp.Request) (io.WriterAt, error) { return h.open(r, false) }

func (h *handler) OpenFile(r *sftp.Request) (sftp.WriterAtReaderAt, error) {
	return h.open(r, r.Pflags().Read)
}

// ---- commands ----

func (h *handler) Filecmd(r *sftp.Request) error {
	p, err := rel(r.Filepath)
	if err != nil {
		return err
	}
	switch r.Method {
	case "Setstat":
		if err := h.sess.allow(canWrite); err != nil {
			return err
		}
		if p == "." {
			return errRoot
		}
		fl, at := r.AttrFlags(), r.Attributes()
		if fl.Size {
			f, err := h.root.OpenFile(p, os.O_WRONLY, 0)
			if err != nil {
				return err
			}
			err = f.Truncate(int64(at.Size))
			f.Close()
			if err != nil {
				return err
			}
		}
		if fl.Permissions {
			// plain permission bits only, no setuid/setgid/sticky
			if err := h.root.Chmod(p, at.FileMode()&0o777); err != nil {
				return err
			}
		}
		if fl.Acmodtime {
			if err := h.root.Chtimes(p, at.AccessTime(), at.ModTime()); err != nil {
				return err
			}
		}
		return nil // owner changes are ignored
	case "Rename":
		return h.rename(r, p, false)
	case "Mkdir":
		if err := h.sess.allow(canWrite); err != nil {
			return err
		}
		if err := h.root.Mkdir(p, 0o755); err != nil {
			return err
		}
		h.sess.count(func(s *Stats) { s.Created++ })
		return nil
	case "Rmdir", "Remove":
		if err := h.sess.allow(canDelete); err != nil {
			return err
		}
		if p == "." {
			return errRoot
		}
		st, err := h.root.Lstat(p)
		if err != nil {
			return err
		}
		if st.IsDir() != (r.Method == "Rmdir") {
			if st.IsDir() {
				return errIsDir
			}
			return errors.New("ist kein Ordner")
		}
		if err := h.root.Remove(p); err != nil {
			return err
		}
		h.sess.count(func(s *Stats) { s.Deleted++ })
		return nil
	}
	// Link, Symlink and anything else: links could point outside the server
	// directory for the game server process, so they are not offered
	return sftp.ErrSSHFxOpUnsupported
}

// PosixRename may replace an existing target (posix-rename@openssh.com).
func (h *handler) PosixRename(r *sftp.Request) error {
	p, err := rel(r.Filepath)
	if err != nil {
		return err
	}
	return h.rename(r, p, true)
}

func (h *handler) rename(r *sftp.Request, p string, replace bool) error {
	if err := h.sess.allow(canWrite); err != nil {
		return err
	}
	q, err := rel(r.Target)
	if err != nil {
		return err
	}
	if p == "." || q == "." {
		return errRoot
	}
	if !replace {
		if _, err := h.root.Lstat(q); err == nil {
			return os.ErrExist // plain SFTP rename never overwrites
		}
	}
	if err := h.root.Rename(p, q); err != nil {
		return err
	}
	h.sess.count(func(s *Stats) { s.Renamed++ })
	return nil
}

// ---- listing ----

type listerAt []os.FileInfo

func (l listerAt) ListAt(out []os.FileInfo, off int64) (int, error) {
	if off >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(out, l[off:])
	if n < len(out) {
		return n, io.EOF
	}
	return n, nil
}

func (h *handler) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	if err := h.sess.allow(canRead); err != nil {
		return nil, err
	}
	p, err := rel(r.Filepath)
	if err != nil {
		return nil, err
	}
	switch r.Method {
	case "List":
		d, err := h.root.Open(p)
		if err != nil {
			return nil, err
		}
		defer d.Close()
		ents, err := d.ReadDir(-1)
		if err != nil {
			return nil, err
		}
		infos := make([]os.FileInfo, 0, len(ents))
		for _, e := range ents {
			if fi, err := e.Info(); err == nil {
				infos = append(infos, fi)
			}
		}
		sort.Slice(infos, func(i, j int) bool { return infos[i].Name() < infos[j].Name() })
		return listerAt(infos), nil
	case "Stat":
		fi, err := h.root.Stat(p)
		if err != nil {
			return nil, err
		}
		return listerAt{fi}, nil
	}
	return nil, sftp.ErrSSHFxOpUnsupported
}

func (h *handler) Lstat(r *sftp.Request) (sftp.ListerAt, error) {
	if err := h.sess.allow(canRead); err != nil {
		return nil, err
	}
	p, err := rel(r.Filepath)
	if err != nil {
		return nil, err
	}
	fi, err := h.root.Lstat(p)
	if err != nil {
		return nil, err
	}
	return listerAt{fi}, nil
}

func (h *handler) Readlink(name string) (string, error) {
	if err := h.sess.allow(canRead); err != nil {
		return "", err
	}
	p, err := rel(name)
	if err != nil {
		return "", err
	}
	return h.root.Readlink(p)
}
