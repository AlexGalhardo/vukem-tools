package controllers

import (
	"bufio"
	"encoding/xml"
	"net/http"
	"strings"

	"github.com/AlexGalhardo/vukem/web"
)

// Sitemap serves /sitemap.xml: the list of pages a crawler should index.
//
// Only pages an anonymous visitor can actually read are in it. That is the
// whole rule, and it is why the list is short: every tool page is behind
// auth.Required, because every conversion needs an account, so a crawler
// following /tools/image-convert is redirected to the sign-in form. A sitemap
// whose entries redirect is worse than one that omits them — it is a list of
// promises the site does not keep, and a crawler treats it that way.
//
// The sign-in and sign-up forms are the other omission, for the opposite
// reason: they are reachable, but they are plumbing rather than content and
// nobody arrives at this site looking for them.
//
// sitemapExcluded records both decisions by path, and
// TestTheSitemapCoversEveryPublicPage in ./tests fails when a public page is
// neither listed nor excluded. Without that gate this file is a hand-kept
// list, which is a list that goes stale.
type Sitemap struct {
	// base is APP_URL without its trailing slash. A sitemap needs absolute
	// URLs, and the Host of the request cannot supply them: it is
	// attacker-controlled, so taking it would let anyone mint a sitemap that
	// advertises their own domain carrying this site's paths.
	base string
}

// NewSitemap returns the handler. base is the public base URL (APP_URL).
func NewSitemap(base string) *Sitemap {
	return &Sitemap{base: strings.TrimSuffix(base, "/")}
}

// sitemapURL is one entry.
//
// There is no <lastmod>. The honest value would be the deploy that last
// changed the page, which this process does not know, and a <lastmod> of "now"
// on every request is the one way to make a crawler distrust the whole file.
type sitemapURL struct {
	Loc        string `xml:"loc"`
	ChangeFreq string `xml:"changefreq"`
	Priority   string `xml:"priority"`
}

// urlset is the document.
type urlset struct {
	XMLName xml.Name     `xml:"urlset"`
	NS      string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

const sitemapNS = "http://www.sitemaps.org/schemas/sitemap/0.9"

// sitemapPages is what the sitemap advertises, in the order it lists them.
var sitemapPages = []sitemapURL{
	{Loc: "/", ChangeFreq: "weekly", Priority: "1.0"},
	{Loc: "/docs", ChangeFreq: "weekly", Priority: "0.8"},
	{Loc: "/licenses", ChangeFreq: "monthly", Priority: "0.3"},
}

// sitemapOmission is a public page left out of the sitemap, and why.
type sitemapOmission struct {
	path string
	why  string
}

// sitemapExcluded names every other public GET page and why it is not in the
// sitemap. The gate in ./tests reads it, so a page added without a decision
// recorded here fails the suite rather than quietly going unindexed.
var sitemapExcluded = []sitemapOmission{
	{path: "/login", why: "a form, not content"},
	{path: "/register", why: "a form, not content"},
	{path: "/forgot-password", why: "a form, not content"},
	{path: "/register/check-email", why: "an availability probe the sign-up form calls, not a page"},
	{path: "/openapi.json", why: "the document /docs reads; the page is what a reader wants"},
	{path: "/docs/scalar.js", why: "an asset of /docs"},
	{path: "/docs/page.js", why: "an asset of /docs"},
	{path: "/up", why: "a liveness probe vukem registers"},
	{path: "/health", why: "an operator endpoint"},
	{path: "/health/deep", why: "an operator endpoint"},
	{path: "/sitemap.xml", why: "the sitemap itself"},
	{path: "/llms.txt", why: "a Markdown index for language models, read on request, not a page for search"},
	{path: "/llms-full.txt", why: "the same index with the content inline, for language models"},
}

// SitemapPaths returns the advertised paths, for the gate in ./tests.
func SitemapPaths() []string {
	paths := make([]string, 0, len(sitemapPages))
	for _, page := range sitemapPages {
		paths = append(paths, page.Loc)
	}

	return paths
}

// SitemapExcluded returns the public paths deliberately left out, mapped to
// the reason, for the gate in ./tests.
func SitemapExcluded() map[string]string {
	out := make(map[string]string, len(sitemapExcluded))
	for _, omission := range sitemapExcluded {
		out[omission.path] = omission.why
	}

	return out
}

// Serve writes the sitemap: GET /sitemap.xml.
func (s *Sitemap) Serve(c *web.Ctx) error {
	doc := urlset{NS: sitemapNS, URLs: make([]sitemapURL, 0, len(sitemapPages))}

	for _, page := range sitemapPages {
		page.Loc = s.base + page.Loc
		doc.URLs = append(doc.URLs, page)
	}

	c.SetHeader("Content-Type", "application/xml; charset=utf-8")
	// A sitemap changes only when the site is deployed, and a crawler holding
	// it for an hour costs nothing.
	c.SetHeader("Cache-Control", "public, max-age=3600")
	c.Writer().WriteHeader(http.StatusOK)

	// Buffered to the socket rather than marshalled into one string and
	// handed over. The document is small enough that it does not matter here,
	// and writing it the other way in the one place where it does not matter
	// is how the habit gets lost.
	w := bufio.NewWriter(c.Writer())

	if _, err := w.WriteString(xml.Header); err != nil {
		return err
	}

	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")

	if err := enc.Encode(doc); err != nil {
		return err
	}

	if _, err := w.WriteString("\n"); err != nil {
		return err
	}

	return w.Flush()
}
