package minecraft

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hearth/internal/httpx"
	"hearth/internal/nbt"
)

// fakeJar writes a jar with a fabric.mod.json and returns its sha1.
func fakeJar(t *testing.T, path, id, env string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("fabric.mod.json")
	meta := map[string]any{"schemaVersion": 1, "id": id, "name": id, "version": "1.0"}
	if env != "" {
		meta["environment"] = env
	}
	json.NewEncoder(w).Encode(meta)
	zw.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	s := sha1.Sum(buf.Bytes())
	return hex.EncodeToString(s[:])
}

func TestBuildPack(t *testing.T) {
	mods := t.TempDir()
	apiSum := fakeJar(t, filepath.Join(mods, "fabric-api-0.100.jar"), "fabric-api", "")
	chunkySum := fakeJar(t, filepath.Join(mods, "chunky-1.4.jar"), "chunky", "")
	fakeJar(t, filepath.Join(mods, "server-tool.jar"), "servertool", "server")
	fakeJar(t, filepath.Join(mods, "homemade.jar"), "homemade", "*")
	fakeJar(t, filepath.Join(mods, "old.jar.disabled"), "old", "")

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version := func(id, project, file, sum1 string, deps ...string) map[string]any {
			d := []any{}
			for _, p := range deps {
				d = append(d, map[string]any{"project_id": p, "dependency_type": "required"})
			}
			return map[string]any{"id": id, "project_id": project, "version_number": "1.0", "version_type": "release",
				"game_versions": []string{"1.21.1"}, "loaders": []string{"fabric"}, "dependencies": d,
				"files": []any{map[string]any{"url": "https://cdn.modrinth.com/data/" + project + "/" + file, "filename": file, "primary": true, "size": 1234,
					"hashes": map[string]string{"sha1": sum1, "sha512": strings.Repeat("ab", 64)}}}}
		}
		switch {
		case r.URL.Path == "/v2/version_files":
			var req struct {
				Hashes    []string `json:"hashes"`
				Algorithm string   `json:"algorithm"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			if req.Algorithm != "sha1" {
				t.Errorf("algorithm %q", req.Algorithm)
			}
			json.NewEncoder(w).Encode(map[string]any{
				apiSum:    version("v1", "P_api", "fabric-api-0.100.jar", apiSum),
				chunkySum: version("v2", "P_chunky", "chunky-1.4.jar", chunkySum),
			})
		case r.URL.Path == "/v2/projects":
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": "P_api", "slug": "fabric-api", "title": "Fabric API", "client_side": "required", "server_side": "required"},
				{"id": "P_chunky", "slug": "chunky", "title": "Chunky", "client_side": "unsupported", "server_side": "required"},
			})
		case r.URL.Path == "/v2/project/sodium/version":
			if !strings.Contains(r.URL.Query().Get("loaders"), "fabric") || !strings.Contains(r.URL.Query().Get("game_versions"), "1.21.1") {
				t.Errorf("sodium query %s", r.URL.RawQuery)
			}
			json.NewEncoder(w).Encode([]any{version("v3", "P_sodium", "sodium-0.6.jar", strings.Repeat("1", 40), "P_api", "P_lib")})
		case r.URL.Path == "/v2/project/P_lib/version":
			json.NewEncoder(w).Encode([]any{version("v4", "P_lib", "lib-2.0.jar", strings.Repeat("2", 40))})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := modrinthAPI
	modrinthAPI = srv.URL + "/v2"
	defer func() { modrinthAPI = old }()
	client := httpx.New(nil)
	g := &Game{HTTP: client, Modrinth: &Modrinth{HTTP: client}}

	var out bytes.Buffer
	rep, err := g.BuildPack(t.Context(), PackRequest{
		ModsDir: mods, Name: "Survival", VersionID: "3", Flavor: "fabric", MCVersion: "1.21.1", LoaderVersion: "0.16.9",
		ClientMods: []string{"sodium"}, BundleUnknown: true, ServerName: "Survival", ServerAddress: "mc.example.de",
	}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Mods != 4 || rep.Linked != 2 || rep.Bundled != 2 || rep.ServerOnly != 2 || rep.ClientMods != 2 || len(rep.Warnings) != 0 {
		t.Fatalf("report %+v", rep)
	}

	zr, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string][]byte{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		entries[f.Name] = b
	}
	for _, want := range []string{"modrinth.index.json", "overrides/mods/homemade.jar", "server-overrides/mods/server-tool.jar", "client-overrides/servers.dat"} {
		if _, ok := entries[want]; !ok {
			t.Errorf("missing %s in pack", want)
		}
	}
	if _, ok := entries["overrides/mods/old.jar.disabled"]; ok {
		t.Error("disabled mod was packed")
	}
	var idx packIndex
	if err := json.Unmarshal(entries["modrinth.index.json"], &idx); err != nil {
		t.Fatal(err)
	}
	if idx.FormatVersion != 1 || idx.Game != "minecraft" || idx.VersionID != "3" || idx.Dependencies["minecraft"] != "1.21.1" || idx.Dependencies["fabric-loader"] != "0.16.9" {
		t.Fatalf("index header %+v", idx)
	}
	files := map[string]packFile{}
	for _, f := range idx.Files {
		files[f.Path] = f
		if len(f.Hashes["sha1"]) != 40 || len(f.Hashes["sha512"]) != 128 || len(f.Downloads) != 1 {
			t.Errorf("%s: hashes/downloads %+v", f.Path, f)
		}
	}
	if len(files) != 4 {
		t.Fatalf("files %v", files)
	}
	if f := files["mods/chunky-1.4.jar"]; f.Env == nil || f.Env.Client != "unsupported" || f.Env.Server != "required" {
		t.Errorf("chunky env %+v", f.Env)
	}
	if f := files["mods/fabric-api-0.100.jar"]; f.Env == nil || f.Env.Client != "required" {
		t.Errorf("fabric-api env %+v", f.Env)
	}
	for _, p := range []string{"mods/sodium-0.6.jar", "mods/lib-2.0.jar"} {
		if f := files[p]; f.Env == nil || f.Env.Client != "required" || f.Env.Server != "unsupported" {
			t.Errorf("%s env %+v", p, f.Env)
		}
	}
	// the local file hashes are used for server mods
	h := sha512.Sum512(func() []byte { b, _ := os.ReadFile(filepath.Join(mods, "chunky-1.4.jar")); return b }())
	if files["mods/chunky-1.4.jar"].Hashes["sha512"] != hex.EncodeToString(h[:]) {
		t.Error("sha512 of server mod does not match the file")
	}

	dat, err := nbt.Decode(entries["client-overrides/servers.dat"], nil)
	if err != nil {
		t.Fatal(err)
	}
	list := nbt.List(nbt.Get(dat, "servers"))
	if len(list) != 1 || nbt.String(nbt.Comp(list[0])["ip"]) != "mc.example.de" || nbt.String(nbt.Comp(list[0])["name"]) != "Survival" {
		t.Fatalf("servers.dat %v", dat)
	}

	// without bundling, unknown mods are reported instead of packed
	out.Reset()
	rep, err = g.BuildPack(t.Context(), PackRequest{ModsDir: mods, Name: "S", VersionID: "4", Flavor: "fabric", MCVersion: "1.21.1", LoaderVersion: "0.16.9"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Bundled != 0 || len(rep.Skipped) != 2 || rep.Mods != 2 {
		t.Fatalf("report without bundling %+v", rep)
	}
}

func TestLoaderVersion(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"libraries/net/fabricmc/fabric-loader/0.15.11", "libraries/net/fabricmc/fabric-loader/0.16.9",
		"libraries/net/minecraftforge/forge/1.20.1-47.3.0", "libraries/net/minecraftforge/forge/1.20.1-47.2.20", "libraries/net/minecraftforge/forge/1.19.2-43.3.0",
		"libraries/net/neoforged/neoforge/21.1.77"} {
		os.MkdirAll(filepath.Join(dir, filepath.FromSlash(p)), 0o755)
	}
	if v := LoaderVersion(dir, "fabric", "1.21.1"); v != "0.16.9" {
		t.Errorf("fabric %q", v)
	}
	if v := LoaderVersion(dir, "forge", "1.20.1"); v != "47.3.0" {
		t.Errorf("forge %q", v)
	}
	if v := LoaderVersion(dir, "neoforge", "1.21.1"); v != "21.1.77" {
		t.Errorf("neoforge %q", v)
	}
}
