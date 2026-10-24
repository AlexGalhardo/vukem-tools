package pdf

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// MaxFormFields caps how many field values one fill may set, so a request
// cannot make the structural JSON edit unbounded.
const MaxFormFields = 1000

// FormField is one fillable field as a person would describe it.
//
// A radio group is one field here even though the file stores one widget per
// choice: Options lists the choices and Value is the chosen one, which is
// what a form filler wants to see.
type FormField struct {
	// Name is the field's full name, the key a fill is addressed by.
	Name string `json:"name"`
	// Type is text, checkbox, radio, choice or button.
	Type string `json:"type"`
	// Value is the current value. A checkbox is "true" or "false"; a radio
	// group is the chosen option or empty.
	Value string `json:"value"`
	// Options are a choice field's entries or a radio group's options.
	Options []string `json:"options,omitempty"`
	// ReadOnly fields cannot be filled.
	ReadOnly bool `json:"read_only"`
	// Page is the first page the field appears on.
	Page int `json:"page"`
}

// FormValue is one value to put in a field.
type FormValue struct {
	Name  string
	Value string
}

// FormRequest is a fill or a flatten.
type FormRequest struct {
	// Dir is the job's working directory.
	Dir string
	// Input and Output are PDFs inside Dir.
	Input, Output string
	// Values are the fields to set. Empty with Flatten set only flattens.
	Values []FormValue
	// Flatten bakes every field into the page afterwards, so the result is a
	// plain document that cannot be edited as a form.
	Flatten bool
}

// formDoc is the part of qpdf's JSON this file reads.
type formDoc struct {
	Acroform struct {
		Has    bool        `json:"hasacroform"`
		Fields []qpdfField `json:"fields"`
	} `json:"acroform"`
	Qpdf []json.RawMessage `json:"qpdf"`
}

type qpdfField struct {
	Annotation struct {
		Object string `json:"object"`
	} `json:"annotation"`
	Choices    []string `json:"choices"`
	FieldFlags int      `json:"fieldflags"`
	FieldType  string   `json:"fieldtype"`
	FullName   string   `json:"fullname"`
	IsCheckbox bool     `json:"ischeckbox"`
	IsChoice   bool     `json:"ischoice"`
	IsRadio    bool     `json:"isradiobutton"`
	IsText     bool     `json:"istext"`
	Object     string   `json:"object"`
	Page       int      `json:"pageposfrom1"`
	Parent     *string  `json:"parent"`
	Value      string   `json:"value"`
}

// pdfDict is a PDF dictionary as qpdf's JSON writes it.
type pdfDict = map[string]any

const (
	flagReadOnly   = 1
	flagComboEdit  = 1 << 18
	offState       = "/Off"
	defaultOnState = "/Yes"
)

// FormFields lists a document's fillable fields.
func (e *Engines) FormFields(ctx context.Context, dir, path string) ([]FormField, error) {
	form, objects, err := e.readForm(ctx, dir, path)
	if err != nil {
		return nil, err
	}

	return describeForm(form, objects), nil
}

// readForm reads the field list and the object table in one pass over the
// file.
func (e *Engines) readForm(ctx context.Context, dir, path string) (formDoc, map[string]pdfDict, error) {
	var form formDoc

	err := e.runJSON(ctx, command{
		engine:  EngineQPDF,
		args:    []string{"--json=latest", "--json-key=acroform", "--json-key=qpdf", path},
		dir:     dir,
		timeout: inspectTimeout,
	}, &form)
	if err != nil {
		return formDoc{}, nil, err
	}

	objects := map[string]pdfDict{}

	if len(form.Qpdf) > 1 {
		var table map[string]struct {
			Value pdfDict `json:"value"`
		}

		if err := json.Unmarshal(form.Qpdf[1], &table); err != nil {
			return formDoc{}, nil, &DamagedError{Detail: "could not read the form's objects: " + err.Error()}
		}

		for key, obj := range table {
			if obj.Value != nil {
				objects[strings.TrimPrefix(key, "obj:")] = obj.Value
			}
		}
	}

	return form, objects, nil
}

// onStates returns the appearance names a widget can be switched to, which
// are the keys of its normal appearance dictionary except /Off. They are the
// only values a checkbox or a radio button can hold.
func onStates(objects map[string]pdfDict, widget pdfDict) []string {
	ap := asDict(objects, widget["/AP"])
	normal := asDict(objects, ap["/N"])

	var out []string

	for key := range normal {
		if key != offState {
			out = append(out, key)
		}
	}

	sort.Strings(out)

	return out
}

// asDict follows a reference if the value is one.
func asDict(objects map[string]pdfDict, v any) pdfDict {
	switch t := v.(type) {
	case pdfDict:
		return t
	case string:
		return objects[t]
	}

	return nil
}

// plain turns qpdf's string encoding into text: "u:" is Unicode, "b:" is hex
// bytes and "/Name" is a name.
func plain(v string) string {
	switch {
	case strings.HasPrefix(v, "u:"):
		return v[2:]
	case strings.HasPrefix(v, "b:"):
		b, err := hex.DecodeString(v[2:])
		if err != nil {
			return ""
		}

		return string(b)
	case strings.HasPrefix(v, "/"):
		return v[1:]
	}

	return v
}

// describeForm folds the widgets into fields, one entry per name.
func describeForm(form formDoc, objects map[string]pdfDict) []FormField {
	byName := map[string]*FormField{}

	var order []string

	for i := range form.Acroform.Fields {
		f := &form.Acroform.Fields[i]
		if f.FullName == "" {
			continue
		}

		field, seen := byName[f.FullName]
		if !seen {
			field = &FormField{
				Name:     f.FullName,
				Type:     kindOf(f),
				Page:     f.Page,
				ReadOnly: f.FieldFlags&flagReadOnly != 0,
			}
			byName[f.FullName] = field
			order = append(order, f.FullName)

			switch field.Type {
			case "checkbox":
				field.Value = strconv.FormatBool(f.Value != "" && f.Value != offState)
			case "radio":
				if f.Value != offState {
					field.Value = plain(f.Value)
				}
			default:
				field.Value = plain(f.Value)
			}

			field.Options = slices.Clone(f.Choices)
		}

		if field.Type == "radio" {
			for _, s := range onStates(objects, objects[f.Annotation.Object]) {
				if !slices.Contains(field.Options, plain(s)) {
					field.Options = append(field.Options, plain(s))
				}
			}

			if f.Value != "" && f.Value != offState {
				field.Value = plain(f.Value)
			}
		}
	}

	out := make([]FormField, 0, len(order))
	for _, name := range order {
		out = append(out, *byName[name])
	}

	return out
}

func kindOf(f *qpdfField) string {
	switch {
	case f.IsCheckbox:
		return "checkbox"
	case f.IsRadio:
		return "radio"
	case f.IsChoice:
		return "choice"
	case f.IsText:
		return "text"
	}

	return "button"
}

// FillForm sets field values and optionally flattens the result. It returns
// how many fields were set.
//
// qpdf has no command to set a field, so the values are written by editing the
// field dictionaries through its JSON interface (--update-from-json): only the
// objects that change are sent back, and qpdf redraws the appearances itself
// (--generate-appearances), which is what makes the new text visible in every
// viewer and not only in those that regenerate fields on open.
func (e *Engines) FillForm(ctx context.Context, req FormRequest) (int, error) {
	if !inside(req.Dir, req.Input) || !inside(req.Dir, req.Output) {
		return 0, &OptionError{Field: "path", Reason: "The file is outside the working directory."}
	}

	if len(req.Values) > MaxFormFields {
		return 0, &OptionError{Field: "values", Reason: "A fill may set at most 1000 fields."}
	}

	form, objects, err := e.readForm(ctx, req.Dir, req.Input)
	if err != nil {
		return 0, err
	}

	if !form.Acroform.Has || len(form.Acroform.Fields) == 0 {
		return 0, &OptionError{Field: "form", Reason: "That PDF has no form fields to fill."}
	}

	changed := map[string]pdfDict{}

	for _, v := range req.Values {
		if err := setField(form, objects, changed, v); err != nil {
			return 0, err
		}
	}

	args := []string{}

	if len(changed) > 0 {
		update := filepath.Join(req.Dir, "form-update.json")
		if err := writeUpdate(update, form, changed); err != nil {
			return 0, err
		}

		args = append(args, "--update-from-json="+update)
	}

	args = append(args, "--generate-appearances")
	if req.Flatten {
		args = append(args, "--flatten-annotations=all")
	}

	args = append(args, req.Input, req.Output)

	if err := e.qpdf(ctx, req.Dir, args); err != nil {
		return 0, err
	}

	return len(req.Values), nil
}

// writeUpdate writes the changed objects in qpdf's update format.
func writeUpdate(path string, form formDoc, changed map[string]pdfDict) error {
	var header json.RawMessage
	if len(form.Qpdf) > 0 {
		header = form.Qpdf[0]
	}

	objs := make(map[string]any, len(changed))
	for ref, dict := range changed {
		objs["obj:"+ref] = map[string]any{"value": dict}
	}

	doc := map[string]any{
		"version":    2,
		"parameters": map[string]any{"decodelevel": "generalized"},
		"qpdf":       []any{header, objs},
	}

	body, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("pdf: encode the form update: %w", err)
	}

	if err := os.WriteFile(path, body, 0o600); err != nil {
		return fmt.Errorf("pdf: write the form update: %w", err)
	}

	return nil
}

// truthy and falsy are the spellings a checkbox value may take.
var (
	truthy = []string{"true", "on", "yes", "1", "checked", "x"}
	falsy  = []string{"false", "off", "no", "0", ""}
)

// setField records the dictionary edits that put one value in one field.
func setField(form formDoc, objects map[string]pdfDict, changed map[string]pdfDict, v FormValue) error {
	var widgets []*qpdfField

	for i := range form.Acroform.Fields {
		if form.Acroform.Fields[i].FullName == v.Name {
			widgets = append(widgets, &form.Acroform.Fields[i])
		}
	}

	if len(widgets) == 0 {
		return &OptionError{Field: "values", Reason: "The form has no field called " + quote(v.Name) + "."}
	}

	first := widgets[0]
	if first.FieldFlags&flagReadOnly != 0 {
		return &OptionError{Field: "values", Reason: "The field " + quote(v.Name) + " is read-only."}
	}

	// The field dictionary is the widget itself when field and widget are
	// one object, and the parent when a field has widgets of its own.
	owner := first.Object
	if first.Parent != nil {
		owner = *first.Parent
	}

	edit := func(ref string) pdfDict {
		if d, ok := changed[ref]; ok {
			return d
		}

		src := objects[ref]
		if src == nil {
			return nil
		}

		d := make(pdfDict, len(src)+1)
		maps.Copy(d, src)

		changed[ref] = d

		return d
	}

	switch kindOf(first) {
	case "text":
		d := edit(owner)
		if d == nil {
			return &DamagedError{Detail: "the field's object is missing"}
		}

		d["/V"] = "u:" + v.Value
	case "choice":
		if first.FieldFlags&flagComboEdit == 0 && len(first.Choices) > 0 && !slices.Contains(first.Choices, v.Value) {
			return &OptionError{Field: "values", Reason: "The field " + quote(v.Name) + " takes one of: " +
				strings.Join(first.Choices, ", ") + "."}
		}

		d := edit(owner)
		if d == nil {
			return &DamagedError{Detail: "the field's object is missing"}
		}

		d["/V"] = "u:" + v.Value
	case "checkbox":
		return setCheckbox(objects, edit, first, owner, v)
	case "radio":
		return setRadio(objects, edit, widgets, owner, v)
	default:
		return &OptionError{
			Field:  "values",
			Reason: "The field " + quote(v.Name) + " is a button, which holds no value.",
		}
	}

	return nil
}

func setCheckbox(objects map[string]pdfDict, edit func(string) pdfDict, w *qpdfField, owner string, v FormValue) error {
	value := strings.ToLower(strings.TrimSpace(v.Value))

	var on bool

	switch {
	case slices.Contains(truthy, value):
		on = true
	case slices.Contains(falsy, value):
		on = false
	default:
		return &OptionError{Field: "values", Reason: "The checkbox " + quote(v.Name) + " takes true or false."}
	}

	state := offState

	if on {
		state = defaultOnState
		if states := onStates(objects, objects[w.Object]); len(states) > 0 {
			state = states[0]
		}
	}

	field, widget := edit(owner), edit(w.Object)
	if field == nil || widget == nil {
		return &DamagedError{Detail: "the checkbox's object is missing"}
	}

	field["/V"] = state
	widget["/AS"] = state

	return nil
}

func setRadio(
	objects map[string]pdfDict,
	edit func(string) pdfDict,
	widgets []*qpdfField,
	owner string,
	v FormValue,
) error {
	want := "/" + strings.TrimPrefix(strings.TrimSpace(v.Value), "/")

	var known []string

	found := false

	for _, w := range widgets {
		for _, s := range onStates(objects, objects[w.Object]) {
			known = append(known, plain(s))

			if s == want {
				found = true
			}
		}
	}

	if !found {
		return &OptionError{Field: "values", Reason: "The radio group " + quote(v.Name) + " takes one of: " +
			strings.Join(slices.Compact(slices.Sorted(slices.Values(known))), ", ") + "."}
	}

	field := edit(owner)
	if field == nil {
		return &DamagedError{Detail: "the radio group's object is missing"}
	}

	field["/V"] = want

	for _, w := range widgets {
		d := edit(w.Object)
		if d == nil {
			return &DamagedError{Detail: "a radio button's object is missing"}
		}

		d["/AS"] = offState

		if slices.Contains(onStates(objects, objects[w.Object]), want) {
			d["/AS"] = want
		}
	}

	return nil
}

func quote(s string) string { return "\"" + s + "\"" }
