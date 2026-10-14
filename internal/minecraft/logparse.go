package minecraft

import (
	"regexp"
	"sort"
	"strings"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// StripANSI removes terminal escape sequences.
func StripANSI(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	return ansiRe.ReplaceAllString(s, "")
}

// LogMessage extracts the message part of a server log line, e.g.
//
//	[12:34:56] [Server thread/INFO]: Steve joined the game          (vanilla, Fabric)
//	[12:34:56 INFO]: Steve joined the game                          (Paper, Purpur)
//	[12:34:56] [Server thread/INFO] [minecraft/DedicatedServer]: … (Forge, NeoForge)
func LogMessage(line string) string {
	line = StripLegacy(StripANSI(line))
	line = strings.TrimRight(line, "\r ")
	if strings.HasPrefix(line, "[") {
		if i := strings.Index(line, "]: "); i >= 0 {
			return line[i+3:]
		}
	} else if strings.HasPrefix(line, ">") { // jline prompt artefacts
		return strings.TrimLeft(line, "> ")
	}
	return line
}

type LineKind int

const (
	LineOther LineKind = iota
	LineUUID
	LineJoin
	LineLogin
	LineLeave
	LineLostConn
	LineChat
	LineAdvancement
	LineDone
	LineVersion
	LineStopping
	LineSaved
	LineBindFailed
	LineEULA
)

type Parsed struct {
	Kind    LineKind
	Name    string
	UUID    string
	Text    string // chat message, advancement title, version
	AdvType string // made / completed / reached
}

var (
	reUUID     = regexp.MustCompile(`^UUID of player (\S+) is ([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$`)
	reJoin     = regexp.MustCompile(`^(\S+) joined the game$`)
	reLogin    = regexp.MustCompile(`^(\S+?)\[[^\]]*\] logged in with entity id \d+`)
	reLeave    = regexp.MustCompile(`^(\S+) left the game$`)
	reLostConn = regexp.MustCompile(`^(\S+) lost connection: (.*)$`)
	reChat     = regexp.MustCompile(`^(?:\[Not Secure\] )?<([^>\s]+)> (.*)$`)
	reAdv      = regexp.MustCompile(`^(\S+) has (made the advancement|completed the challenge|reached the goal) \[(.+)\]$`)
	reDone     = regexp.MustCompile(`^Done \(([\d.,]+)s\)! For help, type`)
	reVersion  = regexp.MustCompile(`^Starting minecraft server version (.+)$`)
	reStopping = regexp.MustCompile(`^Stopping (the )?server$`)
	reSaved    = regexp.MustCompile(`^Saved the (game|world)$`)
)

// ParseLine classifies a log message (already passed through LogMessage).
func ParseLine(msg string) Parsed {
	switch {
	case strings.HasPrefix(msg, "<") || strings.HasPrefix(msg, "[Not Secure] <"):
		if m := reChat.FindStringSubmatch(msg); m != nil {
			return Parsed{Kind: LineChat, Name: m[1], Text: m[2]}
		}
	case strings.HasPrefix(msg, "UUID of player "):
		if m := reUUID.FindStringSubmatch(msg); m != nil {
			return Parsed{Kind: LineUUID, Name: m[1], UUID: strings.ToLower(m[2])}
		}
	case strings.HasSuffix(msg, " joined the game"):
		if m := reJoin.FindStringSubmatch(msg); m != nil {
			return Parsed{Kind: LineJoin, Name: m[1]}
		}
	case strings.HasSuffix(msg, " left the game"):
		if m := reLeave.FindStringSubmatch(msg); m != nil {
			return Parsed{Kind: LineLeave, Name: m[1]}
		}
	case strings.Contains(msg, " logged in with entity id "):
		if m := reLogin.FindStringSubmatch(msg); m != nil {
			return Parsed{Kind: LineLogin, Name: m[1]}
		}
	case strings.Contains(msg, " lost connection: "):
		if m := reLostConn.FindStringSubmatch(msg); m != nil {
			return Parsed{Kind: LineLostConn, Name: m[1], Text: m[2]}
		}
	case strings.Contains(msg, " has made the advancement [") || strings.Contains(msg, " has completed the challenge [") || strings.Contains(msg, " has reached the goal ["):
		if m := reAdv.FindStringSubmatch(msg); m != nil {
			t := "advancement"
			switch m[2] {
			case "completed the challenge":
				t = "challenge"
			case "reached the goal":
				t = "goal"
			}
			return Parsed{Kind: LineAdvancement, Name: m[1], Text: m[3], AdvType: t}
		}
	case strings.HasPrefix(msg, "Done ("):
		if reDone.MatchString(msg) {
			return Parsed{Kind: LineDone}
		}
	case strings.HasPrefix(msg, "Starting minecraft server version "):
		if m := reVersion.FindStringSubmatch(msg); m != nil {
			return Parsed{Kind: LineVersion, Text: strings.TrimSpace(m[1])}
		}
	case strings.HasPrefix(msg, "Stopping "):
		if reStopping.MatchString(msg) {
			return Parsed{Kind: LineStopping}
		}
	case strings.HasPrefix(msg, "Saved the "):
		if reSaved.MatchString(msg) {
			return Parsed{Kind: LineSaved}
		}
	case strings.Contains(msg, "FAILED TO BIND TO PORT"):
		return Parsed{Kind: LineBindFailed}
	case strings.HasPrefix(msg, "You need to agree to the EULA"):
		return Parsed{Kind: LineEULA}
	}
	return Parsed{Kind: LineOther}
}

// ---- deaths ----

// DeathTemplate is one of Minecraft's death messages (en_us).
type DeathTemplate struct {
	Key      string
	Cause    string // grouping key for statistics
	Template string // %1$s victim, %2$s killer, %3$s item
	re       *regexp.Regexp
	prefix   string
}

type Death struct {
	Victim string `json:"victim"`
	Killer string `json:"killer,omitempty"`
	Item   string `json:"item,omitempty"`
	Cause  string `json:"cause"`
	Key    string `json:"key"`
}

var deathTemplates = []DeathTemplate{
	{Key: "anvil", Cause: "squashed", Template: "%1$s was squashed by a falling anvil"},
	{Key: "anvil.player", Cause: "squashed", Template: "%1$s was squashed by a falling anvil while fighting %2$s"},
	{Key: "arrow", Cause: "shot", Template: "%1$s was shot by %2$s"},
	{Key: "arrow.item", Cause: "shot", Template: "%1$s was shot by %2$s using %3$s"},
	{Key: "badRespawnPoint", Cause: "explosion", Template: "%1$s was killed by [Intentional Game Design]"},
	{Key: "cactus", Cause: "cactus", Template: "%1$s was pricked to death"},
	{Key: "cactus.player", Cause: "cactus", Template: "%1$s walked into a cactus while trying to escape %2$s"},
	{Key: "cramming", Cause: "cramming", Template: "%1$s was squished too much"},
	{Key: "cramming.player", Cause: "cramming", Template: "%1$s was squashed by %2$s"},
	{Key: "dragonBreath", Cause: "dragon", Template: "%1$s was roasted in dragon's breath"},
	{Key: "dragonBreath.player", Cause: "dragon", Template: "%1$s was roasted in dragon's breath by %2$s"},
	{Key: "drown", Cause: "drown", Template: "%1$s drowned"},
	{Key: "drown.player", Cause: "drown", Template: "%1$s drowned while trying to escape %2$s"},
	{Key: "dryout", Cause: "dryout", Template: "%1$s died from dehydration"},
	{Key: "dryout.player", Cause: "dryout", Template: "%1$s died from dehydration while trying to escape %2$s"},
	{Key: "even_more_magic", Cause: "magic", Template: "%1$s was killed by even more magic"},
	{Key: "explosion", Cause: "explosion", Template: "%1$s blew up"},
	{Key: "explosion.player", Cause: "explosion", Template: "%1$s was blown up by %2$s"},
	{Key: "explosion.player.item", Cause: "explosion", Template: "%1$s was blown up by %2$s using %3$s"},
	{Key: "fall", Cause: "fall", Template: "%1$s hit the ground too hard"},
	{Key: "fall.player", Cause: "fall", Template: "%1$s hit the ground too hard while trying to escape %2$s"},
	{Key: "fallingBlock", Cause: "squashed", Template: "%1$s was squashed by a falling block"},
	{Key: "fallingBlock.player", Cause: "squashed", Template: "%1$s was squashed by a falling block while fighting %2$s"},
	{Key: "fallingStalactite", Cause: "stalactite", Template: "%1$s was skewered by a falling stalactite"},
	{Key: "fallingStalactite.player", Cause: "stalactite", Template: "%1$s was skewered by a falling stalactite while fighting %2$s"},
	{Key: "fireball", Cause: "fire", Template: "%1$s was fireballed by %2$s"},
	{Key: "fireball.item", Cause: "fire", Template: "%1$s was fireballed by %2$s using %3$s"},
	{Key: "fireworks", Cause: "explosion", Template: "%1$s went off with a bang"},
	{Key: "fireworks.item", Cause: "explosion", Template: "%1$s went off with a bang due to a firework fired from %3$s by %2$s"},
	{Key: "fireworks.player", Cause: "explosion", Template: "%1$s went off with a bang while fighting %2$s"},
	{Key: "flyIntoWall", Cause: "kinetic", Template: "%1$s experienced kinetic energy"},
	{Key: "flyIntoWall.player", Cause: "kinetic", Template: "%1$s experienced kinetic energy while trying to escape %2$s"},
	{Key: "freeze", Cause: "freeze", Template: "%1$s froze to death"},
	{Key: "freeze.player", Cause: "freeze", Template: "%1$s was frozen to death by %2$s"},
	{Key: "generic", Cause: "generic", Template: "%1$s died"},
	{Key: "generic.player", Cause: "generic", Template: "%1$s died because of %2$s"},
	{Key: "genericKill", Cause: "generic", Template: "%1$s was killed"},
	{Key: "genericKill.player", Cause: "generic", Template: "%1$s was killed while fighting %2$s"},
	{Key: "hotFloor", Cause: "magma", Template: "%1$s discovered the floor was lava"},
	{Key: "hotFloor.player", Cause: "magma", Template: "%1$s walked into the danger zone due to %2$s"},
	{Key: "inFire", Cause: "fire", Template: "%1$s went up in flames"},
	{Key: "inFire.player", Cause: "fire", Template: "%1$s walked into fire while fighting %2$s"},
	{Key: "inWall", Cause: "suffocation", Template: "%1$s suffocated in a wall"},
	{Key: "inWall.player", Cause: "suffocation", Template: "%1$s suffocated in a wall while fighting %2$s"},
	{Key: "indirectMagic", Cause: "magic", Template: "%1$s was killed by %2$s using magic"},
	{Key: "indirectMagic.item", Cause: "magic", Template: "%1$s was killed by %2$s using %3$s"},
	{Key: "lava", Cause: "lava", Template: "%1$s tried to swim in lava"},
	{Key: "lava.player", Cause: "lava", Template: "%1$s tried to swim in lava to escape %2$s"},
	{Key: "lightningBolt", Cause: "lightning", Template: "%1$s was struck by lightning"},
	{Key: "lightningBolt.player", Cause: "lightning", Template: "%1$s was struck by lightning while fighting %2$s"},
	{Key: "mace_smash", Cause: "mob", Template: "%1$s was smashed by %2$s"},
	{Key: "mace_smash.item", Cause: "mob", Template: "%1$s was smashed by %2$s with %3$s"},
	{Key: "magic", Cause: "magic", Template: "%1$s was killed by magic"},
	{Key: "magic.player", Cause: "magic", Template: "%1$s was killed by magic while trying to escape %2$s"},
	{Key: "mob", Cause: "mob", Template: "%1$s was slain by %2$s"},
	{Key: "mob.item", Cause: "mob", Template: "%1$s was slain by %2$s using %3$s"},
	{Key: "onFire", Cause: "fire", Template: "%1$s burned to death"},
	{Key: "onFire.item", Cause: "fire", Template: "%1$s was burned to a crisp while fighting %2$s wielding %3$s"},
	{Key: "onFire.player", Cause: "fire", Template: "%1$s was burned to a crisp while fighting %2$s"},
	{Key: "outOfWorld", Cause: "void", Template: "%1$s fell out of the world"},
	{Key: "outOfWorld.player", Cause: "void", Template: "%1$s didn't want to live in the same world as %2$s"},
	{Key: "outsideBorder", Cause: "border", Template: "%1$s left the confines of this world"},
	{Key: "outsideBorder.player", Cause: "border", Template: "%1$s left the confines of this world while fighting %2$s"},
	{Key: "sonic_boom", Cause: "warden", Template: "%1$s was obliterated by a sonically-charged shriek"},
	{Key: "sonic_boom.item", Cause: "warden", Template: "%1$s was obliterated by a sonically-charged shriek while trying to escape %2$s wielding %3$s"},
	{Key: "sonic_boom.player", Cause: "warden", Template: "%1$s was obliterated by a sonically-charged shriek while trying to escape %2$s"},
	{Key: "stalagmite", Cause: "stalagmite", Template: "%1$s was impaled on a stalagmite"},
	{Key: "stalagmite.player", Cause: "stalagmite", Template: "%1$s was impaled on a stalagmite while fighting %2$s"},
	{Key: "starve", Cause: "starve", Template: "%1$s starved to death"},
	{Key: "starve.player", Cause: "starve", Template: "%1$s starved to death while fighting %2$s"},
	{Key: "sting", Cause: "sting", Template: "%1$s was stung to death"},
	{Key: "sting.item", Cause: "sting", Template: "%1$s was stung to death by %2$s using %3$s"},
	{Key: "sting.player", Cause: "sting", Template: "%1$s was stung to death by %2$s"},
	{Key: "sweetBerryBush", Cause: "berrybush", Template: "%1$s was poked to death by a sweet berry bush"},
	{Key: "sweetBerryBush.player", Cause: "berrybush", Template: "%1$s was poked to death by a sweet berry bush while trying to escape %2$s"},
	{Key: "thorns", Cause: "thorns", Template: "%1$s was killed while trying to hurt %2$s"},
	{Key: "thorns.item", Cause: "thorns", Template: "%1$s was killed by %3$s while trying to hurt %2$s"},
	{Key: "thrown", Cause: "mob", Template: "%1$s was pummeled by %2$s"},
	{Key: "thrown.item", Cause: "mob", Template: "%1$s was pummeled by %2$s using %3$s"},
	{Key: "trident", Cause: "mob", Template: "%1$s was impaled by %2$s"},
	{Key: "trident.item", Cause: "mob", Template: "%1$s was impaled by %2$s with %3$s"},
	{Key: "wither", Cause: "wither", Template: "%1$s withered away"},
	{Key: "wither.player", Cause: "wither", Template: "%1$s withered away while fighting %2$s"},
	{Key: "witherSkull", Cause: "wither", Template: "%1$s was shot by a skull from %2$s"},
	{Key: "witherSkull.item", Cause: "wither", Template: "%1$s was shot by a skull from %2$s using %3$s"},
	{Key: "fell.accident.generic", Cause: "fall", Template: "%1$s fell from a high place"},
	{Key: "fell.accident.ladder", Cause: "fall", Template: "%1$s fell off a ladder"},
	{Key: "fell.accident.other_climbable", Cause: "fall", Template: "%1$s fell while climbing"},
	{Key: "fell.accident.scaffolding", Cause: "fall", Template: "%1$s fell off scaffolding"},
	{Key: "fell.accident.twisting_vines", Cause: "fall", Template: "%1$s fell off some twisting vines"},
	{Key: "fell.accident.vines", Cause: "fall", Template: "%1$s fell off some vines"},
	{Key: "fell.accident.weeping_vines", Cause: "fall", Template: "%1$s fell off some weeping vines"},
	{Key: "fell.assist", Cause: "fall", Template: "%1$s was doomed to fall by %2$s"},
	{Key: "fell.assist.item", Cause: "fall", Template: "%1$s was doomed to fall by %2$s using %3$s"},
	{Key: "fell.finish", Cause: "fall", Template: "%1$s fell too far and was finished by %2$s"},
	{Key: "fell.finish.item", Cause: "fall", Template: "%1$s fell too far and was finished by %2$s using %3$s"},
	{Key: "fell.killer", Cause: "fall", Template: "%1$s was doomed to fall"},
	{Key: "spit", Cause: "mob", Template: "%1$s was spit on by %2$s"},
	{Key: "enderPearl", Cause: "fall", Template: "%1$s was killed by an ender pearl"},
}

// DeathCauses maps cause keys to German labels.
var DeathCauses = map[string]string{
	"mob": "Kreatur", "shot": "Erschossen", "fall": "Sturz", "lava": "Lava", "fire": "Feuer", "drown": "Ertrunken",
	"explosion": "Explosion", "void": "Leere", "starve": "Verhungert", "suffocation": "Erstickt", "magic": "Magie",
	"wither": "Wither", "freeze": "Erfroren", "lightning": "Blitz", "cactus": "Kaktus", "squashed": "Zerquetscht",
	"kinetic": "Elytra-Aufprall", "magma": "Magmablock", "warden": "Warden", "dragon": "Drachenatem",
	"sting": "Gestochen", "berrybush": "Beerenstrauch", "thorns": "Dornen", "border": "Weltgrenze",
	"stalactite": "Tropfstein", "stalagmite": "Tropfstein", "cramming": "Gedränge", "dryout": "Ausgetrocknet",
	"generic": "Sonstiges", "pvp": "PvP",
}

func init() {
	for i := range deathTemplates {
		t := &deathTemplates[i]
		pattern := regexp.QuoteMeta(t.Template)
		pattern = strings.Replace(pattern, `%1\$s`, `(?P<victim>\S+)`, 1)
		pattern = strings.Replace(pattern, `%2\$s`, `(?P<killer>.+?)`, 1)
		pattern = strings.Replace(pattern, `%3\$s`, `(?P<item>.+?)`, 1)
		t.re = regexp.MustCompile("^" + pattern + "$")
		t.prefix = strings.SplitN(strings.TrimPrefix(t.Template, "%1$s "), "%", 2)[0]
	}
	// try longer (more specific) templates first
	sort.SliceStable(deathTemplates, func(i, j int) bool {
		return len(deathTemplates[i].Template) > len(deathTemplates[j].Template)
	})
}

// ParseDeath matches a log message against all death messages. isPlayer
// reports whether a name belongs to a known online player (prevents chat
// lines or plugin output from being counted).
func ParseDeath(msg string, isPlayer func(string) bool) *Death {
	sp := strings.IndexByte(msg, ' ')
	if sp <= 0 || !isPlayer(msg[:sp]) {
		return nil
	}
	rest := msg[sp+1:]
	for i := range deathTemplates {
		t := &deathTemplates[i]
		if !strings.HasPrefix(rest, t.prefix) {
			continue
		}
		m := t.re.FindStringSubmatch(msg)
		if m == nil {
			continue
		}
		d := &Death{Cause: t.Cause, Key: t.Key}
		for i, n := range t.re.SubexpNames() {
			switch n {
			case "victim":
				d.Victim = m[i]
			case "killer":
				d.Killer = m[i]
			case "item":
				d.Item = strings.Trim(m[i], "[]")
			}
		}
		if d.Killer != "" && isPlayer(d.Killer) && (t.Cause == "mob" || t.Cause == "shot") {
			d.Cause = "pvp"
		}
		return d
	}
	return nil
}
