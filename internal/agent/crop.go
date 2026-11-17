package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

// CropMargins is how many points to trim from each edge. Empty pages means
// every page; that is pdf.CropRequest's rule and it is not reimplemented here.
type CropMargins struct {
	Left, Bottom, Right, Top float64
}

// DefaultMargins trims half an inch, which is what `pdf crop` uses when the
// caller does not name an edge. The engine refuses a crop that trims nothing.
var DefaultMargins = CropMargins{Left: 36, Bottom: 36, Right: 36, Top: 36}

// CropOutcome is the machine-readable result of a crop.
type CropOutcome struct {
	Output  string `json:"output"`
	Cropped int    `json:"cropped"`
	Bytes   int64  `json:"bytes"`
}

type envLookup struct{}

func (envLookup) Get(key string) string { return os.Getenv(key) }

// OpenPDFEngines resolves qpdf and returns the engine set the crop runs on.
func OpenPDFEngines(ctx context.Context) (*pdf.Engines, error) {
	registry, err := engine.Resolve(ctx, envLookup{}, []engine.Binary{{
		Name:        pdf.EngineQPDF,
		EnvVar:      "QPDF_PATH",
		VersionArgs: []string{"--version"},
	}})
	if err != nil {
		return nil, err
	}

	if !registry.Has(pdf.EngineQPDF) {
		return nil, errors.New("qpdf is not installed")
	}

	runner, err := engine.NewRunner(engine.RunnerOptions{
		Limits: engine.Limits{Timeout: 2 * time.Minute},
	})
	if err != nil {
		return nil, err
	}

	return pdf.NewEngines(registry, runner)
}

// CropPDF crops every page of input (Pages is left empty) and writes output.
//
// The bytes are copied into a job directory and out again. The PDF is never
// read into a single buffer.
func CropPDF(
	ctx context.Context,
	engines *pdf.Engines,
	input, output string,
	margins CropMargins,
) (CropOutcome, error) {
	if engines == nil {
		return CropOutcome{}, errors.New("pdf engines are required")
	}

	if margins == (CropMargins{}) {
		margins = DefaultMargins
	}

	in, err := os.Open(input)
	if err != nil {
		return CropOutcome{}, err
	}
	defer in.Close()

	dir, err := os.MkdirTemp("", "vukem-pdf-crop-*")
	if err != nil {
		return CropOutcome{}, err
	}
	defer os.RemoveAll(dir)

	source := filepath.Join(dir, "input.pdf")
	cropped := filepath.Join(dir, "cropped.pdf")

	if err := copyFile(source, in); err != nil {
		return CropOutcome{}, err
	}

	result, err := engines.Crop(ctx, pdf.CropRequest{
		Dir:    dir,
		Input:  source,
		Output: cropped,
		Pages:  nil,
		Margins: pdf.Margins{
			Left:   margins.Left,
			Bottom: margins.Bottom,
			Right:  margins.Right,
			Top:    margins.Top,
		},
	})
	if err != nil {
		return CropOutcome{}, err
	}

	if err := copyPath(output, cropped); err != nil {
		return CropOutcome{}, err
	}

	info, err := os.Stat(output)
	if err != nil {
		return CropOutcome{}, err
	}

	return CropOutcome{Output: output, Cropped: result.Cropped, Bytes: info.Size()}, nil
}

func copyFile(dst string, src io.Reader) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(out, src)
	closeErr := out.Close()

	if copyErr != nil {
		return copyErr
	}

	return closeErr
}

func copyPath(dst, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}

	return copyFile(dst, in)
}

// WriteError writes one JSON error object. Callers exit non-zero after it.
func WriteError(w io.Writer, err error) {
	payload, marshalErr := json.Marshal(struct {
		Error string `json:"error"`
	}{Error: err.Error()})
	if marshalErr != nil {
		payload = []byte(`{"error":"unencodable error"}`)
	}

	payload = append(payload, '\n')
	if _, err := w.Write(payload); err != nil {
		return
	}
}
