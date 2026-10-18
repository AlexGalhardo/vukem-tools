/**
 * A resumable upload client for the tus 1.0 core protocol.
 *
 * Hand-written rather than `tus-js-client`, for one reason: both ends of this
 * protocol are ours, we use three of its verbs, and the library is larger than
 * the code below. It is not a general client — it does the creation extension,
 * a single PATCH per attempt, and a HEAD to find the offset again after a
 * dropped connection, which is exactly what this app needs and nothing more.
 *
 * What it deliberately does not do: parallel chunks, checksums, or the
 * concatenation extension. If any of those is ever needed, take the library.
 */

/** Progress is reported as bytes sent out of the total. */
export interface Progress {
  sent: number;
  total: number;
}

export interface Options {
  /** Where the tus handler is mounted, with its trailing slash. */
  endpoint: string;
  file: File;
  /** onProgress is called as the body is sent, at the browser's own cadence. */
  onProgress?: (progress: Progress) => void;
  /** signal cancels the upload. A cancelled upload leaves its partial data on
   *  the server, which the scheduled sweep removes. */
  signal?: AbortSignal;
  /** Attempts is how many times a dropped connection is resumed before giving
   *  up. Each retry waits a little longer. */
  attempts?: number;
}

/** UploadError carries what the server said, so the caller can show the real
 *  reason rather than "upload failed". */
export class UploadError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "UploadError";
    this.status = status;
  }
}

const TUS_VERSION = "1.0.0";
const DEFAULT_ATTEMPTS = 4;

/**
 * upload sends file and resolves when the server has all of it.
 *
 * The id is not returned: the tus protocol has no response body on a completed
 * PATCH, and inventing a header for one would mean a non-standard client and a
 * non-standard server. The caller finds the stored file through the API
 * instead, which is also what a page reloaded mid-upload has to do.
 */
export async function upload(options: Options): Promise<void> {
  const { endpoint, file, signal } = options;
  const attempts = options.attempts ?? DEFAULT_ATTEMPTS;

  const location = await create(endpoint, file, signal);

  let offset = 0;

  for (let attempt = 1; ; attempt++) {
    try {
      await send(location, file, offset, options);

      return;
    } catch (err) {
      // A refusal is final: the server has decided this file is not
      // acceptable, and sending the same bytes again cannot change that.
      if (err instanceof UploadError) {
        throw err;
      }

      if (signal?.aborted || attempt >= attempts) {
        throw err;
      }

      // A dropped connection. Ask the server how much it kept rather than
      // starting over — that is the whole reason this protocol is here.
      await wait(attempt * 500, signal);
      offset = await offsetOf(location, signal);
    }
  }
}

/** create reserves the upload and returns the URL to write to. */
async function create(endpoint: string, file: File, signal?: AbortSignal): Promise<string> {
  const response = await fetch(endpoint, {
    method: "POST",
    signal,
    headers: {
      "Tus-Resumable": TUS_VERSION,
      "Upload-Length": String(file.size),
      // The metadata value is base64 per the protocol. The name is the
      // client's to send and the server's to distrust: it sanitises it and
      // forces the extension to match the detected type.
      "Upload-Metadata": `filename ${base64(file.name)}`,
    },
  });

  if (!response.ok) {
    throw new UploadError(await reason(response), response.status);
  }

  const location = response.headers.get("Location");
  if (!location) {
    throw new UploadError("The server accepted the upload but did not say where to send it.", 502);
  }

  return location;
}

/**
 * send writes the rest of the file from offset.
 *
 * XMLHttpRequest rather than fetch, for the one thing it still does better:
 * upload progress events. `fetch` cannot report how much of a request body has
 * been sent in any browser we target, and a half-gigabyte upload with no
 * progress is indistinguishable from a hung one.
 */
function send(location: string, file: File, offset: number, options: Options): Promise<void> {
  const { onProgress, signal } = options;

  return new Promise((resolve, reject) => {
    const request = new XMLHttpRequest();

    const abort = () => request.abort();

    request.open("PATCH", location, true);
    request.setRequestHeader("Tus-Resumable", TUS_VERSION);
    request.setRequestHeader("Upload-Offset", String(offset));
    request.setRequestHeader("Content-Type", "application/offset+octet-stream");

    request.upload.onprogress = (event) => {
      if (onProgress && event.lengthComputable) {
        onProgress({ sent: offset + event.loaded, total: file.size });
      }
    };

    request.onload = () => {
      signal?.removeEventListener("abort", abort);

      if (request.status === 204) {
        onProgress?.({ sent: file.size, total: file.size });
        resolve();

        return;
      }

      // 4xx is the server refusing the file: a type it will not take, a size
      // over the limit, a scanner hit. Final, and the body says why.
      if (request.status >= 400 && request.status < 500) {
        reject(new UploadError(reasonFromText(request.responseText), request.status));

        return;
      }

      reject(new Error(`The upload failed with status ${request.status}.`));
    };

    request.onerror = () => {
      signal?.removeEventListener("abort", abort);
      reject(new Error("The connection dropped during the upload."));
    };

    request.onabort = () => {
      signal?.removeEventListener("abort", abort);
      reject(new DOMException("The upload was cancelled.", "AbortError"));
    };

    if (signal) {
      if (signal.aborted) {
        reject(new DOMException("The upload was cancelled.", "AbortError"));

        return;
      }

      signal.addEventListener("abort", abort, { once: true });
    }

    request.send(file.slice(offset));
  });
}

/** offsetOf asks how many bytes the server already holds. */
async function offsetOf(location: string, signal?: AbortSignal): Promise<number> {
  const response = await fetch(location, {
    method: "HEAD",
    signal,
    headers: { "Tus-Resumable": TUS_VERSION },
  });

  if (!response.ok) {
    throw new UploadError("The interrupted upload has expired. Choose the file again.", response.status);
  }

  const offset = Number(response.headers.get("Upload-Offset"));

  return Number.isFinite(offset) && offset >= 0 ? offset : 0;
}

/** reason pulls the human sentence out of a problem document, falling back to
 *  something honest rather than to the raw body. */
async function reason(response: Response): Promise<string> {
  return reasonFromText(await response.text().catch(() => ""));
}

function reasonFromText(body: string): string {
  try {
    const problem = JSON.parse(body) as { detail?: string; title?: string };

    return problem.detail ?? problem.title ?? "The server refused the file.";
  } catch {
    // Not a problem document. The raw body could be anything, including HTML,
    // so it is not shown.
    return "The server refused the file.";
  }
}

/** base64 encodes a name that may hold characters outside Latin-1, which
 *  btoa alone cannot take. */
function base64(value: string): string {
  const bytes = new TextEncoder().encode(value);
  let binary = "";

  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }

  return btoa(binary);
}

/** wait resolves after ms, or rejects if the signal fires first. */
function wait(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(resolve, ms);

    signal?.addEventListener(
      "abort",
      () => {
        clearTimeout(timer);
        reject(new DOMException("The upload was cancelled.", "AbortError"));
      },
      { once: true },
    );
  });
}
