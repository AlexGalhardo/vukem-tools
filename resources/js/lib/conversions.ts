/**
 * The conversion API, as the interface uses it.
 *
 * Every function here mirrors one endpoint and nothing more: the shapes are
 * the server's, the words in an error are the server's, and no sentence is
 * written twice. When a conversion fails, the message shown comes from
 * `error_message` on the conversion itself, which the server fills from its
 * own taxonomy — so the page cannot word a failure differently from the API.
 */

/** Status mirrors jobs.Status. */
export type Status = "pending" | "running" | "finished" | "error" | "cancelled";

/** Conversion mirrors apiv1.ConversionView. */
export interface Conversion {
  id: string;
  tool: string;
  status: Status;
  progress: number;
  phase: string;
  bytes_in: number;
  bytes_out: number;
  error_type: string;
  error_message: string;
  started_at: string | null;
  finished_at: string | null;
  expires_at: string;
  created_at: string;
}

/** Upload mirrors apiv1.UploadView. */
export interface Upload {
  id: string;
  name: string;
  mime: string;
  size_bytes: number;
  expires_at: string;
  created_at: string;
}

/** ProgressEvent mirrors jobs.Event, the payload of the progress stream. */
export interface ProgressEvent {
  id: number;
  conversion_id: string;
  status: Status;
  progress: number;
  phase: string;
  message: string;
  error_type?: string;
  at: string;
}

/** final reports whether no further event will follow. */
export function final(status: Status): boolean {
  return status === "finished" || status === "error" || status === "cancelled";
}

/** ApiError carries the server's own sentence and the field it belongs to. */
export class ApiError extends Error {
  readonly status: number;
  readonly fields: Record<string, string[]>;

  constructor(message: string, status: number, fields: Record<string, string[]> = {}) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.fields = fields;
  }

  /** first returns the first message for a field, which is what a form shows
   *  beside the input. */
  first(field: string): string | undefined {
    return this.fields[field]?.[0];
  }
}

/**
 * request calls the API with the session cookie and turns a problem document
 * into an ApiError carrying the server's wording.
 *
 * No CSRF token: the framework protects unsafe requests with
 * http.CrossOriginProtection, which checks `Sec-Fetch-Site` and `Origin` —
 * headers the browser sets and a page cannot forge. A same-origin fetch
 * therefore passes on its own, and the token fallback exists only for
 * browsers that send neither header.
 */
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

  if (response.status === 204) {
    return undefined as T;
  }

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

/** uploads lists the caller's unclaimed uploads, newest first. */
export function uploads(): Promise<{ data: Upload[] }> {
  return request("/api/v1/uploads");
}

/** PixelSVGOptions mirrors tools.PixelSVGOptions. Every field is optional and
 *  the server fills the default, so an omitted field is not an error. */
export interface PixelSVGOptions {
  mode?: "square" | "rect" | "contour";
  alpha?: "opacity" | "drop";
  max_colors?: number;
  tolerance?: number;
  scale?: number;
  simplify?: number;
}

/** submitPixelSVG starts a pixel-art to SVG conversion. */
export function submitPixelSVG(
  fileID: string,
  options: PixelSVGOptions,
  idempotencyKey?: string,
): Promise<Conversion> {
  return request("/api/v1/tools/pixels-to-svg", {
    method: "POST",
    body: JSON.stringify({
      file_id: fileID,
      options,
      ...(idempotencyKey ? { idempotency_key: idempotencyKey } : {}),
    }),
  });
}

/** show reads one conversion. It is the polling endpoint, and the fallback
 *  whenever the stream is not available. */
export function show(id: string): Promise<Conversion> {
  return request(`/api/v1/conversions/${encodeURIComponent(id)}`);
}

/** list reads the caller's conversions, newest first. */
export function list(limit = 20): Promise<{ data: Conversion[] }> {
  return request(`/api/v1/conversions?limit=${limit}`);
}

/** cancel stops a conversion. A conversion that has already ended answers
 *  409, which is not an error worth showing: the state the caller wanted is
 *  the state it is in. */
export async function cancel(id: string): Promise<Conversion | null> {
  try {
    return await request(`/api/v1/conversions/${encodeURIComponent(id)}`, { method: "DELETE" });
  } catch (err) {
    if (err instanceof ApiError && err.status === 409) {
      return null;
    }

    throw err;
  }
}

/**
 * watch streams a conversion's progress and calls onEvent for each step.
 *
 * It returns a function that stops watching. The stream sends the current
 * state first, so a conversion that finished before the page opened still
 * reports its outcome rather than hanging on an event that will never come.
 *
 * EventSource reconnects by itself and sends `Last-Event-ID`, which the server
 * uses to replay the trail — so a dropped connection costs nothing. The only
 * thing to handle is a stream that ends because the conversion ended, which is
 * why onEvent closes it on a final status rather than waiting for an error.
 */
export function watch(
  id: string,
  onEvent: (event: ProgressEvent) => void,
  onError?: (err: Error) => void,
): () => void {
  const source = new EventSource(`/api/v1/conversions/${encodeURIComponent(id)}/events`, {
    withCredentials: true,
  });

  let closed = false;

  const close = () => {
    if (!closed) {
      closed = true;
      source.close();
    }
  };

  source.addEventListener("progress", (message) => {
    let event: ProgressEvent;

    try {
      event = JSON.parse((message as MessageEvent<string>).data) as ProgressEvent;
    } catch {
      // A frame we cannot read is not worth tearing the stream down for: the
      // next one, or a poll, will carry the state.
      return;
    }

    onEvent(event);

    if (final(event.status)) {
      close();
    }
  });

  source.onerror = () => {
    // EventSource retries on its own, and `readyState` says whether it gave
    // up. Reporting every transient reconnect as an error would make a
    // perfectly healthy stream look broken.
    if (source.readyState === EventSource.CLOSED) {
      closed = true;
      onError?.(new Error("The progress stream closed. Reload to see the result."));
    }
  };

  return close;
}

/** formatBytes renders a size the way a person reads it. Binary units,
 *  because that is what a file manager shows for the same file. */
export function formatBytes(bytes: number): string {
  if (bytes <= 0) {
    return "0 B";
  }

  const units = ["B", "KiB", "MiB", "GiB"];
  const exponent = Math.min(Math.floor(Math.log2(bytes) / 10), units.length - 1);
  const value = bytes / 1024 ** exponent;

  return `${value.toFixed(exponent === 0 ? 0 : 1)} ${units[exponent]}`;
}

/** savedPercent is how much smaller the output is, or null when it grew or
 *  nothing is known yet. A conversion that produced a larger file is not a
 *  saving, and saying "-40% saved" would be nonsense. */
export function savedPercent(bytesIn: number, bytesOut: number): number | null {
  if (bytesIn <= 0 || bytesOut <= 0 || bytesOut >= bytesIn) {
    return null;
  }

  return Math.round(((bytesIn - bytesOut) / bytesIn) * 100);
}

/** expiresIn is how long a result still has, in whole seconds, floored at
 *  zero. The five-minute lifetime makes this worth showing, not hiding. */
export function expiresIn(expiresAt: string, now: number = Date.now()): number {
  return Math.max(0, Math.floor((new Date(expiresAt).getTime() - now) / 1000));
}
