package toolkit

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
)

// Validate checks a call against the table before anything is sent: how many
// files, and every option against the schema the API publishes. The server
// validates again; this exists so a mistake costs no upload.
func Validate(tool Tool, files []string, options map[string]any) error {
	if tool.Kind != Pipeline {
		return &Error{Code: CodeBrowserOnly, Message: tool.Name + " runs in the browser and has no API call"}
	}

	if tool.Files == nil {
		if len(files) > 0 {
			return invalid(tool.Name + " takes no file")
		}
	} else {
		if len(files) < tool.Files.Min {
			return invalid(fmt.Sprintf("%s needs at least %d %s", tool.Name, tool.Files.Min, plural(tool.Files.Min)))
		}

		if len(files) > tool.Files.Max {
			return invalid(fmt.Sprintf("%s takes at most %d %s", tool.Name, tool.Files.Max, plural(tool.Files.Max)))
		}
	}

	if tool.Options == nil {
		if len(options) > 0 {
			return invalid(tool.Name + " takes no options")
		}

		return nil
	}

	if options == nil && tool.OptionsRequired {
		options = map[string]any{}
	}

	if options == nil {
		return nil
	}

	var schema map[string]any
	if err := json.Unmarshal(tool.Options, &schema); err != nil {
		return fmt.Errorf("toolkit: the schema of %s is not valid: %w", tool.Name, err)
	}

	if err := check(schema, options, "options"); err != nil {
		return invalid(err.Error())
	}

	return nil
}

func plural(n int) string {
	if n == 1 {
		return "file"
	}

	return "files"
}

// check validates v against the part of JSON Schema the API publishes:
// types, enums, bounds, lengths, items and required properties. Unknown
// properties are refused, which the server does too.
func check(schema map[string]any, v any, path string) error {
	switch str(schema["type"]) {
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}

		props, _ := schema["properties"].(map[string]any)

		for key, val := range obj {
			sub, known := props[key].(map[string]any)
			if !known {
				return fmt.Errorf(
					"%s.%s is not an option of this tool (known: %s)",
					path,
					key,
					strings.Join(keys(props), ", "),
				)
			}

			if err := check(sub, val, path+"."+key); err != nil {
				return err
			}
		}

		required, _ := schema["required"].([]any)
		for _, name := range required {
			if key, _ := name.(string); key != "" {
				if _, present := obj[key]; !present {
					return fmt.Errorf("%s.%s is required", path, key)
				}
			}
		}
	case "string":
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("%s must be a string", path)
		}

		if n, ok := num(schema["minLength"]); ok && float64(len([]rune(s))) < n {
			return fmt.Errorf("%s must have at least %d characters", path, int(n))
		}

		if n, ok := num(schema["maxLength"]); ok && float64(len([]rune(s))) > n {
			return fmt.Errorf("%s must have at most %d characters", path, int(n))
		}

		if enum, ok := schema["enum"].([]any); ok && !slices.Contains(enum, any(s)) {
			return fmt.Errorf("%s must be one of %v", path, enum)
		}
	case "integer", "number":
		f, ok := v.(float64)
		if !ok {
			if i, isInt := v.(int); isInt {
				f, ok = float64(i), true
			}
		}

		if !ok {
			return fmt.Errorf("%s must be a number", path)
		}

		if str(schema["type"]) == "integer" && f != math.Trunc(f) {
			return fmt.Errorf("%s must be a whole number", path)
		}

		if n, ok := num(schema["minimum"]); ok && f < n {
			return fmt.Errorf("%s must be at least %v", path, n)
		}

		if n, ok := num(schema["maximum"]); ok && f > n {
			return fmt.Errorf("%s must be at most %v", path, n)
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s must be true or false", path)
		}
	case "array":
		list, ok := v.([]any)
		if !ok {
			return fmt.Errorf("%s must be a list", path)
		}

		if n, ok := num(schema["minItems"]); ok && float64(len(list)) < n {
			return fmt.Errorf("%s must have at least %d items", path, int(n))
		}

		if n, ok := num(schema["maxItems"]); ok && float64(len(list)) > n {
			return fmt.Errorf("%s must have at most %d items", path, int(n))
		}

		if items, ok := schema["items"].(map[string]any); ok {
			for i, item := range list {
				if err := check(items, item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func str(v any) string {
	s, _ := v.(string)

	return s
}

func num(v any) (float64, bool) {
	f, ok := v.(float64)

	return f, ok
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	slices.Sort(out)

	return out
}
