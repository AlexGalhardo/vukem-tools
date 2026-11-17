package desktop_test

import (
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/desktop"
)

func TestToolURLIsTheWebToolPage(t *testing.T) {
	t.Parallel()

	got, err := desktop.ToolURL("http://127.0.0.1:8080", "pdf-crop")
	if err != nil {
		t.Fatal(err)
	}

	const want = "http://127.0.0.1:8080/tools/pdf-crop"
	if got != want {
		t.Fatalf("ToolURL = %s, want %s", got, want)
	}
}
