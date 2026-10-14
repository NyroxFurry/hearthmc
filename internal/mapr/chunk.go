package mapr

import (
	"math"
	"math/bits"

	"hearth/internal/nbt"
)

// section holds the packed block (and biome) data of a 16³ chunk section.
type section struct {
	palette []*BlockInfo
	data    []int64
	bits    uint
	perLong int
	span    bool
	mask    uint64

	biomes   []biomeTint
	bData    []int64
	bBits    uint
	bPerLong int
}

type chunk struct {
	minY, maxY int
	minSec     int
	sections   []*section // index = sectionY - minSec
	surface    []int32    // 256 entries: highest block y, or nil
	legacyBio  []int32    // pre-1.18 biome ids (256 = 2D, 1024 = 3D)
}

var fullStatus = map[string]bool{
	"full": true, "minecraft:full": true, "postprocessed": true, "fullchunk": true, "minecraft:postprocessed": true,
}

func bitsFor(n int) uint {
	if n <= 1 {
		return 0
	}
	return uint(bits.Len(uint(n - 1)))
}

// parseChunk converts chunk NBT into a renderable chunk. Returns nil for
// chunks that are not fully generated.
func parseChunk(root nbt.Compound) *chunk {
	level := root
	legacy := false
	if lv := nbt.Comp(root["Level"]); lv != nil {
		level = lv
		legacy = true
	}
	if st := nbt.String(level["Status"]); st != "" && !fullStatus[st] {
		return nil
	}
	dataVersion, _ := nbt.Int(root["DataVersion"])
	var secList []any
	if legacy {
		secList = nbt.List(level["Sections"])
	} else {
		secList = nbt.List(level["sections"])
	}
	if len(secList) == 0 {
		return nil
	}
	c := &chunk{}
	minSec, maxSec := math.MaxInt32, math.MinInt32
	type rawSec struct {
		y   int
		sec *section
	}
	var parsed []rawSec
	for _, it := range secList {
		s := nbt.Comp(it)
		if s == nil {
			continue
		}
		yv, _ := nbt.Int(s["Y"])
		y := int(int8(yv))
		var sec *section
		if legacy {
			sec = parseLegacySection(s, dataVersion)
		} else {
			sec = parseSection(s)
		}
		if sec == nil {
			continue
		}
		parsed = append(parsed, rawSec{y, sec})
		minSec = min(minSec, y)
		maxSec = max(maxSec, y)
	}
	if len(parsed) == 0 {
		return nil
	}
	c.minSec = minSec
	if yp, ok := nbt.Int(level["yPos"]); ok {
		c.minSec = min(minSec, int(yp))
	}
	c.minY = c.minSec * 16
	c.maxY = (maxSec + 1) * 16
	c.sections = make([]*section, maxSec-c.minSec+1)
	for _, p := range parsed {
		c.sections[p.y-c.minSec] = p.sec
	}
	worldMin := 0
	if yp, ok := nbt.Int(level["yPos"]); ok {
		worldMin = int(yp) * 16
	} else if !legacy {
		worldMin = c.minY
	}
	if hm := nbt.Comp(level["Heightmaps"]); hm != nil {
		arr := nbt.Longs(hm["WORLD_SURFACE"])
		if arr == nil {
			arr = nbt.Longs(hm["MOTION_BLOCKING"])
		}
		if arr != nil {
			c.surface = unpackHeights(arr, dataVersion, worldMin)
		}
	}
	if legacy {
		c.legacyBio = nbt.Ints(level["Biomes"])
		if c.legacyBio == nil {
			if b, ok := level["Biomes"].([]byte); ok { // very old byte array
				c.legacyBio = make([]int32, len(b))
				for i, v := range b {
					c.legacyBio[i] = int32(v)
				}
			}
		}
	}
	return c
}

func unpackHeights(arr []int64, dataVersion int64, worldMin int) []int32 {
	n := len(arr)
	var b uint
	span := false
	// non spanning: ceil(256 / (64/bits)) longs; spanning (pre 1.16): 4*bits longs
	for cand := uint(1); cand <= 16; cand++ {
		per := 64 / int(cand)
		if (256+per-1)/per == n && (dataVersion >= 2529 || 64%cand == 0) {
			b = cand
			break
		}
		if dataVersion < 2529 && int(cand)*4 == n {
			b, span = cand, true
			break
		}
	}
	if b == 0 {
		return nil
	}
	out := make([]int32, 256)
	mask := uint64(1)<<b - 1
	for i := 0; i < 256; i++ {
		var v uint64
		if span {
			bit := i * int(b)
			li, off := bit/64, bit%64
			v = uint64(arr[li]) >> off
			if off+int(b) > 64 && li+1 < n {
				v |= uint64(arr[li+1]) << (64 - off)
			}
		} else {
			per := 64 / int(b)
			v = uint64(arr[i/per]) >> (uint(i%per) * b)
		}
		v &= mask
		out[i] = int32(int(v) + worldMin - 1)
	}
	return out
}

func resolvePalette(list []any) []*BlockInfo {
	pal := make([]*BlockInfo, len(list))
	for i, e := range list {
		name := nbt.String(nbt.Comp(e)["Name"])
		if name == "" {
			name = "minecraft:air"
		}
		pal[i] = Info(name)
	}
	return pal
}

func parseSection(s nbt.Compound) *section {
	bs := nbt.Comp(s["block_states"])
	if bs == nil {
		return nil
	}
	pal := nbt.List(bs["palette"])
	if len(pal) == 0 {
		return nil
	}
	sec := &section{palette: resolvePalette(pal)}
	if len(pal) > 1 {
		sec.data = nbt.Longs(bs["data"])
		sec.bits = max(4, bitsFor(len(pal)))
		sec.perLong = 64 / int(sec.bits)
		sec.mask = 1<<sec.bits - 1
		if len(sec.data) < (4096+sec.perLong-1)/sec.perLong {
			sec.data = nil
		}
	}
	if bio := nbt.Comp(s["biomes"]); bio != nil {
		bp := nbt.List(bio["palette"])
		sec.biomes = make([]biomeTint, len(bp))
		for i, b := range bp {
			sec.biomes[i] = tintFor(nbt.String(b))
		}
		if len(bp) > 1 {
			sec.bData = nbt.Longs(bio["data"])
			sec.bBits = bitsFor(len(bp))
			sec.bPerLong = 64 / int(sec.bBits)
			if len(sec.bData) < (64+sec.bPerLong-1)/sec.bPerLong {
				sec.bData = nil
			}
		}
	}
	return sec
}

func parseLegacySection(s nbt.Compound, dataVersion int64) *section {
	pal := nbt.List(s["Palette"])
	if len(pal) == 0 {
		return nil
	}
	sec := &section{palette: resolvePalette(pal)}
	data := nbt.Longs(s["BlockStates"])
	if len(pal) > 1 && data != nil {
		sec.bits = max(4, bitsFor(len(pal)))
		sec.perLong = 64 / int(sec.bits)
		sec.mask = 1<<sec.bits - 1
		sec.data = data
		// before 1.16 (data version 2529) values could span two longs
		if dataVersion < 2529 && 64%sec.bits != 0 {
			sec.span = true
			if len(data) < 4096*int(sec.bits)/64 {
				sec.data = nil
			}
		} else if len(data) < (4096+sec.perLong-1)/sec.perLong {
			sec.data = nil
		}
	}
	return sec
}

func (s *section) block(x, y, z int) *BlockInfo {
	if s.data == nil {
		return s.palette[0]
	}
	idx := (y&15)<<8 | (z&15)<<4 | (x & 15)
	var v uint64
	if s.span {
		bit := idx * int(s.bits)
		li, off := bit/64, bit%64
		v = uint64(s.data[li]) >> off
		if off+int(s.bits) > 64 && li+1 < len(s.data) {
			v |= uint64(s.data[li+1]) << (64 - off)
		}
	} else {
		v = uint64(s.data[idx/s.perLong]) >> (uint(idx%s.perLong) * s.bits)
	}
	v &= s.mask
	if int(v) >= len(s.palette) {
		return s.palette[0]
	}
	return s.palette[v]
}

func (s *section) biome(x, y, z int) (biomeTint, bool) {
	if len(s.biomes) == 0 {
		return biomeTint{}, false
	}
	if s.bData == nil {
		return s.biomes[0], true
	}
	idx := ((y&15)>>2)<<4 | ((z&15)>>2)<<2 | ((x & 15) >> 2)
	v := uint64(s.bData[idx/s.bPerLong]) >> (uint(idx%s.bPerLong) * s.bBits)
	v &= 1<<s.bBits - 1
	if int(v) >= len(s.biomes) {
		return s.biomes[0], true
	}
	return s.biomes[v], true
}

func (c *chunk) sectionAt(y int) *section {
	i := (y >> 4) - c.minSec
	if i < 0 || i >= len(c.sections) {
		return nil
	}
	return c.sections[i]
}

func (c *chunk) block(x, y, z int) *BlockInfo {
	s := c.sectionAt(y)
	if s == nil {
		return nil
	}
	return s.block(x, y, z)
}

func (c *chunk) biome(x, y, z int) biomeTint {
	if s := c.sectionAt(y); s != nil {
		if t, ok := s.biome(x, y, z); ok {
			return t
		}
	}
	switch len(c.legacyBio) {
	case 256:
		if id, ok := legacyBiomes[c.legacyBio[(z&15)*16+(x&15)]]; ok {
			return tintFor(id)
		}
	case 1024:
		yy := max(0, min(63, y>>2))
		if id, ok := legacyBiomes[c.legacyBio[yy*16+((z&15)>>2)*4+((x&15)>>2)]]; ok {
			return tintFor(id)
		}
	}
	return defaultTint
}

func isAir(bi *BlockInfo) bool { return bi == nil || bi.Invisible }

// column finds the visible surface of a column.
// ceiling > 0 enables cave mode (used for the Nether roof).
func (c *chunk) column(x, z, ceiling int) (bi *BlockInfo, y, depth, waterTop int, ok bool) {
	top := c.maxY - 1
	if c.surface != nil {
		if h := int(c.surface[(z&15)*16+(x&15)]); h < top {
			top = h
		}
	}
	y = top
	if ceiling > 0 {
		if y > ceiling {
			y = ceiling
		}
		// leave the roof: skip solid blocks until we reach open space
		for y >= c.minY && !isAir(c.block(x, y, z)) {
			y--
		}
	}
	waterTop = math.MinInt32
	for ; y >= c.minY; y-- {
		b := c.block(x, y, z)
		if isAir(b) {
			continue
		}
		if b.Water {
			if waterTop == math.MinInt32 {
				waterTop = y
			}
			depth++
			if depth > 40 {
				return b, y, depth, waterTop, true
			}
			continue
		}
		return b, y, depth, waterTop, true
	}
	if depth > 0 {
		return Info("minecraft:water"), waterTop, depth, waterTop, true
	}
	return nil, 0, 0, 0, false
}
