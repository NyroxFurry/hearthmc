// Command mcsim simulates a Minecraft server for testing Hearth without
// downloading Mojang software. It prints realistic console output (Paper or
// vanilla style), answers RCON and Server List Ping, writes stats,
// usercache and playerdata files, and simulates players joining, dying,
// chatting and earning advancements.
//
// Use it as a fake "java": set the server's Java path to a script that runs
// mcsim with any arguments.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type player struct {
	Name    string
	UUID    string
	Online  bool
	X, Y, Z float64
	Dim     string
	Stats   map[string]int64
	Joined  time.Time
}

type sim struct {
	mu       sync.Mutex
	style    string
	props    map[string]string
	players  []*player
	world    string
	modern   bool
	saveOff  bool
	start    time.Time
	stopping bool
	ops      []map[string]any
	white    []map[string]any
}

var names = []string{"Moosbart", "Kieselkind", "Steve", "Alex", "Pixelfuchs", "BuilderBob", "CreeperKing", "Luna_MC"}

var deaths = []string{
	"%s was slain by Zombie", "%s fell from a high place", "%s tried to swim in lava", "%s drowned",
	"%s was shot by Skeleton", "%s blew up", "%s was blown up by Creeper", "%s hit the ground too hard",
	"%s starved to death", "%s was slain by %s", "%s fell out of the world", "%s was killed by magic",
}

var advancements = []string{"Stone Age", "Getting an Upgrade", "Acquire Hardware", "We Need to Go Deeper", "Diamonds!", "Monster Hunter", "Sweet Dreams"}
var chats = []string{"hi zusammen", "wer hat lust auf ne farm?", "lol", "brb", "gg", "wo ist das dorf?", "diamanten gefunden!!"}

func uuidFor(name string) string {
	h := md5.Sum([]byte("sim:" + name))
	h[6] = h[6]&0x0f | 0x40
	h[8] = h[8]&0x3f | 0x80
	s := hex.EncodeToString(h[:])
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
}

func (s *sim) log(level, msg string) {
	now := time.Now().Format("15:04:05")
	if s.style == "vanilla" {
		fmt.Printf("[%s] [Server thread/%s]: %s\n", now, level, msg)
	} else {
		fmt.Printf("\x1b[36m[%s %s]: \x1b[0m%s\n", now, level, msg)
	}
}

func readProps(path string) map[string]string {
	m := map[string]string{}
	b, _ := os.ReadFile(path)
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "#") {
			continue
		}
		if k, v, ok := strings.Cut(l, "="); ok {
			m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return m
}

func main() {
	s := &sim{style: os.Getenv("MCSIM_STYLE"), start: time.Now()}
	if s.style == "" {
		s.style = "paper"
	}
	s.props = readProps("server.properties")
	if !strings.Contains(readFile("eula.txt"), "eula=true") {
		s.log("INFO", "You need to agree to the EULA in order to run the server. Go to eula.txt for more info.")
		os.Exit(1)
	}
	s.world = s.props["level-name"]
	if s.world == "" {
		s.world = "world"
	}
	s.modern = os.Getenv("MCSIM_LAYOUT") == "modern"
	n := 5
	if v, err := strconv.Atoi(os.Getenv("MCSIM_PLAYERS")); err == nil {
		n = v
	}
	for i := 0; i < n && i < len(names); i++ {
		s.players = append(s.players, &player{Name: names[i], UUID: uuidFor(names[i]), Stats: map[string]int64{},
			X: rand.Float64()*400 - 200, Y: 64 + rand.Float64()*20, Z: rand.Float64()*400 - 200, Dim: "minecraft:overworld"})
	}
	port := atoi(s.props["server-port"], 25565)
	s.log("INFO", "Starting minecraft server version 1.21.1")
	s.log("INFO", "Loading properties")
	s.log("INFO", "This server is running Paper version 1.21.1-130 (MC: 1.21.1)")
	s.log("INFO", fmt.Sprintf("Starting Minecraft server on *:%d", port))
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		s.log("WARN", "**** FAILED TO BIND TO PORT!")
		s.log("WARN", "The exception was: java.net.BindException: Address already in use")
		os.Exit(1)
	}
	go s.serveSLP(ln)
	s.log("INFO", fmt.Sprintf("Preparing level \"%s\"", s.world))
	for _, p := range []int{0, 18, 57, 100} {
		time.Sleep(150 * time.Millisecond)
		s.log("INFO", fmt.Sprintf("Preparing spawn area: %d%%", p))
	}
	os.MkdirAll(s.statsDir(), 0o755)
	os.MkdirAll(s.playerDir(), 0o755)
	if s.props["enable-rcon"] == "true" {
		rport := atoi(s.props["rcon.port"], 25575)
		if rl, err := net.Listen("tcp", fmt.Sprintf(":%d", rport)); err == nil {
			s.log("INFO", fmt.Sprintf("RCON running on 0.0.0.0:%d", rport))
			go s.serveRCON(rl)
		}
	}
	s.serveMapPlugins()
	s.log("INFO", fmt.Sprintf("Done (%.3fs)! For help, type \"help\"", time.Since(s.start).Seconds()))

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sig
		s.stop()
	}()
	go s.simulate()
	go s.autosave()
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		out := s.exec(line, false)
		for _, l := range strings.Split(out, "\n") {
			if l != "" {
				s.log("INFO", l)
			}
		}
	}
	// stdin closed (panel gone): keep running like a real server would
	select {}
}

func readFile(p string) string {
	b, _ := os.ReadFile(p)
	return string(b)
}

func atoi(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

func (s *sim) statsDir() string {
	if s.modern {
		return filepath.Join(s.world, "players", "stats")
	}
	return filepath.Join(s.world, "stats")
}

func (s *sim) playerDir() string {
	if s.modern {
		return filepath.Join(s.world, "players", "data")
	}
	return filepath.Join(s.world, "playerdata")
}

func (s *sim) online() []*player {
	var out []*player
	for _, p := range s.players {
		if p.Online {
			out = append(out, p)
		}
	}
	return out
}

func (s *sim) find(name string) *player {
	for _, p := range s.players {
		if strings.EqualFold(p.Name, name) {
			return p
		}
	}
	return nil
}

func (s *sim) simulate() {
	interval := 2 * time.Second
	if v, err := time.ParseDuration(os.Getenv("MCSIM_INTERVAL")); err == nil {
		interval = v
	}
	for {
		time.Sleep(interval + time.Duration(rand.Intn(1000))*time.Millisecond)
		s.mu.Lock()
		if s.stopping {
			s.mu.Unlock()
			return
		}
		on := s.online()
		var offline []*player
		for _, p := range s.players {
			if !p.Online {
				offline = append(offline, p)
			}
		}
		r := rand.Intn(100)
		switch {
		case (r < 35 || len(on) == 0) && len(offline) > 0:
			p := offline[rand.Intn(len(offline))]
			p.Online, p.Joined = true, time.Now()
			s.logAsync("INFO", fmt.Sprintf("UUID of player %s is %s", p.Name, p.UUID))
			s.log("INFO", fmt.Sprintf("%s[/127.0.0.1:%d] logged in with entity id %d at ([%s]%.1f, %.1f, %.1f)", p.Name, 40000+rand.Intn(20000), rand.Intn(9999), s.world, p.X, p.Y, p.Z))
			s.log("INFO", fmt.Sprintf("%s joined the game", p.Name))
			s.addUsercache(p)
		case r < 45 && len(on) > 1:
			p := on[rand.Intn(len(on))]
			s.leave(p, "Disconnected")
		case r < 65 && len(on) > 0:
			p := on[rand.Intn(len(on))]
			t := deaths[rand.Intn(len(deaths))]
			var msg string
			if strings.Count(t, "%s") == 2 {
				killer := on[rand.Intn(len(on))]
				msg = fmt.Sprintf(t, p.Name, killer.Name)
				if killer != p {
					killer.Stats["minecraft:player_kills"]++
				}
			} else {
				msg = fmt.Sprintf(t, p.Name)
			}
			p.Stats["minecraft:deaths"]++
			s.log("INFO", msg)
		case r < 80 && len(on) > 0:
			p := on[rand.Intn(len(on))]
			s.log("INFO", fmt.Sprintf("<%s> %s", p.Name, chats[rand.Intn(len(chats))]))
		case r < 90 && len(on) > 0:
			p := on[rand.Intn(len(on))]
			s.log("INFO", fmt.Sprintf("%s has made the advancement [%s]", p.Name, advancements[rand.Intn(len(advancements))]))
		default:
			for _, p := range on {
				p.Stats["minecraft:mob_kills"] += int64(rand.Intn(3))
				p.Stats["minecraft:jump"] += int64(rand.Intn(20))
			}
		}
		for _, p := range s.online() {
			p.X += rand.Float64()*30 - 15
			p.Z += rand.Float64()*30 - 15
			if rand.Intn(40) == 0 {
				if p.Dim == "minecraft:overworld" {
					p.Dim = "minecraft:the_nether"
				} else {
					p.Dim = "minecraft:overworld"
				}
			}
		}
		s.mu.Unlock()
	}
}

func (s *sim) logAsync(level, msg string) {
	now := time.Now().Format("15:04:05")
	if s.style == "vanilla" {
		fmt.Printf("[%s] [User Authenticator #1/%s]: %s\n", now, level, msg)
	} else {
		fmt.Printf("[%s %s]: %s\n", now, level, msg)
	}
}

func (s *sim) leave(p *player, reason string) {
	p.Online = false
	p.Stats["minecraft:play_time"] += int64(time.Since(p.Joined).Seconds() * 20)
	p.Stats["minecraft:leave_game"]++
	s.log("INFO", fmt.Sprintf("%s lost connection: %s", p.Name, reason))
	s.log("INFO", fmt.Sprintf("%s left the game", p.Name))
}

func (s *sim) addUsercache(p *player) {
	var list []map[string]string
	json.Unmarshal([]byte(readFile("usercache.json")), &list)
	for _, e := range list {
		if e["uuid"] == p.UUID {
			return
		}
	}
	list = append(list, map[string]string{"name": p.Name, "uuid": p.UUID, "expiresOn": time.Now().AddDate(0, 1, 0).Format("2006-01-02 15:04:05 -0700")})
	b, _ := json.Marshal(list)
	os.WriteFile("usercache.json", b, 0o644)
}

func (s *sim) autosave() {
	for {
		time.Sleep(10 * time.Second)
		s.mu.Lock()
		if !s.saveOff && !s.stopping {
			s.save()
		}
		s.mu.Unlock()
	}
}

// save writes stats and playerdata like the real server does on autosave.
func (s *sim) save() {
	s.writeRegions()
	for _, p := range s.players {
		if len(p.Stats) == 0 && !p.Online {
			continue
		}
		play := p.Stats["minecraft:play_time"]
		if p.Online {
			play += int64(time.Since(p.Joined).Seconds() * 20)
		}
		custom := map[string]int64{}
		for k, v := range p.Stats {
			custom[k] = v
		}
		custom["minecraft:play_time"] = play
		custom["minecraft:walk_one_cm"] = play * 7
		custom["minecraft:sprint_one_cm"] = play * 3
		doc := map[string]any{"stats": map[string]any{
			"minecraft:custom": custom,
			"minecraft:mined":  map[string]int64{"minecraft:stone": play / 40, "minecraft:dirt": play / 90, "minecraft:diamond_ore": play / 4000},
			"minecraft:killed": map[string]int64{"minecraft:zombie": custom["minecraft:mob_kills"]},
		}, "DataVersion": 3955}
		b, _ := json.Marshal(doc)
		os.WriteFile(filepath.Join(s.statsDir(), p.UUID+".json"), b, 0o644)
		writePlayerdata(filepath.Join(s.playerDir(), p.UUID+".dat"), p)
	}
}

// ---- minimal NBT writer for playerdata ----

func writePlayerdata(path string, p *player) {
	var b bytes.Buffer
	str := func(s string) {
		binary.Write(&b, binary.BigEndian, uint16(len(s)))
		b.WriteString(s)
	}
	b.WriteByte(10)
	str("")
	b.WriteByte(9) // list
	str("Pos")
	b.WriteByte(6) // of doubles
	binary.Write(&b, binary.BigEndian, int32(3))
	for _, v := range []float64{p.X, p.Y, p.Z} {
		binary.Write(&b, binary.BigEndian, math.Float64bits(v))
	}
	b.WriteByte(8)
	str("Dimension")
	str(p.Dim)
	b.WriteByte(0)
	var out bytes.Buffer
	zw := gzip.NewWriter(&out)
	zw.Write(b.Bytes())
	zw.Close()
	os.WriteFile(path, out.Bytes(), 0o644)
}

// ---- commands ----

func (s *sim) exec(cmd string, rcon bool) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	cmd = strings.TrimPrefix(cmd, "/")
	f := strings.Fields(cmd)
	if len(f) == 0 {
		return ""
	}
	switch f[0] {
	case "stop":
		go s.stop()
		return ""
	case "list":
		on := s.online()
		var names []string
		for _, p := range on {
			if len(f) > 1 && f[1] == "uuids" {
				names = append(names, fmt.Sprintf("%s (%s)", p.Name, p.UUID))
			} else {
				names = append(names, p.Name)
			}
		}
		return fmt.Sprintf("There are %d of a max of %s players online: %s", len(on), orDefault(s.props["max-players"], "20"), strings.Join(names, ", "))
	case "say":
		return "[Server] " + strings.Join(f[1:], " ")
	case "save-all":
		s.save()
		return "Saving the game (this may take a moment!)\nSaved the game"
	case "save-off":
		s.saveOff = true
		return "Automatic saving is now disabled"
	case "save-on":
		s.saveOff = false
		return "Automatic saving is now enabled"
	case "tps":
		return "§6TPS from last 1m, 5m, 15m: §a20.0, §a19.98, §a*20.0"
	case "op", "deop":
		if len(f) < 2 {
			return "Unknown or incomplete command"
		}
		p := s.find(f[1])
		uuid := uuidFor(f[1])
		if p != nil {
			uuid = p.UUID
		}
		var ops []map[string]any
		json.Unmarshal([]byte(readFile("ops.json")), &ops)
		var keep []map[string]any
		for _, o := range ops {
			if !strings.EqualFold(fmt.Sprint(o["name"]), f[1]) {
				keep = append(keep, o)
			}
		}
		if f[0] == "op" {
			keep = append(keep, map[string]any{"uuid": uuid, "name": f[1], "level": 4, "bypassesPlayerLimit": false})
		}
		b, _ := json.MarshalIndent(keep, "", "  ")
		os.WriteFile("ops.json", b, 0o644)
		if f[0] == "op" {
			return "Made " + f[1] + " a server operator"
		}
		return "Made " + f[1] + " no longer a server operator"
	case "whitelist":
		if len(f) >= 3 && (f[1] == "add" || f[1] == "remove") {
			var wl []map[string]any
			json.Unmarshal([]byte(readFile("whitelist.json")), &wl)
			var keep []map[string]any
			for _, o := range wl {
				if !strings.EqualFold(fmt.Sprint(o["name"]), f[2]) {
					keep = append(keep, o)
				}
			}
			if f[1] == "add" {
				keep = append(keep, map[string]any{"uuid": uuidFor(f[2]), "name": f[2]})
			}
			b, _ := json.MarshalIndent(keep, "", "  ")
			os.WriteFile("whitelist.json", b, 0o644)
			return "Added " + f[2] + " to the whitelist"
		}
		return "Whitelist is now turned " + strings.Join(f[1:], " ")
	case "kick":
		if len(f) >= 2 {
			if p := s.find(f[1]); p != nil && p.Online {
				s.leave(p, "Kicked by an operator")
				return "Kicked " + p.Name + ": Kicked by an operator"
			}
		}
		return "No player was found"
	case "data":
		// data get entity <name> Pos|Dimension
		if len(f) >= 5 && f[1] == "get" && f[2] == "entity" {
			p := s.find(f[3])
			if p == nil || !p.Online {
				return "No entity was found"
			}
			if f[4] == "Pos" {
				return fmt.Sprintf("%s has the following entity data: [%.2fd, %.2fd, %.2fd]", p.Name, p.X, p.Y, p.Z)
			}
			if f[4] == "Dimension" {
				return fmt.Sprintf("%s has the following entity data: \"%s\"", p.Name, p.Dim)
			}
		}
		return "Unknown or incomplete command, see below for error"
	case "crash":
		fmt.Println("Exception in server tick loop")
		fmt.Println("java.lang.NullPointerException: simulated crash")
		fmt.Println("\tat net.minecraft.server.MinecraftServer.tick(MinecraftServer.java:42)")
		os.Exit(1)
	case "help":
		return "/list, /say, /stop, /save-all ..."
	}
	return "Unknown or incomplete command, see below for error"
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func (s *sim) stop() {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return
	}
	s.stopping = true
	s.log("INFO", "Stopping the server")
	s.log("INFO", "Stopping server")
	s.log("INFO", "Saving players")
	for _, p := range s.online() {
		s.leave(p, "Server closed")
	}
	s.log("INFO", "Saving worlds")
	s.save()
	s.log("INFO", "Saving chunks for level 'ServerLevel[world]'/minecraft:overworld")
	s.mu.Unlock()
	time.Sleep(300 * time.Millisecond)
	s.log("INFO", "ThreadedAnvilChunkStorage: All dimensions are saved")
	os.Exit(0)
}

// ---- RCON ----

func (s *sim) serveRCON(l net.Listener) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go s.rconConn(c)
	}
}

func readPacket(r io.Reader) (int32, int32, string, error) {
	var size, id, typ int32
	if err := binary.Read(r, binary.LittleEndian, &size); err != nil {
		return 0, 0, "", err
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, 0, "", err
	}
	id = int32(binary.LittleEndian.Uint32(buf[0:]))
	typ = int32(binary.LittleEndian.Uint32(buf[4:]))
	return id, typ, strings.TrimRight(string(buf[8:]), "\x00"), nil
}

func writePacket(w io.Writer, id, typ int32, body string) {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, int32(len(body)+10))
	binary.Write(&b, binary.LittleEndian, id)
	binary.Write(&b, binary.LittleEndian, typ)
	b.WriteString(body)
	b.Write([]byte{0, 0})
	w.Write(b.Bytes())
}

func (s *sim) rconConn(c net.Conn) {
	defer c.Close()
	authed := false
	for {
		id, typ, body, err := readPacket(c)
		if err != nil {
			return
		}
		switch typ {
		case 3:
			if body == s.props["rcon.password"] {
				authed = true
				writePacket(c, id, 2, "")
			} else {
				writePacket(c, -1, 2, "")
			}
		case 2:
			if !authed {
				writePacket(c, -1, 2, "")
				continue
			}
			out := s.exec(body, true)
			writePacket(c, id, 0, strings.ReplaceAll(out, "\n", ""))
		default:
			writePacket(c, id, 0, fmt.Sprintf("Unknown request %x", typ))
		}
	}
}

// ---- Server List Ping ----

func readVarInt(r *bufio.Reader) (int, error) {
	var v int
	for i := 0; i < 5; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		v |= int(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return v, nil
		}
	}
	return 0, fmt.Errorf("varint too long")
}

func varInt(v int) []byte {
	var out []byte
	u := uint32(v)
	for {
		if u&^0x7f == 0 {
			return append(out, byte(u))
		}
		out = append(out, byte(u&0x7f|0x80))
		u >>= 7
	}
}

func (s *sim) serveSLP(l net.Listener) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			c.SetDeadline(time.Now().Add(5 * time.Second))
			r := bufio.NewReader(c)
			for i := 0; i < 3; i++ {
				n, err := readVarInt(r)
				if err != nil {
					return
				}
				buf := make([]byte, n)
				if _, err := io.ReadFull(r, buf); err != nil {
					return
				}
				if len(buf) == 0 {
					continue
				}
				switch {
				case buf[0] == 0 && n > 1: // handshake
				case buf[0] == 0: // status request
					s.mu.Lock()
					var sample []map[string]string
					for _, p := range s.online() {
						sample = append(sample, map[string]string{"name": p.Name, "id": p.UUID})
					}
					status := map[string]any{
						"version":     map[string]any{"name": "Paper 1.21.1", "protocol": 767},
						"players":     map[string]any{"max": atoi(s.props["max-players"], 20), "online": len(sample), "sample": sample},
						"description": map[string]any{"text": orDefault(s.props["motd"], "A Minecraft Server"), "color": "gold"},
					}
					s.mu.Unlock()
					js, _ := json.Marshal(status)
					payload := append([]byte{0}, varInt(len(js))...)
					payload = append(payload, js...)
					c.Write(append(varInt(len(payload)), payload...))
				case buf[0] == 1: // ping
					c.Write(append(varInt(len(buf)), buf...))
					return
				}
			}
		}(c)
	}
}
