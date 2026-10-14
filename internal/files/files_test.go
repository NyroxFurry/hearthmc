package files

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestClean(t *testing.T) {
	cases := map[string]string{
		"":               "",
		"/":              "",
		"world/region":   "world/region",
		"/world/../x":    "x",
		"../../etc":      "etc",
		"a\\b":           "a/b",
		"./plugins/./x/": "plugins/x",
	}
	for in, want := range cases {
		got, err := Clean(in)
		if err != nil || got != want {
			t.Errorf("Clean(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := Clean("a\x00b"); err == nil {
		t.Error("NUL byte accepted")
	}
}

func TestMatchIgnore(t *testing.T) {
	pats := []string{"logs/", "*.log", "/cache", "plugins/dynmap/web/tiles/"}
	cases := []struct {
		rel   string
		dir   bool
		match bool
	}{
		{"logs", true, true},
		{"world/logs", true, true},
		{"logs", false, false},
		{"latest.log", false, true},
		{"cache", true, true},
		{"world/cache", true, false},
		{"plugins/dynmap/web/tiles", true, true},
		{"server.properties", false, false},
	}
	for _, c := range cases {
		if got := MatchIgnore(pats, c.rel, c.dir); got != c.match {
			t.Errorf("MatchIgnore(%q, dir=%v) = %v", c.rel, c.dir, got)
		}
	}
}

func TestSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("geheim"), 0o644)
	os.Symlink(outside, filepath.Join(root, "link"))
	fs := New(root)
	if _, err := fs.ReadText("link/secret.txt", 1<<20); err == nil {
		t.Error("read through symlink escaped the root")
	}
	if err := fs.WriteFile("link/new.txt", []byte("x")); err == nil {
		t.Error("write through symlink escaped the root")
	}
}

func TestZipRoundTrip(t *testing.T) {
	root := t.TempDir()
	fs := New(root)
	fs.WriteFile("world/region/r.0.0.mca", bytes.Repeat([]byte{1, 2, 3}, 1000))
	fs.WriteFile("server.properties", []byte("motd=Hi\n"))
	fs.WriteFile("logs/latest.log", []byte("log"))
	var buf bytes.Buffer
	skip := func(rel string, dir bool) bool { return MatchIgnore([]string{"logs/"}, rel, dir) }
	n, _, err := fs.ZipPaths(&buf, nil, skip, nil)
	if err != nil || n != 2 {
		t.Fatalf("zip: n=%d err=%v", n, err)
	}
	zipPath := filepath.Join(t.TempDir(), "b.zip")
	os.WriteFile(zipPath, buf.Bytes(), 0o644)
	dest := New(t.TempDir())
	if err := dest.ExtractZipFile(zipPath, 0, nil); err != nil {
		t.Fatal(err)
	}
	if b, err := dest.ReadFile("server.properties"); err != nil || string(b) != "motd=Hi\n" {
		t.Errorf("extracted content = %q, %v", b, err)
	}
	if dest.Exists("logs/latest.log") {
		t.Error("ignored file was archived")
	}
}
