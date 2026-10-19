package tests

import (
	"encoding/xml"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/app/controllers"
	"github.com/AlexGalhardo/vukem-tools/bootstrap"
	"github.com/AlexGalhardo/vukem-tools/routes"
)

// sitemapDoc is the document as a crawler parses it.
type sitemapDoc struct {
	XMLName xml.Name `xml:"urlset"`
	URLs    []struct {
		Loc        string `xml:"loc"`
		ChangeFreq string `xml:"changefreq"`
		Priority   string `xml:"priority"`
	} `xml:"url"`
}

func TestTheSitemapIsValidAndAbsolute(t *testing.T) {
	t.Parallel()

	const base = "https://tools.example"

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: map[string]string{"APP_URL": base}})

	res := app.Client(t).Get("/sitemap.xml").AssertOK().
		AssertHeaderContains("Content-Type", "application/xml")

	var doc sitemapDoc
	if err := xml.Unmarshal([]byte(res.Body()), &doc); err != nil {
		t.Fatalf("a crawler cannot parse the sitemap: %v", err)
	}

	if len(doc.URLs) != len(controllers.SitemapPaths()) {
		t.Fatalf("the sitemap holds %d URLs and the list declares %d", len(doc.URLs), len(controllers.SitemapPaths()))
	}

	for i, entry := range doc.URLs {
		// Absolute and on the configured host. A relative <loc> is invalid in
		// a sitemap, and one built from the request's Host would let anyone
		// mint a sitemap advertising their own domain.
		want := base + controllers.SitemapPaths()[i]
		if entry.Loc != want {
			t.Errorf("URL %d is %q, want %q", i, entry.Loc, want)
		}

		if entry.ChangeFreq == "" || entry.Priority == "" {
			t.Errorf("URL %d (%s) is missing changefreq or priority", i, entry.Loc)
		}
	}
}

// Every advertised page must answer 200 to a visitor with no account. A
// sitemap entry that redirects to the sign-in form is a promise the site does
// not keep, and the whole reason the tool pages are not in it.
func TestEverySitemapURLIsReachableWithoutAnAccount(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	for _, path := range controllers.SitemapPaths() {
		res := app.Client(t).Get(path)
		if res.Status() != http.StatusOK {
			t.Errorf("GET %s answers %d to an anonymous visitor, and the sitemap advertises it", path, res.Status())
		}
	}
}

// The gate: a public page is either advertised or deliberately excluded.
//
// Without this, the sitemap is a hand-kept list beside a router that changes,
// which is a list that goes stale on the first page somebody adds. The
// exclusions carry their reason, so leaving a page out stays a decision rather
// than an oversight.
func TestTheSitemapCoversEveryPublicPage(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	if _, err := app.Router.Handler(); err != nil {
		t.Fatal(err)
	}

	advertised := controllers.SitemapPaths()
	excluded := controllers.SitemapExcluded()

	for _, rt := range app.Router.Routes() {
		if rt.Method != http.MethodGet {
			continue
		}

		// The API is a machine surface and documents itself at /docs.
		if rt.Path == routes.APIPrefix || strings.HasPrefix(rt.Path, routes.APIPrefix+"/") {
			continue
		}

		path := sitemapPath(rt.Path)

		// A path parameter is not a page: it is a family of them, and which
		// members exist is a question only the database answers.
		if strings.Contains(path, "{") {
			continue
		}

		// Behind an account, so a crawler cannot read it.
		if slices.Contains(rt.Middleware, "auth.Required") {
			if slices.Contains(advertised, path) {
				t.Errorf("the sitemap advertises %s, which is behind auth.Required", path)
			}

			continue
		}

		if slices.Contains(advertised, path) {
			continue
		}

		if _, ok := excluded[path]; !ok {
			t.Errorf("GET %s is public and is neither in the sitemap nor in sitemapExcluded; "+
				"add it to one or the other in app/controllers/sitemap.go", path)
		}
	}
}

// sitemapPath turns a router pattern into the path a crawler would request.
// The home route is registered as "/{$}", Go's exact-match marker, which is
// not a path parameter and is not what goes in a sitemap.
func sitemapPath(pattern string) string {
	if pattern == "/{$}" {
		return "/"
	}

	return strings.TrimSuffix(pattern, "{$}")
}
