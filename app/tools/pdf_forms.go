package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem/z"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

const pdfFormsTimeout = 10 * time.Minute

// The actions of the forms tool.
const (
	formList    = "list"
	formFill    = "fill"
	formFlatten = "flatten"
)

// PDFFormValue is one field to fill.
type PDFFormValue struct {
	Name  string `json:"name"  validate:"required,max=512" doc:"The field's full name, as the list action reports it."`
	Value string `json:"value" validate:"max=10000"        doc:"The value. A checkbox takes true or false; a radio group or a choice takes one of its options."`
}

// PDFFormsOptions is the input of the forms tool.
type PDFFormsOptions struct {
	// Action is list, fill or flatten.
	Action string `json:"action" validate:"trim,max=16" doc:"list reports the fields as JSON; fill sets the values given; flatten turns the form into plain page content. Default list."`
	// Values are the fields to set, for fill.
	Values []PDFFormValue `json:"values" validate:"max=1000,dive" doc:"The fields to fill, for the fill action."`
	// Flatten also flattens after a fill.
	Flatten bool `json:"flatten" doc:"Flatten the form after filling it, so the values can no longer be edited."`
}

// Normalise implements the option contract.
func (o PDFFormsOptions) Normalise() (PDFFormsOptions, error) {
	if o.Action = strings.ToLower(strings.TrimSpace(o.Action)); o.Action == "" {
		o.Action = formList
	}

	switch o.Action {
	case formList, formFlatten:
		if len(o.Values) > 0 {
			return PDFFormsOptions{}, z.FieldError("options.values", "Only the fill action takes values.")
		}
	case formFill:
		if len(o.Values) == 0 {
			return PDFFormsOptions{}, z.FieldError("options.values", "Give at least one field to fill.")
		}
	default:
		return PDFFormsOptions{}, z.FieldError("options.action", "The action is one of: list, fill, flatten.")
	}

	seen := make(map[string]bool, len(o.Values))

	for i := range o.Values {
		o.Values[i].Name = strings.TrimSpace(o.Values[i].Name)
		if o.Values[i].Name == "" {
			return PDFFormsOptions{}, z.FieldError("options.values", "Every value needs a field name.")
		}

		if seen[o.Values[i].Name] {
			return PDFFormsOptions{}, z.FieldError("options.values",
				"The field "+strconv.Quote(o.Values[i].Name)+" is given twice.")
		}

		seen[o.Values[i].Name] = true
	}

	if o.Action != formFill {
		o.Flatten = false
	}

	return o, nil
}

// pdfFormTools is the forms tool (S6.6). It edits the file's field
// dictionaries through qpdf, so it needs no engine the service did not have.
func pdfFormTools(deps *pdfDeps) []jobs.Tool {
	return []jobs.Tool{&pdfTool[PDFFormsOptions]{
		deps: deps, name: NamePDFForms, ability: abilities.PDFEdit,
		timeout: pdfFormsTimeout, accepted: pdfInputTypes,
		engines: []string{pdf.EngineQPDF}, maxInputs: 1,
		work: formsPDF,
	}}
}

// formsPDF lists, fills or flattens a form.
func formsPDF(ctx context.Context, w pdfWork[PDFFormsOptions]) (pdfResult, error) {
	in := w.Inputs[0]

	doc, err := w.Engines.Inspect(ctx, w.Dir, in.Path)
	if err != nil {
		return pdfResult{}, err
	}

	if w.Options.Action == formList {
		fields, err := w.Engines.FormFields(ctx, w.Dir, in.Path)
		if err != nil {
			return pdfResult{}, err
		}

		out := filepath.Join(w.Dir, "fields.json")

		body, err := json.MarshalIndent(map[string]any{"fields": fields}, "", "  ")
		if err != nil {
			return pdfResult{}, err
		}

		if err := os.WriteFile(out, body, 0o600); err != nil {
			return pdfResult{}, err
		}

		return pdfResult{
			Outputs: []pdfOutput{{Path: out, Name: in.Stem() + "-fields.json", MIME: "application/json"}},
			Message: strconv.Itoa(len(fields)) + " form fields found in " + strconv.Itoa(doc.Count) + " pages.",
		}, nil
	}

	values := make([]pdf.FormValue, 0, len(w.Options.Values))
	for _, v := range w.Options.Values {
		values = append(values, pdf.FormValue{Name: v.Name, Value: v.Value})
	}

	out := filepath.Join(w.Dir, "form.pdf")

	set, err := w.Engines.FillForm(ctx, pdf.FormRequest{
		Dir: w.Dir, Input: in.Path, Output: out,
		Values: values, Flatten: w.Options.Action == formFlatten || w.Options.Flatten,
	})
	if err != nil {
		return pdfResult{}, err
	}

	name, message := in.Stem()+"-filled.pdf", "Filled "+strconv.Itoa(set)+" fields."

	if w.Options.Action == formFlatten {
		name, message = in.Stem()+"-flattened.pdf", "Flattened the form into the page."
	} else if w.Options.Flatten {
		message += " The form was flattened, so the values can no longer be edited."
	}

	return pdfResult{Outputs: []pdfOutput{{Path: out, Name: name, MIME: mimePDF}}, Message: message}, nil
}
