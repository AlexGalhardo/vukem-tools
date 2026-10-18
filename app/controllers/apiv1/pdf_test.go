package apiv1

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/app/tools"
	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

// Every bound that appears both as a `validate` tag and as a constant has to
// agree with the constant.
//
// `validate` tags are static strings, so a bound the library already owns —
// `pdf.MaxDPI`, `pdf.MaxPasswordBytes`, `pdf.MaxMergeInputs` — is written
// twice: once in the tag, which is what the API enforces and what the OpenAPI
// document publishes, and once in the library, which is what the conversion
// enforces. Nothing in the compiler keeps the two equal, so raising a
// constant and forgetting its tag would leave the API refusing a value the
// library would have accepted, with the document stating the old number.
//
// api.RegisterWith can move the *enforcement* onto a schema built from the
// constant, but the framework's own documentation is explicit that the
// document is still generated from the tags — "keep the static rules in the
// tags and let schema add only what they cannot say, or the document
// under-describes what the route enforces". So the tag has to carry the
// number either way, and this test is what makes carrying it safe.
func TestEveryDuplicatedBoundAgreesWithItsConstant(t *testing.T) {
	t.Parallel()

	cases := []struct {
		// what names the pair in a failure, which is the whole value of the
		// message: "SubmitPDFMerge.file_ids" is findable, "case 3" is not.
		name string
		// structure is the type carrying the tag.
		structure reflect.Type
		// field is the field's `json` name.
		field string
		// rule is the validate rule to read, "max" or "min".
		rule string
		// want is the constant the rule must equal.
		want int
	}{
		{
			"SubmitPDFMerge.file_ids", reflect.TypeFor[SubmitPDFMerge](),
			"file_ids", "max", pdf.MaxMergeInputs,
		},
		{
			"SubmitImagesToPDF.file_ids", reflect.TypeFor[SubmitImagesToPDF](),
			"file_ids", "max", pdf.MaxImagesPerPDF,
		},
		{
			"PDFSplitOptions.ranges", reflect.TypeFor[tools.PDFSplitOptions](),
			"ranges", "max", pdf.MaxSplitParts,
		},
		{
			"PDFToImageOptions.dpi", reflect.TypeFor[tools.PDFToImageOptions](),
			"dpi", "max", pdf.MaxDPI,
		},
		{
			"ImagesToPDFOptions.margin", reflect.TypeFor[tools.ImagesToPDFOptions](),
			"margin", "max", pdf.MaxImageMargin,
		},
		{
			"PDFProtectOptions.user_password", reflect.TypeFor[tools.PDFProtectOptions](),
			"user_password", "max", pdf.MaxPasswordBytes,
		},
		{
			"PDFProtectOptions.owner_password", reflect.TypeFor[tools.PDFProtectOptions](),
			"owner_password", "max", pdf.MaxPasswordBytes,
		},
		{
			"PDFUnlockOptions.password", reflect.TypeFor[tools.PDFUnlockOptions](),
			"password", "max", pdf.MaxPasswordBytes,
		},
		{
			"PDFWatermarkOptions.text", reflect.TypeFor[tools.PDFWatermarkOptions](),
			"text", "max", pdf.MaxStampTextBytes,
		},
		{
			"PDFWatermarkOptions.size", reflect.TypeFor[tools.PDFWatermarkOptions](),
			"size", "max", pdf.MaxStampSize,
		},
		{
			"PDFPageNumberOptions.format", reflect.TypeFor[tools.PDFPageNumberOptions](),
			"format", "max", pdf.MaxStampTextBytes,
		},
		{
			"PDFPageNumberOptions.size", reflect.TypeFor[tools.PDFPageNumberOptions](),
			"size", "max", pdf.MaxStampSize,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got, ok := validateRule(c.structure, c.field, c.rule)
			if !ok {
				t.Fatalf("%s has no %s= rule in its validate tag, so the API enforces no bound "+
					"where the library enforces %d", c.name, c.rule, c.want)
			}

			if got != c.want {
				t.Errorf("%s has %s=%d in its validate tag, but the library's constant is %d: "+
					"the API and the conversion disagree, and the OpenAPI document publishes the tag",
					c.name, c.rule, got, c.want)
			}
		})
	}
}

// validateRule reads one numeric rule out of a field's `validate` tag,
// finding the field by its `json` name.
func validateRule(structure reflect.Type, jsonName, rule string) (int, bool) {
	for field := range structure.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name != jsonName {
			continue
		}

		for part := range strings.SplitSeq(field.Tag.Get("validate"), ",") {
			value, found := strings.CutPrefix(strings.TrimSpace(part), rule+"=")
			if !found {
				continue
			}

			n, err := strconv.Atoi(value)
			if err != nil {
				return 0, false
			}

			return n, true
		}

		return 0, false
	}

	return 0, false
}

// The submit structs all carry the two fields the shared submit path reads,
// spelled the same way. A tool whose body named its upload something else
// would be a route the generic helper cannot serve, and the mistake would
// only show up as a conversion with no input.
func TestEveryPDFSubmitBodyNamesItsUploadTheSameWay(t *testing.T) {
	t.Parallel()

	single := []reflect.Type{
		reflect.TypeFor[SubmitPDFSplit](),
		reflect.TypeFor[SubmitPDFExtractPages](),
		reflect.TypeFor[SubmitPDFRemovePages](),
		reflect.TypeFor[SubmitPDFOrganise](),
		reflect.TypeFor[SubmitPDFRotate](),
		reflect.TypeFor[SubmitPDFCrop](),
		reflect.TypeFor[SubmitPDFToImage](),
		reflect.TypeFor[SubmitPDFToPDFA](),
		reflect.TypeFor[SubmitPDFToText](),
		reflect.TypeFor[SubmitPDFCompress](),
		reflect.TypeFor[SubmitPDFRepair](),
		reflect.TypeFor[SubmitPDFProtect](),
		reflect.TypeFor[SubmitPDFUnlock](),
		reflect.TypeFor[SubmitPDFWatermark](),
		reflect.TypeFor[SubmitPDFPageNumbers](),
	}

	many := []reflect.Type{
		reflect.TypeFor[SubmitPDFMerge](),
		reflect.TypeFor[SubmitPDFCompare](),
		reflect.TypeFor[SubmitImagesToPDF](),
	}

	for _, structure := range single {
		assertFields(t, structure, "file_id", "idempotency_key", "options")
	}

	for _, structure := range many {
		assertFields(t, structure, "file_ids", "idempotency_key", "options")
	}

	if len(single)+len(many) != 18 {
		t.Errorf("%d submit bodies are checked, but 18 PDF tools have a route", len(single)+len(many))
	}
}

// assertFields checks a struct carries exactly these `json` names.
func assertFields(t *testing.T, structure reflect.Type, want ...string) {
	t.Helper()

	got := make([]string, 0, structure.NumField())

	for field := range structure.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		got = append(got, name)
	}

	if len(got) != len(want) {
		t.Fatalf("%s has fields %v, want %v", structure.Name(), got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s field %d is %q, want %q", structure.Name(), i, got[i], want[i])
		}
	}
}
