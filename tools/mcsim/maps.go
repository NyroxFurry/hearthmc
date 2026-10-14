package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// World saves: with MCSIM_REGIONS=<dir> the first save copies real region
// files (r.X.Z.mca, optional DIM-1/ and DIM1/ sub folders) into the world,
// like a fresh server writes its spawn area on the first save.

func (s *sim) writeRegions() {
	src := os.Getenv("MCSIM_REGIONS")
	if src == "" {
		return
	}
	dst := filepath.Join(s.world, "region")
	if s.modern {
		dst = filepath.Join(s.world, "dimensions", "minecraft", "overworld", "region")
	}
	if _, err := os.Stat(dst); err == nil {
		return
	}
	copyDir := func(from, to string) {
		ents, err := os.ReadDir(from)
		if err != nil {
			return
		}
		os.MkdirAll(to, 0o755)
		for _, e := range ents {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".mca") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(from, e.Name()))
			if err == nil {
				os.WriteFile(filepath.Join(to, e.Name()), b, 0o644)
			}
		}
	}
	copyDir(src, dst)
	nether := filepath.Join(s.world, "DIM-1", "region")
	end := filepath.Join(s.world, "DIM1", "region")
	if s.style != "vanilla" { // Paper keeps the other dimensions in their own worlds
		nether = filepath.Join(s.world+"_nether", "DIM-1", "region")
		end = filepath.Join(s.world+"_the_end", "DIM1", "region")
	}
	copyDir(filepath.Join(src, "DIM-1"), nether)
	copyDir(filepath.Join(src, "DIM1"), end)
}

// Map plugins: when the jar of BlueMap, squaremap or Dynmap lies in plugins/
// and its configuration names a web server port, a small stand-in web map
// is served there (relative URLs and live updates, like the real ones).

var (
	reHoconPort = regexp.MustCompile(`(?m)^port\s*[:=]\s*(\d+)`)
	reHoconIP   = regexp.MustCompile(`(?m)^ip\s*[:=]\s*"?([^"\s]+)"?`)
	reYAMLPort  = regexp.MustCompile(`(?m)^\s+port:\s*(\d+)`)
	reYAMLBind  = regexp.MustCompile(`(?m)^\s+bind:\s*(\S+)`)
	reDynPort   = regexp.MustCompile(`(?m)^webserver-port:\s*(\d+)`)
	reDynBind   = regexp.MustCompile(`(?m)^webserver-bindaddress:\s*(\S+)`)
)

func hasJar(prefix string) bool {
	ents, _ := os.ReadDir("plugins")
	for _, e := range ents {
		n := strings.ToLower(e.Name())
		if strings.HasPrefix(n, prefix) && strings.HasSuffix(n, ".jar") {
			return true
		}
	}
	return false
}

func (s *sim) serveMapPlugins() {
	type plug struct {
		name, jar, conf string
		port, bind      *regexp.Regexp
	}
	for _, p := range []plug{
		{"BlueMap", "bluemap", "plugins/BlueMap/webserver.conf", reHoconPort, reHoconIP},
		{"squaremap", "squaremap", "plugins/squaremap/config.yml", reYAMLPort, reYAMLBind},
		{"Dynmap", "dynmap", "plugins/dynmap/configuration.txt", reDynPort, reDynBind},
	} {
		conf := readFile(p.conf)
		if conf == "" || !hasJar(p.jar) {
			continue
		}
		m := p.port.FindStringSubmatch(conf)
		if m == nil {
			continue
		}
		bind := "0.0.0.0"
		if b := p.bind.FindStringSubmatch(conf); b != nil {
			bind = b[1]
		}
		addr := net.JoinHostPort(bind, m[1])
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			s.log("WARN", fmt.Sprintf("[%s] Failed to start the webserver on %s: %v", p.name, addr, err))
			continue
		}
		s.log("INFO", fmt.Sprintf("[%s] WebServer bound to: %s", p.name, ln.Addr()))
		go http.Serve(ln, s.mapHandler(p.name))
	}
}

func (s *sim) mapHandler(name string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Set-Cookie", "plugin=1; Path=/")
		fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><title>%s</title><link rel="stylesheet" href="assets/style.css"></head>
<body><h1 id="t">%s (Simulator)</h1><ul id="players"></ul><p id="live">-</p><script src="assets/app.js"></script></body></html>`, name, name)
	})
	mux.HandleFunc("/assets/style.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		io.WriteString(w, "body{font-family:sans-serif;background:#123;color:#fff}")
	})
	mux.HandleFunc("/assets/app.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		io.WriteString(w, `fetch('live/players.json').then(r=>r.json()).then(d=>{document.getElementById('players').innerHTML=d.players.map(p=>'<li>'+p.name+'</li>').join('')});
const es=new EventSource('sse');es.onmessage=e=>{document.getElementById('live').textContent=e.data};`)
	})
	mux.HandleFunc("/live/players.json", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		var list []string
		for _, p := range s.online() {
			list = append(list, fmt.Sprintf(`{"name":%q,"uuid":%q}`, p.Name, p.UUID))
		}
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"players":[%s]}`, strings.Join(list, ","))
	})
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		fl, _ := w.(http.Flusher)
		for i := 0; ; i++ {
			fmt.Fprintf(w, "data: tick %d\n\n", i)
			if fl != nil {
				fl.Flush()
			}
			select {
			case <-r.Context().Done():
				return
			case <-time.After(time.Second):
			}
		}
	})
	mux.HandleFunc("/debug/request", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "%s %s\n", r.Method, r.URL.RequestURI())
		for k, v := range r.Header {
			fmt.Fprintf(w, "%s: %s\n", k, strings.Join(v, ", "))
		}
	})
	return mux
}
