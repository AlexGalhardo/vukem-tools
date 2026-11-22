package toolkit_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/toolkit"
)

func mustTool(t *testing.T, name string) toolkit.Tool {
	t.Helper()

	tool, ok := toolkit.Find(name)
	if !ok {
		t.Fatalf("no tool %q in the table", name)
	}

	return tool
}

func TestValidateRefusesWhatTheSchemaRefusesBeforeAnyCall(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		tool    string
		files   int
		options map[string]any
		want    string
	}{
		{"no file", "image-convert", 0, nil, "at least 1 file"},
		{"too many files", "image-convert", 2, nil, "at most 1 file"},
		{"unknown option", "image-convert", 1, map[string]any{"colour": "red"}, "options.colour"},
		{"wrong type", "image-convert", 1, map[string]any{"quality": "high"}, "options.quality"},
		{"above maximum", "image-convert", 1, map[string]any{"quality": 101}, "at most 100"},
		{"fraction for an integer", "image-convert", 1, map[string]any{"quality": 80.5}, "whole number"},
		{"a required option", "video-download", 0, map[string]any{}, "options.url is required"},
		{
			"a file for a tool that reads none",
			"video-download",
			1,
			map[string]any{"url": "https://example.com/v"},
			"no file",
		},
		{"merge needs two", "pdf-merge", 1, nil, "at least 2 files"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			files := make([]string, tc.files)
			for i := range files {
				files[i] = "f.bin"
			}

			err := toolkit.Validate(mustTool(t, tc.tool), files, tc.options)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate = %v, want an error containing %q", err, tc.want)
			}

			var te *toolkit.Error
			if !asError(err, &te) || te.Code != toolkit.CodeInvalidInput {
				t.Fatalf("error %v has no %s code", err, toolkit.CodeInvalidInput)
			}
		})
	}
}

func TestValidateAcceptsAGoodCall(t *testing.T) {
	t.Parallel()

	if err := toolkit.Validate(
		mustTool(t, "image-convert"),
		[]string{"a.png"},
		map[string]any{"to": "webp", "quality": 80},
	); err != nil {
		t.Fatal(err)
	}

	raw := `{"crop_left":1}`

	var opts map[string]any
	if err := json.Unmarshal([]byte(raw), &opts); err != nil {
		t.Fatal(err)
	}

	if err := toolkit.Validate(mustTool(t, "image-convert"), []string{"a.png"}, opts); err != nil {
		t.Fatalf("a JSON-decoded integer was refused: %v", err)
	}
}

func asError(err error, target **toolkit.Error) bool { return errors.As(err, target) }
