package pdf_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

// writeFormFixture writes a one-page PDF with every kind of field a form
// holds: text, a checkbox, a two-button radio group and a combo box. It is
// assembled by hand, with a correct cross-reference table, so the test needs
// no engine to make its input.
func writeFormFixture(t *testing.T, dir string) string {
	t.Helper()

	stream := func(body string) string {
		return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body)
	}

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R 6 0 R 7 0 R 12 0 R 13 0 R] /DA (/Helv 0 Tf 0 g) /DR << /Font << /Helv 4 0 R >> >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 300] /Resources << /Font << /Helv 4 0 R >> >> /Annots [5 0 R 6 0 R 8 0 R 9 0 R 12 0 R 13 0 R] >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (old) /DA (/Helv 12 Tf 0 g) /Rect [20 250 200 270] /F 4 >>",
		"<< /Type /Annot /Subtype /Widget /FT /Btn /T (agree) /V /Off /AS /Off /Rect [20 220 40 240] /F 4 /AP << /N << /Yes 10 0 R /Off 11 0 R >> >> >>",
		"<< /FT /Btn /Ff 32768 /T (size) /Kids [8 0 R 9 0 R] /V /Off >>",
		"<< /Type /Annot /Subtype /Widget /Parent 7 0 R /Rect [20 190 40 210] /F 4 /AS /Off /AP << /N << /small 10 0 R /Off 11 0 R >> >> >>",
		"<< /Type /Annot /Subtype /Widget /Parent 7 0 R /Rect [60 190 80 210] /F 4 /AS /Off /AP << /N << /large 10 0 R /Off 11 0 R >> >> >>",
		stream("0 0 20 20 re S"),
		stream(""),
		"<< /Type /Annot /Subtype /Widget /FT /Ch /Ff 131072 /T (country) /Opt [(Brazil) (Chile)] /V (Brazil) /DA (/Helv 12 Tf 0 g) /Rect [20 150 200 170] /F 4 >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /Ff 1 /T (locked) /V (fixed) /DA (/Helv 12 Tf 0 g) /Rect [20 100 200 120] /F 4 >>",
	}

	var b strings.Builder

	b.WriteString("%PDF-1.7\n")

	offsets := make([]int, 0, len(objs))

	for i, o := range objs {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}

	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)

	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}

	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)

	path := filepath.Join(dir, "form.pdf")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write the form fixture: %v", err)
	}

	return path
}

func fieldsByName(fields []pdf.FormField) map[string]pdf.FormField {
	out := make(map[string]pdf.FormField, len(fields))
	for _, f := range fields {
		out[f.Name] = f
	}

	return out
}

func TestFormFieldsAreListedWithTheirKindAndOptions(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()

	fields, err := engines.FormFields(context.Background(), dir, writeFormFixture(t, dir))
	if err != nil {
		t.Fatalf("FormFields: %v", err)
	}

	got := fieldsByName(fields)

	if len(fields) != len(got) || len(got) != 5 {
		t.Fatalf("got %d fields (%d names), want the five names once each: %+v", len(fields), len(got), fields)
	}

	checks := map[string]struct{ kind, value string }{
		"name":    {"text", "old"},
		"agree":   {"checkbox", "false"},
		"size":    {"radio", ""},
		"country": {"choice", "Brazil"},
		"locked":  {"text", "fixed"},
	}

	for name, want := range checks {
		f, ok := got[name]
		if !ok || f.Type != want.kind || f.Value != want.value {
			t.Errorf("%s = %+v, want a %s holding %q", name, f, want.kind, want.value)
		}
	}

	if size := got["size"]; strings.Join(size.Options, ",") != "small,large" {
		t.Errorf("the radio group's options = %v, want small and large, in the order the form shows them", size.Options)
	}

	if !got["locked"].ReadOnly {
		t.Error("the read-only field is not marked read-only")
	}
}

func TestFillingAFormShowsTheValuesInTheDocument(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EnginePDFToText)
	dir := t.TempDir()
	in := writeFormFixture(t, dir)
	out := filepath.Join(dir, "filled.pdf")

	set, err := engines.FillForm(context.Background(), pdf.FormRequest{
		Dir: dir, Input: in, Output: out,
		Values: []pdf.FormValue{
			{Name: "name", Value: "Alice"},
			{Name: "agree", Value: "true"},
			{Name: "size", Value: "large"},
			{Name: "country", Value: "Chile"},
		},
	})
	if err != nil || set != 4 {
		t.Fatalf("FillForm = %d, %v; want 4 fields set", set, err)
	}

	fields, err := engines.FormFields(context.Background(), dir, out)
	if err != nil {
		t.Fatalf("FormFields after fill: %v", err)
	}

	got := fieldsByName(fields)
	for name, want := range map[string]string{"name": "Alice", "agree": "true", "size": "large", "country": "Chile", "locked": "fixed"} {
		if got[name].Value != want {
			t.Errorf("%s = %q after the fill, want %q", name, got[name].Value, want)
		}
	}
}

func TestFlatteningRemovesTheFormButKeepsTheValues(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EnginePDFToText)
	dir := t.TempDir()
	out := filepath.Join(dir, "flat.pdf")

	_, err := engines.FillForm(context.Background(), pdf.FormRequest{
		Dir: dir, Input: writeFormFixture(t, dir), Output: out, Flatten: true,
		Values: []pdf.FormValue{{Name: "name", Value: "Alice"}},
	})
	if err != nil {
		t.Fatalf("FillForm: %v", err)
	}

	if _, err := engines.FillForm(context.Background(), pdf.FormRequest{
		Dir: dir, Input: out, Output: filepath.Join(dir, "again.pdf"),
	}); err == nil {
		t.Error("a flattened document still has form fields")
	}

	txt := filepath.Join(dir, "flat.txt")
	if err := engines.ExtractText(
		context.Background(),
		pdf.TextRequest{Dir: dir, Input: out, Output: txt},
	); err != nil {
		t.Fatalf("ExtractText: %v", err)
	}

	body, _ := os.ReadFile(txt)
	if !strings.Contains(string(body), "Alice") {
		t.Errorf("the flattened text %q does not contain the value", body)
	}
}

func TestAFillRefusesWhatItCannotDoHonestly(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()
	in := writeFormFixture(t, dir)

	cases := map[string]pdf.FormValue{
		"an unknown field":      {Name: "nothing", Value: "x"},
		"a read-only field":     {Name: "locked", Value: "x"},
		"a bad checkbox value":  {Name: "agree", Value: "maybe"},
		"a radio option absent": {Name: "size", Value: "huge"},
		"a choice not offered":  {Name: "country", Value: "Mars"},
	}

	for name, value := range cases {
		_, err := engines.FillForm(context.Background(), pdf.FormRequest{
			Dir: dir, Input: in, Output: filepath.Join(dir, "x.pdf"), Values: []pdf.FormValue{value},
		})
		if _, ok := errors.AsType[*pdf.OptionError](err); !ok {
			t.Errorf("%s: err = %v, want an OptionError", name, err)
		}
	}
}
