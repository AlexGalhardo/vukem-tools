package routes

// archiveRequestExamples maps each archive operation's id to its request body
// example.
//
// A file of its own for the reason routes/apidocs_pdf.go is: a domain's
// examples added to the shared map would make every tool a conflict with every
// other tool being added at the same time. requestExamples concatenates it.
//
// Both examples are complete, valid requests that would actually run. The
// document is what somebody copies from, and an example that has to be fixed
// before it works is worse than none.
func archiveRequestExamples() map[string]any {
	return map[string]any{
		"tools.archive-create": map[string]any{
			"file_ids": []any{
				"01JQ8Z5X0K7M3N9P2R4T6V8W0Y",
				"01JQ8Z6A1M8N4P0R3T5V7X9Z1B",
			},
			"idempotency_key": "release-assets-v2",
			"options": map[string]any{
				"format": "zip",
				"level":  6,
				"name":   "release-assets",
			},
		},
		"tools.archive-extract": map[string]any{
			"file_id":         "01JQ8Z5X0K7M3N9P2R4T6V8W0Y",
			"idempotency_key": "unpack-release-assets",
			"options":         map[string]any{},
		},
	}
}
