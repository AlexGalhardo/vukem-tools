package routes

// spriteRequestExamples maps each sprite operation's id to its request body
// example, and the capability answer to its own.
//
// A file of its own, as every other domain has, so adding a tool is one line
// rather than a conflict in a shared map.
//
// Both examples are complete, valid requests that would actually run: the
// document is what somebody copies from, and an example that has to be fixed
// before it works is worse than none.
func spriteRequestExamples() map[string]any {
	return map[string]any{
		"tools.sprite-split": map[string]any{
			"file_id":         "01JQ8Z5X0K7M3N9P2R4T6V8W0Y",
			"idempotency_key": "hero-walk-cycle",
			"options": map[string]any{
				"mode":     "grid",
				"cell_w":   32,
				"cell_h":   32,
				"margin":   0,
				"spacing":  0,
				"trim":     true,
				"prefix":   "walk",
				"metadata": []any{"json-hash", "libgdx"},
				"aseprite": true,
				"frame_ms": 80,
			},
		},
		"tools.sprite-build": map[string]any{
			"file_id":         "01JQ8Z6A1M8N4P0R3T5V7X9Z1B",
			"idempotency_key": "hero-atlas-v3",
			"options": map[string]any{
				"layout":   "compact",
				"margin":   2,
				"padding":  2,
				"extrude":  1,
				"max_size": 2048,
				"pot":      true,
				"metadata": []any{"json-array", "sparrow"},
			},
		},
	}
}

// spriteCapabilitiesExample is the format list, abridged to two entries: the
// document is read, and five near-identical blocks teach nothing the first
// two do not.
func spriteCapabilitiesExample() map[string]any {
	return map[string]any{
		"formats": []any{
			map[string]any{
				"name":      "json-hash",
				"extension": ".json",
				"mime":      "application/json",
				"imports":   "Phaser 3 (load.atlas) and PixiJS. The usual choice for a web game.",
				"trim":      true,
			},
			map[string]any{
				"name":      "css",
				"extension": ".css",
				"mime":      "text/css",
				"imports":   "A web page, as a CSS sprite sheet: one class per frame. Cannot express trimming.",
				"trim":      false,
			},
		},
		"modes":       []any{"grid", "auto"},
		"layouts":     []any{"grid", "compact"},
		"max_formats": 4,
		"max_frames":  2048,
		"max_size":    8192,
	}
}
