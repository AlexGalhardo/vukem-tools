// Package desktop is the Wails shell's view of the web app.
//
// The window opens the same pages the browser does. It does not draw a tool.
package desktop

import (
	"errors"
	"net/url"
	"strings"
)

// ToolURL is the page the desktop window opens for a catalog tool.
// base is the local server origin, for example http://127.0.0.1:8080.
func ToolURL(base, name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("desktop: a tool name is required")
	}

	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("desktop: base must be an absolute URL")
	}

	u.Path = "/tools/" + name
	u.RawQuery = ""
	u.Fragment = ""

	return u.String(), nil
}
