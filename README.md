# Hearth

Hearth ist ein Panel für Gameserver mit eigener Benutzer- und Rechteverwaltung. Es besteht aus **einer einzigen Programmdatei** ohne Abhängigkeiten: Datenbank, Weboberfläche und Kartenrenderer sind eingebaut. Minecraft: Java Edition ist voll integriert, andere Spiele laufen über einen frei definierbaren Startbefehl, und weitere Spiele lassen sich als Module ergänzen.

> Hearth ist kein offizielles Minecraft-Produkt und weder von Mojang noch von Microsoft genehmigt oder mit ihnen verbunden.
>
> *NOT AN OFFICIAL MINECRAFT PRODUCT. NOT APPROVED BY OR ASSOCIATED WITH MOJANG OR MICROSOFT.*

## Funktionen

**Server betreiben**
- Minecraft-Server mit wenigen Klicks: Paper, Purpur, Vanilla, Fabric, Forge, NeoForge oder eine eigene JAR. Die passende Java-Version (8 bis 25) wird automatisch heruntergeladen.
- Ausführung direkt auf dem Host oder im Docker-Container mit festem RAM- und CPU-Limit. Container laufen weiter, wenn das Panel neu startet.
- Live-Konsole mit Befehlsverlauf, CPU-, RAM- und Spieler-Diagramme, Absturzerkennung mit automatischem Neustart (und Schutz vor Neustart-Schleifen).
- **Echte Speicheranzeige für Java:** Java reserviert den kompletten Heap schon beim Start, der Prozess steht deshalb fast immer am Limit. Hearth liest den tatsächlich genutzten Heap direkt aus der JVM (wie `jcmd GC.heap_info`, ab Java 9) und zeigt ihn in Übersicht, Dashboard und Diagrammen.
- Dateimanager mit Editor, Hochladen per Drag & Drop (auch ganze Ordner), ZIP-Archive packen und entpacken.
- **SFTP-Zugang pro Server** für FileZilla, WinSCP und Co.: Anmeldung als `benutzer.serverid` mit dem Panel-Passwort oder einem SSH-Schlüssel, nur im Ordner dieses Servers und im Rahmen der Dateirechte.
- Backups mit kurzer Speicherpause für konsistente Welten, Wiederherstellung, Sperren gegen automatisches Löschen, Ausnahmen pro Server.
- Zeitpläne mit Cron-Ausdrücken und Schritten: Befehle, Chat-Nachrichten, Warten, Neustart, Backup. Vorlagen wie „Neustart mit Vorwarnung“ sind dabei.
- Versionswechsel, server.properties als Formular, Servericon, Welt zurücksetzen.
- Plugins und Mods direkt aus Modrinth suchen, installieren (inklusive Abhängigkeiten) und aktualisieren.
- **Modpack auf Knopfdruck** (Fabric, Forge, NeoForge): Hearth baut aus den Mods des Servers ein Modrinth-Pack (.mrpack). Beim Erstellen wählst du Client-Mods wie Sodium, Iris oder eine Minikarte dazu, die Serveradresse steht schon in der Serverliste. Spieler importieren das Pack in die Modrinth App, auf Wunsch direkt von der öffentlichen Seite.

**Spieler und Community**
- Spielerliste mit Spielzeit, Toden, Kills, abgebauten Blöcken, Strecke und Fortschritten, eingelesen aus den Statistik-Dateien der Welt.
- Ereignisverlauf mit Beitritten, Toden (mit Ursache), Fortschritten und optional Chat.
- Whitelist, Operatoren, Sperren und IP-Sperren verwalten, auch bei gestopptem Server.
- **Eingebaute Weltkarte** direkt aus den Regionsdateien, mit Zoomstufen, Nether und End sowie Live-Positionen der Spieler. Eine neue Welt erscheint wenige Minuten nach dem ersten Start von selbst.
- **Karten-Plugins auf Knopfdruck:** BlueMap, squaremap oder Dynmap beim Anlegen des Servers (oder später unter „Karte“) auswählen. Das Panel lädt das Plugin aus Modrinth, stellt seinen Webserver ein und zeigt die Karte im Panel und auf der öffentlichen Seite.
- **Öffentliche Statusseite pro Server** unter `/s/<name>`: Status und Adresse zum Kopieren, Spieler online, Karte, Bestenlisten, Todesfeed mit Todesursachen, Fortschritte, Aktivität und Spielerprofile. Was sichtbar ist, entscheidest du pro Server. Dazu ein Status-Badge für Webseiten und Discord.

**Benutzer und Sicherheit**
- Benutzerverwaltung mit Rollen, Kontingenten (Anzahl Server, Arbeitsspeicher) und Einladungslinks.
- Rechte **pro Server** aus 25 Einzelrechten, mit Vorlagen (Zuschauer, Moderator, Co-Admin). Niemand kann Rechte vergeben, die er selbst nicht hat.
- Zwei-Faktor-Anmeldung (TOTP) mit Wiederherstellungscodes, Sitzungsverwaltung, API-Schlüssel.
- Protokoll aller Änderungen, pro Server und panelweit.
- Benachrichtigungen an Discord oder eigene Webhooks: Start, Stopp, Absturz, Backups, Beitritte, Tode, Fortschritte.
- Automatisches HTTPS mit Let's Encrypt.

## Installation (Linux)

Lade die passende Datei von der Release-Seite herunter (`hearth-…-linux-amd64` für die meisten Server, `linux-arm64` für Raspberry Pi 4/5 und ARM-VPS) und starte die Installation:

```bash
chmod +x hearth-1.2.0-linux-amd64
sudo ./hearth-1.2.0-linux-amd64 install
```

Der Installer kopiert das Programm nach `/usr/local/bin/hearth`, legt den Systembenutzer `hearth` und den Datenordner `/var/lib/hearth` an, richtet einen systemd-Dienst ein und startet ihn. Am Ende zeigt er die Adresse des Panels und einen **Setup-Code**. Öffne die Adresse im Browser, gib den Code ein und lege dein Administrator-Konto an. Fertig.

Mit eigener Domain und automatischem HTTPS:

```bash
sudo ./hearth-1.2.0-linux-amd64 install --domain panel.example.de --email du@example.de
```

Dafür müssen die Ports 80 und 443 aus dem Internet erreichbar sein und die Domain auf den Server zeigen.

Weitere Optionen: `--listen :9000` (anderer Port), `--data /pfad` (anderer Datenordner), `--user name` (anderer Systembenutzer), `--no-start`.

### Firewall

Freigeben musst du den Port des Panels (Standard 8080, bei HTTPS 80 und 443), den Kartenport (Panel-Port + 1, also 8081, bei HTTPS 8443), den SFTP-Port 2022 und die Spielports. Neue Server bekommen automatisch einen freien Port aus dem Bereich 25565 bis 25665 (einstellbar). RCON-Ports und die Webserver der Karten-Plugins werden nur lokal verwendet und müssen nicht freigegeben werden.

```bash
sudo ufw allow 8080/tcp
sudo ufw allow 8081/tcp          # Kartenport, nur für Karten-Plugins nötig
sudo ufw allow 2022/tcp          # SFTP
sudo ufw allow 25565:25665/tcp
sudo ufw allow 25565:25665/udp   # für Voice-Chat-Mods und Bedrock-Proxys
```

### Karten-Plugins und Kartenport

BlueMap, squaremap und Dynmap bringen eigene Webseiten mit. Das Panel richtet ihren Webserver so ein, dass er nur lokal erreichbar ist, und reicht die Karte über einen eigenen Port weiter. Der eigene Port sorgt dafür, dass die Webseite des Plugins nicht auf die Anmeldung im Panel zugreifen kann. Durchgereicht werden nur lesende Anfragen; nicht öffentliche Karten sehen nur Mitglieder des Servers.

### SFTP

Hearth bringt einen eigenen SFTP-Server mit (Port 2022). Die Zugangsdaten stehen im Dateien-Bereich jedes Servers unter **SFTP**:

- Server: Adresse des Panels, Port `2022`
- Benutzer: `<panel-benutzer>.<server-id>`, z. B. `alex.pbwn79nv`
- Passwort: das Panel-Passwort. Mit aktiver Zwei-Faktor-Anmeldung geht SFTP nur mit SSH-Schlüssel, damit das Passwort allein nicht reicht. Schlüssel hinterlegst du unter **Konto & Sicherheit**.

Man sieht nur den Ordner dieses einen Servers. Lesen, Schreiben und Löschen richten sich nach den Dateirechten; Mitglieder brauchen zusätzlich das Recht „SFTP-Zugang“. Symlinks lassen sich nicht anlegen, und Verweise aus dem Serverordner heraus werden nicht verfolgt. Jede Anmeldung und jede Sitzung mit Änderungen steht im Aktivitätsprotokoll. Den Fingerabdruck des Servers (den dein Programm beim ersten Verbinden abfragt) zeigt das Panel ebenfalls an.

### Docker (optional)

Ist Docker installiert, kann jeder Server wahlweise im Container laufen. Der Installer nimmt den Benutzer `hearth` dafür in die Gruppe `docker` auf. Ohne Docker laufen alle Server direkt auf dem Host, Java wird dann automatisch heruntergeladen.

## Ohne Installation starten

Für Tests, Windows oder macOS reicht:

```bash
./hearth serve --data ./data --listen :8080
```

Unter Windows: `hearth-1.2.0-windows-amd64.exe serve --data .\data`.

## Betrieb

| Aufgabe | Befehl |
| --- | --- |
| Status und Logs | `sudo systemctl status hearth`, `sudo journalctl -u hearth -f` |
| Neu starten | `sudo systemctl restart hearth` |
| Update | neue Datei nach `/usr/local/bin/hearth` kopieren, dann `sudo systemctl restart hearth` |
| Passwort vergessen | `sudo -u hearth hearth user reset-password NAME` |
| 2FA zurücksetzen | `sudo -u hearth hearth user reset-2fa NAME` |
| Benutzer anlegen | `sudo -u hearth hearth user create NAME --admin` |
| Dienst entfernen | `sudo hearth uninstall` (Daten bleiben erhalten) |

Laufende Docker-Server bleiben bei einem Update online, direkt gestartete Server werden sauber gestoppt und danach wieder gestartet.

### Datenordner

Alles liegt in einem Ordner (Standard `/var/lib/hearth`):

```
hearth.db                Datenbank: Benutzer, Rechte, Einstellungen, Statistiken
servers/                 ein Ordner pro Server
backups/                 ZIP-Backups
maps/                    gerenderte Kartenkacheln
modpacks/                zuletzt erstellte Modpacks
java/                    automatisch geladene Java-Versionen
sftp_host_ed25519_key    Schlüssel des SFTP-Servers (beim Umzug mitnehmen)
config.json              optionale Grundeinstellungen
```

Zum Sichern des ganzen Panels: Dienst stoppen und den Ordner kopieren.

### Konfiguration

Die meisten Einstellungen nimmst du in der Oberfläche unter **Administration, Einstellungen** vor. Grundlegendes geht über Startparameter, Umgebungsvariablen oder `config.json` im Datenordner:

| Umgebungsvariable | Bedeutung |
| --- | --- |
| `HEARTH_DATA` | Datenordner |
| `HEARTH_LISTEN` | Adresse und Port, z. B. `:8080` oder `127.0.0.1:8080` |
| `HEARTH_TLS_DOMAIN`, `HEARTH_TLS_EMAIL` | automatisches HTTPS |
| `HEARTH_TLS_CERT`, `HEARTH_TLS_KEY` | eigenes Zertifikat |
| `HEARTH_TRUST_PROXY=1` | hinter einem Reverse-Proxy, der `X-Forwarded-For` setzt |
| `HEARTH_MAP_LISTEN` | Adresse des Kartenports, z. B. `:8081`; `off` schaltet ihn ab |
| `HEARTH_SFTP_LISTEN` | Adresse des SFTP-Servers, Standard `:2022`; `off` schaltet ihn ab |

### Hinter einem Reverse-Proxy

Das Panel nutzt WebSockets unter `/api/ws`. Beispiel für Caddy:

```
panel.example.de {
    reverse_proxy 127.0.0.1:8080
}
```

Für nginx `proxy_http_version 1.1` sowie die Header `Upgrade` und `Connection` weiterreichen. Setze `HEARTH_TRUST_PROXY=1` und trage unter Einstellungen die öffentliche Adresse ein, damit Einladungslinks und Statusseiten stimmen.

Nutzt du Karten-Plugins, braucht der Kartenport eine eigene Adresse, am besten eine Subdomain. Trag sie unter **Administration, Einstellungen, Adresse des Kartenports** ein:

```
karte.example.de {
    reverse_proxy 127.0.0.1:8081
}
```

## Öffentliche Seiten und API

- Statusseite: `https://panel.example.de/s/<adresse>`, eingerichtet unter **Server, Öffentliche Seite**.
- Badge: `https://panel.example.de/api/public/<adresse>/badge.svg`
- Modpack: `https://panel.example.de/api/public/<adresse>/modpack.mrpack`, sobald es unter **Server, Modpack** für die öffentliche Seite freigegeben ist.
- Die öffentliche JSON-Schnittstelle liegt unter `/api/public/<adresse>` (Status, Spieler, Ereignisse, Karte).
- Für eigene Skripte und Bots: unter **Konto & Sicherheit** einen API-Schlüssel erstellen und als `Authorization: Bearer hth_…` mitsenden. Der Schlüssel hat die Rechte deines Kontos.

## Aus dem Quellcode bauen

Benötigt Go 1.26 oder neuer. Für Änderungen an der Oberfläche zusätzlich Node.js 22.

```bash
make            # Oberfläche bauen und ./dist/hearth erzeugen
make release    # Binaries für Linux, Windows und macOS
make test       # Tests und Typprüfung
make dev        # Entwicklung mit Hot-Reload
make licenses   # THIRD_PARTY_LICENSES.txt nach Änderungen an Abhängigkeiten neu erzeugen
```

Die gebaute Oberfläche liegt bereits in `internal/web/dist`, ein `go build ./cmd/hearth` funktioniert daher auch ohne Node.js.

### Aufbau

```
cmd/hearth            Einstiegspunkt und Kommandozeile
internal/api          HTTP-Schnittstelle und WebSocket
internal/servers      Lebenszyklus der Server, Abstürze, Ressourcen
internal/runtime      Ausführung direkt oder über die Docker-Engine-API
internal/minecraft    Installation, Java, RCON, Log-Auswertung, Statistiken, Modrinth
internal/mapr         Kartenrenderer für Anvil-Regionen (1.13 bis 26.x)
internal/game         Schnittstelle für weitere Spiele
internal/generic      Spieltyp „Eigener Startbefehl“
web/                  Svelte-Oberfläche (Panel und öffentliche Seite)
tools/mcsim           Simulator eines Minecraft-Servers für Tests
```

Ein weiteres Spiel implementiert das Interface `game.Game` (Installation, Start, Stopp, Bereit-Erkennung) und wird in `internal/app` registriert. Konsole, Dateien, Backups, Zeitpläne, Rechte und Benachrichtigungen funktionieren dann automatisch.

## Lizenz

Hearth steht unter der [MIT-Lizenz](LICENSE).

Eingebaute Bibliotheken und Schriften stehen unter eigenen, freien Lizenzen (MIT, ISC, BSD, Apache 2.0, SIL Open Font License 1.1; SQLite ist gemeinfrei). Ihre Lizenztexte und Urheberhinweise stehen in [THIRD_PARTY_LICENSES.txt](THIRD_PARTY_LICENSES.txt), im Panel unter `/licenses/THIRD_PARTY_LICENSES.txt` (verlinkt unter **Administration, System & Java** und auf der Anmeldeseite). `make licenses` erzeugt die Datei neu und bricht ab, wenn eine Abhängigkeit keine Lizenzdatei oder eine nicht freigegebene Lizenz hat.

Hearth enthält keine Dateien von Minecraft. Die Serversoftware (Vanilla, Paper, Purpur, Fabric, Forge, NeoForge), Java und Karten-Plugins lädt das Panel bei der Einrichtung direkt von den jeweiligen Anbietern herunter; für Minecraft-Server muss die [Minecraft-EULA](https://aka.ms/MinecraftEULA) akzeptiert werden. Minecraft ist eine Marke von Mojang Synergies AB.
