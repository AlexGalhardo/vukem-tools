import { type Field, type ToolForm } from "../toolForm";

/**
 * The PDF tools' forms.
 *
 * One file per domain, which is the same decision routes/apidocs_pdf.go made
 * on the server and for the same reason: nineteen entries added to a shared
 * table would make every tool a conflict with every other tool being added at
 * the same time. lib/toolForm.ts concatenates the domain tables.
 */

const pageRangeField: Field = {
  name: "pages",
  label: "Pages",
  kind: "text",
  placeholder: "every page",
  help: "A range such as 1-3,5,8-z, where z is the last page. A page the document does not have is refused rather than skipped.",
};

const positionChoices = [
  "top-left",
  "top-center",
  "top-right",
  "middle-left",
  "center",
  "middle-right",
  "bottom-left",
  "bottom-center",
  "bottom-right",
];

/** pdfForms is every PDF tool's form, by tool name. */
export const pdfForms: Record<string, ToolForm> = {
  "pdf-merge": {
    files: "many",
    accept: ["application/pdf"],
    fields: [],
    note: "The order you add the files is the order of the pages. The first document's bookmarks are kept; the others' cannot be.",
  },
  "pdf-split": {
    files: "one",
    accept: ["application/pdf"],
    fields: [
      {
        name: "mode",
        label: "How to split",
        kind: "choice",
        choices: ["every", "ranges"],
        help: "every makes a file per group of n pages; ranges uses the ranges you name.",
      },
      { name: "every", label: "Pages per file", kind: "number", min: 1, max: 10000, placeholder: "1" },
    ],
    note: "Each part is a separate result with its own expiry, so all of them have to be downloaded before the result lifetime runs out.",
  },
  "pdf-extract-pages": {
    files: "one",
    accept: ["application/pdf"],
    fields: [{ ...pageRangeField, required: true, placeholder: "1-3,5" }],
    note: "The bookmarks are dropped, because some would point at pages that are no longer there.",
  },
  "pdf-remove-pages": {
    files: "one",
    accept: ["application/pdf"],
    fields: [{ ...pageRangeField, label: "Pages to remove", required: true, placeholder: "2,5-7" }],
  },
  "pdf-organise": {
    files: "one",
    accept: ["application/pdf"],
    fields: [
      {
        ...pageRangeField,
        label: "The new order",
        required: true,
        placeholder: "3,1,2",
        help: "The pages in the order they should appear. A page may appear twice to duplicate it.",
      },
    ],
  },
  "pdf-rotate": {
    files: "one",
    accept: ["application/pdf"],
    fields: [
      { name: "degrees", label: "Turn by", kind: "choice", choices: ["90", "180", "270"] },
      pageRangeField,
      {
        name: "absolute",
        label: "Set rather than add",
        kind: "toggle",
        help: "Off adds to each page's current rotation, which is what turning a page means.",
      },
    ],
  },
  "pdf-crop": {
    files: "one",
    accept: ["application/pdf"],
    fields: [
      { name: "left", label: "Left margin", kind: "number", min: 0, max: 1000, placeholder: "0" },
      { name: "bottom", label: "Bottom margin", kind: "number", min: 0, max: 1000, placeholder: "0" },
      { name: "right", label: "Right margin", kind: "number", min: 0, max: 1000, placeholder: "0" },
      { name: "top", label: "Top margin", kind: "number", min: 0, max: 1000, placeholder: "0" },
      pageRangeField,
    ],
    note: "Margins are in points: 72 to the inch. The content is hidden rather than deleted, so the crop can be undone.",
  },
  "images-to-pdf": {
    files: "many",
    accept: ["image/jpeg", "image/png", "image/webp", "image/tiff", "image/avif", "image/gif", "image/bmp"],
    fields: [
      { name: "page_size", label: "Page size", kind: "choice", choices: ["a4", "a3", "a5", "letter", "legal"] },
      { name: "orientation", label: "Orientation", kind: "choice", choices: ["auto", "portrait", "landscape"] },
      {
        name: "fit",
        label: "Fit",
        kind: "choice",
        choices: ["contain", "fill", "exact"],
        help: "contain fits inside the margins, fill covers the page, exact makes the page the image's own size.",
      },
      { name: "margin", label: "Margin", kind: "number", min: 0, max: 144, placeholder: "0" },
    ],
    note: "A JPEG is embedded exactly as uploaded, re-encoded zero times. Anything else is converted to JPEG once.",
  },
  "pdf-to-image": {
    files: "one",
    accept: ["application/pdf"],
    fields: [
      { name: "format", label: "Format", kind: "choice", choices: ["png", "jpeg"] },
      { name: "dpi", label: "Resolution", kind: "number", min: 36, max: 600, placeholder: "150" },
      { name: "quality", label: "JPEG quality", kind: "number", min: 1, max: 100, placeholder: "85" },
      pageRangeField,
    ],
    note: "One image per page. Pages are rendered from the crop box, so an image matches what a reader shows.",
  },
  "pdf-to-pdfa": {
    files: "one",
    accept: ["application/pdf"],
    fields: [
      {
        name: "part",
        label: "Conformance",
        kind: "choice",
        choices: ["1", "2", "3"],
        help: "1b forbids transparency, 3b allows attachments. 2b is the usual answer.",
      },
    ],
    note: "Content is kept in preference to conformance: a document that cannot be made conformant comes back intact, with the converter's own list of what is wrong.",
  },
  "pdf-to-text": {
    files: "one",
    accept: ["application/pdf"],
    fields: [
      { name: "format", label: "Format", kind: "choice", choices: ["text", "markdown"] },
      { name: "layout", label: "Keep the column spacing", kind: "toggle" },
      { name: "page_rules", label: "A rule between pages", kind: "toggle" },
      { ...pageRangeField, help: "One continuous range such as 1-20. Empty reads the whole document." },
    ],
    note: "A text conversion with heading and list heuristics, not a layout reconstruction: a document whose headings are marked only by font size comes out as paragraphs. A scanned page has no text to extract.",
  },
  "pdf-compress": {
    files: "one",
    accept: ["application/pdf"],
    fields: [
      {
        name: "level",
        label: "How hard to try",
        kind: "choice",
        choices: ["light", "balanced", "strong"],
        help: "The levels downsample images to 300, 150 and 96 DPI. Light keeps every page's appearance.",
      },
    ],
    note: "A document that is already smaller than the compressed version comes back unchanged rather than larger.",
  },
  "pdf-repair": {
    files: "one",
    accept: ["application/pdf"],
    fields: [],
    note: "It rebuilds the structure, not content the file no longer holds. A file it cannot rebuild into a readable document is refused rather than returned empty.",
  },
  "pdf-protect": {
    files: "one",
    accept: ["application/pdf"],
    fields: [
      {
        name: "user_password",
        label: "Password to open",
        kind: "password",
        help: "Leave empty to restrict what readers may do without requiring a password to open it.",
      },
      {
        name: "owner_password",
        label: "Owner password",
        kind: "password",
        required: true,
        help: "Required, and it must differ from the opening password: without it any PDF tool can lift the restrictions.",
      },
      { name: "print", label: "Printing", kind: "choice", choices: ["full", "low", "none"] },
      {
        name: "modify",
        label: "Changes",
        kind: "choice",
        choices: ["all", "annotate", "form", "assembly", "none"],
      },
      { name: "allow_extract", label: "Allow copying text", kind: "toggle" },
    ],
    note: "AES-256. Nobody, including this service, can recover a password you lose.",
  },
  "pdf-unlock": {
    files: "one",
    accept: ["application/pdf"],
    fields: [
      {
        name: "password",
        label: "The document's password",
        kind: "password",
        required: true,
        help: "It is tried once. This removes a password you know; it does not recover one you do not have.",
      },
    ],
  },
  "pdf-compare": {
    files: "many",
    accept: ["application/pdf"],
    fields: [],
    note: "Add exactly two documents, the earlier one first. It compares the text: a change to a font, a margin or a picture does not appear.",
  },
  "pdf-watermark": {
    files: "one",
    accept: ["application/pdf"],
    fields: [
      {
        name: "text",
        label: "The text",
        kind: "text",
        required: true,
        placeholder: "DRAFT",
        help: "Unaccented Latin letters, digits and punctuation: the standard PDF fonts are the only ones stamped without embedding a font file.",
      },
      { name: "position", label: "Position", kind: "choice", choices: positionChoices },
      { name: "size", label: "Size", kind: "number", min: 4, max: 144, placeholder: "fit to the page" },
      { name: "rotate", label: "Angle", kind: "number", min: -360, max: 360, placeholder: "0" },
      { name: "opacity", label: "Opacity", kind: "number", min: 0, max: 1, step: 0.01, placeholder: "0.18" },
      { name: "gray", label: "Ink", kind: "number", min: 0, max: 1, step: 0.01, placeholder: "0 (black)" },
      { name: "bold", label: "Bold", kind: "toggle" },
      pageRangeField,
    ],
    note: "A watermark is a visible mark, not protection: anybody with a PDF editor can remove it.",
  },
  "pdf-page-numbers": {
    files: "one",
    accept: ["application/pdf"],
    fields: [
      {
        name: "format",
        label: "The label",
        kind: "text",
        placeholder: "{n}",
        help: "{n} is the number and {total} the count, so \"Page {n} of {total}\" works.",
      },
      { name: "position", label: "Position", kind: "choice", choices: positionChoices },
      { name: "start_at", label: "Start at", kind: "number", min: 1, max: 100000, placeholder: "1" },
      { name: "size", label: "Size", kind: "number", min: 4, max: 144, placeholder: "10" },
      { ...pageRangeField, help: "A range such as 2-z to leave a cover page unnumbered." },
    ],
  },
};
