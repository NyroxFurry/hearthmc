// Package jvm reads the heap usage of a running HotSpot JVM through its
// attach listener, the same mechanism jcmd uses. Minecraft servers usually
// reserve their whole heap at start (-Xms = -Xmx, AlwaysPreTouch), so the
// resident memory of the process says little about how much memory the
// server really uses. The heap numbers do.
//
// Hearth starts Java with -XX:+StartAttachListener, so the listener socket
// exists from the start and no signal has to be sent to the process.
package jvm

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// Heap is a heap usage sample in bytes.
type Heap struct {
	Used      uint64 `json:"used"`
	Committed uint64 `json:"committed"`
	Max       uint64 `json:"max,omitempty"` // 0 = unknown
}

var (
	ErrUnsupported = errors.New("heap info not supported on this platform")
	ErrNoListener  = errors.New("no attach listener")
)

// request encodes an attach request (protocol version 1): the version, the
// command and exactly three arguments, each terminated by a NUL byte.
func request(cmd string, args ...string) []byte {
	parts := append([]string{"1", cmd}, args...)
	for len(parts) < 5 {
		parts = append(parts, "")
	}
	var b strings.Builder
	for _, p := range parts[:5] {
		b.WriteString(p)
		b.WriteByte(0)
	}
	return []byte(b.String())
}

// parseReply splits a reply into its result code and output.
func parseReply(b []byte) (int, string, error) {
	s := string(b)
	line, rest, _ := strings.Cut(s, "\n")
	code, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		return 0, "", errors.New("unexpected reply from attach listener")
	}
	return code, rest, nil
}

func size(n, unit string) uint64 {
	v, err := strconv.ParseUint(n, 10, 64)
	if err != nil {
		return 0
	}
	switch unit {
	case "K", "k":
		return v << 10
	case "M", "m":
		return v << 20
	case "G", "g":
		return v << 30
	}
	return v
}

var (
	// G1, Parallel and Serial: "garbage-first heap   total 262144K, used 23404K [..."
	// (Parallel and Serial print one line per generation).
	reTotalUsed = regexp.MustCompile(`total\s+(\d+)([KMG])?,\s+used\s+(\d+)([KMG])?`)
	// ZGC: "ZHeap           used 30M, capacity 256M, max capacity 4096M"
	reZGC = regexp.MustCompile(`used\s+(\d+)([KMG])?,\s+capacity\s+(\d+)([KMG])?,\s+max capacity\s+(\d+)([KMG])?`)
	// Shenandoah: " 4194304K max, 4194304K soft max, 262144K committed, 23404K used"
	reShenandoah = regexp.MustCompile(`(\d+)([KMG])?\s+max,.*?(\d+)([KMG])?\s+committed,\s+(\d+)([KMG])?\s+used`)
)

// parseHeapInfo reads the output of the GC.heap_info diagnostic command.
func parseHeapInfo(out string) (Heap, bool) {
	if m := reZGC.FindStringSubmatch(out); m != nil {
		return Heap{Used: size(m[1], m[2]), Committed: size(m[3], m[4]), Max: size(m[5], m[6])}, true
	}
	if m := reShenandoah.FindStringSubmatch(out); m != nil {
		return Heap{Used: size(m[5], m[6]), Committed: size(m[3], m[4]), Max: size(m[1], m[2])}, true
	}
	var h Heap
	found := false
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "Metaspace") || strings.HasPrefix(t, "class space") {
			continue
		}
		if m := reTotalUsed.FindStringSubmatch(t); m != nil {
			h.Committed += size(m[1], m[2])
			h.Used += size(m[3], m[4])
			found = true
		}
	}
	return h, found && h.Committed > 0
}
