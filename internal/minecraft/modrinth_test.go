package minecraft

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hearth/internal/httpx"
)

// fake Modrinth: BlueMap without a release for 1.21.10, but one for 1.21.4;
// a Fabric build that requires fabric-api.
func fakeModrinth(t *testing.T) (*httptest.Server, map[string]int) {
	jar := []byte("PK fake jar")
	sum := sha512.Sum512(jar)
	hits := map[string]int{}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits[r.URL.Path]++
		file := func(name string) map[string]any {
			return map[string]any{"url": srv.URL + "/dl/" + name, "filename": name, "primary": true, "hashes": map[string]string{"sha512": hex.EncodeToString(sum[:])}}
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/dl/"):
			w.Write(jar)
		case r.URL.Path == "/v2/project/bluemap/version":
			gv := r.URL.Query().Get("game_versions")
			loaders := r.URL.Query().Get("loaders")
			if strings.Contains(gv, "1.21.10") {
				w.Write([]byte("[]"))
				return
			}
			out := []map[string]any{}
			if strings.Contains(loaders, "fabric") {
				out = append(out, map[string]any{"id": "bmfab", "project_id": "bluemapid", "version_number": "5.7-fabric", "version_type": "release",
					"game_versions": []string{"1.21.4"}, "loaders": []string{"fabric"}, "files": []any{file("bluemap-5.7-fabric.jar")},
					"dependencies": []any{map[string]any{"project_id": "fabricapi", "dependency_type": "required"}}})
			} else {
				out = append(out,
					map[string]any{"id": "bmbeta", "project_id": "bluemapid", "version_number": "5.8-beta", "version_type": "beta", "game_versions": []string{"1.21.10"}, "files": []any{file("bluemap-5.8-beta.jar")}},
					map[string]any{"id": "bm57", "project_id": "bluemapid", "version_number": "5.7", "version_type": "release", "game_versions": []string{"1.21.3", "1.21.4"}, "files": []any{file("bluemap-5.7-paper.jar")}},
					map[string]any{"id": "bm56", "project_id": "bluemapid", "version_number": "5.6", "version_type": "release", "game_versions": []string{"1.20.6"}, "files": []any{file("bluemap-5.6-paper.jar")}},
				)
			}
			json.NewEncoder(w).Encode(out)
		case r.URL.Path == "/v2/version_files":
			// fabric-api is already installed (by hash)
			json.NewEncoder(w).Encode(map[string]any{"x": map[string]any{"id": "fa1", "project_id": "fabricapi"}})
		default:
			http.NotFound(w, r)
		}
	}))
	return srv, hits
}

func TestInstallProjectFallbackAndDependencies(t *testing.T) {
	srv, hits := fakeModrinth(t)
	defer srv.Close()
	old := modrinthAPI
	modrinthAPI = srv.URL + "/v2"
	defer func() { modrinthAPI = old }()
	client := httpx.New(nil)
	g := &Game{HTTP: client, Modrinth: &Modrinth{HTTP: client}}

	dir := t.TempDir()
	var log []string
	files, err := g.InstallProject(t.Context(), InstallRequest{Dir: dir, Project: "bluemap", Loaders: ModrinthLoaders("paper"), GameVersion: "1.21.10",
		Dependencies: true, Log: func(s string) { log = append(log, s) }})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "bluemap-5.7-paper.jar" {
		t.Fatalf("files %v", files)
	}
	if _, err := os.Stat(filepath.Join(dir, "bluemap-5.7-paper.jar")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(log, "\n"), "nehme 5.7") {
		t.Errorf("no fallback note: %v", log)
	}

	// Fabric: the required fabric-api is already there and must not be downloaded twice
	mods := t.TempDir()
	os.WriteFile(filepath.Join(mods, "fabric-api-0.100.jar"), []byte("PK old api"), 0o644)
	files, err = g.InstallProject(t.Context(), InstallRequest{Dir: mods, Project: "bluemap", Loaders: ModrinthLoaders("fabric"), GameVersion: "1.21.4", Dependencies: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "bluemap-5.7-fabric.jar" {
		t.Fatalf("fabric files %v", files)
	}
	if hits["/v2/project/fabricapi/version"] > 0 {
		t.Errorf("fabric-api was looked up although installed")
	}
}
