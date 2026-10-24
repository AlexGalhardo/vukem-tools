package bot

import (
	"context"
	"slices"
	"strings"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/clients/apiclient"
)

// The tool table is the whole of what the bot can do with a file, and it is
// deliberately a table: the menus, the prompts, the choices, the types each
// tool accepts and the call that runs it are all read from here, so a tool
// cannot be offered without being wired and cannot be wired without being
// offered.
//
// Only tools whose whole input fits a chat are here: one file or a short list
// of files, and at most one choice made with a button. A tool that needs a
// typed password, a page range or a pixel geometry is left to the web
// interface rather than turned into a conversation.

// Group is one button on the main menu.
type Group struct {
	ID    string
	Label string
}

// groups, in menu order. "dl" is the link-based download, which has a flow of
// its own.
var groups = []Group{ //nolint:gochecknoglobals // read-only table
	{ID: "img", Label: "Images"},
	{ID: "av", Label: "Audio and video"},
	{ID: "pdf", Label: "PDF"},
	{ID: "doc", Label: "Documents to PDF"},
	{ID: "arc", Label: "Archives"},
}

// Option is one button of a choice.
type Option struct {
	Label string
	Value string
}

// Choice is the one question a tool asks, answered with a button.
type Choice struct {
	Question string
	Options  []Option
}

// Tool is one thing a person can do with a file.
type Tool struct {
	// ID is the tool's key in a button, lower case and short.
	ID    string
	Group string
	Label string
	// API is the tool's name in the HTTP API.
	API string
	// Ability is what the key must have been granted to run it.
	Ability string
	// Prompt tells the person what to send.
	Prompt string
	// Accept lists the detected media types the tool reads, as exact types or
	// "type/" prefixes. The server detects the type from the bytes; the bot
	// checks it before submitting so the answer is a sentence, not a refusal
	// from deep inside a worker.
	Accept []string
	// Min and Max bound how many files it takes; both 1 for a single file.
	Min, Max int
	// Choice is the question asked after the files, or nil.
	Choice *Choice
	// Submit starts the conversion.
	Submit func(ctx context.Context, api API, ids []string, choice string) (apiclient.ConversionView, error)
}

// multi reports whether the tool collects several files before running.
func (t Tool) multi() bool { return t.Max > 1 }

// accepts reports whether a detected media type is one the tool reads.
func (t Tool) accepts(mime string) bool {
	mime = strings.ToLower(mime)

	for _, a := range t.Accept {
		// An empty entry means any type: the archive tool packs whatever it is
		// given.
		if a == "" || a == mime || (strings.HasSuffix(a, "/") && strings.HasPrefix(mime, a)) {
			return true
		}
	}

	return false
}

func opts(pairs ...string) []Option {
	out := make([]Option, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, Option{Label: pairs[i], Value: pairs[i+1]})
	}

	return out
}

func one(ids []string) string { return ids[0] }

var (
	imageTypes = []string{"image/"}
	pdfTypes   = []string{"application/pdf"}
	wordTypes  = []string{
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/msword",
		"application/vnd.oasis.opendocument.text",
	}
	slideTypes = []string{
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
		"application/vnd.ms-powerpoint",
		"application/vnd.oasis.opendocument.presentation",
	}
	sheetTypes = []string{
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.ms-excel",
		"application/vnd.oasis.opendocument.spreadsheet",
	}
	mediaTypes   = []string{"audio/", "video/"}
	videoTypes   = []string{"video/"}
	archiveTypes = []string{"application/zip", "application/gzip", "application/x-tar"}
)

// tools, in menu order within each group.
var tools = []Tool{ //nolint:gochecknoglobals // read-only table
	{
		ID:      "imgconv",
		Group:   "img",
		Label:   "Convert an image",
		API:     "image-convert",
		Ability: abilities.ImageConvert,
		Prompt:  "Send the image, as a photo or as a file.",
		Accept:  imageTypes,
		Min:     1,
		Max:     1,
		Choice: &Choice{
			Question: "Convert to which format?",
			Options:  opts("PNG", "png", "JPEG", "jpeg", "WebP", "webp", "GIF", "gif"),
		},
		Submit: func(ctx context.Context, api API, ids []string, choice string) (apiclient.ConversionView, error) {
			return api.ToolsImageConvert(ctx, apiclient.ToolsImageConvertBody{
				FileID: one(ids), Options: &apiclient.ImageConvertOptions{To: &choice},
			})
		},
	},
	{
		ID:      "imgcomp",
		Group:   "img",
		Label:   "Make an image smaller",
		API:     "image-compress",
		Ability: abilities.ImageCompress,
		Prompt:  "Send the image, as a file so Telegram does not recompress it first.",
		Accept:  imageTypes,
		Min:     1,
		Max:     1,
		Submit: func(ctx context.Context, api API, ids []string, _ string) (apiclient.ConversionView, error) {
			return api.ToolsImageCompress(ctx, apiclient.ToolsImageCompressBody{FileID: one(ids)})
		},
	},
	{
		ID: "px2svg", Group: "img", Label: "Pixel art to SVG", API: "pixels-to-svg", Ability: abilities.PixelsSVG,
		Prompt: "Send the pixel art as a file (PNG, GIF, BMP or JPEG).",
		Accept: imageTypes, Min: 1, Max: 1,
		Submit: func(ctx context.Context, api API, ids []string, _ string) (apiclient.ConversionView, error) {
			return api.ToolsPixelsToSvg(ctx, apiclient.ToolsPixelsToSvgBody{FileID: one(ids)})
		},
	},
	{
		ID: "img2pdf", Group: "img", Label: "Images to one PDF", API: "images-to-pdf", Ability: abilities.PDFConvert,
		Prompt: "Send the images one at a time, in page order. Press Done when you have sent them all.",
		Accept: imageTypes, Min: 1, Max: 20,
		Submit: func(ctx context.Context, api API, ids []string, _ string) (apiclient.ConversionView, error) {
			return api.ToolsImagesToPdf(ctx, apiclient.ToolsImagesToPdfBody{FileIds: ids})
		},
	},
	{
		ID:      "audconv",
		Group:   "av",
		Label:   "Convert audio",
		API:     "audio-convert",
		Ability: abilities.AudioConvert,
		Prompt:  "Send the audio file, a voice note, or a video to take the sound from.",
		Accept:  mediaTypes,
		Min:     1,
		Max:     1,
		Choice: &Choice{
			Question: "Convert to which format?",
			Options:  opts("MP3", "mp3", "Opus", "opus", "AAC", "aac", "FLAC", "flac", "WAV", "wav"),
		},
		Submit: func(ctx context.Context, api API, ids []string, choice string) (apiclient.ConversionView, error) {
			return api.ToolsAudioConvert(ctx, apiclient.ToolsAudioConvertBody{
				FileID: one(ids), Options: &apiclient.AudioConvertOptions{To: &choice},
			})
		},
	},
	{
		ID:      "vidconv",
		Group:   "av",
		Label:   "Convert a video",
		API:     "video-convert",
		Ability: abilities.VideoConvert,
		Prompt:  "Send the video, as a file.",
		Accept:  videoTypes,
		Min:     1,
		Max:     1,
		Choice: &Choice{
			Question: "Convert to which container?",
			Options:  opts("MP4", "mp4", "WebM", "webm", "MKV", "mkv", "MOV", "mov"),
		},
		Submit: func(ctx context.Context, api API, ids []string, choice string) (apiclient.ConversionView, error) {
			return api.ToolsVideoConvert(ctx, apiclient.ToolsVideoConvertBody{
				FileID: one(ids), Options: &apiclient.VideoConvertOptions{Container: &choice},
			})
		},
	},
	{
		ID: "vidcomp", Group: "av", Label: "Make a video smaller", API: "video-compress", Ability: abilities.VideoEdit,
		Prompt: "Send the video, as a file.",
		Accept: videoTypes, Min: 1, Max: 1,
		Submit: func(ctx context.Context, api API, ids []string, _ string) (apiclient.ConversionView, error) {
			return api.ToolsVideoCompress(ctx, apiclient.ToolsVideoCompressBody{FileID: one(ids)})
		},
	},
	{
		ID: "vid2gif", Group: "av", Label: "Video to GIF", API: "video-to-gif", Ability: abilities.VideoConvert,
		Prompt: "Send the video, as a file. The GIF is the first 15 seconds at most.",
		Accept: videoTypes, Min: 1, Max: 1,
		Submit: func(ctx context.Context, api API, ids []string, _ string) (apiclient.ConversionView, error) {
			return api.ToolsVideoToGif(ctx, apiclient.ToolsVideoToGifBody{FileID: one(ids)})
		},
	},
	{
		ID:      "pdfcomp",
		Group:   "pdf",
		Label:   "Make a PDF smaller",
		API:     "pdf-compress",
		Ability: abilities.PDFCompress,
		Prompt:  "Send the PDF.",
		Accept:  pdfTypes,
		Min:     1,
		Max:     1,
		Choice: &Choice{
			Question: "How hard?",
			Options: opts(
				"Light, keeps every page as it is",
				"light",
				"Balanced",
				"balanced",
				"Strong, for email",
				"strong",
			),
		},
		Submit: func(ctx context.Context, api API, ids []string, choice string) (apiclient.ConversionView, error) {
			return api.ToolsPdfCompress(ctx, apiclient.ToolsPdfCompressBody{
				FileID: one(ids), Options: &apiclient.PDFCompressOptions{Level: &choice},
			})
		},
	},
	{
		ID: "pdftxt", Group: "pdf", Label: "PDF to text", API: "pdf-to-text", Ability: abilities.PDFConvert,
		Prompt: "Send the PDF.",
		Accept: pdfTypes, Min: 1, Max: 1,
		Choice: &Choice{Question: "Which format?", Options: opts("Plain text", "text", "Markdown", "markdown")},
		Submit: func(ctx context.Context, api API, ids []string, choice string) (apiclient.ConversionView, error) {
			return api.ToolsPdfToText(ctx, apiclient.ToolsPdfToTextBody{
				FileID: one(ids), Options: &apiclient.PDFToTextOptions{Format: &choice},
			})
		},
	},
	{
		ID:      "pdfocr",
		Group:   "pdf",
		Label:   "Make a scan searchable",
		API:     "pdf-ocr",
		Ability: abilities.PDFConvert,
		Prompt:  "Send the scanned PDF. At most 100 pages.",
		Accept:  pdfTypes,
		Min:     1,
		Max:     1,
		Choice: &Choice{
			Question: "What language is it in?",
			Options:  opts("English", "eng", "Portuguese", "por", "Both", "eng+por"),
		},
		Submit: func(ctx context.Context, api API, ids []string, choice string) (apiclient.ConversionView, error) {
			return api.ToolsPdfOcr(ctx, apiclient.ToolsPdfOcrBody{
				FileID: one(ids), Options: &apiclient.PDFOCROptions{Languages: strings.Split(choice, "+")},
			})
		},
	},
	{
		ID: "pdfrep", Group: "pdf", Label: "Repair a PDF", API: "pdf-repair", Ability: abilities.PDFCompress,
		Prompt: "Send the damaged PDF.",
		Accept: pdfTypes, Min: 1, Max: 1,
		Submit: func(ctx context.Context, api API, ids []string, _ string) (apiclient.ConversionView, error) {
			return api.ToolsPdfRepair(ctx, apiclient.ToolsPdfRepairBody{FileID: one(ids)})
		},
	},
	{
		ID: "pdfmrg", Group: "pdf", Label: "Merge PDFs", API: "pdf-merge", Ability: abilities.PDFStructure,
		Prompt: "Send the PDFs one at a time, in the order of the pages. Press Done when you have sent them all.",
		Accept: pdfTypes, Min: 2, Max: 10,
		Submit: func(ctx context.Context, api API, ids []string, _ string) (apiclient.ConversionView, error) {
			return api.ToolsPdfMerge(ctx, apiclient.ToolsPdfMergeBody{FileIds: ids})
		},
	},
	{
		ID: "word", Group: "doc", Label: "Word to PDF", API: "word-to-pdf", Ability: abilities.PDFConvert,
		Prompt: "Send the Word document (.docx, .doc or .odt).",
		Accept: wordTypes, Min: 1, Max: 1,
		Submit: func(ctx context.Context, api API, ids []string, _ string) (apiclient.ConversionView, error) {
			return api.ToolsWordToPdf(ctx, apiclient.ToolsWordToPdfBody{FileID: one(ids)})
		},
	},
	{
		ID: "slides", Group: "doc", Label: "PowerPoint to PDF", API: "powerpoint-to-pdf", Ability: abilities.PDFConvert,
		Prompt: "Send the presentation (.pptx, .ppt or .odp).",
		Accept: slideTypes, Min: 1, Max: 1,
		Submit: func(ctx context.Context, api API, ids []string, _ string) (apiclient.ConversionView, error) {
			return api.ToolsPowerpointToPdf(ctx, apiclient.ToolsPowerpointToPdfBody{FileID: one(ids)})
		},
	},
	{
		ID: "sheet", Group: "doc", Label: "Excel to PDF", API: "excel-to-pdf", Ability: abilities.PDFConvert,
		Prompt: "Send the spreadsheet (.xlsx, .xls or .ods).",
		Accept: sheetTypes, Min: 1, Max: 1,
		Submit: func(ctx context.Context, api API, ids []string, _ string) (apiclient.ConversionView, error) {
			return api.ToolsExcelToPdf(ctx, apiclient.ToolsExcelToPdfBody{FileID: one(ids)})
		},
	},
	{
		ID: "arcx", Group: "arc", Label: "Extract an archive", API: "archive-extract", Ability: abilities.Archive,
		Prompt: "Send the archive (ZIP, TAR, TAR.GZ or GZ).",
		Accept: archiveTypes, Min: 1, Max: 1,
		Submit: func(ctx context.Context, api API, ids []string, _ string) (apiclient.ConversionView, error) {
			return api.ToolsArchiveExtract(ctx, apiclient.ToolsArchiveExtractBody{FileID: one(ids)})
		},
	},
	{
		ID:      "arcz",
		Group:   "arc",
		Label:   "Pack files into an archive",
		API:     "archive-create",
		Ability: abilities.Archive,
		Prompt:  "Send the files one at a time. Press Done when you have sent them all.",
		Accept:  []string{""},
		Min:     1,
		Max:     10,
		Choice:  &Choice{Question: "Which format?", Options: opts("ZIP", "zip", "TAR.GZ", "tar.gz")},
		Submit: func(ctx context.Context, api API, ids []string, choice string) (apiclient.ConversionView, error) {
			return api.ToolsArchiveCreate(ctx, apiclient.ToolsArchiveCreateBody{
				FileIds: ids, Options: &apiclient.ArchiveCreateOptions{Format: &choice},
			})
		},
	},
}

// download is the link-based flow. It has no entry in tools: it takes a URL,
// not a file.
const downloadAbility = abilities.VideoDownload

// toolByID finds a tool by its button key.
func toolByID(id string) (Tool, bool) {
	i := slices.IndexFunc(tools, func(t Tool) bool { return t.ID == id })
	if i < 0 {
		return Tool{}, false
	}

	return tools[i], true
}

// toolsIn lists a group's tools in menu order.
func toolsIn(group string) []Tool {
	var out []Tool

	for i := range tools {
		if tools[i].Group == group {
			out = append(out, tools[i])
		}
	}

	return out
}

func groupByID(id string) (Group, bool) {
	i := slices.IndexFunc(groups, func(g Group) bool { return g.ID == id })
	if i < 0 {
		return Group{}, false
	}

	return groups[i], true
}

// choiceAllows reports whether value is one of the tool's own options. The
// check is the point: a button press is a string a client sent, and only a
// value the bot itself put in a button may reach a request.
func (t Tool) choiceAllows(value string) bool {
	if t.Choice == nil {
		return value == ""
	}

	return slices.ContainsFunc(t.Choice.Options, func(o Option) bool { return o.Value == value })
}

// UsedAbilities is every ability the table needs, for the test that holds
// the server's grant to it.
func UsedAbilities() []string {
	seen := map[string]bool{downloadAbility: true}

	for i := range tools {
		seen[tools[i].Ability] = true
	}

	out := make([]string, 0, len(seen))
	for a := range seen {
		out = append(out, a)
	}

	slices.Sort(out)

	return out
}

// APITools lists the API tool names the menus run, with the ability each
// needs, for the same test.
func APITools() map[string]string {
	out := map[string]string{"video-download": downloadAbility}
	for i := range tools {
		out[tools[i].API] = tools[i].Ability
	}

	return out
}
