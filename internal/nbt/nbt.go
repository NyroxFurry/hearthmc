// Package nbt decodes Minecraft's Named Binary Tag format.
package nbt

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

const (
	TagEnd byte = iota
	TagByte
	TagShort
	TagInt
	TagLong
	TagFloat
	TagDouble
	TagByteArray
	TagString
	TagList
	TagCompound
	TagIntArray
	TagLongArray
)

type Compound = map[string]any

var ErrTruncated = errors.New("nbt: unexpected end of data")

type decoder struct {
	b    []byte
	pos  int
	skip map[string]bool
	err  error
}

func (d *decoder) need(n int) bool {
	if d.err != nil {
		return false
	}
	if n < 0 || d.pos+n > len(d.b) {
		d.err = ErrTruncated
		return false
	}
	return true
}

func (d *decoder) u8() byte {
	if !d.need(1) {
		return 0
	}
	v := d.b[d.pos]
	d.pos++
	return v
}

func (d *decoder) u16() uint16 {
	if !d.need(2) {
		return 0
	}
	v := binary.BigEndian.Uint16(d.b[d.pos:])
	d.pos += 2
	return v
}

func (d *decoder) u32() uint32 {
	if !d.need(4) {
		return 0
	}
	v := binary.BigEndian.Uint32(d.b[d.pos:])
	d.pos += 4
	return v
}

func (d *decoder) u64() uint64 {
	if !d.need(8) {
		return 0
	}
	v := binary.BigEndian.Uint64(d.b[d.pos:])
	d.pos += 8
	return v
}

func (d *decoder) str() string {
	n := int(d.u16())
	if !d.need(n) {
		return ""
	}
	s := string(d.b[d.pos : d.pos+n]) // modified UTF-8; fine for identifiers
	d.pos += n
	return s
}

func (d *decoder) length() int {
	n := int(int32(d.u32()))
	if n < 0 {
		n = 0
	}
	return n
}

// skipPayload advances past a payload without materializing it.
func (d *decoder) skipPayload(t byte, depth int) {
	if depth > 512 {
		d.err = errors.New("nbt: nesting too deep")
		return
	}
	switch t {
	case TagByte:
		d.need(1)
		d.pos++
	case TagShort:
		d.need(2)
		d.pos += 2
	case TagInt, TagFloat:
		d.need(4)
		d.pos += 4
	case TagLong, TagDouble:
		d.need(8)
		d.pos += 8
	case TagByteArray:
		n := d.length()
		if d.need(n) {
			d.pos += n
		}
	case TagString:
		n := int(d.u16())
		if d.need(n) {
			d.pos += n
		}
	case TagList:
		et := d.u8()
		n := d.length()
		for i := 0; i < n && d.err == nil; i++ {
			d.skipPayload(et, depth+1)
		}
	case TagCompound:
		for d.err == nil {
			ct := d.u8()
			if ct == TagEnd {
				return
			}
			n := int(d.u16())
			if d.need(n) {
				d.pos += n
			}
			d.skipPayload(ct, depth+1)
		}
	case TagIntArray:
		n := d.length()
		if d.need(n * 4) {
			d.pos += n * 4
		}
	case TagLongArray:
		n := d.length()
		if d.need(n * 8) {
			d.pos += n * 8
		}
	default:
		d.err = fmt.Errorf("nbt: unknown tag %d", t)
	}
}

func (d *decoder) payload(t byte, depth int) any {
	if depth > 512 {
		d.err = errors.New("nbt: nesting too deep")
		return nil
	}
	switch t {
	case TagByte:
		return int8(d.u8())
	case TagShort:
		return int16(d.u16())
	case TagInt:
		return int32(d.u32())
	case TagLong:
		return int64(d.u64())
	case TagFloat:
		return math.Float32frombits(d.u32())
	case TagDouble:
		return math.Float64frombits(d.u64())
	case TagByteArray:
		n := d.length()
		if !d.need(n) {
			return nil
		}
		v := d.b[d.pos : d.pos+n]
		d.pos += n
		return v
	case TagString:
		return d.str()
	case TagList:
		et := d.u8()
		n := d.length()
		if n > len(d.b) { // corrupt length
			d.err = ErrTruncated
			return nil
		}
		out := make([]any, 0, n)
		for i := 0; i < n && d.err == nil; i++ {
			out = append(out, d.payload(et, depth+1))
		}
		return out
	case TagCompound:
		out := Compound{}
		for d.err == nil {
			ct := d.u8()
			if ct == TagEnd {
				break
			}
			name := d.str()
			if d.skip[name] {
				d.skipPayload(ct, depth+1)
				continue
			}
			out[name] = d.payload(ct, depth+1)
		}
		return out
	case TagIntArray:
		n := d.length()
		if !d.need(n * 4) {
			return nil
		}
		v := make([]int32, n)
		for i := range v {
			v[i] = int32(binary.BigEndian.Uint32(d.b[d.pos+i*4:]))
		}
		d.pos += n * 4
		return v
	case TagLongArray:
		n := d.length()
		if !d.need(n * 8) {
			return nil
		}
		v := make([]int64, n)
		for i := range v {
			v[i] = int64(binary.BigEndian.Uint64(d.b[d.pos+i*8:]))
		}
		d.pos += n * 8
		return v
	}
	d.err = fmt.Errorf("nbt: unknown tag %d", t)
	return nil
}

// Decode parses an uncompressed NBT document whose root is a compound.
// Keys listed in skip are not decoded (useful for large unneeded data).
func Decode(b []byte, skip map[string]bool) (Compound, error) {
	d := &decoder{b: b, skip: skip}
	t := d.u8()
	if t != TagCompound {
		return nil, fmt.Errorf("nbt: root is not a compound (tag %d)", t)
	}
	d.str() // root name
	v := d.payload(TagCompound, 0)
	if d.err != nil {
		return nil, d.err
	}
	return v.(Compound), nil
}

// Decompress inflates gzip or zlib data (auto-detected).
func Decompress(b []byte) ([]byte, error) {
	if len(b) >= 2 && b[0] == 0x1f && b[1] == 0x8b {
		r, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(r)
	}
	if len(b) >= 2 && b[0]&0x0f == 8 && (uint16(b[0])<<8|uint16(b[1]))%31 == 0 {
		r, err := zlib.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(r)
	}
	return b, nil
}

// ReadFile reads a (possibly gzip compressed) NBT file such as level.dat or playerdata.
func ReadFile(path string, skip map[string]bool) (Compound, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	data, err := Decompress(raw)
	if err != nil {
		return nil, err
	}
	return Decode(data, skip)
}

// ---- typed accessors ----

func Get(c Compound, path ...string) any {
	var cur any = c
	for _, p := range path {
		m, ok := cur.(Compound)
		if !ok {
			return nil
		}
		cur = m[p]
	}
	return cur
}

func Int(v any) (int64, bool) {
	switch n := v.(type) {
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	}
	return 0, false
}

func Float(v any) (float64, bool) {
	switch n := v.(type) {
	case float32:
		return float64(n), true
	case float64:
		return n, true
	}
	if i, ok := Int(v); ok {
		return float64(i), true
	}
	return 0, false
}

func String(v any) string {
	s, _ := v.(string)
	return s
}

func List(v any) []any {
	l, _ := v.([]any)
	return l
}

func Comp(v any) Compound {
	c, _ := v.(Compound)
	return c
}

func Longs(v any) []int64 {
	l, _ := v.([]int64)
	return l
}

func Ints(v any) []int32 {
	l, _ := v.([]int32)
	return l
}
