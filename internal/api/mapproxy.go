package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hearth/internal/minecraft"
	"hearth/internal/perms"
	"hearth/internal/servers"
	"hearth/internal/settings"
	"hearth/internal/store"
)

// Kartenport: the web pages of map plugins (BlueMap, squaremap, Dynmap) are
// served on a separate port. The separate origin keeps the plugins'
// JavaScript away from the panel: it cannot read panel pages or call the API
// with a user's session, so a weakness in a plugin's web page (e.g. an
// unescaped player-chosen marker name) cannot be turned against panel users.
//
// The map port never looks at panel cookies. Public maps are open; members
// get a signed link (/{id}/t/{token}/) from the panel that stays valid for
// twelve hours. Relative URLs inside the plugin's page keep the token.

const mapTokenTTL = 12 * time.Hour

var mapKey = func() []byte {
	k := make([]byte, 32)
	rand.Read(k)
	return k
}()

func mapMAC(serverID string, exp uint64) []byte {
	m := hmac.New(sha256.New, mapKey)
	fmt.Fprintf(m, "map:%s:%d", serverID, exp)
	return m.Sum(nil)[:18]
}

// mapToken signs access to the plugin map of a server.
func mapToken(serverID string) string {
	exp := uint64(time.Now().Add(mapTokenTTL).Unix())
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, exp)
	return base64.RawURLEncoding.EncodeToString(append(b, mapMAC(serverID, exp)...))
}

func validMapToken(serverID, tok string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil || len(raw) != 8+18 {
		return false
	}
	exp := binary.BigEndian.Uint64(raw[:8])
	if time.Now().Unix() > int64(exp) {
		return false
	}
	return hmac.Equal(raw[8:], mapMAC(serverID, exp))
}

var mapListen struct {
	sync.Mutex
	err string
}

// SetMapListenError records that the map port could not be opened.
func (a *API) SetMapListenError(err error) {
	mapListen.Lock()
	defer mapListen.Unlock()
	if err != nil {
		mapListen.err = err.Error()
	} else {
		mapListen.err = ""
	}
}

func (a *API) mapListenError() string {
	mapListen.Lock()
	defer mapListen.Unlock()
	return mapListen.err
}

func (a *API) mapPortTLS() bool {
	return a.Cfg.TLSDomain != "" || (a.Cfg.TLSCert != "" && a.Cfg.TLSKey != "")
}

// mapBaseURL is where browsers reach the map port ("" = not available).
func (a *API) mapBaseURL(r *http.Request) string {
	if u := strings.TrimRight(a.Settings.Get(settings.MapPublicURL), "/"); u != "" {
		return u
	}
	addr := a.Cfg.MapAddr()
	if addr == "" || a.mapListenError() != "" {
		return ""
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	scheme := "http"
	if a.mapPortTLS() {
		scheme = "https"
	}
	host := r.Host
	if a.Cfg.TrustProxy && r.Header.Get("X-Forwarded-Host") != "" {
		host = r.Header.Get("X-Forwarded-Host")
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host + ":" + port
}

// MapOrigin is the origin of the map port as seen by the browser (for the CSP).
func (a *API) MapOrigin(r *http.Request) string {
	base := a.mapBaseURL(r)
	if base == "" {
		return ""
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// mapPortInfo describes the map port for the UI.
func (a *API) mapPortInfo(r *http.Request) map[string]any {
	base := a.mapBaseURL(r)
	out := map[string]any{"url": base, "error": a.mapListenError(), "custom": a.Settings.Get(settings.MapPublicURL) != ""}
	if addr := a.Cfg.MapAddr(); addr != "" {
		if _, port, err := net.SplitHostPort(addr); err == nil {
			out["port"] = port
		}
	} else {
		out["disabled"] = true
	}
	// an https page cannot embed a plain http map
	out["mixed"] = a.isHTTPS(r) && strings.HasPrefix(base, "http://")
	return out
}

// mapPluginView is the map plugin part of the map info.
func (a *API) mapPluginView(r *http.Request, inst *servers.Instance, h *minecraft.Handler) map[string]any {
	st := h.MapPlugin()
	if st == nil {
		return nil
	}
	out := map[string]any{"id": st.ID, "name": st.Name, "port": st.Port, "file": st.File, "enabled": st.Enabled, "supported": st.Supported}
	if base := a.mapBaseURL(r); base != "" {
		// signed link: members see the map even if it is not public
		out["url"] = base + "/" + inst.ID() + "/t/" + mapToken(inst.ID()) + "/"
	}
	if inst.Running() && st.Port > 0 {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", st.Port), 400*time.Millisecond)
		if err == nil {
			c.Close()
			out["listening"] = true
		}
	}
	return out
}

// mapPublic reports whether everybody may see the plugin map of a server.
func (a *API) mapPublic(srv *store.Server) bool {
	return a.Settings.Bool(settings.PublicPagesEnabled) && srv.PublicEnabled && decodePublic(srv.PublicConfig).ShowMap
}

var mapTransport = &http.Transport{
	Proxy:                 nil, // always local
	DialContext:           (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
	MaxIdleConnsPerHost:   16,
	IdleConnTimeout:       60 * time.Second,
	ResponseHeaderTimeout: 60 * time.Second,
}

// MapProxy is the handler of the map port.
func (a *API) MapProxy() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "no-referrer")
		if r.URL.Path == "/healthz" {
			hd.Set("Access-Control-Allow-Origin", "*")
			hd.Set("Cache-Control", "no-store")
			w.Write([]byte("ok"))
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			// read-only: no web chat, no logins into the plugin
			http.Error(w, "Nur Lesezugriff", http.StatusMethodNotAllowed)
			return
		}
		// /{id}/...           public map
		// /{id}/t/{token}/...  signed link for members
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 4)
		id := parts[0]
		prefix, rest, complete := "/"+id+"/", "", len(parts) > 1
		signed := false
		if len(parts) >= 3 && parts[1] == "t" {
			if !validMapToken(id, parts[2]) {
				a.mapPage(w, nil, http.StatusForbidden, "Link abgelaufen", "Öffne die Karte erneut im Panel, dann bekommst du einen frischen Link.")
				return
			}
			signed = true
			prefix += "t/" + parts[2] + "/"
			complete = len(parts) == 4
			if complete {
				rest = parts[3]
			}
		} else if len(parts) > 1 {
			rest = strings.Join(parts[1:], "/")
		}
		inst := a.Servers.Get(id)
		var h *minecraft.Handler
		if inst != nil {
			h, _ = inst.Handler().(*minecraft.Handler)
		}
		if h == nil {
			a.mapPage(w, nil, http.StatusNotFound, "Karte nicht gefunden", "Unter dieser Adresse gibt es keine Karte.")
			return
		}
		srv := inst.Server()
		s := h.Settings()
		if s.Map.Plugin == "" || s.Map.PluginPort <= 0 {
			a.mapPage(w, srv, http.StatusNotFound, "Keine Plugin-Karte", "Für diesen Server ist kein Karten-Plugin eingerichtet.")
			return
		}
		if !signed && !a.mapPublic(srv) {
			a.mapPage(w, srv, http.StatusForbidden, "Karte nicht öffentlich", "Öffne die Karte im Panel, um sie zu sehen.")
			return
		}
		if !complete {
			// relative URLs in the plugin's page need the trailing slash
			http.Redirect(w, r, prefix, http.StatusFound)
			return
		}
		if !inst.Running() {
			a.mapPage(w, srv, http.StatusServiceUnavailable, "Server offline", "Die Karte ist wieder da, sobald der Server läuft.")
			return
		}
		clean := path.Clean("/" + rest)
		if strings.HasSuffix(rest, "/") && clean != "/" {
			clean += "/"
		}
		target := &url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", s.Map.PluginPort)}
		name := s.Map.Plugin
		if p, ok := minecraft.MapPluginByID(s.Map.Plugin); ok {
			name = p.Name
		}
		rp := &httputil.ReverseProxy{
			Transport:     mapTransport,
			FlushInterval: 100 * time.Millisecond, // live updates (server-sent events)
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(target)
				pr.Out.URL.Path, pr.Out.URL.RawPath = clean, ""
				pr.Out.Host = target.Host
				// the plugin never sees panel credentials
				pr.Out.Header.Del("Cookie")
				pr.Out.Header.Del("Authorization")
				pr.SetXForwarded()
			},
			ModifyResponse: func(resp *http.Response) error {
				resp.Header.Del("Set-Cookie")
				resp.Header.Del("X-Frame-Options") // embedded by the panel and the public page
				return nil
			},
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				a.mapPage(w, srv, http.StatusBadGateway, name+" startet noch",
					"Das Karten-Plugin antwortet noch nicht. Nach dem Serverstart kann das eine Minute dauern.")
			},
		}
		rp.ServeHTTP(w, r)
	})
}

// mapPage renders a small notice in the server's colors.
func (a *API) mapPage(w http.ResponseWriter, srv *store.Server, status int, title, text string) {
	fl := flavors[0]
	if srv != nil {
		fl = flavorFor(srv.Color, srv.ID)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html><html lang="de"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title>
<style>html,body{margin:0;height:100%%}body{display:grid;place-items:center;background:%s;color:#fff;font:16px/1.5 system-ui,sans-serif;text-align:center;padding:24px;box-sizing:border-box}
.g{position:fixed;left:50%%;top:50%%;width:min(420px,80vw);aspect-ratio:1;border-radius:50%%;transform:translate(-50%%,-50%%);background:%s;opacity:.28;filter:blur(64px)}
main{position:relative;max-width:42ch}h1{margin:0 0 8px;font-size:22px;font-weight:800;letter-spacing:-.01em}p{margin:0;color:rgba(255,255,255,.8)}</style></head>
<body><div class="g"></div><main><h1>%s</h1><p>%s</p></main></body></html>`,
		html.EscapeString(title), fl.BG, fl.Color, html.EscapeString(title), html.EscapeString(text))
}

// PUT /api/servers/{id}/mc/map/plugin {plugin}
// Installs, switches or removes the map plugin of a server.
func (a *API) mcMapPlugin(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	if !ps.Has(perms.PluginsManage) {
		return errForbidden
	}
	var req struct {
		Plugin string `json:"plugin"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	s := h.Settings()
	dir := filepath.Join(inst.Dir(), minecraft.ModDir(s.Flavor))
	disable := func(id string) {
		if file, enabled := minecraft.FindMapPluginJar(inst.Dir(), s.Flavor, id); file != "" && enabled {
			os.Rename(filepath.Join(dir, file), filepath.Join(dir, file+".disabled"))
		}
	}
	save := func(plugin string, port int) error {
		srv := inst.Server()
		cur := minecraft.DecodeSettings(srv.Settings)
		cur.Map.Plugin, cur.Map.PluginPort = plugin, port
		srv.Settings = cur.Encode()
		return a.Servers.Update(r.Context(), srv)
	}
	if req.Plugin == "" {
		if s.Map.Plugin != "" {
			// reversible: the jar is only disabled, its data stays
			disable(s.Map.Plugin)
			if err := save("", 0); err != nil {
				return badRequest(err.Error())
			}
			a.audit(r, inst.ID(), "map.plugin", map[string]any{"plugin": ""})
		}
		ok(w, map[string]any{"info": a.mapInfo(r, inst, h), "restartRequired": inst.State() != "offline"})
		return nil
	}
	p, found := minecraft.MapPluginByID(req.Plugin)
	if !found {
		return badRequest("Unbekanntes Karten-Plugin")
	}
	if !p.Supports(s.Flavor) {
		return badRequest(p.Name + " gibt es nicht für diese Server-Art")
	}
	if s.Map.Plugin != "" && s.Map.Plugin != p.ID {
		disable(s.Map.Plugin)
	}
	port := s.Map.PluginPort
	if port <= 0 {
		var err error
		if port, err = a.Servers.AllocatePort(r.Context(), 0, true); err != nil {
			return badRequest(err.Error())
		}
	}
	if err := save(p.ID, port); err != nil {
		return badRequest(err.Error())
	}
	// a previously disabled copy is simply switched back on
	if file, enabled := minecraft.FindMapPluginJar(inst.Dir(), s.Flavor, p.ID); file != "" && !enabled {
		os.Rename(filepath.Join(dir, file), filepath.Join(dir, strings.TrimSuffix(file, ".disabled")))
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	if err := h.InstallMapPlugin(ctx); err != nil {
		return badRequest(err.Error())
	}
	a.audit(r, inst.ID(), "map.plugin", map[string]any{"plugin": p.ID})
	ok(w, map[string]any{"info": a.mapInfo(r, inst, h), "restartRequired": inst.State() != "offline"})
	return nil
}
