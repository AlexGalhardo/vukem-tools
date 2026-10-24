package tests

import (
	"os"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/app/catalog"
)

// A client tool is live as soon as it is in the catalogue, so a name with no
// interface in Tools/Dev.tsx would be advertised on the index and open a page
// that says it has none.
func TestEveryClientToolHasAnInterface(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("../resources/js/pages/Tools/Dev.tsx")
	if err != nil {
		t.Fatalf("read Dev.tsx: %v", err)
	}

	for _, tool := range catalog.All(nil) {
		if !tool.Client {
			continue
		}

		if tool.Status != catalog.Live {
			t.Errorf("%s is a client tool that is not live; a client tool has no pipeline to wait for", tool.Name)
		}

		if !strings.Contains(string(src), "\""+tool.Name+"\":") &&
			!strings.Contains(string(src), "\n  "+tool.Name+":") {
			t.Errorf("%s is in the catalogue but has no entry in resources/js/pages/Tools/Dev.tsx", tool.Name)
		}
	}
}
