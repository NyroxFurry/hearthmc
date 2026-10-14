package minecraft

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetYAMLKeys(t *testing.T) {
	in := `# squaremap config
config-version: 2
settings:
  language-file: lang-en.yml
  internal-webserver:
    # comment
    bind: 0.0.0.0
    port: 8080
    enabled: true
  web-directory:
    path: web
world-settings:
  default:
    map:
      enabled: true
`
	out, err := setYAMLKeys(in, []string{"settings", "internal-webserver"}, [][2]string{{"bind", "127.0.0.1"}, {"port", "41005"}, {"enabled", "true"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"    bind: 127.0.0.1\n", "    port: 41005\n", "  web-directory:\n    path: web\n", "world-settings:\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "8080") {
		t.Errorf("old port still present:\n%s", out)
	}
	// missing blocks are created
	out2, _ := setYAMLKeys("config-version: 2\n", []string{"settings", "internal-webserver"}, [][2]string{{"port", "41001"}})
	if !strings.Contains(out2, "settings:\n  internal-webserver:\n    port: 41001\n") {
		t.Errorf("block not created:\n%s", out2)
	}
	// existing settings block without the sub block
	out3, _ := setYAMLKeys("settings:\n  language-file: x\nother: 1\n", []string{"settings", "internal-webserver"}, [][2]string{{"port", "41002"}})
	if !strings.Contains(out3, "settings:\n  language-file: x\n  internal-webserver:\n    port: 41002\nother: 1\n") {
		t.Errorf("sub block not inserted correctly:\n%s", out3)
	}
}

func TestConfigureBlueMap(t *testing.T) {
	dir := t.TempDir()
	if err := ConfigureMapPlugin(dir, "paper", "bluemap", 41010, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	core, _ := os.ReadFile(filepath.Join(dir, "plugins", "BlueMap", "core.conf"))
	if !strings.Contains(string(core), "accept-download: true") {
		t.Errorf("core.conf: %s", core)
	}
	web, _ := os.ReadFile(filepath.Join(dir, "plugins", "BlueMap", "webserver.conf"))
	for _, want := range []string{"port: 41010", `ip: "127.0.0.1"`, "enabled: true"} {
		if !strings.Contains(string(web), want) {
			t.Errorf("webserver.conf misses %q:\n%s", want, web)
		}
	}
	// the template written by BlueMap itself is patched in place
	tmpl := "# Default is true\nenabled: true\nwebroot: \"bluemap/web\"\n# Default is 8100\nport: 8100\nlog: {\n  file: \"bluemap/logs/webserver.log\"\n  append: false\n}\n"
	os.WriteFile(filepath.Join(dir, "plugins", "BlueMap", "webserver.conf"), []byte(tmpl), 0o644)
	os.WriteFile(filepath.Join(dir, "plugins", "BlueMap", "core.conf"), []byte("accept-download: false\nrender-thread-count: 2\n"), 0o644)
	if err := ConfigureMapPlugin(dir, "purpur", "bluemap", 41011, "0.0.0.0"); err != nil {
		t.Fatal(err)
	}
	web, _ = os.ReadFile(filepath.Join(dir, "plugins", "BlueMap", "webserver.conf"))
	if !strings.Contains(string(web), "port: 41011") || strings.Contains(string(web), "8100\nlog") || !strings.Contains(string(web), `ip: "0.0.0.0"`) || !strings.Contains(string(web), "append: false") {
		t.Errorf("patched webserver.conf:\n%s", web)
	}
	core, _ = os.ReadFile(filepath.Join(dir, "plugins", "BlueMap", "core.conf"))
	if string(core) != "accept-download: true\nrender-thread-count: 2\n" {
		t.Errorf("patched core.conf:\n%s", core)
	}
	// mods use config/bluemap
	if err := ConfigureMapPlugin(dir, "fabric", "bluemap", 41012, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config", "bluemap", "webserver.conf")); err != nil {
		t.Error(err)
	}
}

func TestConfigureSquaremap(t *testing.T) {
	dir := t.TempDir()
	if err := ConfigureMapPlugin(dir, "paper", "squaremap", 41020, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "plugins", "squaremap", "config.yml"))
	if !strings.Contains(string(b), "  internal-webserver:\n    enabled: true\n    bind: 127.0.0.1\n    port: 41020\n") {
		t.Errorf("config.yml:\n%s", b)
	}
	if err := ConfigureMapPlugin(dir, "fabric", "squaremap", 41021, "0.0.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "squaremap", "config.yml")); err != nil {
		t.Error(err)
	}
}

func writeJar(t *testing.T, path string, files map[string]string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	f.Close()
}

func TestConfigureDynmapFromJar(t *testing.T) {
	dir := t.TempDir()
	tmpl := "deftemplatesuffix: hires\ncomponents:\n  - class: org.dynmap.InternalClientUpdateComponent\n    allowwebchat: true\n  #  allowwebchat: true\n#webserver-bindaddress: 0.0.0.0\n\n# The TCP-port the webserver will listen on.\nwebserver-port: 8123\ndisable-webserver: false\n"
	writeJar(t, filepath.Join(dir, "plugins", "Dynmap-3.7-beta-6-spigot.jar"), map[string]string{
		"plugin.yml":        "name: dynmap\nversion: 3.7\n",
		"configuration.txt": tmpl,
	})
	if f, enabled := FindMapPluginJar(dir, "paper", "dynmap"); f == "" || !enabled {
		t.Fatalf("jar not found: %q %v", f, enabled)
	}
	if err := ConfigureMapPlugin(dir, "paper", "dynmap", 41030, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "plugins", "dynmap", "configuration.txt"))
	text := string(b)
	for _, want := range []string{"webserver-bindaddress: 127.0.0.1", "webserver-port: 41030", "    allowwebchat: false", "  #  allowwebchat: true", "deftemplatesuffix: hires"} {
		if !strings.Contains(text, want) {
			t.Errorf("configuration.txt misses %q:\n%s", want, text)
		}
	}
}

func TestFindMapPluginJarIgnoresAddons(t *testing.T) {
	dir := t.TempDir()
	writeJar(t, filepath.Join(dir, "plugins", "BlueMap-Towny-1.2.jar"), map[string]string{"plugin.yml": "name: BlueMapTowny\nversion: 1\n"})
	if f, _ := FindMapPluginJar(dir, "paper", "bluemap"); f != "" {
		t.Errorf("addon mistaken for BlueMap: %s", f)
	}
	writeJar(t, filepath.Join(dir, "plugins", "bluemap-5.7-paper.jar"), map[string]string{"plugin.yml": "name: BlueMap\nversion: 5.7\n"})
	if f, en := FindMapPluginJar(dir, "paper", "bluemap"); f != "bluemap-5.7-paper.jar" || !en {
		t.Errorf("got %q %v", f, en)
	}
	writeJar(t, filepath.Join(dir, "mods", "squaremap-fabric-mc1.21.4-1.3.4.jar"), map[string]string{"fabric.mod.json": `{"id":"squaremap","name":"squaremap","version":"1.3.4"}`})
	if f, _ := FindMapPluginJar(dir, "fabric", "squaremap"); f != "squaremap-fabric-mc1.21.4-1.3.4.jar" {
		t.Errorf("fabric jar: %q", f)
	}
}

func TestBestFile(t *testing.T) {
	v := &MRVersion{Files: []MRFile{
		{Filename: "squaremap-fabric-mc1.21.4-1.3.4.jar", Primary: true},
		{Filename: "squaremap-paper-mc1.21.4-1.3.4.jar"},
	}}
	if f := BestFile(v, ModrinthLoaders("paper")); f.Filename != "squaremap-paper-mc1.21.4-1.3.4.jar" {
		t.Errorf("paper: %s", f.Filename)
	}
	if f := BestFile(v, ModrinthLoaders("fabric")); f.Filename != "squaremap-fabric-mc1.21.4-1.3.4.jar" {
		t.Errorf("fabric: %s", f.Filename)
	}
	one := &MRVersion{Files: []MRFile{{Filename: "BlueMap-5.7-paper.jar", Primary: true}}}
	if f := BestFile(one, ModrinthLoaders("purpur")); f.Filename != "BlueMap-5.7-paper.jar" {
		t.Errorf("single: %s", f.Filename)
	}
}

func TestSameVersionLine(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"1.21.4", "1.21.10", true}, {"1.21", "1.21.1", true}, {"1.20.6", "1.21", false}, {"26.1", "26.1.2", true}, {"26.1", "26.2", false}, {"", "1.21", false}} {
		if got := sameVersionLine(c.a, c.b); got != c.want {
			t.Errorf("%s/%s = %v", c.a, c.b, got)
		}
	}
}
