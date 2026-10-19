import { ApiError, type Conversion } from "./conversions";

/**
 * The PDF tools' client.
 *
 * It is a file of its own rather than more functions in conversions.ts for the
 * same reason the server has one route file per domain: nineteen tools added
 * to the shared module would make every tool a conflict with every other tool
 * being added at the same time.
 */

/** ResultFile is one output of a finished conversion, with a signed link. */
export interface ResultFile {
  name: string;
  mime: string;
  size_bytes: number;
  url: string;
  expires_at: string;
}

/** request is the JSON fetch every call here goes through.
 *
 *  It is a local copy rather than an import because conversions.ts keeps its
 *  own `request` unexported. The duplication is four lines and the alternative
 *  is editing a module three other domains are also adding to; see the
 *  framework feedback note about it. */
async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(path, {
    ...init,
    credentials: "same-origin",
    headers: {
      Accept: "application/json",
      ...(init.body ? { "Content-Type": "application/json" } : {}),
      ...init.headers,
    },
  });

  const body = (await response.json().catch(() => null)) as
    | { detail?: string; title?: string; errors?: Record<string, string[]> }
    | null;

  if (!response.ok) {
    throw new ApiError(
      body?.detail ?? body?.title ?? "That did not work.",
      response.status,
      body?.errors ?? {},
    );
  }

  return body as T;
}

/** results reads the signed links to a finished conversion's output.
 *
 *  A PDF tool can produce many files — a split into twenty parts, a page per
 *  image — so this returns all of them rather than the first. Every one has
 *  to be downloaded before the result expires. */
export async function results(conversionID: string): Promise<ResultFile[]> {
  const body = await request<{ data: ResultFile[] }>(
    `/api/v1/conversions/${encodeURIComponent(conversionID)}/result`,
  );

  return body.data;
}

/** submitOne starts a conversion from one upload. */
export function submitOne(
  tool: string,
  fileID: string,
  options: Record<string, unknown>,
): Promise<Conversion> {
  return request(`/api/v1/tools/${tool}`, {
    method: "POST",
    body: JSON.stringify({ file_id: fileID, options }),
  });
}

/** submitMany starts a conversion from several uploads, in the order given.
 *
 *  The order is the instruction, not a detail: page one of a merged document
 *  is page one of the first file, and the earlier document comes first in a
 *  comparison. */
export function submitMany(
  tool: string,
  fileIDs: string[],
  options: Record<string, unknown>,
): Promise<Conversion> {
  return request(`/api/v1/tools/${tool}`, {
    method: "POST",
    body: JSON.stringify({ file_ids: fileIDs, options }),
  });
}

/** FieldKind is how one option is edited. */
export type FieldKind = "text" | "number" | "choice" | "toggle" | "password";

/** Field describes one of a tool's options, for the form.
 *
 *  The form is generated from this rather than written per tool, which is the
 *  same decision as the one behind the single generic tool on the server: the
 *  parts that differ between nineteen PDF tools are the fields, and the parts
 *  that are the same — upload, submit, watch, download, show the failure the
 *  server reported — are the parts worth having one of. */
export interface Field {
  /** name is the option's key in the request body. */
  name: string;
  label: string;
  kind: FieldKind;
  /** help is one line under the field. It says what the option does, and
   *  where there is an honest limit it says that too. */
  help?: string;
  /** choices are the values for a choice field. */
  choices?: string[];
  /** min and max bound a number field, matching the server's own bounds so
   *  the browser refuses what the API would refuse. */
  min?: number;
  max?: number;
  step?: number;
  /** placeholder is shown when the field is empty, usually the default the
   *  server will apply. */
  placeholder?: string;
  /** required marks an option the tool cannot run without. */
  required?: boolean;
}

/** ToolForm is one tool's form. */
export interface ToolForm {
  /** files is how many uploads the tool takes: "one" or "many". */
  files: "one" | "many";
  /** accept are the MIME types the picker filters by. It is a hint; the
   *  server identifies the bytes. */
  accept: string[];
  fields: Field[];
  /** note is a sentence shown above the form where the tool has a limit
   *  somebody should know before they convert rather than after. */
  note?: string;
}

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

/** forms is every PDF tool's form, by tool name. */
export const forms: Record<string, ToolForm> = {
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

/** optionsFrom turns the form's values into the request body.
 *
 *  An empty field is left out rather than sent as an empty string or a zero:
 *  every optional field on the server means "the documented default" when it
 *  is absent, and sending a zero would mean "zero". */
export function optionsFrom(form: ToolForm, values: Record<string, string | boolean>): Record<string, unknown> {
  const out: Record<string, unknown> = {};

  for (const field of form.fields) {
    const value = values[field.name];

    if (value === undefined || value === "" ) {
      continue;
    }

    if (field.kind === "toggle") {
      if (value === true) {
        out[field.name] = true;
      }

      continue;
    }

    if (field.kind === "number") {
      const parsed = Number(value);

      if (!Number.isNaN(parsed)) {
        out[field.name] = parsed;
      }

      continue;
    }

    // "degrees" and "part" are choices of numbers rather than of words, so
    // they are sent as numbers: the API's field is an integer.
    if (field.name === "degrees" || field.name === "part") {
      out[field.name] = Number(value);

      continue;
    }

    out[field.name] = value;
  }

  return out;
}
