package catalog

// spriteCatalog is the sprite sheets and frame atlases domain (S7).
//
// A catalogue entry is written when the tool is planned, not when it ships:
// Status says which. See the package comment for why a missing tool is listed
// rather than silently absent.
//
// Three entries for two tools. The third is not a tool and never will be: it
// is the answer to "can I use this with Godot or Unity", which a visitor
// comparing this with TexturePacker will look for and otherwise conclude is
// missing. Godot 4 needs no metadata file for a uniform grid and Unity's
// slicing is editor-only, so the honest deliverable for both is a grid-packed
// sheet and one instruction — and saying that here is what stops somebody
// searching for a `.tres` writer that would be undocumented and
// version-fragile if it existed.
var spriteCatalog = []Tool{
	{
		Name:    "sprite-split",
		Title:   "Split a sprite sheet",
		Summary: "Cuts a sheet into frames and writes the atlas a game engine imports.",
		In:      []string{"PNG", "GIF", "JPEG"},
		Out:     []string{"ZIP", "JSON", "XML", "CSS"},
		Group:   GroupImages,
		Icon:    "grid",
		Status:  Live,
		Keywords: []string{
			"sprite", "spritesheet", "atlas", "frames", "texturepacker", "phaser",
			"aseprite", "libgdx", "starling", "game",
		},
	},
	{
		Name:    "sprite-build",
		Title:   "Build a sprite sheet",
		Summary: "Packs a ZIP of frames into one sheet, with the atlas that describes it.",
		In:      []string{"ZIP"},
		Out:     []string{"PNG", "JSON", "XML", "CSS"},
		Group:   GroupImages,
		Icon:    "grid",
		Status:  Live,
		Keywords: []string{
			"sprite", "spritesheet", "atlas", "pack", "texturepacker", "phaser",
			"libgdx", "starling", "game", "tileset",
		},
	},
	{
		Name:    "sprite-godot-unity",
		Title:   "Sprite sheets for Godot and Unity",
		Summary: "Build a sheet in grid layout and import it with the frame size; neither engine needs a metadata file.",
		In:      []string{"ZIP"},
		Out:     []string{"PNG"},
		Group:   GroupImages,
		Icon:    "grid",
		Status:  Skipped,
		Reason: "Neither engine needs a file this service could write. Godot 4 slices a uniform " +
			"grid from AtlasTexture.region or from the frame counts on an AnimatedSprite2D, and " +
			"Unity's slicing is editor-only — SpriteMetaData has no documented text form. A " +
			"hand-written .tres would be undocumented and version-fragile, and .tpsheet is a " +
			"third-party plugin's format rather than Godot's. Use Build a sprite sheet in grid " +
			"layout and give the importer the frame size.",
		Keywords: []string{"godot", "unity", "tres", "atlastexture", "animatedsprite", "tpsheet"},
	},
}
