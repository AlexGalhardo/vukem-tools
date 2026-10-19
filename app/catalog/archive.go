package catalog

// archiveCatalog is the archives domain.
//
// A catalogue entry is written when the tool is planned, not when it ships:
// Status says which. See the package comment for why a missing tool is listed
// rather than silently absent.
//
// Two entries where the roadmap had one. The single "Create or extract an
// archive" entry promised ZIP, TAR, GZ, XZ and ZSTD in both directions; what
// shipped is ZIP, TAR, TAR.GZ and GZ, as two tools. Both changes are
// deliberate and recorded in docs/brainstorming.md: XZ and ZSTD are not in the
// standard library and would each have cost a dependency and a /licenses
// entry for a format the other four already cover, and the two directions take
// different inputs, so one tool would have had to accept every type the
// service knows and the submission-time type gate would have been gone.
var archiveCatalog = []Tool{
	{
		Name:    "archive-create",
		Title:   "Create an archive",
		Summary: "Packs several files into one ZIP, TAR.GZ, TAR or GZ, with the compression level yours to set.",
		// In is every type, which the index renders as "any file": an archive
		// takes whatever you have, and listing twenty formats here would say
		// less than one word does.
		In:       []string{"Any file"},
		Out:      []string{"ZIP", "TAR.GZ", "TAR", "GZ"},
		Group:    GroupArchives,
		Icon:     "archive",
		Status:   Live,
		Keywords: []string{"zip", "tar", "tgz", "gzip", "compress", "pack", "bundle"},
	},
	{
		Name:     "archive-extract",
		Title:    "Extract an archive",
		Summary:  "Unpacks a ZIP, TAR, TAR.GZ or GZ into its files, each one a download of its own.",
		In:       []string{"ZIP", "TAR", "TAR.GZ", "GZ"},
		Out:      []string{"Any file"},
		Group:    GroupArchives,
		Icon:     "archive",
		Status:   Live,
		Keywords: []string{"unzip", "untar", "gunzip", "unpack", "open", "7z", "rar"},
	},
}
