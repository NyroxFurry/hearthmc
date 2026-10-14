// Package mapr renders top-down maps from Minecraft region files (Anvil
// format, Minecraft 1.13 up to 26.x) into a Leaflet compatible tile pyramid.
package mapr

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"hearth/internal/nbt"
)

// Region is a loaded .mca file.
type Region struct {
	Path string
	X, Z int
	data []byte
}

// ParseRegionName extracts the region coordinates from "r.X.Z.mca".
func ParseRegionName(name string) (int, int, bool) {
	parts := strings.Split(name, ".")
	if len(parts) != 4 || parts[0] != "r" || parts[3] != "mca" {
		return 0, 0, false
	}
	x, err1 := strconv.Atoi(parts[1])
	z, err2 := strconv.Atoi(parts[2])
	return x, z, err1 == nil && err2 == nil
}

func OpenRegion(path string) (*Region, error) {
	x, z, ok := ParseRegionName(filepath.Base(path))
	if !ok {
		return nil, fmt.Errorf("ungültiger Region-Dateiname: %s", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 8192 {
		return &Region{Path: path, X: x, Z: z}, nil // empty region
	}
	return &Region{Path: path, X: x, Z: z, data: b}, nil
}

// chunkSkip lists NBT keys the renderer never needs.
var chunkSkip = map[string]bool{
	"BlockLight": true, "SkyLight": true, "block_entities": true, "block_ticks": true, "fluid_ticks": true,
	"PostProcessing": true, "structures": true, "Entities": true, "TileEntities": true, "TileTicks": true,
	"LiquidTicks": true, "Lights": true, "LiquidsToBeTicked": true, "ToBeTicked": true, "CarvingMasks": true,
	"Properties": true, "Structures": true, "blending_data": true, "below_zero_retrogen": true,
	"CarvingMask": true, "carving_mask": true, "UpgradeData": true, "starlight.light_version": true,
	"isLightOn": true, "InhabitedTime": true, "LastUpdate": true,
}

var ErrNoChunk = errors.New("chunk not present")

// Chunk returns the decoded NBT of a chunk (local coordinates 0..31).
func (r *Region) Chunk(lx, lz int) (nbt.Compound, error) {
	if r.data == nil {
		return nil, ErrNoChunk
	}
	idx := 4 * ((lx & 31) + (lz&31)*32)
	loc := r.data[idx : idx+4]
	offset := int(loc[0])<<16 | int(loc[1])<<8 | int(loc[2])
	if offset < 2 || loc[3] == 0 {
		return nil, ErrNoChunk
	}
	start := offset * 4096
	if start+5 > len(r.data) {
		return nil, ErrNoChunk
	}
	length := int(binary.BigEndian.Uint32(r.data[start:]))
	ctype := r.data[start+4]
	var payload []byte
	if ctype&0x80 != 0 {
		// oversized chunk stored in c.X.Z.mcc next to the region file
		gx, gz := r.X*32+lx, r.Z*32+lz
		ext, err := os.ReadFile(filepath.Join(filepath.Dir(r.Path), fmt.Sprintf("c.%d.%d.mcc", gx, gz)))
		if err != nil {
			return nil, err
		}
		payload = ext
		ctype &= 0x7f
	} else {
		if length < 1 || start+4+length > len(r.data) {
			return nil, errors.New("beschädigter Chunk")
		}
		payload = r.data[start+5 : start+4+length]
	}
	raw, err := decompress(ctype, payload)
	if err != nil {
		return nil, err
	}
	return nbt.Decode(raw, chunkSkip)
}

func decompress(ctype byte, b []byte) ([]byte, error) {
	switch ctype {
	case 1:
		r, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(r)
	case 2:
		r, err := zlib.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(r)
	case 3:
		return b, nil
	case 4:
		return lz4BlockStream(b)
	}
	return nil, fmt.Errorf("unbekannte Chunk-Kompression %d", ctype)
}

// lz4BlockStream decodes the LZ4Block framing of lz4-java (used by
// region-file-compression=lz4 since Minecraft 1.20.5).
func lz4BlockStream(b []byte) ([]byte, error) {
	var out []byte
	for len(b) >= 21 {
		if string(b[:8]) != "LZ4Block" {
			return nil, errors.New("lz4: ungültiger Block-Header")
		}
		method := b[8] & 0xf0
		compLen := int(binary.LittleEndian.Uint32(b[9:]))
		origLen := int(binary.LittleEndian.Uint32(b[13:]))
		b = b[21:]
		if origLen == 0 {
			break
		}
		if compLen < 0 || compLen > len(b) {
			return nil, errors.New("lz4: Block abgeschnitten")
		}
		block := b[:compLen]
		b = b[compLen:]
		switch method {
		case 0x10:
			out = append(out, block...)
		case 0x20:
			dec, err := lz4Block(block, origLen)
			if err != nil {
				return nil, err
			}
			out = append(out, dec...)
		default:
			return nil, fmt.Errorf("lz4: unbekannte Methode %x", method)
		}
	}
	return out, nil
}

func lz4Block(src []byte, size int) ([]byte, error) {
	dst := make([]byte, 0, size)
	i := 0
	errCorrupt := errors.New("lz4: beschädigte Daten")
	for i < len(src) {
		token := src[i]
		i++
		lit := int(token >> 4)
		if lit == 15 {
			for {
				if i >= len(src) {
					return nil, errCorrupt
				}
				c := src[i]
				i++
				lit += int(c)
				if c != 255 {
					break
				}
			}
		}
		if i+lit > len(src) {
			return nil, errCorrupt
		}
		dst = append(dst, src[i:i+lit]...)
		i += lit
		if i >= len(src) {
			break // last sequence has only literals
		}
		if i+2 > len(src) {
			return nil, errCorrupt
		}
		off := int(src[i]) | int(src[i+1])<<8
		i += 2
		ml := int(token & 15)
		if ml == 15 {
			for {
				if i >= len(src) {
					return nil, errCorrupt
				}
				c := src[i]
				i++
				ml += int(c)
				if c != 255 {
					break
				}
			}
		}
		ml += 4
		start := len(dst) - off
		if off == 0 || start < 0 {
			return nil, errCorrupt
		}
		for k := 0; k < ml; k++ {
			dst = append(dst, dst[start+k])
		}
		if len(dst) > size {
			return nil, errCorrupt
		}
	}
	return dst, nil
}
