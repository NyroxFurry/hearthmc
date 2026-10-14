package config

import "testing"

func TestMapAddr(t *testing.T) {
	for _, c := range []struct {
		cfg  Config
		want string
	}{
		{Config{Listen: ":8080"}, ":8081"},
		{Config{Listen: "127.0.0.1:9000"}, "127.0.0.1:9001"},
		{Config{Listen: ":8080", TLSDomain: "panel.example.de"}, ":8443"},
		{Config{Listen: ":8080", MapListen: ":9100"}, ":9100"},
		{Config{Listen: ":8080", MapListen: "off"}, ""},
		{Config{Listen: "[::1]:8080"}, "[::1]:8081"},
	} {
		if got := c.cfg.MapAddr(); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.cfg, got, c.want)
		}
	}
}
