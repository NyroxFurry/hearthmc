package minecraft

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLogMessageFormats(t *testing.T) {
	cases := map[string]string{
		"[12:34:56] [Server thread/INFO]: Steve joined the game":                                 "Steve joined the game",
		"[12:34:56 INFO]: Steve joined the game":                                                 "Steve joined the game",
		"\x1b[36m[12:34:56 INFO]: \x1b[0mSteve joined the game":                                  "Steve joined the game",
		"[12:34:56] [Server thread/INFO] [minecraft/MinecraftServer]: Steve joined the game":     "Steve joined the game",
		"[12:34:56] [Server thread/INFO] [net.minecraft.server.MinecraftServer/]: <Steve> a]: b": "<Steve> a]: b",
		"plain output without prefix":                                                            "plain output without prefix",
		"[12:34:56 INFO]: §eSteve joined the game":                                               "Steve joined the game",
	}
	for in, want := range cases {
		if got := LogMessage(in); got != want {
			t.Errorf("LogMessage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseLine(t *testing.T) {
	type tc struct {
		msg  string
		kind LineKind
		name string
		text string
	}
	cases := []tc{
		{"UUID of player Steve is 069a79f4-44e9-4726-a5be-fca90e38aaf5", LineUUID, "Steve", ""},
		{"Steve joined the game", LineJoin, "Steve", ""},
		{"Steve[/127.0.0.1:51234] logged in with entity id 123 at (1.5, 64.0, -3.2)", LineLogin, "Steve", ""},
		{"Steve left the game", LineLeave, "Steve", ""},
		{"Steve lost connection: Disconnected", LineLostConn, "Steve", "Disconnected"},
		{"<Steve> hello world", LineChat, "Steve", "hello world"},
		{"[Not Secure] <Steve> hi", LineChat, "Steve", "hi"},
		{"Steve has made the advancement [Stone Age]", LineAdvancement, "Steve", "Stone Age"},
		{"Steve has completed the challenge [Monster Hunter]", LineAdvancement, "Steve", "Monster Hunter"},
		{`Done (12.345s)! For help, type "help"`, LineDone, "", ""},
		{`Done (3,21s)! For help, type "help" or "?"`, LineDone, "", ""},
		{"Starting minecraft server version 26.1.2", LineVersion, "", "26.1.2"},
		{"Stopping server", LineStopping, "", ""},
		{"Saved the game", LineSaved, "", ""},
		{"**** FAILED TO BIND TO PORT!", LineBindFailed, "", ""},
		{"Steve (/1.2.3.4:5678) lost connection: You are not white-listed", LineOther, "", ""},
		{"<Alex> Steve joined the game", LineChat, "Alex", "Steve joined the game"},
	}
	for _, c := range cases {
		p := ParseLine(c.msg)
		if p.Kind != c.kind || p.Name != c.name || (c.text != "" && p.Text != c.text) {
			t.Errorf("ParseLine(%q) = %+v, want kind %d name %q text %q", c.msg, p, c.kind, c.name, c.text)
		}
	}
}

func TestParseDeath(t *testing.T) {
	online := map[string]bool{"Steve": true, "Alex": true}
	isPlayer := func(n string) bool { return online[n] }
	type tc struct {
		msg, cause, killer, item string
	}
	cases := []tc{
		{"Steve was slain by Zombie", "mob", "Zombie", ""},
		{"Steve was slain by Alex", "pvp", "Alex", ""},
		{"Steve was slain by Alex using [Excalibur]", "pvp", "Alex", "Excalibur"},
		{"Steve was shot by Skeleton", "shot", "Skeleton", ""},
		{"Steve fell from a high place", "fall", "", ""},
		{"Steve hit the ground too hard while trying to escape Creeper", "fall", "Creeper", ""},
		{"Steve tried to swim in lava", "lava", "", ""},
		{"Steve drowned", "drown", "", ""},
		{"Steve was blown up by Creeper", "explosion", "Creeper", ""},
		{"Steve didn't want to live in the same world as Alex", "void", "Alex", ""},
		{"Steve was obliterated by a sonically-charged shriek", "warden", "", ""},
		{"Steve was killed by [Intentional Game Design]", "explosion", "", ""},
		{"Steve fell too far and was finished by Zombie using Iron Sword", "fall", "Zombie", "Iron Sword"},
		{"Steve was slain by Herobrine the Great", "mob", "Herobrine the Great", ""},
	}
	for _, c := range cases {
		d := ParseDeath(c.msg, isPlayer)
		if d == nil {
			t.Errorf("ParseDeath(%q) = nil", c.msg)
			continue
		}
		if d.Victim != "Steve" || d.Cause != c.cause || d.Killer != c.killer || d.Item != c.item {
			t.Errorf("ParseDeath(%q) = %+v, want cause %q killer %q item %q", c.msg, d, c.cause, c.killer, c.item)
		}
	}
	for _, msg := range []string{"Bob was slain by Zombie", "Steve joined the game", "Steve says hello", "<Steve> Alex drowned"} {
		if d := ParseDeath(msg, isPlayer); d != nil {
			t.Errorf("ParseDeath(%q) should be nil, got %+v", msg, d)
		}
	}
}

func TestPropertiesRoundTrip(t *testing.T) {
	in := "#Minecraft server properties\n#Mon Oct 13 12:00:00 CEST 2026\nmotd=\\u00A76Hallo \\u00E4\\u00F6\\u00FC Welt\nserver-port=25565\nlevel-seed=\nmax-players=20\n"
	p := ParseProperties([]byte(in))
	if v, _ := p.Get("motd"); v != "§6Hallo äöü Welt" {
		t.Fatalf("motd = %q", v)
	}
	p.Set("motd", "Neuer Server: ☃")
	p.Set("white-list", "true")
	out := string(p.Bytes())
	if !strings.HasPrefix(out, "#Minecraft server properties\n") {
		t.Errorf("comments lost: %q", out)
	}
	if !strings.Contains(out, "motd=Neuer Server: \\u2603") {
		t.Errorf("unexpected motd encoding: %q", out)
	}
	p2 := ParseProperties([]byte(out))
	if v, _ := p2.Get("motd"); v != "Neuer Server: ☃" {
		t.Errorf("round trip motd = %q", v)
	}
	if v, _ := p2.Get("white-list"); v != "true" {
		t.Errorf("white-list = %q", v)
	}
	if v, ok := p2.Get("level-seed"); !ok || v != "" {
		t.Errorf("empty value lost")
	}
}

func TestVersions(t *testing.T) {
	if CompareVersions("1.21.10", "1.21.9") <= 0 {
		t.Error("1.21.10 should be newer than 1.21.9")
	}
	if CompareVersions("26.1", "1.21.11") <= 0 {
		t.Error("26.1 should be newer than 1.21.11")
	}
	if CompareVersions("1.21.9-rc1", "1.21.9") >= 0 {
		t.Error("rc should be older than release")
	}
	for v, want := range map[string]int{"1.8.9": 8, "1.16.5": 8, "1.17.1": 17, "1.18.2": 17, "1.20.4": 17, "1.20.5": 21, "1.21.11": 21, "26.1": 25, "26.3.1": 25} {
		if got := GuessJava(v); got != want {
			t.Errorf("GuessJava(%s) = %d, want %d", v, got, want)
		}
	}
	for nf, mc := range map[string]string{"21.1.77": "1.21.1", "21.0.5-beta": "1.21", "20.4.237": "1.20.4", "26.1.0.15-beta": "26.1", "26.1.1.3": "26.1.1"} {
		if got := NeoForgeMC(nf); got != mc {
			t.Errorf("NeoForgeMC(%s) = %s, want %s", nf, got, mc)
		}
	}
}

func TestUUIDs(t *testing.T) {
	if got := OfflineUUID("Notch"); got != "b50ad385-829d-3141-a216-7e7d7539ba7f" {
		t.Errorf("OfflineUUID(Notch) = %s", got)
	}
	if got := NormalizeUUID("069A79F444E94726A5BEFCA90E38AAF5"); got != "069a79f4-44e9-4726-a5be-fca90e38aaf5" {
		t.Errorf("NormalizeUUID = %s", got)
	}
	if NormalizeUUID("../../etc/passwd") != "" {
		t.Error("invalid uuid accepted")
	}
}

func TestChatToLegacy(t *testing.T) {
	raw := json.RawMessage(`{"text":"","extra":[{"text":"Hearth ","color":"gold","bold":true},{"text":"Server","color":"#ff0000"}]}`)
	got := ChatToLegacy(raw)
	if got != "§6§lHearth §x§f§f§0§0§0§0Server" {
		t.Errorf("ChatToLegacy = %q", got)
	}
	if ChatToLegacy(json.RawMessage(`"plain"`)) != "plain" {
		t.Error("string description")
	}
}

func TestParseStats(t *testing.T) {
	data := []byte(`{"stats":{"minecraft:custom":{"minecraft:play_time":72000,"minecraft:deaths":3,"minecraft:walk_one_cm":1000,"minecraft:fall_one_cm":500,"minecraft:sprint_one_cm":200},"minecraft:mined":{"minecraft:stone":10,"minecraft:dirt":5}},"DataVersion":3955}`)
	u, err := ParseStats("x", data)
	if err != nil {
		t.Fatal(err)
	}
	if u.PlayTicks != 72000 || u.Deaths != 3 || u.DistanceCM != 1200 || u.BlocksMined != 15 {
		t.Errorf("unexpected stats %+v", u)
	}
	old := []byte(`{"stats":{"minecraft:custom":{"minecraft:play_one_minute":1200}}}`)
	if u, _ := ParseStats("x", old); u.PlayTicks != 1200 {
		t.Errorf("legacy play time = %d", u.PlayTicks)
	}
	adv := []byte(`{"minecraft:story/root":{"criteria":{},"done":true},"minecraft:recipes/misc/x":{"done":true},"minecraft:story/mine_stone":{"done":false},"DataVersion":3955}`)
	if n := CountAdvancements(adv); n != 1 {
		t.Errorf("CountAdvancements = %d", n)
	}
}

func TestSplitArgs(t *testing.T) {
	got := SplitArgs(`-Xmx4G "-Dfoo=bar baz" -jar 'my server.jar' nogui`)
	want := []string{"-Xmx4G", "-Dfoo=bar baz", "-jar", "my server.jar", "nogui"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("SplitArgs = %q", got)
	}
}

func TestPluginMetaParsers(t *testing.T) {
	kv, lists := parseSimpleYAML("name: EssentialsX\nversion: '2.21.0'\nauthors: [Zenexer, md678685]\ndescription: \"Provides an essential, core set of commands\"\ncommands:\n  home:\n    description: x\n")
	if kv["name"] != "EssentialsX" || kv["version"] != "2.21.0" || len(lists["authors"]) != 2 || !strings.HasPrefix(kv["description"], "Provides") {
		t.Errorf("yaml: %v %v", kv, lists)
	}
	toml := `modLoader="javafml"
loaderVersion="[47,)"
license="MIT"
[[mods]]
modId="examplemod"
version="1.2.3"
displayName="Example Mod"
description='''
A multi line
description.
'''
[[dependencies.examplemod]]
modId="forge"
`
	m := parseModsToml(toml)
	if m["modId"] != "examplemod" || m["version"] != "1.2.3" || m["displayName"] != "Example Mod" || !strings.Contains(m["description"], "multi line") {
		t.Errorf("toml: %v", m)
	}
}
