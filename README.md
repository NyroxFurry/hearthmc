# Hearth

Hearth is a game server management panel with built-in user and permission management. It runs as a single executable without external dependencies, with the database, web interface and map renderer included in the binary.

Minecraft: Java Edition is fully supported. Other games can be run using a custom startup command, and additional games can be added through modules.

> Hearth is not an official Minecraft product and is not approved by or associated with Mojang or Microsoft.
>
> NOT AN OFFICIAL MINECRAFT PRODUCT. NOT APPROVED BY OR ASSOCIATED WITH MOJANG OR MICROSOFT.

## Features

### Server Management

- Minecraft server installation for Paper, Purpur, Vanilla, Fabric, Forge, NeoForge and custom JAR files. The required Java version (8 to 25) is downloaded automatically.
- Run servers directly on the host or in Docker containers with configurable RAM and CPU limits. Docker containers continue running when the panel restarts.
- Live console with command history, CPU and RAM usage, player charts, crash detection and automatic restarts with protection against restart loops.
- File manager with a built-in editor, drag and drop uploads (including entire folders), and support for creating and extracting ZIP archives.
- Backups with a short save pause to keep world data consistent. Backups can be restored, protected against automatic deletion or excluded from automatic backup jobs on a per-server basis.
- Scheduled tasks using cron expressions and configurable steps. Supported actions include commands, chat messages, delays, restarts and backups. Presets such as "Restart with warning" are included.
- Change server versions, edit `server.properties` through a form, manage server icons and reset worlds.
- Search for plugins and mods on Modrinth, install them together with their dependencies and update them directly from the panel.

### Players and Community

- Player statistics including playtime, deaths, kills, blocks mined, distance travelled and advancements. Data is read from the world's statistics files.
- Event history covering player joins, deaths and their causes, advancements and optionally chat messages.
- Manage the whitelist, operators, bans and IP bans, including when a server is stopped.
- Built-in world map renderer that reads Minecraft region files. Supports multiple zoom levels, the Nether, the End and live player positions. Maps are generated automatically a few minutes after a new world is first started.
- Support for BlueMap, squaremap and Dynmap. A map plugin can be selected when creating a server or installed later through the "Map" section. Hearth downloads the plugin from Modrinth, configures its web server and makes the map available inside the panel and on the public server page.
- Public status page for each server at `/s/<name>`. It can display the server status, address, online players, map, leaderboards, death feed with causes, advancements, activity and player profiles. Visibility can be configured separately for each server. A status badge is also available for websites and Discord.

### Users and Security

- User management with roles, resource quotas for server count and RAM, and invitation links.
- Per-server permissions with 24 individual permissions and predefined roles such as Viewer, Moderator and Co-Admin. Users cannot grant permissions they do not have themselves.
- Two-factor authentication using TOTP, recovery codes, session management and API keys.
- Audit logs for changes made to individual servers and the panel as a whole.
- Notifications through Discord or custom webhooks for server starts, stops, crashes, backups, player joins, deaths and advancements.
- Automatic HTTPS configuration using Let's Encrypt.

## Installation on Linux

Download the appropriate binary from the releases page. Most servers use `hearth-…-linux-amd64`. For Raspberry Pi 4/5 and ARM-based VPS systems, use `linux-arm64`.

Make the file executable and run the installer:

```bash
chmod +x hearth-1.1.1-linux-amd64
sudo ./hearth-1.1.1-linux-amd64 install
```

The installer copies the binary to `/usr/local/bin/hearth`, creates the `hearth` system user and the `/var/lib/hearth` data directory, sets up a systemd service and starts it.

Once the installation is complete, the installer displays the panel address and a setup code. Open the address in your browser, enter the code and create the administrator account.

To configure a domain and automatic HTTPS during installation:

```bash
sudo ./hearth-1.1.1-linux-amd64 install --domain panel.example.de --email you@example.com
```

The domain must point to the server, and ports 80 and 443 must be reachable from the internet.

Additional installer options:

- `--listen :9000` changes the listening address and port.
- `--data /path` sets a different data directory.
- `--user name` uses a different system user.
- `--no-start` skips starting the service after installation.

### Firewall

The following ports need to be accessible, depending on your setup:

- Panel port: 8080 by default.
- HTTPS: ports 80 and 443 when using automatic HTTPS.
- Map port: panel port + 1, which is 8081 by default or 8443 with HTTPS.
- Game ports: new servers are assigned an available port from 25565 to 25665 by default. The range can be changed in the settings.

RCON ports and the web servers used by map plugins are only accessible locally and do not need to be exposed.

Example using UFW:

```bash
sudo ufw allow 8080/tcp
sudo ufw allow 8081/tcp          # Map port, if required
sudo ufw allow 25565:25665/tcp
sudo ufw allow 25565:25665/udp   # Voice chat mods and Bedrock proxies
```

### Map Plugins and the Map Port

BlueMap, squaremap and Dynmap each provide their own web interface. Hearth configures these web servers to listen locally and makes the maps available through a separate port.

The separate port keeps the plugin web interface isolated from the panel login. Only read requests are forwarded. Maps that are not public remain accessible only to server members.

### Docker (Optional)

If Docker is installed, individual servers can be run in containers. During installation, Hearth adds the `hearth` user to the `docker` group.

Without Docker, servers run directly on the host. Java is downloaded automatically when required.

## Running Without Installation

Hearth can also be started directly for testing or on Windows and macOS.

On Linux:

```bash
./hearth serve --data ./data --listen :8080
```

On Windows:

```powershell
hearth-1.1.1-windows-amd64.exe serve --data .\data
```

## Service Management

| Task | Command |
| --- | --- |
| Check service status | `sudo systemctl status hearth` |
| Follow logs | `sudo journalctl -u hearth -f` |
| Restart the service | `sudo systemctl restart hearth` |
| Update Hearth | Replace `/usr/local/bin/hearth` with the new binary, then restart the service |
| Reset a password | `sudo -u hearth hearth user reset-password NAME` |
| Reset 2FA | `sudo -u hearth hearth user reset-2fa NAME` |
| Create an administrator | `sudo -u hearth hearth user create NAME --admin` |
| Uninstall Hearth | `sudo hearth uninstall` |

Uninstalling the service does not remove the data directory.

During an update, Docker containers continue running. Servers started directly on the host are stopped cleanly and started again afterwards.

### Data Directory

By default, Hearth stores its data in `/var/lib/hearth`.

```text
hearth.db      Database containing users, permissions, settings and statistics
servers/       Server files, with a separate directory for each server
backups/       ZIP backups
maps/          Rendered map tiles
java/          Automatically downloaded Java versions
config.json    Optional configuration file
```

To back up the entire panel, stop the service and copy the data directory.

### Configuration

Most settings can be managed through "Administration > Settings". Basic configuration is also available through startup flags, environment variables and `config.json` in the data directory.

| Environment variable | Description |
| --- | --- |
| `HEARTH_DATA` | Data directory |
| `HEARTH_LISTEN` | Listening address and port, e.g. `:8080` or `127.0.0.1:8080` |
| `HEARTH_TLS_DOMAIN`, `HEARTH_TLS_EMAIL` | Configuration for automatic HTTPS |
| `HEARTH_TLS_CERT`, `HEARTH_TLS_KEY` | Custom TLS certificate and key |
| `HEARTH_TRUST_PROXY=1` | Enables trusted proxy handling for a reverse proxy that sets `X-Forwarded-For` |
| `HEARTH_MAP_LISTEN` | Map port address, e.g. `:8081`; set to `off` to disable it |

### Reverse Proxy Setup

Hearth uses WebSockets at `/api/ws`.

Example configuration for Caddy:

```text
panel.example.de {
    reverse_proxy 127.0.0.1:8080
}
```

For nginx, use `proxy_http_version 1.1` and forward the `Upgrade` and `Connection` headers.

Set `HEARTH_TRUST_PROXY=1` and configure the public address in the panel settings. This ensures that invitation links and public status pages use the correct address.

If map plugins are enabled, the map port should have its own address, preferably a separate subdomain. Configure it under "Administration > Settings > Map port address".

Example Caddy configuration:

```text
map.example.de {
    reverse_proxy 127.0.0.1:8081
}
```

## Public Pages and API

- **Public status page:** `https://panel.example.de/s/<address>`. Configure it under "Server > Public page".
- **Status badge:** `https://panel.example.de/api/public/<address>/badge.svg`
- **Public JSON API:** `/api/public/<address>`. Provides server status, player information, events and map data.
- **API keys:** Create a key under "Account & Security" for use with scripts and bots. Send it in the `Authorization: Bearer hth_…` header. API requests use the permissions assigned to the account.

## Building from Source

Go 1.26 or newer is required. Node.js 22 is also needed if you want to modify the web interface.

Available Make targets:

```bash
make           # Build the web interface and create ./dist/hearth
make release   # Build binaries for Linux, Windows and macOS
make test      # Run tests and type checks
make dev       # Start development with hot reload
make licenses  # Regenerate THIRD_PARTY_LICENSES.txt after dependency changes
```

The compiled web interface is already included in `internal/web/dist`. This means `go build ./cmd/hearth` can be used without Node.js.

### Project Structure

```text
cmd/hearth            Entry point and command-line interface
internal/api          HTTP API and WebSocket
internal/servers      Server lifecycle, crash handling and resources
internal/runtime      Host execution and Docker Engine API integration
internal/minecraft    Installation, Java, RCON, log parsing, statistics and Modrinth
internal/mapr         Map renderer for Anvil regions (1.13 to 26.x)
internal/game         Interface for additional games
internal/generic      Custom startup command game type
web/                  Svelte interface for the panel and public pages
tools/mcsim            Minecraft server simulator for testing
```

Additional games can be integrated by implementing the `game.Game` interface, which covers installation, starting, stopping and readiness detection. The game must then be registered in `internal/app`.

Once registered, the existing console, file manager, backups, scheduled tasks, permissions and notifications are available automatically.

## License

Hearth is released under the [MIT License](LICENSE).

Built-in libraries and fonts use their respective open-source licenses, including MIT, ISC, BSD, Apache 2.0 and SIL Open Font License 1.1. SQLite is in the public domain.

License texts and copyright notices are included in [THIRD_PARTY_LICENSES.txt](THIRD_PARTY_LICENSES.txt). They are also available in the panel at `/licenses/THIRD_PARTY_LICENSES.txt`, linked from "Administration > System & Java" and the login page.

Running `make licenses` regenerates this file. The command fails if a dependency has no license file or uses a license that is not permitted.

Hearth does not include any Minecraft files. Server software (Vanilla, Paper, Purpur, Fabric, Forge and NeoForge), Java and map plugins are downloaded directly from their respective providers during setup.

Minecraft servers require acceptance of the [Minecraft EULA](https://aka.ms/MinecraftEULA).

Minecraft is a trademark of Mojang Synergies AB.