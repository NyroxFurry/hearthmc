// Command licenses collects the license texts of everything that ends up in a
// Hearth build: the Go standard library and the modules compiled into the
// binaries of all release platforms, the npm packages bundled into the web UI,
// and the fonts.
//
//	go run ./tools/licenses
//
// writes THIRD_PARTY_LICENSES.txt in the repository root. The web build
// (web/vite.config.ts) copies it to /licenses/THIRD_PARTY_LICENSES.txt, so the
// panel serves it and it is embedded in every binary.
//
// The command fails when a dependency has no license text or uses a license
// that is not on the allowlist below, so new dependencies get a second look.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Release platforms, kept in sync with TARGETS in the Makefile. Some modules
// are only compiled in on one platform (e.g. go-ole on Windows).
var targets = []string{"linux/amd64", "linux/arm64", "windows/amd64", "darwin/arm64", "darwin/amd64"}

// Licenses accepted for bundled npm packages without further review.
var npmAllowed = map[string]bool{
	"MIT": true, "ISC": true, "BSD-2-Clause": true, "BSD-3-Clause": true,
	"Apache-2.0": true, "0BSD": true, "MIT-0": true, "BlueOak-1.0.0": true,
}

type entry struct {
	name    string
	license string // SPDX id if known
	texts   []string
}

var licenseFiles = []string{"LICENSE", "LICENSE.txt", "LICENSE.md", "LICENCE", "LICENCE.txt", "LICENCE.md", "COPYING", "COPYING.txt", "NOTICE", "NOTICE.txt", "NOTICE.md"}

func readLicenses(dir string) []string {
	var out []string
	seen := map[string]bool{}
	ents, _ := os.ReadDir(dir)
	for _, want := range licenseFiles {
		for _, e := range ents {
			if e.IsDir() || !strings.EqualFold(e.Name(), want) || seen[strings.ToLower(e.Name())] {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err == nil && len(bytes.TrimSpace(b)) > 0 {
				seen[strings.ToLower(e.Name())] = true
				out = append(out, strings.TrimSpace(strings.ReplaceAll(string(b), "\r\n", "\n")))
			}
		}
	}
	return out
}

const mitText = `Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.`

func fail(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "licenses:", err)
		os.Exit(1)
	}
}

func goEnv(key string) string {
	out, err := exec.Command("go", "env", key).Output()
	fail(err)
	return strings.TrimSpace(string(out))
}

func goEntries() []entry {
	out := []entry{{name: "Go standard library (" + goEnv("GOVERSION") + ")", license: "BSD-3-Clause",
		texts: readLicenses(goEnv("GOROOT"))}}
	if len(out[0].texts) == 0 {
		fail(fmt.Errorf("no LICENSE found in GOROOT"))
	}
	type mod struct{ name, dir string }
	mods := map[string]mod{}
	for _, t := range targets {
		goos, goarch, _ := strings.Cut(t, "/")
		cmd := exec.Command("go", "list", "-deps", "-json=Module", "./cmd/hearth")
		cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
		cmd.Stderr = os.Stderr
		raw, err := cmd.Output()
		fail(err)
		dec := json.NewDecoder(bytes.NewReader(raw))
		for dec.More() {
			var p struct {
				Module *struct {
					Path, Version, Dir string
					Main               bool
					Replace            *struct{ Path, Version, Dir string }
				}
			}
			fail(dec.Decode(&p))
			m := p.Module
			if m == nil || m.Main {
				continue
			}
			version, dir := m.Version, m.Dir
			if r := m.Replace; r != nil {
				// The code that is compiled in comes from the replacement
				// (an official mirror of the same module).
				version, dir = r.Version, r.Dir
			}
			mods[m.Path] = mod{m.Path + " " + version, dir}
		}
	}
	keys := make([]string, 0, len(mods))
	for k := range mods {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		texts := readLicenses(mods[k].dir)
		if len(texts) == 0 {
			fail(fmt.Errorf("no license file found for Go module %s", mods[k].name))
		}
		out = append(out, entry{name: mods[k].name, texts: texts})
	}
	return out
}

func npmEntries(webDir string) []entry {
	cmd := exec.Command("npm", "ls", "--omit=dev", "--all", "--parseable")
	cmd.Dir = webDir
	raw, err := cmd.Output()
	if err != nil && len(raw) == 0 {
		fail(fmt.Errorf("npm ls: %w (run npm ci in web/ first)", err))
	}
	var out []entry
	seen := map[string]bool{}
	for _, dir := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		dir = strings.TrimSpace(dir)
		if dir == "" || filepath.Clean(dir) == filepath.Clean(webDir) {
			continue
		}
		var pkg struct {
			Name    string          `json:"name"`
			Version string          `json:"version"`
			License json.RawMessage `json:"license"`
			Author  json.RawMessage `json:"author"`
		}
		b, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err != nil || json.Unmarshal(b, &pkg) != nil || pkg.Name == "" {
			continue
		}
		id := pkg.Name + " " + pkg.Version
		if seen[id] {
			continue
		}
		seen[id] = true
		lic := nameField(pkg.License, "type")
		if !npmAllowed[lic] {
			fail(fmt.Errorf("npm package %s uses license %q, which is not on the allowlist in tools/licenses", id, lic))
		}
		texts := readLicenses(dir)
		if len(texts) == 0 {
			author := nameField(pkg.Author, "name")
			if lic != "MIT" || author == "" {
				fail(fmt.Errorf("npm package %s ships no license file", id))
			}
			// The package declares MIT but ships no LICENSE file. Reproduce the
			// MIT terms with the author from package.json as copyright holder.
			texts = []string{"MIT License (declared in package.json; the package ships no license file)\n\nCopyright (c) " + author + "\n\n" + mitText}
		}
		out = append(out, entry{name: id, license: lic, texts: texts})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// nameField reads a package.json field that is either a string or an object
// with the given key ("license": {"type": …}, "author": {"name": …}). An author
// string like "Jane Doe <jane@example.com> (https://…)" is cut to the name.
func nameField(raw json.RawMessage, key string) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		var m map[string]any
		if json.Unmarshal(raw, &m) == nil {
			s, _ = m[key].(string)
		}
	}
	if i := strings.IndexAny(s, "<("); i > 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func fontEntries(fontDir string) []entry {
	var out []entry
	for _, f := range []struct{ name, file string }{{"Manrope (font)", "LICENSE-Manrope.txt"}, {"Unbounded (font)", "LICENSE-Unbounded.txt"}} {
		b, err := os.ReadFile(filepath.Join(fontDir, f.file))
		fail(err)
		out = append(out, entry{name: f.name, license: "OFL-1.1", texts: []string{strings.TrimSpace(string(b))}})
	}
	return out
}

func main() {
	root, err := os.Getwd()
	fail(err)
	if _, err := os.Stat(filepath.Join(root, "cmd", "hearth")); err != nil {
		fail(fmt.Errorf("run this from the repository root"))
	}
	var sb strings.Builder
	sb.WriteString("Hearth includes the following third-party software and fonts.\n")
	sb.WriteString("Their licenses and copyright notices are reproduced below.\n")
	sb.WriteString("This file is generated by `go run ./tools/licenses` (make licenses).\n")
	section := func(title string, list []entry) {
		sb.WriteString("\n\n" + strings.Repeat("=", 78) + "\n" + title + "\n" + strings.Repeat("=", 78) + "\n")
		for _, e := range list {
			sb.WriteString("\n" + strings.Repeat("-", 78) + "\n" + e.name)
			if e.license != "" {
				sb.WriteString("  [" + e.license + "]")
			}
			sb.WriteString("\n" + strings.Repeat("-", 78) + "\n\n")
			sb.WriteString(strings.Join(e.texts, "\n\n"))
			sb.WriteString("\n")
		}
	}
	section("Go (compiled into the hearth binary)", goEntries())
	section("JavaScript (bundled into the web interface)", npmEntries(filepath.Join(root, "web")))
	section("Fonts (embedded in the web interface)", fontEntries(filepath.Join(root, "web", "src", "assets", "fonts")))
	text := []byte(sb.String())
	fail(os.WriteFile(filepath.Join(root, "THIRD_PARTY_LICENSES.txt"), text, 0o644))
	fmt.Printf("THIRD_PARTY_LICENSES.txt: %d KB\n", len(text)/1024)
}
