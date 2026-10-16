// Package install sets the panel up as a systemd service (Linux).
package install

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"hearth/internal/config"
)

type Options struct {
	DataDir   string
	Listen    string
	User      string
	TLSDomain string
	TLSEmail  string
	NoStart   bool
}

const (
	binTarget = "/usr/local/bin/hearth"
	unitPath  = "/etc/systemd/system/hearth.service"
)

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func step(msg string) { fmt.Printf("  \033[38;5;208m›\033[0m %s\n", msg) }

func Install(o Options) error {
	if goruntime.GOOS != "linux" {
		return errors.New("Die automatische Installation gibt es nur für Linux mit systemd. Unter Windows einfach hearth.exe starten.")
	}
	if os.Geteuid() != 0 {
		return errors.New("Bitte mit Root-Rechten ausführen: sudo ./hearth install")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("systemd wurde nicht gefunden")
	}
	if o.DataDir == "" {
		o.DataDir = "/var/lib/hearth"
	}
	if o.Listen == "" {
		o.Listen = ":8080"
	}
	if o.User == "" {
		o.User = "hearth"
	}
	fmt.Println("\n\033[1mHearth wird installiert\033[0m")

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	if exe != binTarget {
		step("Kopiere Programm nach " + binTarget)
		if err := copyFile(exe, binTarget+".new", 0o755); err != nil {
			return err
		}
		if err := os.Rename(binTarget+".new", binTarget); err != nil {
			return err
		}
	}

	if o.User != "root" {
		if _, err := user.Lookup(o.User); err != nil {
			step("Lege Systembenutzer „" + o.User + "“ an")
			if err := run("useradd", "--system", "--home-dir", o.DataDir, "--shell", "/usr/sbin/nologin", o.User); err != nil {
				return fmt.Errorf("Benutzer konnte nicht angelegt werden: %w", err)
			}
		}
		if _, err := user.LookupGroup("docker"); err == nil {
			step("Erlaube Docker-Zugriff (Gruppe docker)")
			run("usermod", "-aG", "docker", o.User)
		}
	}

	step("Datenverzeichnis " + o.DataDir)
	if err := os.MkdirAll(o.DataDir, 0o750); err != nil {
		return err
	}
	cfg := &config.Config{DataDir: o.DataDir, Listen: o.Listen, TLSDomain: o.TLSDomain, TLSEmail: o.TLSEmail}
	if _, err := os.Stat(filepath.Join(o.DataDir, "config.json")); err != nil {
		if err := cfg.Save(); err != nil {
			return err
		}
	} else {
		step("config.json existiert bereits – wird beibehalten")
	}
	if o.User != "root" {
		run("chown", "-R", o.User+":"+o.User, o.DataDir)
	}

	step("Richte systemd-Dienst ein")
	caps := ""
	if o.TLSDomain != "" || listensPrivileged(o.Listen) {
		caps = "AmbientCapabilities=CAP_NET_BIND_SERVICE\n"
	}
	unit := fmt.Sprintf(`[Unit]
Description=Hearth – Game Server Panel
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=simple
User=%[1]s
Group=%[1]s
WorkingDirectory=%[2]s
ExecStart=%[3]s serve --data %[2]s
Restart=on-failure
RestartSec=5
# stop game servers gracefully before the panel exits
KillMode=mixed
TimeoutStopSec=150
LimitNOFILE=1048576
%[4]s
[Install]
WantedBy=multi-user.target
`, o.User, o.DataDir, binTarget, caps)
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		return err
	}
	run("systemctl", "daemon-reload")
	os.Remove(filepath.Join(o.DataDir, "setup-code.txt"))
	if o.NoStart {
		run("systemctl", "enable", "hearth")
		fmt.Println("\nFertig. Starten mit: sudo systemctl start hearth")
		return nil
	}
	if err := run("systemctl", "enable", "--now", "hearth"); err != nil {
		return err
	}
	run("systemctl", "restart", "hearth")
	step("Warte auf den Start …")
	code := ""
	for i := 0; i < 60; i++ {
		if b, err := os.ReadFile(filepath.Join(o.DataDir, "setup-code.txt")); err == nil {
			code = strings.TrimSpace(string(b))
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	url := "http://<server-ip>" + o.Listen
	if strings.HasPrefix(o.Listen, ":") {
		url = "http://" + guessIP() + o.Listen
	}
	if o.TLSDomain != "" {
		url = "https://" + o.TLSDomain
	}
	fmt.Println()
	fmt.Println("\033[1;32m✔ Hearth läuft!\033[0m")
	fmt.Println("  Panel:      " + url)
	if code != "" {
		fmt.Println("  Setup-Code: \033[1m" + code + "\033[0m")
	} else {
		fmt.Println("  Setup-Code: siehe  sudo journalctl -u hearth")
	}
	fmt.Println()
	fmt.Println("  Logs:       sudo journalctl -u hearth -f")
	fmt.Println("  Firewall:   Panel, Kartenport (für Karten-Plugins), SFTP und Spielports freigeben, z. B.")
	fmt.Println("              " + firewallHint(o))
	fmt.Println()
	return nil
}

// firewallHint lists the ports to open as one ufw command line.
func firewallHint(o Options) string {
	cfg := &config.Config{Listen: o.Listen, TLSDomain: o.TLSDomain}
	var ports []string
	if o.TLSDomain != "" {
		ports = append(ports, "80", "443")
	} else if i := strings.LastIndex(o.Listen, ":"); i >= 0 {
		ports = append(ports, o.Listen[i+1:])
	}
	if m := cfg.MapAddr(); m != "" {
		if i := strings.LastIndex(m, ":"); i >= 0 {
			ports = append(ports, m[i+1:])
		}
	}
	if sf := cfg.SFTPAddr(); sf != "" {
		if i := strings.LastIndex(sf, ":"); i >= 0 {
			ports = append(ports, sf[i+1:])
		}
	}
	var parts []string
	for _, p := range ports {
		parts = append(parts, "sudo ufw allow "+p+"/tcp")
	}
	parts = append(parts, "sudo ufw allow 25565:25665/tcp", "sudo ufw allow 25565:25665/udp")
	return strings.Join(parts, " && ")
}

func listensPrivileged(listen string) bool {
	i := strings.LastIndex(listen, ":")
	if i < 0 {
		return false
	}
	var p int
	fmt.Sscanf(listen[i+1:], "%d", &p)
	return p > 0 && p < 1024
}

func guessIP() string {
	out, err := exec.Command("hostname", "-I").Output()
	if err == nil {
		if f := strings.Fields(string(out)); len(f) > 0 {
			return f[0]
		}
	}
	return "localhost"
}

func Uninstall() error {
	if os.Geteuid() != 0 {
		return errors.New("Bitte mit Root-Rechten ausführen: sudo hearth uninstall")
	}
	run("systemctl", "disable", "--now", "hearth")
	os.Remove(unitPath)
	run("systemctl", "daemon-reload")
	fmt.Println("Der Dienst wurde entfernt. Daten (z. B. /var/lib/hearth) und " + binTarget + " bleiben erhalten.")
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
