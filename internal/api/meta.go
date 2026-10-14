package api

import (
	"net/http"
	"sort"

	"hearth/internal/minecraft"
	"hearth/internal/notify"
	"hearth/internal/perms"
	"hearth/internal/settings"
	"hearth/internal/store"
)

// GET /api/meta
func (a *API) meta(w http.ResponseWriter, r *http.Request, u *store.User) error {
	games := []map[string]any{}
	for id, g := range a.Servers.Games() {
		games = append(games, map[string]any{"id": id, "name": g.Name(), "adminOnly": id != "minecraft"})
	}
	sort.Slice(games, func(i, j int) bool { return games[i]["id"].(string) > games[j]["id"].(string) })
	dockerOK := a.Servers.Docker().Check(r.Context()) == nil
	ok(w, map[string]any{
		"games":          games,
		"flavors":        minecraft.Flavors,
		"permissions":    perms.All,
		"presets":        perms.Presets,
		"webhookEvents":  notify.Events,
		"docker":         dockerOK,
		"defaultRuntime": a.Settings.Get(settings.DefaultRuntime),
		"defaultMemory":  a.Settings.Int(settings.DefaultMemoryMB),
		"portRange":      []int{a.Settings.Int(settings.PortStart), a.Settings.Int(settings.PortEnd)},
		"deathCauses":    minecraft.DeathCauses,
		"mapPlugins":     minecraft.MapPlugins,
	})
	return nil
}

// GET /api/meta/mc/versions?flavor=&snapshots=1
func (a *API) mcVersions(w http.ResponseWriter, r *http.Request, u *store.User) error {
	flavor := r.URL.Query().Get("flavor")
	if _, ok := minecraft.FlavorByID(flavor); !ok {
		return badRequest("Unbekannte Server-Art")
	}
	list, err := a.MC.Versions.List(r.Context(), flavor, r.URL.Query().Get("snapshots") == "1")
	if err != nil {
		return badRequest("Versionsliste nicht verfügbar: " + err.Error())
	}
	if list == nil {
		list = []minecraft.VersionInfo{}
	}
	ok(w, list)
	return nil
}

// GET /api/meta/mc/builds?flavor=&version=
func (a *API) mcBuilds(w http.ResponseWriter, r *http.Request, u *store.User) error {
	flavor, version := r.URL.Query().Get("flavor"), r.URL.Query().Get("version")
	list, err := a.MC.Versions.Builds(r.Context(), flavor, version)
	if err != nil {
		return badRequest("Builds nicht verfügbar: " + err.Error())
	}
	if list == nil {
		list = []minecraft.BuildInfo{}
	}
	if len(list) > 100 {
		list = list[:100]
	}
	ok(w, list)
	return nil
}
