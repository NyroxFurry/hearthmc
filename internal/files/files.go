// Package files implements traversal-safe file operations inside a server
// directory. All access goes through os.Root, which rejects paths (including
// symlinks) that would escape the server directory.
package files

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	kflate "github.com/klauspost/compress/flate"
)

var (
	ErrInvalidPath = errors.New("ungültiger Pfad")
	ErrIsDir       = errors.New("Pfad ist ein Ordner")
	ErrTooLarge    = errors.New("Datei ist zu groß für den Editor")
	ErrExists      = errors.New("Ziel existiert bereits")
)

// Clean normalizes a user supplied path to a slash separated path relative to
// the server root ("" = root). It rejects NUL bytes.
func Clean(p string) (string, error) {
	if strings.ContainsRune(p, 0) {
		return "", ErrInvalidPath
	}
	p = strings.ReplaceAll(p, "\\", "/")
	c := path.Clean("/" + p)
	c = strings.TrimPrefix(c, "/")
	return c, nil
}

func osPath(rel string) string {
	if rel == "" {
		return "."
	}
	return filepath.FromSlash(rel)
}

type FS struct {
	Root string
}

func New(root string) *FS { return &FS{Root: root} }

func (f *FS) open() (*os.Root, error) {
	if err := os.MkdirAll(f.Root, 0o755); err != nil {
		return nil, err
	}
	return os.OpenRoot(f.Root)
}

type Entry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	IsDir    bool   `json:"isDir"`
	Size     int64  `json:"size"`
	ModTime  int64  `json:"modTime"`
	Mode     string `json:"mode"`
	Symlink  bool   `json:"symlink,omitempty"`
	Editable bool   `json:"editable"`
	Archive  bool   `json:"archive,omitempty"`
}

var textExt = map[string]bool{
	".txt": true, ".yml": true, ".yaml": true, ".json": true, ".json5": true, ".properties": true, ".toml": true,
	".cfg": true, ".conf": true, ".config": true, ".ini": true, ".log": true, ".md": true, ".sh": true, ".bat": true,
	".cmd": true, ".ps1": true, ".js": true, ".ts": true, ".mcmeta": true, ".mcfunction": true, ".csv": true,
	".xml": true, ".html": true, ".htm": true, ".css": true, ".lang": true, ".env": true, ".lua": true, ".py": true,
	".sk": true, ".snbt": true, ".hocon": true, ".list": true, ".secret": true, ".jsonc": true, ".gitignore": true,
}

// IsTextName guesses from the file name whether a file is editable text.
func IsTextName(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		lower := strings.ToLower(name)
		return lower == "eula" || lower == "dockerfile" || lower == "makefile" || strings.HasPrefix(lower, "readme")
	}
	return textExt[ext]
}

func IsArchiveName(name string) bool {
	n := strings.ToLower(name)
	return strings.HasSuffix(n, ".zip") || strings.HasSuffix(n, ".tar.gz") || strings.HasSuffix(n, ".tgz") ||
		strings.HasSuffix(n, ".tar") || strings.HasSuffix(n, ".mrpack")
}

func (f *FS) List(rel string) ([]Entry, error) {
	r, err := f.open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	dir, err := r.Open(osPath(rel))
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	st, err := dir.Stat()
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, errors.New("Pfad ist kein Ordner")
	}
	ents, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(ents))
	for _, e := range ents {
		info, err := e.Info()
		if err != nil {
			continue
		}
		en := Entry{
			Name:    e.Name(),
			Path:    path.Join(rel, e.Name()),
			IsDir:   e.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().Unix(),
			Mode:    info.Mode().Perm().String(),
		}
		if e.Type()&fs.ModeSymlink != 0 {
			en.Symlink = true
			if tst, err := r.Stat(osPath(en.Path)); err == nil {
				en.IsDir = tst.IsDir()
				en.Size = tst.Size()
			}
		}
		if !en.IsDir {
			en.Editable = IsTextName(en.Name) && en.Size < 8<<20
			en.Archive = IsArchiveName(en.Name)
		}
		out = append(out, en)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].IsDir != out[b].IsDir {
			return out[a].IsDir
		}
		return strings.ToLower(out[a].Name) < strings.ToLower(out[b].Name)
	})
	return out, nil
}

func (f *FS) Stat(rel string) (os.FileInfo, error) {
	r, err := f.open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.Stat(osPath(rel))
}

func (f *FS) Exists(rel string) bool {
	_, err := f.Stat(rel)
	return err == nil
}

// ReadText reads a file for the editor (max bytes, rejects binary content).
func (f *FS) ReadText(rel string, max int64) ([]byte, error) {
	r, err := f.open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	fh, err := r.Open(osPath(rel))
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return nil, ErrIsDir
	}
	if st.Size() > max {
		return nil, ErrTooLarge
	}
	b, err := io.ReadAll(io.LimitReader(fh, max))
	if err != nil {
		return nil, err
	}
	if bytes.IndexByte(b[:min(len(b), 8192)], 0) >= 0 {
		return nil, errors.New("Binärdatei – kann nicht im Editor geöffnet werden")
	}
	return b, nil
}

// ReadFile reads a whole (small) file.
func (f *FS) ReadFile(rel string) ([]byte, error) {
	r, err := f.open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.ReadFile(osPath(rel))
}

// WriteFile atomically replaces a file (write temp + rename).
func (f *FS) WriteFile(rel string, data []byte) error {
	if rel == "" {
		return ErrInvalidPath
	}
	r, err := f.open()
	if err != nil {
		return err
	}
	defer r.Close()
	if dir := path.Dir(rel); dir != "." {
		if err := r.MkdirAll(osPath(dir), 0o755); err != nil {
			return err
		}
	}
	mode := os.FileMode(0o644)
	if st, err := r.Stat(osPath(rel)); err == nil {
		if st.IsDir() {
			return ErrIsDir
		}
		mode = st.Mode().Perm()
	}
	tmp := rel + fmt.Sprintf(".hearth-%d.tmp", time.Now().UnixNano())
	if err := r.WriteFile(osPath(tmp), data, mode); err != nil {
		return err
	}
	if err := r.Rename(osPath(tmp), osPath(rel)); err != nil {
		r.Remove(osPath(tmp))
		return err
	}
	return nil
}

func (f *FS) Mkdir(rel string) error {
	if rel == "" {
		return ErrInvalidPath
	}
	r, err := f.open()
	if err != nil {
		return err
	}
	defer r.Close()
	return r.MkdirAll(osPath(rel), 0o755)
}

func (f *FS) Rename(from, to string) error {
	if from == "" || to == "" {
		return ErrInvalidPath
	}
	if to == from || strings.HasPrefix(to+"/", from+"/") {
		return errors.New("Ein Ordner kann nicht in sich selbst verschoben werden")
	}
	r, err := f.open()
	if err != nil {
		return err
	}
	defer r.Close()
	if _, err := r.Lstat(osPath(to)); err == nil {
		return ErrExists
	}
	if dir := path.Dir(to); dir != "." {
		if err := r.MkdirAll(osPath(dir), 0o755); err != nil {
			return err
		}
	}
	return r.Rename(osPath(from), osPath(to))
}

func (f *FS) Remove(rel string) error {
	if rel == "" {
		return ErrInvalidPath
	}
	r, err := f.open()
	if err != nil {
		return err
	}
	defer r.Close()
	return r.RemoveAll(osPath(rel))
}

// Copy duplicates a file or directory tree.
func (f *FS) Copy(from, to string) error {
	if from == "" || to == "" {
		return ErrInvalidPath
	}
	if strings.HasPrefix(to+"/", from+"/") {
		return errors.New("Ein Ordner kann nicht in sich selbst kopiert werden")
	}
	r, err := f.open()
	if err != nil {
		return err
	}
	defer r.Close()
	if _, err := r.Lstat(osPath(to)); err == nil {
		return ErrExists
	}
	return fs.WalkDir(r.FS(), osPathFS(from), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(p, osPathFS(from))
		target := to + rel
		if d.IsDir() {
			return r.MkdirAll(osPath(target), 0o755)
		}
		if !d.Type().IsRegular() {
			return nil // skip symlinks/devices
		}
		if dir := path.Dir(target); dir != "." {
			r.MkdirAll(osPath(dir), 0o755)
		}
		return copyFile(r, p, target)
	})
}

// osPathFS converts to the slash form fs.FS expects.
func osPathFS(rel string) string {
	if rel == "" {
		return "."
	}
	return rel
}

func copyFile(r *os.Root, from, to string) error {
	src, err := r.Open(osPath(from))
	if err != nil {
		return err
	}
	defer src.Close()
	st, _ := src.Stat()
	mode := os.FileMode(0o644)
	if st != nil {
		mode = st.Mode().Perm()
	}
	dst, err := r.OpenFile(osPath(to), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}

// OpenRead opens a file for download.
func (f *FS) OpenRead(rel string) (*os.File, os.FileInfo, error) {
	r, err := f.open()
	if err != nil {
		return nil, nil, err
	}
	defer r.Close()
	fh, err := r.Open(osPath(rel))
	if err != nil {
		return nil, nil, err
	}
	st, err := fh.Stat()
	if err != nil {
		fh.Close()
		return nil, nil, err
	}
	return fh, st, nil
}

// Create opens a file for writing (uploads), creating parent directories.
func (f *FS) Create(rel string) (*os.File, error) {
	if rel == "" {
		return nil, ErrInvalidPath
	}
	r, err := f.open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if dir := path.Dir(rel); dir != "." {
		if err := r.MkdirAll(osPath(dir), 0o755); err != nil {
			return nil, err
		}
	}
	return r.OpenFile(osPath(rel), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
}

// ---- archives ----

var storeExt = map[string]bool{
	".mca": true, ".mcc": true, ".jar": true, ".zip": true, ".gz": true, ".tgz": true, ".xz": true, ".bz2": true,
	".zst": true, ".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".ogg": true, ".mp3": true, ".mp4": true,
	".dat": true, ".7z": true, ".rar": true, ".lz4": true, ".mrpack": true, ".nbt": true, ".dat_old": true,
}

// NewZipWriter returns a zip writer using a fast deflate implementation.
func NewZipWriter(w io.Writer) *zip.Writer {
	zw := zip.NewWriter(w)
	zw.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		return kflate.NewWriter(out, 3)
	})
	return zw
}

// ZipEntryMethod picks Store for already compressed formats (region files,
// jars, images) – compressing them again costs time and saves nothing.
func ZipEntryMethod(name string) uint16 {
	if storeExt[strings.ToLower(filepath.Ext(name))] {
		return zip.Store
	}
	return zip.Deflate
}

// Progress is called with the number of bytes written so far.
type Progress func(files int64, bytes int64)

// ZipPaths writes the given paths (files or folders, relative to the root)
// into a zip stream. skip may exclude paths (relative, slash separated).
func (f *FS) ZipPaths(w io.Writer, paths []string, skip func(rel string, isDir bool) bool, progress Progress) (int64, int64, error) {
	r, err := f.open()
	if err != nil {
		return 0, 0, err
	}
	defer r.Close()
	zw := NewZipWriter(w)
	var nFiles, nBytes int64
	base := ""
	if len(paths) > 0 {
		base = path.Dir(paths[0])
		for _, p := range paths[1:] {
			if path.Dir(p) != base {
				base = ""
				break
			}
		}
		if base == "." {
			base = ""
		}
	}
	if len(paths) == 0 {
		paths = []string{""}
	}
	for _, root := range paths {
		err := fs.WalkDir(r.FS(), osPathFS(root), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // unreadable entries are skipped
			}
			rel := p
			if rel == "." {
				rel = ""
			}
			if skip != nil && rel != "" && skip(rel, d.IsDir()) {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			name := rel
			if base != "" {
				name = strings.TrimPrefix(strings.TrimPrefix(rel, base), "/")
			}
			if d.IsDir() {
				if name != "" {
					hdr := &zip.FileHeader{Name: name + "/", Method: zip.Store}
					if info, err := d.Info(); err == nil {
						hdr.Modified = info.ModTime()
						hdr.SetMode(info.Mode())
					}
					_, err := zw.CreateHeader(hdr)
					return err
				}
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			hdr, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			hdr.Name = name
			hdr.Method = ZipEntryMethod(name)
			wr, err := zw.CreateHeader(hdr)
			if err != nil {
				return err
			}
			src, err := r.Open(osPath(rel))
			if err != nil {
				return nil
			}
			n, err := io.Copy(wr, src)
			src.Close()
			if err != nil {
				return err
			}
			nFiles++
			nBytes += n
			if progress != nil {
				progress(nFiles, nBytes)
			}
			return nil
		})
		if err != nil {
			zw.Close()
			return nFiles, nBytes, err
		}
	}
	return nFiles, nBytes, zw.Close()
}

// Compress creates a zip archive inside the server directory.
func (f *FS) Compress(paths []string, dest string) error {
	if dest == "" {
		return ErrInvalidPath
	}
	out, err := f.Create(dest + ".hearth-tmp")
	if err != nil {
		return err
	}
	skip := func(rel string, isDir bool) bool { return rel == dest || rel == dest+".hearth-tmp" }
	if _, _, err := f.ZipPaths(out, paths, skip, nil); err != nil {
		out.Close()
		f.Remove(dest + ".hearth-tmp")
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	f.Remove(dest)
	return f.Rename(dest+".hearth-tmp", dest)
}

// Extract unpacks a zip, tar or tar.gz archive located inside the server
// directory into destDir. maxBytes limits the total extracted size (0 = no limit).
func (f *FS) Extract(archive, destDir string, maxBytes int64) error {
	fh, st, err := f.OpenRead(archive)
	if err != nil {
		return err
	}
	defer fh.Close()
	r, err := f.open()
	if err != nil {
		return err
	}
	defer r.Close()
	lower := strings.ToLower(archive)
	switch {
	case strings.HasSuffix(lower, ".zip") || strings.HasSuffix(lower, ".mrpack"):
		return extractZip(r, fh, st.Size(), destDir, maxBytes)
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		gz, err := gzip.NewReader(fh)
		if err != nil {
			return err
		}
		defer gz.Close()
		return extractTar(r, gz, destDir, maxBytes)
	case strings.HasSuffix(lower, ".tar"):
		return extractTar(r, fh, destDir, maxBytes)
	}
	return errors.New("Archivformat wird nicht unterstützt (zip, tar, tar.gz)")
}

// ExtractZipFile extracts a zip file from anywhere on disk (backups) into the root.
func (f *FS) ExtractZipFile(zipPath string, maxBytes int64, progress Progress) error {
	fh, err := os.Open(zipPath)
	if err != nil {
		return err
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		return err
	}
	r, err := f.open()
	if err != nil {
		return err
	}
	defer r.Close()
	return extractZipProgress(r, fh, st.Size(), "", maxBytes, progress)
}

func safeJoin(destDir, name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	c := path.Clean("/" + name)
	if c == "/" || strings.Contains(name, "\x00") {
		return "", ErrInvalidPath
	}
	return strings.TrimPrefix(path.Join(destDir, c), "/"), nil
}

func extractZip(r *os.Root, ra io.ReaderAt, size int64, destDir string, maxBytes int64) error {
	return extractZipProgress(r, ra, size, destDir, maxBytes, nil)
}

func extractZipProgress(r *os.Root, ra io.ReaderAt, size int64, destDir string, maxBytes int64, progress Progress) error {
	zr, err := zip.NewReader(ra, size)
	if err != nil {
		return fmt.Errorf("Zip-Archiv kann nicht gelesen werden: %w", err)
	}
	zr.RegisterDecompressor(zip.Deflate, func(in io.Reader) io.ReadCloser { return kflate.NewReader(in) })
	var total, files int64
	for _, zf := range zr.File {
		target, err := safeJoin(destDir, zf.Name)
		if err != nil {
			continue
		}
		mode := zf.Mode()
		if mode&fs.ModeSymlink != 0 {
			continue
		}
		if zf.FileInfo().IsDir() || strings.HasSuffix(zf.Name, "/") {
			if err := r.MkdirAll(osPath(target), 0o755); err != nil {
				return err
			}
			continue
		}
		if dir := path.Dir(target); dir != "." {
			if err := r.MkdirAll(osPath(dir), 0o755); err != nil {
				return err
			}
		}
		src, err := zf.Open()
		if err != nil {
			return err
		}
		perm := mode.Perm()
		if perm == 0 {
			perm = 0o644
		}
		dst, err := r.OpenFile(osPath(target), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm|0o200)
		if err != nil {
			src.Close()
			return err
		}
		var rd io.Reader = src
		if maxBytes > 0 {
			rd = io.LimitReader(src, maxBytes-total+1)
		}
		n, err := io.Copy(dst, rd)
		src.Close()
		dst.Close()
		if err != nil {
			return err
		}
		total += n
		files++
		if maxBytes > 0 && total > maxBytes {
			return errors.New("Archiv überschreitet das Speicherlimit")
		}
		if !zf.Modified.IsZero() {
			r.Chtimes(osPath(target), zf.Modified, zf.Modified)
		}
		if progress != nil {
			progress(files, total)
		}
	}
	return nil
}

func extractTar(r *os.Root, rd io.Reader, destDir string, maxBytes int64) error {
	tr := tar.NewReader(rd)
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeJoin(destDir, h.Name)
		if err != nil {
			continue
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := r.MkdirAll(osPath(target), 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if dir := path.Dir(target); dir != "." {
				if err := r.MkdirAll(osPath(dir), 0o755); err != nil {
					return err
				}
			}
			perm := os.FileMode(h.Mode).Perm()
			if perm == 0 {
				perm = 0o644
			}
			dst, err := r.OpenFile(osPath(target), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm|0o200)
			if err != nil {
				return err
			}
			var src io.Reader = tr
			if maxBytes > 0 {
				src = io.LimitReader(tr, maxBytes-total+1)
			}
			n, err := io.Copy(dst, src)
			dst.Close()
			if err != nil {
				return err
			}
			total += n
			if maxBytes > 0 && total > maxBytes {
				return errors.New("Archiv überschreitet das Speicherlimit")
			}
			r.Chtimes(osPath(target), h.ModTime, h.ModTime)
		default:
			// symlinks, devices etc. are skipped for safety
		}
	}
}

// ArchiveEntries lists the contents of a zip archive (for preview).
func (f *FS) ArchiveEntries(rel string, limit int) ([]Entry, error) {
	fh, st, err := f.OpenRead(rel)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	zr, err := zip.NewReader(fh, st.Size())
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, zf := range zr.File {
		if len(out) >= limit {
			break
		}
		out = append(out, Entry{Name: path.Base(zf.Name), Path: zf.Name, IsDir: zf.FileInfo().IsDir(),
			Size: int64(zf.UncompressedSize64), ModTime: zf.Modified.Unix()})
	}
	return out, nil
}

// MatchIgnore reports whether rel matches one of the gitignore-like patterns:
// "logs/" (directory anywhere), "/cache" (anchored), "*.log" (glob on name).
func MatchIgnore(patterns []string, rel string, isDir bool) bool {
	name := path.Base(rel)
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}
		dirOnly := strings.HasSuffix(p, "/")
		p = strings.TrimSuffix(p, "/")
		if dirOnly && !isDir {
			continue
		}
		if strings.HasPrefix(p, "/") || strings.Contains(p, "/") {
			pat := strings.TrimPrefix(p, "/")
			if ok, _ := path.Match(pat, rel); ok {
				return true
			}
			continue
		}
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}
