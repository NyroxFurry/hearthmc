// Command hearth is a self-hosted game server panel with first-class Minecraft support.
//
//	hearth                         start the panel (data in ./data, port 8080)
//	hearth serve --data DIR        start with a specific data directory
//	sudo hearth install            install as systemd service (Linux)
//	hearth user create NAME        manage users from the command line
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	_ "time/tzdata"

	"hearth/internal/app"
	"hearth/internal/auth"
	"hearth/internal/config"
	"hearth/internal/db"
	"hearth/internal/install"
	"hearth/internal/store"
	"hearth/internal/version"
)

func main() {
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve", "run":
		err = serve(args)
	case "install":
		err = installCmd(args)
	case "uninstall":
		err = install.Uninstall()
	case "user", "users":
		err = userCmd(args)
	case "version", "--version", "-v":
		fmt.Printf("Hearth %s %s %s\n", version.Version, version.Commit, version.Date)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "Unbekannter Befehl: %s\n\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Fehler:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`Hearth – Game Server Panel

Befehle:
  hearth [serve] [--data DIR] [--listen :8080]   Panel starten
  sudo hearth install [--data /var/lib/hearth] [--listen :8080] [--domain panel.example.com --email you@example.com]
                                                  Als systemd-Dienst installieren (Linux)
  sudo hearth uninstall                           Dienst entfernen (Daten bleiben)
  hearth user list                                Benutzer anzeigen
  hearth user create NAME [--admin]               Benutzer anlegen (Passwort wird erzeugt)
  hearth user reset-password NAME                 Neues Passwort setzen
  hearth user reset-2fa NAME                      Zwei-Faktor-Anmeldung zurücksetzen
  hearth version                                  Version anzeigen

Umgebungsvariablen: HEARTH_DATA, HEARTH_LISTEN, HEARTH_TLS_DOMAIN, HEARTH_TLS_EMAIL, HEARTH_TRUST_PROXY=1
`)
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l}))
}

func serve(args []string) error {
	cfg, err := config.Load(args)
	if err != nil {
		return err
	}
	log := newLogger(cfg.LogLevel)
	a, err := app.New(cfg, log)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return a.Run(ctx)
}

func installCmd(args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	o := install.Options{}
	fs.StringVar(&o.DataDir, "data", "/var/lib/hearth", "Datenverzeichnis")
	fs.StringVar(&o.Listen, "listen", ":8080", "Adresse des Webinterfaces")
	fs.StringVar(&o.User, "user", "hearth", "Systembenutzer für den Dienst")
	fs.StringVar(&o.TLSDomain, "domain", "", "Domain für automatisches HTTPS (Let's Encrypt)")
	fs.StringVar(&o.TLSEmail, "email", "", "E-Mail für Let's Encrypt")
	fs.BoolVar(&o.NoStart, "no-start", false, "Dienst nicht sofort starten")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return install.Install(o)
}

func userCmd(args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}
	sub := args[0]
	fs := flag.NewFlagSet("user", flag.ContinueOnError)
	dataDir := fs.String("data", "", "Datenverzeichnis")
	admin := fs.Bool("admin", false, "Administrator")
	password := fs.String("password", "", "Passwort (sonst zufällig)")
	rest := args[1:]
	var name string
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		name, rest = rest[0], rest[1:]
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	cfgArgs := []string{}
	if *dataDir != "" {
		cfgArgs = append(cfgArgs, "--data", *dataDir)
	} else if _, err := os.Stat("/var/lib/hearth/hearth.db"); err == nil && os.Getenv("HEARTH_DATA") == "" {
		if _, err := os.Stat("data/hearth.db"); err != nil {
			cfgArgs = append(cfgArgs, "--data", "/var/lib/hearth")
		}
	}
	cfg, err := config.Load(cfgArgs)
	if err != nil {
		return err
	}
	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer database.Close()
	st := store.New(database)
	ctx := context.Background()
	switch sub {
	case "list":
		users, err := st.ListUsers(ctx)
		if err != nil {
			return err
		}
		for _, u := range users {
			flags := []string{}
			if u.IsAdmin {
				flags = append(flags, "admin")
			}
			if u.Disabled {
				flags = append(flags, "deaktiviert")
			}
			if u.TOTPEnabled {
				flags = append(flags, "2FA")
			}
			fmt.Printf("%-4d %-24s %s\n", u.ID, u.Username, strings.Join(flags, ", "))
		}
	case "create":
		if name == "" {
			return fmt.Errorf("Name fehlt: hearth user create NAME")
		}
		pw := *password
		if pw == "" {
			pw = auth.RandomToken(12)
		}
		hash, err := auth.HashPassword(pw)
		if err != nil {
			return err
		}
		u := &store.User{Username: name, PasswordHash: hash, IsAdmin: *admin, CanCreateServers: *admin}
		if err := st.CreateUser(ctx, u); err != nil {
			return err
		}
		fmt.Printf("Benutzer %s angelegt. Passwort: %s\n", name, pw)
	case "reset-password":
		u, err := st.GetUserByName(ctx, name)
		if err != nil {
			return fmt.Errorf("Benutzer %q nicht gefunden", name)
		}
		pw := *password
		if pw == "" {
			pw = auth.RandomToken(12)
		}
		hash, err := auth.HashPassword(pw)
		if err != nil {
			return err
		}
		st.SetPassword(ctx, u.ID, hash)
		st.DeleteUserSessions(ctx, u.ID, "")
		fmt.Printf("Neues Passwort für %s: %s\n", u.Username, pw)
	case "reset-2fa":
		u, err := st.GetUserByName(ctx, name)
		if err != nil {
			return fmt.Errorf("Benutzer %q nicht gefunden", name)
		}
		st.SetTOTP(ctx, u.ID, "", false, nil)
		fmt.Printf("Zwei-Faktor-Anmeldung für %s deaktiviert.\n", u.Username)
	default:
		return fmt.Errorf("unbekannter Unterbefehl %q", sub)
	}
	return nil
}
