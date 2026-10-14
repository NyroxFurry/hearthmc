// Package javart manages Java runtimes for native game servers. Runtimes are
// downloaded on demand from Eclipse Adoptium (Temurin) and stored in the
// panel's data directory, so no system-wide Java installation is required.
package javart

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"hearth/internal/httpx"
)

type Manager struct {
	Dir  string
	HTTP *httpx.Client

	mu    sync.Mutex
	locks map[int]*sync.Mutex
}

type Runtime struct {
	Major   int    `json:"major"`
	Path    string `json:"path"`
	Version string `json:"version"`
	Size    int64  `json:"size"`
	Source  string `json:"source"` // managed | system
}

func javaExe() string {
	if goruntime.GOOS == "windows" {
		return "java.exe"
	}
	return "java"
}

func (m *Manager) lock(major int) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locks == nil {
		m.locks = map[int]*sync.Mutex{}
	}
	l, ok := m.locks[major]
	if !ok {
		l = &sync.Mutex{}
		m.locks[major] = l
	}
	return l
}

// findJava locates bin/java inside an extracted runtime directory.
func findJava(dir string) string {
	candidates := []string{
		filepath.Join(dir, "bin", javaExe()),
		filepath.Join(dir, "Contents", "Home", "bin", javaExe()),
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

func readRelease(dir string) string {
	for _, p := range []string{filepath.Join(dir, "release"), filepath.Join(dir, "Contents", "Home", "release")} {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if v, ok := strings.CutPrefix(sc.Text(), "JAVA_VERSION="); ok {
				f.Close()
				return strings.Trim(v, `"`)
			}
		}
		f.Close()
	}
	return ""
}

// List returns all installed managed runtimes.
func (m *Manager) List() []Runtime {
	ents, _ := os.ReadDir(m.Dir)
	var out []Runtime
	for _, e := range ents {
		major, err := strconv.Atoi(e.Name())
		if err != nil || !e.IsDir() {
			continue
		}
		dir := filepath.Join(m.Dir, e.Name())
		java := findJava(dir)
		if java == "" {
			continue
		}
		var size int64
		filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if i, err := d.Info(); err == nil {
					size += i.Size()
				}
			}
			return nil
		})
		out = append(out, Runtime{Major: major, Path: java, Version: readRelease(dir), Size: size, Source: "managed"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Major < out[j].Major })
	return out
}

// Installed returns the java executable for a managed runtime, or "".
func (m *Manager) Installed(major int) string {
	return findJava(filepath.Join(m.Dir, strconv.Itoa(major)))
}

// System returns the java on PATH (if any) and its major version.
func System() (string, int) {
	p, err := exec.LookPath("java")
	if err != nil {
		return "", 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, p, "-version").CombinedOutput()
	return p, ParseVersionOutput(string(out))
}

// ParseVersionOutput extracts the major version from `java -version` output.
func ParseVersionOutput(s string) int {
	i := strings.Index(s, "version \"")
	if i < 0 {
		return 0
	}
	v := s[i+9:]
	if j := strings.IndexByte(v, '"'); j >= 0 {
		v = v[:j]
	}
	parts := strings.Split(v, ".")
	if parts[0] == "1" && len(parts) > 1 { // 1.8.0_402
		n, _ := strconv.Atoi(parts[1])
		return n
	}
	n, _ := strconv.Atoi(strings.SplitN(parts[0], "-", 2)[0])
	return n
}

func adoptiumOS() string {
	switch goruntime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "mac"
	case "linux":
		if m, _ := filepath.Glob("/lib/ld-musl-*.so.1"); len(m) > 0 {
			return "alpine-linux"
		}
		return "linux"
	}
	return goruntime.GOOS
}

func adoptiumArch() string {
	switch goruntime.GOARCH {
	case "amd64":
		return "x64"
	case "arm64":
		return "aarch64"
	case "386":
		return "x32"
	}
	return goruntime.GOARCH
}

type adoptiumAsset struct {
	Binary struct {
		ImageType string `json:"image_type"`
		Package   struct {
			Checksum string `json:"checksum"`
			Link     string `json:"link"`
			Name     string `json:"name"`
			Size     int64  `json:"size"`
		} `json:"package"`
	} `json:"binary"`
	ReleaseName string `json:"release_name"`
}

// Ensure returns the java executable for the given major version,
// downloading it from Adoptium if necessary.
func (m *Manager) Ensure(ctx context.Context, major int, log func(string)) (string, error) {
	if p := m.Installed(major); p != "" {
		return p, nil
	}
	l := m.lock(major)
	l.Lock()
	defer l.Unlock()
	if p := m.Installed(major); p != "" {
		return p, nil
	}
	if log == nil {
		log = func(string) {}
	}
	var asset *adoptiumAsset
	for _, image := range []string{"jre", "jdk"} {
		url := fmt.Sprintf("https://api.adoptium.net/v3/assets/latest/%d/hotspot?architecture=%s&image_type=%s&os=%s&vendor=eclipse",
			major, adoptiumArch(), image, adoptiumOS())
		var list []adoptiumAsset
		if err := m.HTTP.GetJSON(ctx, url, time.Hour, &list); err != nil {
			if image == "jdk" {
				return "", fmt.Errorf("Java %d konnte nicht gefunden werden: %w", major, err)
			}
			continue
		}
		if len(list) > 0 {
			asset = &list[0]
			break
		}
	}
	if asset == nil {
		return "", fmt.Errorf("Für Java %d gibt es keinen Download für %s/%s", major, adoptiumOS(), adoptiumArch())
	}
	pkg := asset.Binary.Package
	log(fmt.Sprintf("Lade Java %d herunter (%s, %.0f MB) …", major, asset.ReleaseName, float64(pkg.Size)/1e6))
	if err := os.MkdirAll(m.Dir, 0o755); err != nil {
		return "", err
	}
	archive := filepath.Join(m.Dir, ".download-"+pkg.Name)
	defer os.Remove(archive)
	lastPct := -1
	err := m.HTTP.Download(ctx, pkg.Link, archive, &httpx.Checksum{Algo: "sha256", Hex: pkg.Checksum}, func(done, total int64) {
		if total > 0 {
			pct := int(done * 100 / total)
			if pct/20 != lastPct/20 {
				lastPct = pct
				log(fmt.Sprintf("Java %d: %d%%", major, pct))
			}
		}
	})
	if err != nil {
		return "", fmt.Errorf("Download von Java %d fehlgeschlagen: %w", major, err)
	}
	log(fmt.Sprintf("Entpacke Java %d …", major))
	tmp := filepath.Join(m.Dir, fmt.Sprintf(".extract-%d", major))
	os.RemoveAll(tmp)
	if strings.HasSuffix(pkg.Name, ".zip") {
		err = extractZip(archive, tmp)
	} else {
		err = extractTarGz(archive, tmp)
	}
	if err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("Java %d konnte nicht entpackt werden: %w", major, err)
	}
	final := filepath.Join(m.Dir, strconv.Itoa(major))
	os.RemoveAll(final)
	if err := os.Rename(tmp, final); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	p := findJava(final)
	if p == "" {
		return "", fmt.Errorf("Java %d: ausführbare Datei nicht gefunden", major)
	}
	log(fmt.Sprintf("Java %d installiert (%s).", major, readRelease(final)))
	return p, nil
}

func (m *Manager) Remove(major int) error {
	l := m.lock(major)
	l.Lock()
	defer l.Unlock()
	dir := filepath.Join(m.Dir, strconv.Itoa(major))
	if _, err := os.Stat(dir); err != nil {
		return errors.New("Java-Version ist nicht installiert")
	}
	return os.RemoveAll(dir)
}

// stripFirst removes the top-level directory from an archive path.
func stripFirst(name string) string {
	name = strings.TrimPrefix(path.Clean("/"+strings.ReplaceAll(name, "\\", "/")), "/")
	if i := strings.IndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return ""
}

func extractTarGz(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		rel := stripFirst(h.Name)
		if rel == "" {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(target), 0o755)
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode).Perm()|0o600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		case tar.TypeSymlink:
			// only allow links that stay inside the runtime directory
			resolved := path.Clean(path.Join(path.Dir(rel), h.Linkname))
			if path.IsAbs(h.Linkname) || strings.HasPrefix(resolved, "..") {
				continue
			}
			os.MkdirAll(filepath.Dir(target), 0o755)
			os.Symlink(h.Linkname, target)
		}
	}
}

func extractZip(archive, dest string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		rel := stripFirst(zf.Name)
		if rel == "" {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if zf.FileInfo().IsDir() {
			os.MkdirAll(target, 0o755)
			continue
		}
		os.MkdirAll(filepath.Dir(target), 0o755)
		src, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, zf.Mode().Perm()|0o600)
		if err != nil {
			src.Close()
			return err
		}
		_, err = io.Copy(out, src)
		src.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
