import { ApiError, type Conversion } from "./conversions";
import { archiveForms } from "./forms/archive";
import { imageForms } from "./forms/image";
import { pdfForms } from "./forms/pdf";

/**
 * The client and the form description shared by every tool whose interface is
 * a file picker, a field list and a download.
 *
 * It is a file of its own rather than more functions in conversions.ts for the
 * same reason the server has one route file per domain: twenty tools added to
 * the shared module would make every tool a conflict with every other tool
 * being added at the same time.
 *
 * This module was the PDF domain's client first, and the PDF domain is still
 * the bulk of what uses it. Nothing in it is PDF-specific, which is why the
 * archive tools submit through the same two functions rather than through a
 * second copy of them.
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
 *  A tool can produce many files — a PDF split into twenty parts, a page per
 *  image, every member of an extracted archive — so this returns all of them
 *  rather than the first. Every one has to be downloaded before the result
 *  expires. */
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
 *  is page one of the first file, the earlier document comes first in a
 *  comparison, and the entries of an archive are written in the order they
 *  were added. */
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

/** forms is every tool's form, by tool name.
 *
 *  Concatenated from one table per domain rather than written here, so a
 *  domain adds its tools in its own file. This mirrors how the server builds
 *  its OpenAPI examples in routes/apidocs.go.
 *
 *  A tool whose interaction is genuinely different, rather than only its
 *  fields, gets a page of its own and no entry here — the sprite tools draw a
 *  grid over the sheet, which no field list can express. */
export const forms: Record<string, ToolForm> = { ...pdfForms, ...imageForms, ...archiveForms };

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
