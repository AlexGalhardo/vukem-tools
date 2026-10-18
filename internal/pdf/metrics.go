package pdf

// The widths of the two standard fonts this package writes, in 1/1000 of the
// font size, for the printable ASCII range.
//
// They are here rather than measured from a font file because the two fonts
// are the base-14 ones every PDF reader has built in: a stamp that names
// Helvetica needs no embedded font, which is what keeps a watermark from
// adding half a megabyte to every page. The numbers are the published Adobe
// AFM widths, and the only thing they are used for is placing text — a
// centred watermark and a right-aligned page number both need to know how
// wide the string will be before it is drawn.
//
// Index zero is code 32 (space); the tables end at code 126 (tilde). A
// character outside that range is refused by checkWinAnsi rather than
// guessed at, because a wrong width is a watermark that runs off the page.
var (
	helveticaWidths = [...]int{
		278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278,
		556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556,
		1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778,
		667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556,
		333, 556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556,
		556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334, 260, 334, 584,
	}

	helveticaBoldWidths = [...]int{
		278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278,
		556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 333, 333, 584, 584, 584, 611,
		975, 722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833, 722, 778,
		667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 333, 278, 333, 584, 556,
		333, 556, 611, 556, 611, 556, 333, 611, 611, 278, 278, 556, 278, 889, 611, 611,
		611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500, 389, 280, 389, 584,
	}
)

// The codes the width tables cover.
const (
	firstPrintable = 32
	lastPrintable  = 126
)

// Font is one of the two standard fonts a stamp may use.
type Font string

// The fonts. Both are base-14, so neither is embedded.
const (
	// Helvetica is the default: a page number or a watermark in a sans-serif
	// face reads as an annotation rather than as part of the document.
	Helvetica Font = "Helvetica"
	// HelveticaBold is for a watermark that has to be legible over text.
	HelveticaBold Font = "Helvetica-Bold"
)

// Valid reports whether f is a font this package can write.
func (f Font) Valid() bool { return f == Helvetica || f == HelveticaBold }

// widths returns the table for f.
func (f Font) widths() []int {
	if f == HelveticaBold {
		return helveticaBoldWidths[:]
	}

	return helveticaWidths[:]
}

// TextWidth is how wide text will be at size points, in points.
//
// A character the font tables do not cover counts as a space, which only
// happens for text checkWinAnsi already refused, so it is a fallback that
// cannot be reached through the API rather than a silent approximation.
func TextWidth(f Font, text string, size float64) float64 {
	table := f.widths()

	var thousandths int

	for _, r := range text {
		if r < firstPrintable || r > lastPrintable {
			thousandths += table[0]

			continue
		}

		thousandths += table[r-firstPrintable]
	}

	return float64(thousandths) * size / 1000
}

// checkWinAnsi refuses text this package cannot place accurately.
//
// The limit is the printable ASCII range rather than all of WinAnsi, because
// the width tables above stop at 126 and a character whose width is unknown is
// a stamp that runs off the page. Refusing is the honest answer: a watermark
// silently missing its accents would be worse than being told to rewrite it.
func checkWinAnsi(field, text string) error {
	for _, r := range text {
		if r < firstPrintable || r > lastPrintable {
			return &OptionError{
				Field: field,
				Reason: "Stamped text is limited to unaccented Latin letters, digits and " +
					"punctuation, because the standard PDF fonts are the only ones this tool " +
					"embeds. Rewrite it without \"" + string(r) + "\".",
			}
		}
	}

	return nil
}
