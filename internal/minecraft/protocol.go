package minecraft

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// ---- RCON ----

// RCON is a minimal Source-RCON client as implemented by Minecraft.
type RCON struct {
	mu     sync.Mutex
	conn   net.Conn
	br     *bufio.Reader
	nextID int32
}

var ErrRCONAuth = errors.New("RCON-Anmeldung fehlgeschlagen (falsches Passwort)")

func DialRCON(addr, password string, timeout time.Duration) (*RCON, error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	r := &RCON{conn: conn, br: bufio.NewReader(conn), nextID: 1}
	conn.SetDeadline(time.Now().Add(timeout))
	id := r.id()
	if err := r.write(id, 3, password); err != nil {
		conn.Close()
		return nil, err
	}
	for {
		rid, _, _, err := r.read()
		if err != nil {
			conn.Close()
			return nil, err
		}
		if rid == -1 {
			conn.Close()
			return nil, ErrRCONAuth
		}
		if rid == id {
			break
		}
	}
	conn.SetDeadline(time.Time{})
	return r, nil
}

func (r *RCON) id() int32 {
	r.nextID++
	if r.nextID > 1<<30 {
		r.nextID = 1
	}
	return r.nextID
}

func (r *RCON) write(id, typ int32, body string) error {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, int32(4+4+len(body)+2))
	binary.Write(&b, binary.LittleEndian, id)
	binary.Write(&b, binary.LittleEndian, typ)
	b.WriteString(body)
	b.Write([]byte{0, 0})
	_, err := r.conn.Write(b.Bytes())
	return err
}

func (r *RCON) read() (id, typ int32, body string, err error) {
	var size int32
	if err = binary.Read(r.br, binary.LittleEndian, &size); err != nil {
		return
	}
	if size < 10 || size > 1<<20 {
		err = fmt.Errorf("ungültige RCON-Paketgröße %d", size)
		return
	}
	buf := make([]byte, size)
	if _, err = io.ReadFull(r.br, buf); err != nil {
		return
	}
	id = int32(binary.LittleEndian.Uint32(buf[0:4]))
	typ = int32(binary.LittleEndian.Uint32(buf[4:8]))
	body = string(bytes.TrimRight(buf[8:], "\x00"))
	return
}

// Exec runs a command and returns its (possibly multi-packet) response.
func (r *RCON) Exec(cmd string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conn == nil {
		return "", errors.New("RCON getrennt")
	}
	r.conn.SetDeadline(time.Now().Add(15 * time.Second))
	defer r.conn.SetDeadline(time.Time{})
	id := r.id()
	sentinel := r.id()
	if err := r.write(id, 2, cmd); err != nil {
		return "", err
	}
	// Minecraft answers unknown packet types with "Unknown request"; that
	// response marks the end of a fragmented command response.
	if err := r.write(sentinel, 0, ""); err != nil {
		return "", err
	}
	var out strings.Builder
	for {
		rid, _, body, err := r.read()
		if err != nil {
			return out.String(), err
		}
		if rid == sentinel {
			return out.String(), nil
		}
		if rid == id {
			out.WriteString(body)
		}
	}
}

func (r *RCON) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conn != nil {
		r.conn.Close()
		r.conn = nil
	}
}

// ---- Server List Ping ----

type PingResult struct {
	Version struct {
		Name     string `json:"name"`
		Protocol int    `json:"protocol"`
	} `json:"version"`
	Players struct {
		Max    int `json:"max"`
		Online int `json:"online"`
		Sample []struct {
			Name string `json:"name"`
			ID   string `json:"id"`
		} `json:"sample"`
	} `json:"players"`
	Description json.RawMessage `json:"description"`
	Favicon     string          `json:"favicon"`
	MOTD        string          `json:"motd"`
	LatencyMS   int64           `json:"latencyMs"`
}

func writeVarInt(w *bytes.Buffer, v int32) {
	u := uint32(v)
	for {
		if u&^0x7f == 0 {
			w.WriteByte(byte(u))
			return
		}
		w.WriteByte(byte(u&0x7f | 0x80))
		u >>= 7
	}
}

func readVarInt(r io.ByteReader) (int32, error) {
	var v uint32
	for i := 0; i < 5; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		v |= uint32(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return int32(v), nil
		}
	}
	return 0, errors.New("VarInt zu lang")
}

func packet(id int32, payload []byte) []byte {
	var body bytes.Buffer
	writeVarInt(&body, id)
	body.Write(payload)
	var out bytes.Buffer
	writeVarInt(&out, int32(body.Len()))
	out.Write(body.Bytes())
	return out.Bytes()
}

// Ping queries a Minecraft server via the Server List Ping protocol (1.7+).
func Ping(ctx context.Context, host string, port int) (*PingResult, error) {
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(6 * time.Second))

	var hs bytes.Buffer
	writeVarInt(&hs, -1) // protocol version: "any"
	writeVarInt(&hs, int32(len(host)))
	hs.WriteString(host)
	binary.Write(&hs, binary.BigEndian, uint16(port))
	writeVarInt(&hs, 1) // next state: status
	if _, err := conn.Write(append(packet(0, hs.Bytes()), packet(0, nil)...)); err != nil {
		return nil, err
	}
	start := time.Now()
	br := bufio.NewReader(conn)
	length, err := readVarInt(br)
	if err != nil {
		return nil, err
	}
	if length <= 0 || length > 1<<21 {
		return nil, errors.New("ungültige Antwort")
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(br, data); err != nil {
		return nil, err
	}
	rd := bytes.NewReader(data)
	if id, err := readVarInt(rd); err != nil || id != 0 {
		return nil, errors.New("unerwartetes Paket")
	}
	slen, err := readVarInt(rd)
	if err != nil || int(slen) > rd.Len() || slen < 0 {
		return nil, errors.New("ungültiger Status")
	}
	js := make([]byte, slen)
	io.ReadFull(rd, js)
	var res PingResult
	if err := json.Unmarshal(js, &res); err != nil {
		return nil, err
	}
	res.LatencyMS = time.Since(start).Milliseconds()
	res.MOTD = ChatToLegacy(res.Description)
	return &res, nil
}

// ---- chat components ----

var legacyColors = map[string]string{
	"black": "0", "dark_blue": "1", "dark_green": "2", "dark_aqua": "3", "dark_red": "4", "dark_purple": "5",
	"gold": "6", "gray": "7", "dark_gray": "8", "blue": "9", "green": "a", "aqua": "b", "red": "c",
	"light_purple": "d", "yellow": "e", "white": "f",
}

// ChatToLegacy converts a JSON text component into a §-formatted string.
func ChatToLegacy(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var b strings.Builder
	var walk func(v any)
	walk = func(v any) {
		switch c := v.(type) {
		case string:
			b.WriteString(c)
		case []any:
			for _, e := range c {
				walk(e)
			}
		case map[string]any:
			if col, ok := c["color"].(string); ok {
				if code, ok := legacyColors[col]; ok {
					b.WriteString("§" + code)
				} else if strings.HasPrefix(col, "#") && len(col) == 7 {
					b.WriteString("§x")
					for _, ch := range col[1:] {
						b.WriteString("§" + string(ch))
					}
				}
			}
			for _, f := range []struct{ key, code string }{{"bold", "l"}, {"italic", "o"}, {"underlined", "n"}, {"strikethrough", "m"}, {"obfuscated", "k"}} {
				if on, ok := c[f.key].(bool); ok && on {
					b.WriteString("§" + f.code)
				}
			}
			if t, ok := c["text"].(string); ok {
				b.WriteString(t)
			} else if t, ok := c["translate"].(string); ok {
				b.WriteString(t)
			}
			if extra, ok := c["extra"].([]any); ok {
				for _, e := range extra {
					walk(e)
				}
			}
		}
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	walk(v)
	return b.String()
}

// StripLegacy removes § formatting codes.
func StripLegacy(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		if rs[i] == '§' && i+1 < len(rs) {
			i++
			continue
		}
		b.WriteRune(rs[i])
	}
	return b.String()
}
