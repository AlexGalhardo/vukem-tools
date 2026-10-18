package pixelsvg

import (
	"image/color"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The expressions below parse back exactly the SVG this package writes. They
// are deliberately strict: anything the encoder emits that they do not
// recognise fails a test rather than being silently ignored, which is what
// lets the geometry assertions claim to cover the whole document.
var (
	reRoot = regexp.MustCompile(
		`^<svg xmlns="http://www\.w3\.org/2000/svg" width="(\d+)" height="(\d+)" ` +
			`viewBox="0 0 (\d+) (\d+)" shape-rendering="crispEdges">\n`)
	reDefs  = regexp.MustCompile(`^<defs><rect id="p" width="(\d+)" height="(\d+)"/></defs>\n`)
	reGroup = regexp.MustCompile(
		`^<g fill="#([0-9a-f]{6})"(?: fill-opacity="(0\.\d{3})")?>(.*?)</g>\n`)
	reRect  = regexp.MustCompile(`^<rect x="(\d+)" y="(\d+)" width="(\d+)" height="(\d+)"/>`)
	reUse   = regexp.MustCompile(`^<use href="#p" x="(\d+)" y="(\d+)"/>`)
	rePath  = regexp.MustCompile(`^<path d="([^"]+)"/>`)
	reVerts = regexp.MustCompile(`^([MHVLZ])(\d+)?(?: (\d+))?`)
)

// parsedGroup is one colour group of a parsed document.
type parsedGroup struct {
	fill  color.NRGBA
	rects []rect
	loops [][]point
}

// parsedSVG is a whole parsed document.
type parsedSVG struct {
	width, height int
	viewW, viewH  int
	groups        []parsedGroup
}

// atoi fails the test rather than returning an error, because every number it
// is given came out of a regexp group of digits.
func atoi(t *testing.T, s string) int {
	t.Helper()

	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}

	return n
}

// parseSVG parses a document this package wrote, failing the test on anything
// it does not recognise.
func parseSVG(t *testing.T, doc string) parsedSVG {
	t.Helper()

	m := reRoot.FindStringSubmatch(doc)
	if m == nil {
		t.Fatalf("root element not recognised in:\n%s", doc)
	}

	out := parsedSVG{
		width:  atoi(t, m[1]),
		height: atoi(t, m[2]),
		viewW:  atoi(t, m[3]),
		viewH:  atoi(t, m[4]),
	}
	rest := doc[len(m[0]):]

	unit := 1
	if d := reDefs.FindStringSubmatch(rest); d != nil {
		unit = atoi(t, d[1])
		if h := atoi(t, d[2]); h != unit {
			t.Fatalf("unit rect is %dx%d, want a square", unit, h)
		}

		rest = rest[len(d[0]):]
	}

	for !strings.HasPrefix(rest, "</svg>") {
		g := reGroup.FindStringSubmatch(rest)
		if g == nil {
			t.Fatalf("group not recognised at:\n%s", rest)
		}

		out.groups = append(out.groups, parseGroup(t, g, unit))
		rest = rest[len(g[0]):]
	}

	if rest != "</svg>\n" {
		t.Fatalf("trailing content after the last group: %q", rest)
	}

	return out
}

// parseGroup turns one matched group into its colour and its geometry.
func parseGroup(t *testing.T, g []string, unit int) parsedGroup {
	t.Helper()

	rgb, err := strconv.ParseUint(g[1], 16, 32)
	if err != nil {
		t.Fatalf("parse fill %q: %v", g[1], err)
	}

	out := parsedGroup{fill: color.NRGBA{
		R: uint8(rgb >> 16 & 0xff),
		G: uint8(rgb >> 8 & 0xff),
		B: uint8(rgb & 0xff),
		A: 0xff,
	}}

	if g[2] != "" {
		f, err := strconv.ParseFloat(g[2], 64)
		if err != nil {
			t.Fatalf("parse fill-opacity %q: %v", g[2], err)
		}

		out.fill.A = uint8(int(f*255+0.5) & 0xff)
	}

	body := g[3]

	for body != "" {
		switch {
		case reRect.MatchString(body):
			m := reRect.FindStringSubmatch(body)
			out.rects = append(out.rects, rect{
				x: atoi(t, m[1]), y: atoi(t, m[2]),
				w: atoi(t, m[3]), h: atoi(t, m[4]),
			})
			body = body[len(m[0]):]
		case reUse.MatchString(body):
			m := reUse.FindStringSubmatch(body)
			out.rects = append(out.rects, rect{
				x: atoi(t, m[1]), y: atoi(t, m[2]),
				w: unit, h: unit,
			})
			body = body[len(m[0]):]
		case rePath.MatchString(body):
			m := rePath.FindStringSubmatch(body)
			out.loops = append(out.loops, parsePath(t, m[1])...)
			body = body[len(m[0]):]
		default:
			t.Fatalf("element not recognised at:\n%s", body)
		}
	}

	return out
}

// parsePath turns a path data string into its closed loops.
func parsePath(t *testing.T, d string) [][]point {
	t.Helper()

	var (
		loops [][]point
		cur   []point
		at    point
	)

	for d != "" {
		m := reVerts.FindStringSubmatch(d)
		if m == nil {
			t.Fatalf("path command not recognised at %q", d)
		}

		switch m[1] {
		case "M":
			if len(cur) != 0 {
				t.Fatalf("path has an unclosed subpath before %q", d)
			}

			at = point{x: atoi(t, m[2]), y: atoi(t, m[3])}
			cur = []point{at}
		case "H":
			at.x = atoi(t, m[2])
			cur = append(cur, at)
		case "V":
			at.y = atoi(t, m[2])
			cur = append(cur, at)
		case "L":
			at = point{x: atoi(t, m[2]), y: atoi(t, m[3])}
			cur = append(cur, at)
		case "Z":
			if len(cur) < 3 {
				t.Fatalf("path closes a subpath with only %d vertices", len(cur))
			}

			loops = append(loops, cur)
			cur = nil
		}

		d = d[len(m[0]):]
	}

	if len(cur) != 0 {
		t.Fatalf("path ends with an unclosed subpath")
	}

	return loops
}

// coverage returns the colour of every pixel the rectangles of a parsed
// document cover, failing the test if two shapes cover the same pixel.
func coverage(t *testing.T, doc parsedSVG) map[point]color.NRGBA {
	t.Helper()

	out := make(map[point]color.NRGBA)

	for _, g := range doc.groups {
		for _, r := range g.rects {
			if r.w <= 0 || r.h <= 0 {
				t.Fatalf("shape %+v has a non-positive size", r)
			}

			for y := r.y; y < r.y+r.h; y++ {
				for x := r.x; x < r.x+r.w; x++ {
					p := point{x: x, y: y}
					if old, seen := out[p]; seen {
						t.Fatalf("pixel (%d,%d) is covered twice, by %v and %v", x, y, old, g.fill)
					}

					out[p] = g.fill
				}
			}
		}
	}

	return out
}
