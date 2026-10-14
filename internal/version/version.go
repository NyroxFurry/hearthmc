// Package version holds build information injected via -ldflags.
package version

// Set at build time: -ldflags "-X hearth/internal/version.Version=1.0.0"
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// UserAgent returns the HTTP User-Agent used for outgoing API requests.
// Several APIs (PaperMC, Modrinth) require a non-generic agent with contact info.
func UserAgent(contact string) string {
	if contact == "" {
		contact = "self-hosted"
	}
	return "Hearth-Panel/" + Version + " (game server panel; contact: " + contact + ")"
}
