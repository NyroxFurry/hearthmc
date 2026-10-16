package jvm

import (
	"bytes"
	"testing"
)

func TestRequest(t *testing.T) {
	got := request("jcmd", "GC.heap_info")
	want := []byte("1\x00jcmd\x00GC.heap_info\x00\x00\x00")
	if !bytes.Equal(got, want) {
		t.Fatalf("request = %q, want %q", got, want)
	}
}

func TestParseReply(t *testing.T) {
	code, out, err := parseReply([]byte("0\n garbage-first heap   total 1024K, used 512K\n"))
	if err != nil || code != 0 || out == "" {
		t.Fatalf("parseReply: %d %q %v", code, out, err)
	}
	if _, _, err := parseReply([]byte("garbage")); err == nil {
		t.Fatal("expected error for garbage reply")
	}
}

func TestParseHeapInfo(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want Heap
	}{
		{"g1", ` garbage-first heap   total 262144K, used 23404K [0x00000000f0000000, 0x0000000100000000)
  region size 1024K, 23 young (23552K), 0 survivors (0K)
 Metaspace       used 1159K, committed 1344K, reserved 1114112K
  class space    used 92K, committed 192K, reserved 1048576K
`, Heap{Used: 23404 << 10, Committed: 262144 << 10}},
		{"parallel", ` PSYoungGen      total 76288K, used 3932K [0x000000076ab00000, 0x0000000770000000, 0x00000007c0000000)
  eden space 65536K, 6% used [0x000000076ab00000,0x000000076aed7240,0x000000076eb00000)
  from space 10752K, 0% used [0x000000076f580000,0x000000076f580000,0x0000000770000000)
  to   space 10752K, 0% used [0x000000076eb00000,0x000000076eb00000,0x000000076f580000)
 ParOldGen       total 175104K, used 1000K [0x00000006c0000000, 0x00000006cab00000, 0x000000076ab00000)
  object space 175104K, 0% used [0x00000006c0000000,0x00000006c0000000,0x00000006cab00000)
 Metaspace       used 1159K, committed 1344K, reserved 1114112K
`, Heap{Used: (3932 + 1000) << 10, Committed: (76288 + 175104) << 10}},
		{"serial", ` def new generation   total 78656K, used 4198K [0x00000006c0000000, 0x00000006c5550000, 0x0000000715550000)
  eden space 69952K,   6% used [0x00000006c0000000, 0x00000006c0419a10, 0x00000006c4450000)
 tenured generation   total 174784K, used 0K [0x0000000715550000, 0x0000000720000000, 0x00000007c0000000)
   the space 174784K,   0% used [0x0000000715550000, 0x0000000715550000, 0x0000000715550200, 0x0000000720000000)
`, Heap{Used: 4198 << 10, Committed: (78656 + 174784) << 10}},
		{"zgc", ` ZHeap           used 30M, capacity 256M, max capacity 4096M
 Metaspace       used 1159K, committed 1344K, reserved 1114112K
`, Heap{Used: 30 << 20, Committed: 256 << 20, Max: 4096 << 20}},
		{"shenandoah", `Shenandoah Heap
 4194304K max, 4194304K soft max, 262144K committed, 23404K used
 2048 x 2048K regions
`, Heap{Used: 23404 << 10, Committed: 262144 << 10, Max: 4194304 << 10}},
	}
	for _, c := range cases {
		got, ok := parseHeapInfo(c.out)
		if !ok || got != c.want {
			t.Errorf("%s: got %+v (%v), want %+v", c.name, got, ok, c.want)
		}
	}
	if _, ok := parseHeapInfo("java.lang.IllegalArgumentException: Unknown diagnostic command"); ok {
		t.Error("unexpected heap from error text")
	}
}
