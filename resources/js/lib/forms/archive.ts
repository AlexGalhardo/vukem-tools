import { type ToolForm } from "../toolForm";

/**
 * The archive tools' forms.
 *
 * One file per domain, matching routes/apidocs_archive.go on the server.
 *
 * The formats here are the server's own, from internal/archive. A Go test
 * asserts the two lists agree, so adding a format to the library without
 * offering it in the interface — or offering one the library does not write —
 * fails CI rather than shipping a select whose values are refused.
 */
export const archiveForms: Record<string, ToolForm> = {
  "archive-create": {
    files: "many",
    // Empty rather than a list: an archive takes whatever you have, and the
    // picker filtering by type would be wrong rather than helpful.
    accept: [],
    fields: [
      {
        name: "format",
        label: "Format",
        kind: "choice",
        choices: ["zip", "tar.gz", "tar", "gz"],
        help: "zip is read everywhere. tar.gz compresses the whole set at once, so it is usually the smaller of the two. tar is uncompressed. gz holds exactly one file.",
      },
      {
        name: "level",
        label: "Compression level",
        kind: "number",
        min: 1,
        max: 9,
        placeholder: "the format's default",
        help: "1 is fastest, 9 is smallest. The difference is usually a few per cent and several times the time.",
      },
      {
        name: "name",
        label: "Archive name",
        kind: "text",
        placeholder: "archive",
        help: "Without an extension — that comes from the format.",
      },
    ],
    note: "The order you add the files is the order of the entries, and each one keeps the name its upload had. gz is the exception: it compresses a single stream, so it takes exactly one file.",
  },
  "archive-extract": {
    files: "one",
    accept: ["application/zip", "application/gzip", "application/x-tar"],
    fields: [],
    note: "Each file inside becomes a download of its own, and all of them have to be fetched before the result expires. Folders are flattened: only a member's filename is kept, and two members that then share a name are both kept, numbered. An archive is refused if it holds more than 64 files, unpacks to more than 512 MB, or contains a link or a device rather than a file.",
  },
};
