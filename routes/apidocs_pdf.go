package routes

// pdfRequestExamples maps each PDF operation's id to its request body
// example.
//
// It is a file of its own for the reason the route file is: nineteen entries
// added to the shared map would make every tool a conflict with every other
// tool being added at the same time. requestExamples concatenates it.
//
// Every example is a complete, valid request that would actually run — the
// document is what somebody copies from, and an example that has to be fixed
// before it works is worse than none. The file ids are the right shape for a
// ULID and nothing here is a real credential: the passwords are obviously
// placeholders, which TestNoExampleLeaksACredential relies on.
func pdfRequestExamples() map[string]any {
	examples := map[string]any{
		"tools.pdf-merge": map[string]any{
			"file_ids": []any{
				"01JQ8Z5X0K7M3N9P2R4T6V8W0Y",
				"01JQ8Z6A1M8N4P0R3T5V7X9Z1B",
			},
			"idempotency_key": "contract-and-annexes",
			"options":         map[string]any{},
		},
		"tools.pdf-split": withFile(map[string]any{
			"mode":  "every",
			"every": 1,
		}),
		"tools.pdf-extract-pages": withFile(map[string]any{"pages": "1-3,8"}),
		"tools.pdf-remove-pages":  withFile(map[string]any{"pages": "2,5-7"}),
		"tools.pdf-organise":      withFile(map[string]any{"pages": "3,1,2"}),
		"tools.pdf-rotate": withFile(map[string]any{
			"degrees":  90,
			"pages":    "1-z",
			"absolute": false,
		}),
		"tools.pdf-crop": withFile(map[string]any{
			"left":   36,
			"bottom": 36,
			"right":  36,
			"top":    36,
			"pages":  "",
		}),
		"tools.images-to-pdf": map[string]any{
			"file_ids": []any{
				"01JQ8Z5X0K7M3N9P2R4T6V8W0Y",
				"01JQ8Z6A1M8N4P0R3T5V7X9Z1B",
			},
			"idempotency_key": "scanned-pages",
			"options": map[string]any{
				"page_size":   "a4",
				"orientation": "auto",
				"fit":         "contain",
				"margin":      18,
				"quality":     90,
			},
		},
		"tools.pdf-to-image": withFile(map[string]any{
			"format":  "png",
			"dpi":     150,
			"quality": 85,
			"pages":   "1-5",
		}),
		"tools.pdf-to-pdfa": withFile(map[string]any{"part": 2}),
		"tools.pdf-to-text": withFile(map[string]any{
			"format":     "markdown",
			"layout":     true,
			"page_rules": false,
			"pages":      "",
		}),
		"tools.pdf-compress": withFile(map[string]any{"level": "balanced"}),
		"tools.pdf-repair":   withFile(map[string]any{}),
		"tools.pdf-protect": withFile(map[string]any{
			"user_password":  "the-password-readers-will-need",
			"owner_password": "a-different-password-you-keep",
			"print":          "low",
			"modify":         "none",
			"allow_extract":  false,
		}),
		"tools.pdf-unlock": withFile(map[string]any{
			"password": "the-password-you-already-know",
		}),
		"tools.pdf-compare": map[string]any{
			"file_ids": []any{
				"01JQ8Z5X0K7M3N9P2R4T6V8W0Y",
				"01JQ8Z6A1M8N4P0R3T5V7X9Z1B",
			},
			"idempotency_key": "contract-v1-vs-v2",
			"options":         map[string]any{},
		},
		"tools.pdf-watermark": withFile(map[string]any{
			"text":     "DRAFT",
			"pages":    "",
			"position": "center",
			"size":     0,
			"rotate":   45,
			"opacity":  0.18,
			"gray":     0,
			"bold":     true,
		}),
		"tools.pdf-page-numbers": withFile(map[string]any{
			"format":   "Page {n} of {total}",
			"pages":    "2-z",
			"start_at": 1,
			"position": "bottom-center",
			"size":     10,
		}),
	}

	return examples
}

// exampleFileID is the shape of an upload id: a ULID, as the upload endpoint
// returns.
const exampleFileID = "01JQ8Z5X0K7M3N9P2R4T6V8W0Y"

// withFile wraps a single-input tool's options in the body that carries them.
func withFile(options map[string]any) map[string]any {
	return map[string]any{
		"file_id":         exampleFileID,
		"idempotency_key": "my-retry-key",
		"options":         options,
	}
}
