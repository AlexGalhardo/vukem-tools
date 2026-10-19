import { results, submitOne, type ResultFile } from "./toolForm";
import { type Conversion } from "./conversions";

/**
 * The sprite tools' client.
 *
 * The two pages share the submit, the result list and the capability fetch,
 * and differ in everything on screen — which is why they are two pages and
 * not an entry in the generic form table.
 */

/** MetaFormat is one metadata format, as the server publishes it. */
export interface MetaFormat {
  name: string;
  extension: string;
  mime: string;
  /** imports is the one sentence saying what reads this format, which is the
   *  only thing somebody choosing between five of them needs. */
  imports: string;
  /** trim is false for CSS, which has nowhere to record a trim offset. */
  trim: boolean;
}

/** Capabilities is what the server says these tools can produce. */
export interface Capabilities {
  formats: MetaFormat[];
  modes: string[];
  layouts: string[];
  max_formats: number;
  max_frames: number;
  max_size: number;
}

/** capabilities reads the format list from the server.
 *
 *  Fetched rather than hardcoded, so the page and the API cannot disagree
 *  about what can be written — the same decision the media pages made. */
export async function capabilities(): Promise<Capabilities> {
  const response = await fetch("/api/v1/tools/sprite-split/formats", {
    headers: { Accept: "application/json" },
    credentials: "same-origin",
  });

  if (!response.ok) {
    throw new Error("The metadata formats could not be read.");
  }

  return (await response.json()) as Capabilities;
}

/** submitSprite starts a sprite conversion. */
export function submitSprite(
  tool: string,
  fileID: string,
  options: Record<string, unknown>,
): Promise<Conversion> {
  return submitOne(tool, fileID, options);
}

export { results, type ResultFile };

/** sheetSize reads an image's dimensions without drawing it.
 *
 *  Dimensions first, picture second: a 4096x4096 sheet takes a moment to
 *  decode and paint, and the frame count is what somebody adjusting a cell
 *  size is watching. Showing the number before the preview is what keeps the
 *  page responsive on a large sheet.
 */
export function sheetSize(file: File): Promise<{ width: number; height: number; url: string }> {
  return new Promise((resolve, reject) => {
    const url = URL.createObjectURL(file);
    const img = new Image();

    img.onload = () => resolve({ width: img.naturalWidth, height: img.naturalHeight, url });
    img.onerror = () => {
      URL.revokeObjectURL(url);
      reject(new Error("That file could not be read as an image."));
    };

    img.src = url;
  });
}

/**
 * zipEntryCount counts the files in a ZIP without inflating any of them.
 *
 * It reads the end-of-central-directory record, which is in the last 64 KiB
 * of the file and carries the entry count as a field. That is a few hundred
 * bytes of reading rather than a decompression of the whole archive, which
 * matters because the point is to answer "how many frames is this?" the
 * instant the file is dropped.
 *
 * Returns undefined for anything it cannot read as a ZIP. The server is the
 * authority on that; this is a courtesy, and a wrong guess here must not stop
 * somebody submitting a file the server would have accepted.
 */
export async function zipEntryCount(file: File): Promise<number | undefined> {
  // The record is 22 bytes plus a comment of up to 65535, so the last 64 KiB
  // and a little always contains it.
  const tailBytes = Math.min(file.size, 22 + 0xffff);
  const tail = new DataView(await file.slice(file.size - tailBytes).arrayBuffer());

  const signature = 0x06054b50;

  // Scanned backwards: the comment can contain the signature's bytes, and the
  // real record is the last one.
  for (let i = tail.byteLength - 22; i >= 0; i--) {
    if (tail.getUint32(i, true) !== signature) {
      continue;
    }

    // Offset 10 is "total number of entries in the central directory".
    const total = tail.getUint16(i + 10, true);

    // 0xffff is the Zip64 marker: the real count is in a record this does not
    // read, so the honest answer is "I do not know" rather than 65535.
    return total === 0xffff ? undefined : total;
  }

  return undefined;
}
