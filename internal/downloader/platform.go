package downloader

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// Platform is a site this service will download from.
//
// The list is closed on purpose. yt-dlp supports around a thousand sites and
// a `generic` extractor that will try any URL at all, which turns the
// downloader into an open proxy: anybody could point it at an internal
// address and read the response back out of the result. Naming the four
// platforms the product promises, and the extractors each one may use, is
// what keeps that from being possible.
type Platform struct {
	// Name is the stable id a client sends and the API reports.
	Name string
	// Label is what a person calls it.
	Label string
	// Extractors are the yt-dlp extractor patterns this platform may use,
	// passed to --use-extractors. `generic` is in no set, and no pattern
	// here matches it.
	Extractors []string
	// Hosts are the registrable domains that select this platform. A host is
	// matched exactly or as a subdomain of one of these.
	Hosts []string
}

// The platforms, in the order the ROADMAP lists them.
var platforms = []Platform{
	{
		Name:  "youtube",
		Label: "YouTube",
		// youtube.* covers youtube, youtube:tab and the short-link
		// extractors. It cannot match `generic`, which is the point.
		Extractors: []string{"youtube.*"},
		Hosts:      []string{"youtube.com", "youtu.be", "youtube-nocookie.com"},
	},
	{
		Name:       "instagram",
		Label:      "Instagram",
		Extractors: []string{"instagram.*"},
		Hosts:      []string{"instagram.com"},
	},
	{
		Name:  "x",
		Label: "X",
		// yt-dlp still calls the extractor `twitter`, whatever the site is
		// called this year.
		Extractors: []string{"twitter.*"},
		Hosts:      []string{"x.com", "twitter.com"},
	},
	{
		Name:       "tiktok",
		Label:      "TikTok",
		Extractors: []string{"tiktok.*"},
		Hosts:      []string{"tiktok.com"},
	},
}

// ErrUnsupportedURL means the URL is not one of the platforms this service
// downloads from.
var ErrUnsupportedURL = errors.New("downloader: unsupported URL")

// Platforms returns every platform, for the API and the interface.
func Platforms() []Platform {
	out := make([]Platform, len(platforms))
	for i := range platforms {
		out[i] = platforms[i]
		out[i].Extractors = slices.Clone(platforms[i].Extractors)
		out[i].Hosts = slices.Clone(platforms[i].Hosts)
	}

	return out
}

// PlatformOf returns the platform a URL belongs to.
//
// It also rejects anything that is not an http or https URL, which is the
// first half of not being a proxy: a `file://` or `ftp://` URL would be
// handed straight to yt-dlp otherwise.
func PlatformOf(raw string) (Platform, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return Platform{}, fmt.Errorf("%w: it is not a URL", ErrUnsupportedURL)
	}

	switch parsed.Scheme {
	case "http", "https":
	default:
		return Platform{}, fmt.Errorf("%w: only http and https URLs are accepted", ErrUnsupportedURL)
	}

	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return Platform{}, fmt.Errorf("%w: the URL has no host", ErrUnsupportedURL)
	}

	for i := range platforms {
		for _, candidate := range platforms[i].Hosts {
			// Exact, or a subdomain: "m.youtube.com" and "www.youtube.com"
			// are YouTube, and "youtube.com.example.net" is not. Comparing
			// the suffix with its leading dot is what makes the difference.
			if host == candidate || strings.HasSuffix(host, "."+candidate) {
				return platforms[i], nil
			}
		}
	}

	return Platform{}, fmt.Errorf("%w: %s is not a platform this service downloads from", ErrUnsupportedURL, host)
}

// Supported reports whether a URL can be downloaded, for a cheap check that
// does not need the reason.
func Supported(raw string) bool {
	_, err := PlatformOf(raw)
	return err == nil
}
