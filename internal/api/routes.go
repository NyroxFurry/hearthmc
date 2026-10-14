package api

import (
	"net/http"
	"strings"

	"hearth/internal/perms"
)

// Handler returns the complete HTTP handler (API, WebSocket, public pages, web UI).
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	// setup & auth
	mux.HandleFunc("GET /api/setup", a.setupStatus)
	mux.HandleFunc("POST /api/setup", a.setup)
	mux.HandleFunc("POST /api/auth/login", a.login)
	mux.HandleFunc("POST /api/auth/2fa", a.login2FA)
	mux.HandleFunc("POST /api/auth/logout", a.logout)
	mux.HandleFunc("GET /api/auth/me", a.me)
	mux.HandleFunc("GET /api/invites/{token}", a.inviteInfo)
	mux.HandleFunc("POST /api/invites/{token}", a.inviteAccept)

	// account
	mux.Handle("PUT /api/account", a.authed(a.updateAccount))
	mux.Handle("PUT /api/account/password", a.authed(a.changePassword))
	mux.Handle("POST /api/account/2fa/setup", a.authed(a.twoFASetup))
	mux.Handle("POST /api/account/2fa/enable", a.authed(a.twoFAEnable))
	mux.Handle("POST /api/account/2fa/disable", a.authed(a.twoFADisable))
	mux.Handle("GET /api/account/sessions", a.authed(a.listSessions))
	mux.Handle("DELETE /api/account/sessions/{key}", a.authed(a.deleteSession))
	mux.Handle("GET /api/account/keys", a.authed(a.listKeys))
	mux.Handle("POST /api/account/keys", a.authed(a.createKey))
	mux.Handle("DELETE /api/account/keys/{kid}", a.authed(a.deleteKey))

	// meta
	mux.Handle("GET /api/meta", a.authed(a.meta))
	mux.Handle("GET /api/meta/mc/versions", a.authed(a.mcVersions))
	mux.Handle("GET /api/meta/mc/builds", a.authed(a.mcBuilds))
	mux.Handle("GET /api/cron/preview", a.authed(a.cronPreview))
	mux.Handle("GET /api/users/search", a.authed(a.searchUsers))
	mux.Handle("GET /api/ws", a.authed(a.websocket))

	// admin
	mux.Handle("GET /api/admin/users", a.admin(a.adminUsers))
	mux.Handle("POST /api/admin/users", a.admin(a.adminCreateUser))
	mux.Handle("PATCH /api/admin/users/{uid}", a.admin(a.adminUpdateUser))
	mux.Handle("DELETE /api/admin/users/{uid}", a.admin(a.adminDeleteUser))
	mux.Handle("POST /api/admin/users/{uid}/reset-2fa", a.admin(a.adminReset2FA))
	mux.Handle("GET /api/admin/invites", a.admin(a.adminInvites))
	mux.Handle("POST /api/admin/invites", a.admin(a.adminCreateInvite))
	mux.Handle("DELETE /api/admin/invites/{iid}", a.admin(a.adminDeleteInvite))
	mux.Handle("GET /api/admin/settings", a.admin(a.adminSettings))
	mux.Handle("PUT /api/admin/settings", a.admin(a.adminUpdateSettings))
	mux.Handle("GET /api/admin/system", a.admin(a.adminSystem))
	mux.Handle("GET /api/admin/audit", a.admin(a.adminAudit))
	mux.Handle("GET /api/admin/java", a.admin(a.adminJava))
	mux.Handle("POST /api/admin/java/{major}", a.admin(a.adminInstallJava))
	mux.Handle("DELETE /api/admin/java/{major}", a.admin(a.adminRemoveJava))
	mux.Handle("GET /api/admin/webhooks", a.admin(a.adminWebhooks))
	mux.Handle("POST /api/admin/webhooks", a.admin(a.adminSaveWebhook))
	mux.Handle("PUT /api/admin/webhooks/{wid}", a.admin(a.adminSaveWebhook))
	mux.Handle("DELETE /api/admin/webhooks/{wid}", a.admin(a.adminDeleteWebhook))
	mux.Handle("POST /api/admin/webhooks/{wid}/test", a.admin(a.adminTestWebhook))

	// servers
	mux.Handle("GET /api/servers", a.authed(a.listServers))
	mux.Handle("POST /api/servers", a.authed(a.createServer))
	mux.Handle("GET /api/servers/{id}", a.srv("", a.getServer))
	mux.Handle("PATCH /api/servers/{id}", a.srv("", a.updateServer))
	mux.Handle("DELETE /api/servers/{id}", a.srv("", a.deleteServer))
	mux.Handle("POST /api/servers/{id}/power", a.srv("", a.power))
	mux.Handle("POST /api/servers/{id}/command", a.srv(perms.ConsoleCommand, a.command))
	mux.Handle("GET /api/servers/{id}/console", a.srv(perms.ConsoleRead, a.consoleHistory))
	mux.Handle("GET /api/servers/{id}/metrics", a.srv("", a.metrics))
	mux.Handle("POST /api/servers/{id}/reinstall", a.srv(perms.SettingsEdit, a.reinstall))
	mux.Handle("GET /api/servers/{id}/icon.png", a.srv("", a.serverIcon))
	mux.Handle("GET /api/servers/{id}/members", a.srv(perms.MembersManage, a.members))
	mux.Handle("POST /api/servers/{id}/members", a.srv(perms.MembersManage, a.addMember))
	mux.Handle("PUT /api/servers/{id}/members/{uid}", a.srv(perms.MembersManage, a.updateMember))
	mux.Handle("DELETE /api/servers/{id}/members/{uid}", a.srv(perms.MembersManage, a.removeMember))
	mux.Handle("POST /api/servers/{id}/invites", a.srv(perms.MembersManage, a.serverInvite))
	mux.Handle("GET /api/servers/{id}/activity", a.srv(perms.ActivityRead, a.activity))
	mux.Handle("GET /api/servers/{id}/webhooks", a.srv(perms.WebhooksManage, a.serverWebhooks))
	mux.Handle("POST /api/servers/{id}/webhooks", a.srv(perms.WebhooksManage, a.serverSaveWebhook))
	mux.Handle("PUT /api/servers/{id}/webhooks/{wid}", a.srv(perms.WebhooksManage, a.serverSaveWebhook))
	mux.Handle("DELETE /api/servers/{id}/webhooks/{wid}", a.srv(perms.WebhooksManage, a.serverDeleteWebhook))
	mux.Handle("POST /api/servers/{id}/webhooks/{wid}/test", a.srv(perms.WebhooksManage, a.serverTestWebhook))
	mux.Handle("GET /api/servers/{id}/public", a.srv(perms.PublicManage, a.getPublicConfig))
	mux.Handle("PUT /api/servers/{id}/public", a.srv(perms.PublicManage, a.setPublicConfig))

	// files
	mux.Handle("GET /api/servers/{id}/files", a.srv(perms.FilesRead, a.listFiles))
	mux.Handle("GET /api/servers/{id}/files/content", a.srv(perms.FilesRead, a.readFile))
	mux.Handle("PUT /api/servers/{id}/files/content", a.srv(perms.FilesWrite, a.writeFile))
	mux.Handle("GET /api/servers/{id}/files/download", a.srv(perms.FilesRead, a.downloadFiles))
	mux.Handle("POST /api/servers/{id}/files/upload", a.srv(perms.FilesWrite, a.uploadFiles))
	mux.Handle("POST /api/servers/{id}/files/{op}", a.srv("", a.fileOp))

	// backups
	mux.Handle("GET /api/servers/{id}/backups", a.srv(perms.BackupsRead, a.listBackups))
	mux.Handle("POST /api/servers/{id}/backups", a.srv(perms.BackupsCreate, a.createBackup))
	mux.Handle("PUT /api/servers/{id}/backups/settings", a.srv(perms.SettingsEdit, a.backupSettings))
	mux.Handle("GET /api/servers/{id}/backups/{bid}/download", a.srv(perms.BackupsRead, a.downloadBackup))
	mux.Handle("POST /api/servers/{id}/backups/{bid}/restore", a.srv(perms.BackupsRestore, a.restoreBackup))
	mux.Handle("PATCH /api/servers/{id}/backups/{bid}", a.srv(perms.BackupsRead, a.updateBackup))
	mux.Handle("DELETE /api/servers/{id}/backups/{bid}", a.srv(perms.BackupsDelete, a.deleteBackup))

	// schedules
	mux.Handle("GET /api/servers/{id}/schedules", a.srv(perms.SchedulesRead, a.listSchedules))
	mux.Handle("POST /api/servers/{id}/schedules", a.srv(perms.SchedulesEdit, a.createSchedule))
	mux.Handle("PUT /api/servers/{id}/schedules/{sid}", a.srv(perms.SchedulesEdit, a.updateSchedule))
	mux.Handle("DELETE /api/servers/{id}/schedules/{sid}", a.srv(perms.SchedulesEdit, a.deleteSchedule))
	mux.Handle("POST /api/servers/{id}/schedules/{sid}/run", a.srv(perms.SchedulesEdit, a.runSchedule))

	// minecraft
	mux.Handle("GET /api/servers/{id}/mc/status", a.mc("", a.mcStatus))
	mux.Handle("GET /api/servers/{id}/mc/properties", a.mc(perms.SettingsRead, a.mcProperties))
	mux.Handle("PUT /api/servers/{id}/mc/properties", a.mc(perms.SettingsEdit, a.mcSetProperties))
	mux.Handle("GET /api/servers/{id}/mc/players", a.mc(perms.PlayersRead, a.mcPlayers))
	mux.Handle("GET /api/servers/{id}/mc/players/{uuid}", a.mc(perms.PlayersRead, a.mcPlayer))
	mux.Handle("GET /api/servers/{id}/mc/lists", a.mc(perms.PlayersRead, a.mcLists))
	mux.Handle("POST /api/servers/{id}/mc/action", a.mc(perms.PlayersManage, a.mcAction))
	mux.Handle("GET /api/servers/{id}/mc/events", a.mc(perms.PlayersRead, a.mcEvents))
	mux.Handle("POST /api/servers/{id}/mc/stats/refresh", a.mc(perms.PlayersRead, a.mcRefreshStats))
	mux.Handle("POST /api/servers/{id}/mc/version", a.mc(perms.SettingsEdit, a.mcChangeVersion))
	mux.Handle("POST /api/servers/{id}/mc/icon", a.mc(perms.SettingsEdit, a.mcIcon))
	mux.Handle("DELETE /api/servers/{id}/mc/icon", a.mc(perms.SettingsEdit, a.mcIcon))
	mux.Handle("GET /api/servers/{id}/mc/worlds", a.mc(perms.SettingsRead, a.mcWorlds))
	mux.Handle("POST /api/servers/{id}/mc/worlds/reset", a.mc(perms.SettingsEdit, a.mcResetWorld))
	mux.Handle("GET /api/servers/{id}/mc/plugins", a.mc(perms.PluginsManage, a.mcPlugins))
	mux.Handle("POST /api/servers/{id}/mc/plugins/check-updates", a.mc(perms.PluginsManage, a.mcCheckUpdates))
	mux.Handle("POST /api/servers/{id}/mc/plugins/toggle", a.mc(perms.PluginsManage, a.mcTogglePlugin))
	mux.Handle("DELETE /api/servers/{id}/mc/plugins", a.mc(perms.PluginsManage, a.mcDeletePlugin))
	mux.Handle("GET /api/servers/{id}/mc/modrinth/search", a.mc(perms.PluginsManage, a.mcModrinthSearch))
	mux.Handle("GET /api/servers/{id}/mc/modrinth/versions", a.mc(perms.PluginsManage, a.mcModrinthVersions))
	mux.Handle("POST /api/servers/{id}/mc/modrinth/install", a.mc(perms.PluginsManage, a.mcModrinthInstall))
	mux.Handle("GET /api/servers/{id}/mc/map", a.mc("", a.mcMap))
	mux.Handle("POST /api/servers/{id}/mc/map/render", a.mc(perms.MapManage, a.mcMapRender))
	mux.Handle("PUT /api/servers/{id}/mc/map/settings", a.mc(perms.MapManage, a.mcMapSettings))
	mux.Handle("PUT /api/servers/{id}/mc/map/plugin", a.mc(perms.MapManage, a.mcMapPlugin))
	mux.Handle("GET /api/servers/{id}/mc/map/players", a.mc("", a.mcMapPlayers))
	mux.Handle("GET /api/servers/{id}/mc/map/tiles/{dim}/{z}/{x}/{y}", a.mc("", a.mcMapTile))

	// public pages
	mux.Handle("GET /api/public/{slug}", a.public(a.publicInfo))
	mux.Handle("GET /api/public/{slug}/players", a.public(a.publicPlayers))
	mux.Handle("GET /api/public/{slug}/players/{uuid}", a.public(a.publicPlayer))
	mux.Handle("GET /api/public/{slug}/events", a.public(a.publicEvents))
	mux.Handle("GET /api/public/{slug}/activity", a.public(a.publicActivity))
	mux.Handle("GET /api/public/{slug}/map", a.public(a.publicMap))
	mux.Handle("GET /api/public/{slug}/map/players", a.public(a.publicMapPlayers))
	mux.Handle("GET /api/public/{slug}/map/tiles/{dim}/{z}/{x}/{y}", a.public(a.publicTile))
	mux.Handle("GET /api/public/{slug}/icon.png", a.public(a.publicIcon))
	mux.Handle("GET /api/public/{slug}/badge.svg", a.public(a.publicBadge))
	mux.HandleFunc("GET /api/heads/{uuid}", a.head)

	// public status pages (server side Open Graph tags)
	mux.HandleFunc("GET /s/{slug}", func(w http.ResponseWriter, r *http.Request) {
		a.Index(w, r, "public", a.PublicPageMeta(r, r.PathValue("slug")))
	})
	mux.HandleFunc("GET /s/{slug}/{rest...}", func(w http.ResponseWriter, r *http.Request) {
		a.Index(w, r, "public", a.PublicPageMeta(r, r.PathValue("slug")))
	})
	// everything else: unknown API routes, static assets and the SPA
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/"):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "Unbekannter API-Endpunkt"})
		case r.Method != http.MethodGet && r.Method != http.MethodHead:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		case strings.HasPrefix(r.URL.Path, "/assets/") || strings.Contains(r.URL.Path[1:], "."):
			a.Static.ServeHTTP(w, r)
		default:
			a.Index(w, r, "panel", "")
		}
	})
	return a.middleware(mux)
}
