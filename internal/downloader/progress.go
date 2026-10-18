package downloader

import (
	"encoding/json"
	"strconv"
	"strings"
)

// progressTemplate is the --progress-template yt-dlp is given.
//
// Progress is read from a structured stream rather than scraped off the
// progress bar, which is a project rule and also the only thing that works:
// the bar is drawn with carriage returns and ANSI escapes, its wording has
// changed between releases, and a fragmented download redraws it per
// fragment.
//
// Every field is rendered as a *string* even though three of them are
// numbers. yt-dlp renders an absent field as "NA" in an output template, and
// "total":NA is not JSON — one unknown size would make every later line
// unparseable. Quoting them makes the line always valid and moves the
// "unknown" case into parseBytes, where it belongs.
const progressTemplate = `download:{"status":"%(progress.status)s",` +
	`"downloaded":"%(progress.downloaded_bytes)s",` +
	`"total":"%(progress.total_bytes)s",` +
	`"estimate":"%(progress.total_bytes_estimate)s"}`

// Progress is one update from the engine.
type Progress struct {
	// Status is yt-dlp's own word: "downloading", "finished" or "error".
	Status string
	// Downloaded is how many bytes have arrived.
	Downloaded int64
	// Total is the size, from the platform's figure or its estimate. Zero
	// means the platform has not said, which is normal for a fragmented
	// stream and is why Percent can return -1.
	Total int64
}

// Percent is how far along the download is, or -1 when the size is unknown.
//
// -1 rather than 0: a caller showing 0% for an hour is showing a lie, and a
// caller that can tell "unknown" from "nothing yet" can show a spinner
// instead of a stuck bar.
func (p Progress) Percent() int {
	if p.Total <= 0 || p.Downloaded <= 0 {
		return -1
	}

	percent := int(float64(p.Downloaded) / float64(p.Total) * 100)

	return min(max(percent, 0), 100)
}

// Done reports whether this was the last update for the stream.
func (p Progress) Done() bool { return p.Status == "finished" }

// progressLine is the wire shape of progressTemplate.
type progressLine struct {
	Status     string `json:"status"`
	Downloaded string `json:"downloaded"`
	Total      string `json:"total"`
	Estimate   string `json:"estimate"`
}

// progressReader returns the line handler for engine.Command.Progress, or
// nil when nobody is listening — nil makes the runner wire stdout straight
// through instead of starting a reader goroutine for no reason.
func progressReader(report func(Progress)) func(string) {
	if report == nil {
		return nil
	}

	return func(line string) {
		if p, ok := ParseProgress(line); ok {
			report(p)
		}
	}
}

// ParseProgress reads one progress line, reporting false for anything that is
// not one.
//
// It is exported so the parsing can be tested on the exact bytes yt-dlp
// emits, which is the part most likely to break on an engine bump.
//
// A line that does not parse is dropped rather than logged or returned as an
// error: yt-dlp also writes warnings and post-processor notices to stdout, and
// a download must not fail because a notice was not JSON.
func ParseProgress(line string) (Progress, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "{") {
		return Progress{}, false
	}

	var raw progressLine
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return Progress{}, false
	}

	if raw.Status == "" {
		return Progress{}, false
	}

	total := parseBytes(raw.Total)
	if total == 0 {
		total = parseBytes(raw.Estimate)
	}

	return Progress{
		Status:     raw.Status,
		Downloaded: parseBytes(raw.Downloaded),
		Total:      total,
	}, true
}

// parseBytes reads one of the quoted numbers, treating every spelling of
// "unknown" as zero.
//
// yt-dlp writes "NA" for an absent field; a Python None reaching the template
// renders as "None"; and a float arrives for an estimate. All three have to
// become zero or a number rather than an error, because a progress line is
// not worth failing a download over.
func parseBytes(s string) int64 {
	s = strings.TrimSpace(s)

	switch s {
	case "", "NA", "N/A", "None", "none", "null":
		return 0
	}

	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return max(n, 0)
	}

	if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0 {
		return int64(f)
	}

	return 0
}
