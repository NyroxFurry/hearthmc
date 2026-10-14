package mapr

import (
	"hash/fnv"
	"strings"
	"sync"
)

// RGB is a color without alpha.
type RGB struct{ R, G, B uint8 }

func hexRGB(h uint32) RGB { return RGB{uint8(h >> 16), uint8(h >> 8), uint8(h)} }

// Tint describes biome dependent coloring.
type Tint uint8

const (
	TintNone Tint = iota
	TintGrass
	TintFoliage
	TintWater
)

// BlockInfo is the render information for a block id.
type BlockInfo struct {
	Color     RGB
	Tint      Tint
	Invisible bool // skipped when looking down (air, torches, glass panes ...)
	Water     bool // water or waterlogged plants
	Lava      bool
}

var blockColors = map[string]uint32{
	// terrain
	"grass_block": 0x91bd59, "dirt": 0x866043, "coarse_dirt": 0x77553b, "rooted_dirt": 0x90674c, "podzol": 0x7a5a2e,
	"mycelium": 0x6f6265, "dirt_path": 0x94793e, "farmland": 0x6e4e30, "mud": 0x3c3a3d, "muddy_mangrove_roots": 0x463b2d,
	"sand": 0xdbcfa3, "red_sand": 0xbe6621, "suspicious_sand": 0xd7cb9d, "gravel": 0x847f7e, "suspicious_gravel": 0x817c7b,
	"clay": 0xa0a6b3, "snow": 0xf9fefe, "snow_block": 0xf9fefe, "powder_snow": 0xf8fdfd, "ice": 0x91b8fd,
	"packed_ice": 0x8db4fa, "blue_ice": 0x74a8fd, "frosted_ice": 0x8cb6fc, "moss_block": 0x596e2d, "moss_carpet": 0x596e2d,
	"pale_moss_block": 0x6b7563, "pale_moss_carpet": 0x6b7563,
	"stone": 0x7e7e7e, "granite": 0x95675a, "diorite": 0xbdbdbd, "andesite": 0x888889, "deepslate": 0x505053,
	"cobbled_deepslate": 0x4d4d51, "tuff": 0x6c6d66, "calcite": 0xdfe0dc, "dripstone_block": 0x866b5c,
	"pointed_dripstone": 0x81665a, "bedrock": 0x555555, "obsidian": 0x0f0b19, "crying_obsidian": 0x200a3c,
	"cobblestone": 0x7a7a7a, "mossy_cobblestone": 0x6e7a5f, "smooth_stone": 0x9e9e9e, "stone_bricks": 0x7a7979,
	"mossy_stone_bricks": 0x737e68, "cracked_stone_bricks": 0x767575, "chiseled_stone_bricks": 0x787878,
	"smooth_basalt": 0x48484e, "amethyst_block": 0x8562bf, "budding_amethyst": 0x8460bb, "magma_block": 0x8e3f1f,
	"water": 0x3f76e4, "lava": 0xcf5c12, "bubble_column": 0x3f76e4,
	"sandstone": 0xd8cb9b, "cut_sandstone": 0xd9cd9e, "chiseled_sandstone": 0xd8ca9c, "smooth_sandstone": 0xe0d6aa,
	"red_sandstone": 0xba6620, "cut_red_sandstone": 0xbc6722, "smooth_red_sandstone": 0xb5621f,
	"terracotta": 0x985e43, "packed_mud": 0x8e6b50, "mud_bricks": 0x89684f,
	// vegetation
	"short_grass": 0x7fae4a, "grass": 0x7fae4a, "tall_grass": 0x7fae4a, "fern": 0x6b9d3c, "large_fern": 0x6b9d3c,
	"vine": 0x6a9c33, "sugar_cane": 0x95c35a, "cactus": 0x5d8e2a, "bamboo": 0x5d8f1e, "lily_pad": 0x208030,
	"sweet_berry_bush": 0x3f6d35, "azalea": 0x5f7c2b, "flowering_azalea": 0x70763f, "big_dripleaf": 0x6f9e33,
	"small_dripleaf": 0x5c8a2c, "glow_lichen": 0x707e72, "hanging_roots": 0x9d6e5b, "spore_blossom": 0xcf6aa6,
	"dandelion": 0xf0d33c, "poppy": 0xc32c20, "blue_orchid": 0x2aa6e2, "allium": 0xb278d5, "azure_bluet": 0xe2e7e8,
	"red_tulip": 0xc8371c, "orange_tulip": 0xe77f1d, "white_tulip": 0xe6e6e6, "pink_tulip": 0xebb1c9,
	"oxeye_daisy": 0xe6e4d7, "cornflower": 0x456be3, "lily_of_the_valley": 0xe6e6e6, "sunflower": 0xf4c83c,
	"lilac": 0xc28fc2, "rose_bush": 0xa12a26, "peony": 0xe6b1d7, "wither_rose": 0x282b1a, "torchflower": 0xf09b3a,
	"pitcher_plant": 0x6b8fba, "pink_petals": 0xf2a8c8, "wildflowers": 0xe8d24c, "leaf_litter": 0x8d6b3a,
	"pumpkin": 0xe38a1d, "carved_pumpkin": 0xe38a1d, "jack_o_lantern": 0xe9a234, "melon": 0x7e9e2a,
	"hay_block": 0xa88b10, "dried_kelp_block": 0x3c4a2c, "wheat": 0xb59b45, "carrots": 0x4f8a23, "potatoes": 0x5c8f27,
	"beetroots": 0x5f8a2e, "cocoa": 0x8e5428, "mushroom_stem": 0xcbc4b9, "red_mushroom_block": 0xc82e2d,
	"brown_mushroom_block": 0x956f51, "red_mushroom": 0xc82e2d, "brown_mushroom": 0x956f51,
	"cherry_leaves": 0xeeb4d8, "spruce_leaves": 0x619961, "birch_leaves": 0x80a755, "azalea_leaves": 0x5a7a2f,
	"flowering_azalea_leaves": 0x6e8a3c, "pale_oak_leaves": 0x6e7a6b,
	// nether & end
	"netherrack": 0x6f3535, "soul_sand": 0x513e32, "soul_soil": 0x4b3a2f, "basalt": 0x515156, "polished_basalt": 0x636366,
	"blackstone": 0x2a2328, "polished_blackstone": 0x353038, "glowstone": 0xab8654, "nether_wart_block": 0x730302,
	"warped_wart_block": 0x167879, "crimson_nylium": 0x831f1f, "warped_nylium": 0x2b7265, "shroomlight": 0xf19d47,
	"ancient_debris": 0x5f4039, "nether_gold_ore": 0x73362b, "nether_quartz_ore": 0x75413e, "crimson_roots": 0x7e0c18,
	"warped_roots": 0x14947a, "nether_sprouts": 0x14947a, "weeping_vines": 0x7d0d0d, "twisting_vines": 0x149581,
	"crimson_fungus": 0xa1222b, "warped_fungus": 0x4a9888, "nether_wart": 0x6e0f13, "fire": 0xe58f2a, "soul_fire": 0x37c2c4,
	"end_stone": 0xdbde9e, "purpur_block": 0xa97ea9, "purpur_pillar": 0xab81ab, "chorus_plant": 0x5e3a5e,
	"chorus_flower": 0x977897, "end_rod": 0xe0d7cf, "dragon_egg": 0x0c0910, "end_portal_frame": 0x5b7b6e,
	"end_gateway": 0x060607, "end_portal": 0x060607,
	// minerals & metal
	"iron_block": 0xdcdcdc, "gold_block": 0xf6d03d, "diamond_block": 0x62ede4, "emerald_block": 0x2acb57,
	"lapis_block": 0x1e43a8, "redstone_block": 0xaf1805, "coal_block": 0x101010, "netherite_block": 0x433d40,
	"copper_block": 0xc06c50, "exposed_copper": 0xa27e68, "weathered_copper": 0x6c9970, "oxidized_copper": 0x52a285,
	"raw_iron_block": 0xa6876b, "raw_gold_block": 0xdda92e, "raw_copper_block": 0x9a6a4f, "quartz_block": 0xebe5de,
	"smooth_quartz": 0xebe5de, "quartz_pillar": 0xebe6e0, "chiseled_quartz_block": 0xe7e2d9, "quartz_bricks": 0xeae4dd,
	// building
	"bricks": 0x966152, "prismarine": 0x63a499, "prismarine_bricks": 0x63ac9e, "dark_prismarine": 0x335b4c,
	"sea_lantern": 0xacc7be, "nether_bricks": 0x2c161a, "red_nether_bricks": 0x460709, "end_stone_bricks": 0xdadea0,
	"glass": 0xc0dde3, "tinted_glass": 0x2c272e, "sponge": 0xc4c04b, "wet_sponge": 0xafb240, "slime_block": 0x78c865,
	"honey_block": 0xf9bc3b, "honeycomb_block": 0xe5941e, "bookshelf": 0xa2834f, "chiseled_bookshelf": 0xa2834f,
	"crafting_table": 0x7f5f39, "furnace": 0x6e6e6e, "blast_furnace": 0x6c6c6c, "smoker": 0x5c5245, "chest": 0x9e6f2f,
	"trapped_chest": 0x9e6f2f, "ender_chest": 0x2b3c3e, "barrel": 0x7e5f35, "beehive": 0xb88f4b, "bee_nest": 0xc2963f,
	"composter": 0x77552a, "lectern": 0xa2834f, "smithing_table": 0x3a3a44, "cartography_table": 0x66473a,
	"fletching_table": 0xc9bf8a, "loom": 0xa08257, "stonecutter": 0x7c7c7c, "grindstone": 0x8e8e8e, "bell": 0xe8c04a,
	"campfire": 0x5b4429, "soul_campfire": 0x3b4f4f, "lantern": 0x6b5a3a, "soul_lantern": 0x445860,
	"anvil": 0x444444, "chipped_anvil": 0x444444, "damaged_anvil": 0x444444, "enchanting_table": 0x7e2930,
	"brewing_stand": 0x7a6650, "cauldron": 0x4b4b4b, "water_cauldron": 0x3f76e4, "lava_cauldron": 0xcf5c12,
	"hopper": 0x474747, "beacon": 0x75ddd7, "conduit": 0x9f8b6b, "respawn_anchor": 0x2a1a4b, "lodestone": 0x939394,
	"target": 0xe3b2a5, "tnt": 0xdb4325, "note_block": 0x583a28, "jukebox": 0x583a28, "dispenser": 0x616161,
	"dropper": 0x616161, "observer": 0x616161, "piston": 0xa2834f, "sticky_piston": 0x7aa95b, "crafter": 0x6b6b6b,
	"daylight_detector": 0xa2834f, "sculk": 0x0d1e24, "sculk_catalyst": 0x0f2027, "sculk_shrieker": 0x4d5a4c,
	"sculk_sensor": 0x0e4c56, "calibrated_sculk_sensor": 0x0e4c56, "sculk_vein": 0x0d1e24, "reinforced_deepslate": 0x50534f,
	"trial_spawner": 0x3b4a55, "vault": 0x3a4650, "heavy_core": 0x4c4c55, "creaking_heart": 0x5b4f4c, "spawner": 0x2a3a47,
	"tube_coral_block": 0x3157cf, "brain_coral_block": 0xcf5ba2, "bubble_coral_block": 0xa118a0, "fire_coral_block": 0xa3232e,
	"horn_coral_block": 0xd8c842, "sea_pickle": 0x6a7034, "scaffolding": 0xb8955a, "rail": 0x7d6e4e,
}

var woodColors = map[string]uint32{
	"oak": 0xa2834f, "spruce": 0x725430, "birch": 0xc0af79, "jungle": 0xa07351, "acacia": 0xa85a32,
	"dark_oak": 0x432b14, "mangrove": 0x763631, "cherry": 0xe2b2ac, "bamboo": 0xc2ad51, "crimson": 0x6a3449,
	"warped": 0x2b6963, "pale_oak": 0xe3d9d6,
}

var barkColors = map[string]uint32{
	"oak": 0x6d5534, "spruce": 0x3a2615, "birch": 0xd8d7d2, "jungle": 0x564419, "acacia": 0x676157,
	"dark_oak": 0x3c2e1a, "mangrove": 0x544429, "cherry": 0x371c22, "crimson": 0x5c1a1e, "warped": 0x3a3a4d,
	"pale_oak": 0x57504c, "bamboo": 0x7f9131,
}

var materialColors = map[string]uint32{
	"stone": 0x7e7e7e, "cobblestone": 0x7a7a7a, "mossy_cobblestone": 0x6e7a5f, "stone_brick": 0x7a7979,
	"mossy_stone_brick": 0x737e68, "smooth_stone": 0x9e9e9e, "granite": 0x95675a, "polished_granite": 0x9a6a59,
	"diorite": 0xbdbdbd, "polished_diorite": 0xc1c1c3, "andesite": 0x888889, "polished_andesite": 0x848686,
	"cobbled_deepslate": 0x4d4d51, "polished_deepslate": 0x484849, "deepslate_brick": 0x464647, "deepslate_tile": 0x363637,
	"tuff": 0x6c6d66, "polished_tuff": 0x626963, "tuff_brick": 0x625f57, "brick": 0x966152, "sandstone": 0xd8cb9b,
	"smooth_sandstone": 0xe0d6aa, "cut_sandstone": 0xd9cd9e, "red_sandstone": 0xba6620, "smooth_red_sandstone": 0xb5621f,
	"cut_red_sandstone": 0xbc6722, "nether_brick": 0x2c161a, "red_nether_brick": 0x460709, "quartz": 0xebe5de,
	"smooth_quartz": 0xebe5de, "prismarine": 0x63a499, "prismarine_brick": 0x63ac9e, "dark_prismarine": 0x335b4c,
	"purpur": 0xa97ea9, "end_stone_brick": 0xdadea0, "blackstone": 0x2a2328, "polished_blackstone": 0x353038,
	"polished_blackstone_brick": 0x302a30, "mud_brick": 0x89684f, "resin_brick": 0xc8582b,
	"cut_copper": 0xbf6b51, "exposed_cut_copper": 0x9a7965, "weathered_cut_copper": 0x6d9170, "oxidized_cut_copper": 0x50997f,
	"petrified_oak": 0xa2834f,
}

var dyeColors = map[string][4]uint32{ // wool, concrete, terracotta, glass
	"white":      {0xe9ecec, 0xcfd5d6, 0xd1b2a1, 0xf5f5f5},
	"orange":     {0xf07613, 0xe06100, 0xa15325, 0xd87f33},
	"magenta":    {0xbd44b3, 0xa9309f, 0x95576c, 0xb24cd8},
	"light_blue": {0x3aafd9, 0x2389c6, 0x706c8a, 0x6699d8},
	"yellow":     {0xf8c627, 0xf0af15, 0xba8523, 0xe5e533},
	"lime":       {0x70b919, 0x5ea818, 0x677534, 0x7fcc19},
	"pink":       {0xed8dac, 0xd5658e, 0xa14e4e, 0xf27fa5},
	"gray":       {0x3e4447, 0x36393d, 0x392a23, 0x4c4c4c},
	"light_gray": {0x8e8e86, 0x7d7d73, 0x876a61, 0x999999},
	"cyan":       {0x158991, 0x157788, 0x575b5b, 0x4c7f99},
	"purple":     {0x792aac, 0x64209c, 0x764656, 0x7f3fb2},
	"blue":       {0x35399d, 0x2c2e8f, 0x4a3b5b, 0x334cb2},
	"brown":      {0x724728, 0x603b1f, 0x4d3323, 0x664c33},
	"green":      {0x546d1b, 0x495b24, 0x4c532a, 0x667f33},
	"red":        {0xa12722, 0x8e2020, 0x8f3d2e, 0x993333},
	"black":      {0x141519, 0x080a0f, 0x251610, 0x191919},
}

var dyeOrder = []string{"light_blue", "light_gray", "white", "orange", "magenta", "yellow", "lime", "pink", "gray", "cyan", "purple", "blue", "brown", "green", "red", "black"}

// invisible blocks are skipped when searching the visible surface.
var invisible = map[string]bool{
	"air": true, "cave_air": true, "void_air": true, "light": true, "barrier": true, "structure_void": true,
	"torch": true, "wall_torch": true, "soul_torch": true, "soul_wall_torch": true, "redstone_torch": true,
	"redstone_wall_torch": true, "copper_torch": true, "copper_wall_torch": true, "tripwire": true, "tripwire_hook": true,
	"redstone_wire": true, "lever": true, "string": true, "ladder": true, "glass_pane": true, "iron_bars": true,
	"chain": true, "end_rod": false, "repeater": false, "comparator": false, "flower_pot": true, "moving_piston": true,
	"piston_head": false, "nether_portal": true, "cobweb": true, "frogspawn": true, "item_frame": true,
	"powder_snow_cauldron": false, "scaffolding": false, "lightning_rod": true,
}

var waterPlants = map[string]bool{
	"water": true, "bubble_column": true, "seagrass": true, "tall_seagrass": true, "kelp": true, "kelp_plant": true,
	"sea_pickle": false,
}

var (
	infoMu    sync.RWMutex
	infoCache = map[string]*BlockInfo{}
)

// Info returns the render info for a namespaced block id (cached).
func Info(id string) *BlockInfo {
	infoMu.RLock()
	bi, ok := infoCache[id]
	infoMu.RUnlock()
	if ok {
		return bi
	}
	bi = compute(id)
	infoMu.Lock()
	infoCache[id] = bi
	infoMu.Unlock()
	return bi
}

func hasPrefixAny(s string, list map[string]uint32) (string, uint32, bool) {
	best := ""
	var col uint32
	for k, v := range list {
		if strings.HasPrefix(s, k+"_") && len(k) > len(best) {
			best, col = k, v
		}
	}
	return best, col, best != ""
}

func compute(id string) *BlockInfo {
	name := id
	if i := strings.IndexByte(name, ':'); i >= 0 {
		if name[:i] != "minecraft" {
			// modded block: hash based color, but honour common naming
			name = name[i+1:]
			if bi := heuristic(name); bi != nil {
				return bi
			}
			return &BlockInfo{Color: hashColor(id)}
		}
		name = name[i+1:]
	}
	if invisible[name] {
		return &BlockInfo{Invisible: true}
	}
	if waterPlants[name] {
		return &BlockInfo{Color: hexRGB(blockColors["water"]), Tint: TintWater, Water: true}
	}
	if name == "lava" {
		return &BlockInfo{Color: hexRGB(blockColors["lava"]), Lava: true}
	}
	if strings.HasPrefix(name, "waxed_") {
		name = strings.TrimPrefix(name, "waxed_")
	}
	if c, ok := blockColors[name]; ok {
		bi := &BlockInfo{Color: hexRGB(c)}
		switch name {
		case "grass_block", "short_grass", "grass", "tall_grass", "fern", "large_fern", "sugar_cane":
			bi.Tint = TintGrass
		case "vine":
			bi.Tint = TintFoliage
		case "water", "water_cauldron":
			bi.Tint = TintWater
		}
		return bi
	}
	if bi := heuristic(name); bi != nil {
		return bi
	}
	return &BlockInfo{Color: hashColor(id)}
}

func heuristic(name string) *BlockInfo {
	switch {
	case strings.HasSuffix(name, "_leaves"):
		return &BlockInfo{Color: hexRGB(0x59ae30), Tint: TintFoliage}
	case strings.HasSuffix(name, "_log") || strings.HasSuffix(name, "_wood") || strings.HasSuffix(name, "_stem") || strings.HasSuffix(name, "_hyphae"):
		base := strings.TrimPrefix(name, "stripped_")
		for _, suf := range []string{"_log", "_wood", "_stem", "_hyphae"} {
			base = strings.TrimSuffix(base, suf)
		}
		if strings.HasPrefix(name, "stripped_") {
			if c, ok := woodColors[base]; ok {
				return &BlockInfo{Color: hexRGB(c)}
			}
		}
		if c, ok := barkColors[base]; ok {
			return &BlockInfo{Color: hexRGB(c)}
		}
		return &BlockInfo{Color: hexRGB(0x6d5534)}
	case strings.HasSuffix(name, "_sapling") || strings.HasSuffix(name, "_propagule"):
		return &BlockInfo{Color: hexRGB(0x4f7f28), Tint: TintFoliage}
	case strings.Contains(name, "coral"):
		if strings.HasPrefix(name, "dead_") {
			return &BlockInfo{Color: hexRGB(0x847d79)}
		}
		for _, c := range []string{"tube", "brain", "bubble", "fire", "horn"} {
			if strings.HasPrefix(name, c) {
				return &BlockInfo{Color: hexRGB(blockColors[c+"_coral_block"]), Water: !strings.HasSuffix(name, "_block")}
			}
		}
	case strings.HasSuffix(name, "_ore"):
		if strings.HasPrefix(name, "deepslate_") {
			return &BlockInfo{Color: hexRGB(0x5a5a5e)}
		}
		return &BlockInfo{Color: hexRGB(0x7d7d7d)}
	case strings.Contains(name, "copper"):
		switch {
		case strings.Contains(name, "oxidized"):
			return &BlockInfo{Color: hexRGB(0x52a285)}
		case strings.Contains(name, "weathered"):
			return &BlockInfo{Color: hexRGB(0x6c9970)}
		case strings.Contains(name, "exposed"):
			return &BlockInfo{Color: hexRGB(0xa27e68)}
		}
		return &BlockInfo{Color: hexRGB(0xc06c50)}
	case strings.HasSuffix(name, "_carpet") && name != "moss_carpet":
		for _, d := range dyeOrder {
			if strings.HasPrefix(name, d+"_") {
				return &BlockInfo{Color: hexRGB(dyeColors[d][0])}
			}
		}
	case strings.HasSuffix(name, "_bed") || strings.HasSuffix(name, "_banner") || strings.HasSuffix(name, "_candle") || strings.HasSuffix(name, "_shulker_box") || strings.HasSuffix(name, "_wool"):
		if strings.HasSuffix(name, "_wall_banner") {
			return &BlockInfo{Invisible: true}
		}
		for _, d := range dyeOrder {
			if strings.HasPrefix(name, d+"_") {
				return &BlockInfo{Color: hexRGB(dyeColors[d][0])}
			}
		}
	case strings.HasSuffix(name, "_concrete") || strings.HasSuffix(name, "_concrete_powder"):
		for _, d := range dyeOrder {
			if strings.HasPrefix(name, d+"_") {
				c := hexRGB(dyeColors[d][1])
				if strings.HasSuffix(name, "_powder") {
					c = mix(c, RGB{255, 255, 255}, 0.15)
				}
				return &BlockInfo{Color: c}
			}
		}
	case strings.HasSuffix(name, "_terracotta"):
		for _, d := range dyeOrder {
			if strings.HasPrefix(name, d+"_") {
				return &BlockInfo{Color: hexRGB(dyeColors[d][2])}
			}
		}
	case strings.HasSuffix(name, "_stained_glass") || strings.HasSuffix(name, "_stained_glass_pane"):
		if strings.HasSuffix(name, "_pane") {
			return &BlockInfo{Invisible: true}
		}
		for _, d := range dyeOrder {
			if strings.HasPrefix(name, d+"_") {
				return &BlockInfo{Color: hexRGB(dyeColors[d][3])}
			}
		}
	case strings.HasSuffix(name, "_sign") || strings.HasSuffix(name, "_button") || strings.HasSuffix(name, "_head") || strings.HasSuffix(name, "_skull") || strings.HasSuffix(name, "_rail") || strings.HasPrefix(name, "potted_"):
		return &BlockInfo{Invisible: true}
	}
	for _, suf := range []string{"_planks", "_stairs", "_slab", "_fence_gate", "_fence", "_door", "_trapdoor", "_pressure_plate", "_wall", "_mosaic"} {
		if !strings.HasSuffix(name, suf) {
			continue
		}
		base := strings.TrimSuffix(name, suf)
		if c, ok := woodColors[base]; ok {
			return &BlockInfo{Color: hexRGB(c)}
		}
		if c, ok := materialColors[base]; ok {
			return &BlockInfo{Color: hexRGB(c)}
		}
		if c, ok := blockColors[base]; ok {
			return &BlockInfo{Color: hexRGB(c)}
		}
		if c, ok := materialColors[strings.TrimSuffix(base, "s")]; ok {
			return &BlockInfo{Color: hexRGB(c)}
		}
		if _, c, ok := hasPrefixAny(base+"_", woodColors); ok {
			return &BlockInfo{Color: hexRGB(c)}
		}
		if suf == "_pressure_plate" {
			if strings.HasPrefix(name, "light_weighted") {
				return &BlockInfo{Color: hexRGB(blockColors["gold_block"])}
			}
			if strings.HasPrefix(name, "heavy_weighted") {
				return &BlockInfo{Color: hexRGB(blockColors["iron_block"])}
			}
		}
	}
	if strings.HasPrefix(name, "infested_") {
		return Info("minecraft:" + strings.TrimPrefix(name, "infested_"))
	}
	if strings.HasPrefix(name, "chiseled_") || strings.HasPrefix(name, "cracked_") || strings.HasPrefix(name, "polished_") {
		for _, p := range []string{"chiseled_", "cracked_", "polished_"} {
			if base := strings.TrimPrefix(name, p); base != name {
				if c, ok := blockColors[base]; ok {
					return &BlockInfo{Color: hexRGB(c)}
				}
				if c, ok := materialColors[strings.TrimSuffix(base, "s")]; ok {
					return &BlockInfo{Color: hexRGB(c)}
				}
			}
		}
	}
	return nil
}

func hashColor(id string) RGB {
	h := fnv.New32a()
	h.Write([]byte(id))
	v := h.Sum32()
	// muted colors around gray so unknown blocks do not dominate the map
	return RGB{uint8(90 + v%70), uint8(90 + (v>>8)%70), uint8(90 + (v>>16)%70)}
}

func mix(a, b RGB, t float64) RGB {
	return RGB{
		uint8(float64(a.R)*(1-t) + float64(b.R)*t),
		uint8(float64(a.G)*(1-t) + float64(b.G)*t),
		uint8(float64(a.B)*(1-t) + float64(b.B)*t),
	}
}

// ---- biomes ----

type biomeTint struct{ grass, foliage, water uint32 }

var biomeTints = map[string]biomeTint{
	"plains": {0x91bd59, 0x77ab2f, 0x3f76e4}, "sunflower_plains": {0x91bd59, 0x77ab2f, 0x3f76e4},
	"meadow": {0x83bb6d, 0x63a948, 0x0e4ecf}, "cherry_grove": {0xb6db61, 0xb6db61, 0x5db7ef},
	"forest": {0x79c05a, 0x59ae30, 0x3f76e4}, "flower_forest": {0x79c05a, 0x59ae30, 0x3f76e4},
	"birch_forest": {0x88bb67, 0x6ba941, 0x3f76e4}, "old_growth_birch_forest": {0x88bb67, 0x6ba941, 0x3f76e4},
	"dark_forest": {0x507a32, 0x59ae30, 0x3f76e4}, "pale_garden": {0x778272, 0x878d76, 0x76889d},
	"taiga": {0x86b783, 0x68a464, 0x287082}, "old_growth_pine_taiga": {0x86b87f, 0x68a55f, 0x3f76e4},
	"old_growth_spruce_taiga": {0x86b87f, 0x68a55f, 0x3f76e4}, "snowy_taiga": {0x80b497, 0x60a17b, 0x205e83},
	"snowy_plains": {0x80b497, 0x60a17b, 0x3f76e4}, "ice_spikes": {0x80b497, 0x60a17b, 0x3f76e4},
	"snowy_slopes": {0x80b497, 0x60a17b, 0x3f76e4}, "grove": {0x80b497, 0x60a17b, 0x3f76e4},
	"frozen_peaks": {0x80b497, 0x60a17b, 0x3f76e4}, "jagged_peaks": {0x80b497, 0x60a17b, 0x3f76e4},
	"stony_peaks": {0x9abe4b, 0x82ac1e, 0x3f76e4}, "snowy_beach": {0x83b593, 0x64a278, 0x3d57d6},
	"frozen_river": {0x80b497, 0x60a17b, 0x3938c9}, "frozen_ocean": {0x8eb971, 0x71a74d, 0x3938c9},
	"deep_frozen_ocean": {0x8eb971, 0x71a74d, 0x3938c9}, "cold_ocean": {0x8eb971, 0x71a74d, 0x3d57d6},
	"deep_cold_ocean": {0x8eb971, 0x71a74d, 0x3d57d6}, "ocean": {0x8eb971, 0x71a74d, 0x3f76e4},
	"deep_ocean": {0x8eb971, 0x71a74d, 0x3f76e4}, "lukewarm_ocean": {0x8eb971, 0x71a74d, 0x45adf2},
	"deep_lukewarm_ocean": {0x8eb971, 0x71a74d, 0x45adf2}, "warm_ocean": {0x8eb971, 0x71a74d, 0x43d5ee},
	"river": {0x8eb971, 0x71a74d, 0x3f76e4}, "beach": {0x91bd59, 0x77ab2f, 0x3f76e4},
	"stony_shore": {0x8ab689, 0x6da36b, 0x3f76e4}, "jungle": {0x59c93c, 0x30bb0b, 0x3f76e4},
	"sparse_jungle": {0x64c73f, 0x3eb80f, 0x3f76e4}, "bamboo_jungle": {0x59c93c, 0x30bb0b, 0x3f76e4},
	"swamp": {0x6a7039, 0x6a7039, 0x617b64}, "mangrove_swamp": {0x6a7039, 0x8db127, 0x3a7a6a},
	"desert": {0xbfb755, 0xaea42a, 0x32a598}, "savanna": {0xbfb755, 0xaea42a, 0x2c8b9c},
	"savanna_plateau": {0xbfb755, 0xaea42a, 0x2590a8}, "windswept_savanna": {0xbfb755, 0xaea42a, 0x2590a8},
	"badlands": {0x90814d, 0x9e814d, 0x4e7f81}, "wooded_badlands": {0x90814d, 0x9e814d, 0x497f99},
	"eroded_badlands": {0x90814d, 0x9e814d, 0x497f99}, "windswept_hills": {0x8ab689, 0x6da36b, 0x007bf7},
	"windswept_gravelly_hills": {0x8ab689, 0x6da36b, 0x0e63ab}, "windswept_forest": {0x8ab689, 0x6da36b, 0x0e63ab},
	"mushroom_fields": {0x55c93f, 0x2bbb0f, 0x8a8997}, "dripstone_caves": {0x91bd59, 0x77ab2f, 0x3f76e4},
	"lush_caves": {0x91bd59, 0x77ab2f, 0x3f76e4}, "deep_dark": {0x91bd59, 0x77ab2f, 0x3f76e4},
}

var defaultTint = biomeTints["plains"]

// legacyBiomes maps pre-1.18 numeric biome ids to names (subset, enough for tinting).
var legacyBiomes = map[int32]string{
	0: "ocean", 1: "plains", 2: "desert", 3: "windswept_hills", 4: "forest", 5: "taiga", 6: "swamp", 7: "river",
	10: "frozen_ocean", 11: "frozen_river", 12: "snowy_plains", 13: "snowy_slopes", 14: "mushroom_fields",
	15: "mushroom_fields", 16: "beach", 17: "desert", 18: "forest", 19: "taiga", 20: "windswept_hills", 21: "jungle",
	22: "jungle", 23: "sparse_jungle", 24: "deep_ocean", 25: "stony_shore", 26: "snowy_beach", 27: "birch_forest",
	28: "birch_forest", 29: "dark_forest", 30: "snowy_taiga", 31: "snowy_taiga", 32: "old_growth_pine_taiga",
	33: "old_growth_pine_taiga", 34: "windswept_forest", 35: "savanna", 36: "savanna_plateau", 37: "badlands",
	38: "wooded_badlands", 39: "badlands", 44: "warm_ocean", 45: "lukewarm_ocean", 46: "cold_ocean",
	47: "warm_ocean", 48: "deep_lukewarm_ocean", 49: "deep_cold_ocean", 50: "deep_frozen_ocean", 129: "sunflower_plains",
	130: "desert", 131: "windswept_gravelly_hills", 132: "flower_forest", 133: "taiga", 134: "swamp", 140: "ice_spikes",
	149: "jungle", 151: "sparse_jungle", 155: "old_growth_birch_forest", 156: "old_growth_birch_forest",
	157: "dark_forest", 158: "snowy_taiga", 160: "old_growth_spruce_taiga", 161: "old_growth_spruce_taiga",
	162: "windswept_gravelly_hills", 163: "windswept_savanna", 164: "windswept_savanna", 165: "eroded_badlands",
	166: "wooded_badlands", 167: "badlands", 168: "bamboo_jungle", 169: "bamboo_jungle",
}

func tintFor(biome string) biomeTint {
	if i := strings.IndexByte(biome, ':'); i >= 0 {
		biome = biome[i+1:]
	}
	if t, ok := biomeTints[biome]; ok {
		return t
	}
	return defaultTint
}
