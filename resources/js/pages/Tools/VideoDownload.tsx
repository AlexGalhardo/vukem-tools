import { Head } from "@inertiajs/react";
import { useCallback, useEffect, useRef, useState } from "react";

import Icon from "../../components/Icon";
import {
  ApiError,
  expiresIn,
  final,
  formatBytes,
  formatDuration,
  probe,
  results,
  show,
  submitVideoDownload,
  watch,
  type Conversion,
  type DownloadOptions,
  type Media,
  type ResultFile,
} from "../../lib/conversions";

interface Platform {
  name: string;
  label: string;
  hosts: string[];
}

interface Props {
  tool: { name: string; title: string; summary: string; in: string[]; out: string[] };
  platforms: Platform[];
  maxClipSeconds: number;
}

/** A choice of format, and the options it submits. */
interface Choice {
  id: string;
  label: string;
  hint: string;
  options: Omit<DownloadOptions, "url">;
}

/** The four choices, which are the four the bot offers too. They are written
 *  out rather than derived from the kinds and qualities the API publishes,
 *  because not every combination is worth offering: "smallest audio" is the
 *  same file as "best audio" for every platform here. */
const choices: Choice[] = [
  {
    id: "best",
    label: "MP4, best quality",
    hint: "The highest the platform offers, within the size cap.",
    options: { kind: "video", quality: "best" },
  },
  {
    id: "smallest",
    label: "MP4, smallest",
    hint: "The smallest stream that still has picture and sound.",
    options: { kind: "video", quality: "smallest" },
  },
  {
    id: "audio",
    label: "MP3, audio only",
    hint: "The sound, re-encoded to MP3.",
    options: { kind: "audio", quality: "best" },
  },
  {
    id: "clips",
    label: "Clips",
    hint: "Consecutive clips, delivered individually and as a zip.",
    options: { kind: "video", quality: "best", clips: true },
  },
];

const primaryButton =
  "inline-flex items-center gap-1.5 rounded-field bg-signal px-3 py-1.5 text-sm font-medium " +
  "text-raised transition-opacity duration-100 hover:opacity-90 disabled:opacity-50 dark:text-surface";

const textInput =
  "w-full rounded-field border border-edge bg-raised px-3 py-1.5 text-sm text-ink " +
  "transition-colors duration-100 placeholder:text-ink-faint focus:border-signal focus:outline-none";

/**
 * The video download tool.
 *
 * The flow is paste, check, choose, watch, save — and every step belongs to
 * the server. The page never decides whether a URL is acceptable, how large a
 * download may be, or what a failure means: it shows what the metadata
 * endpoint and the conversion report, so it cannot disagree with the API about
 * any of them.
 *
 * The one judgement it makes is when to stop offering. `too_large` from the
 * probe means no quality would succeed, and saying so before the user picks a
 * format is kinder than a refusal afterwards.
 */
export default function VideoDownload({
  tool,
  platforms,
  maxClipSeconds,
}: Props): React.ReactElement {
  const [url, setUrl] = useState("");
  const [media, setMedia] = useState<Media | null>(null);
  const [probing, setProbing] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [conversion, setConversion] = useState<Conversion | null>(null);
  const [files, setFiles] = useState<ResultFile[]>([]);
  const [phase, setPhase] = useState("");

  // The watcher is a side effect, not state: holding it in state would
  // re-subscribe on every render.
  const stopWatching = useRef<(() => void) | null>(null);

  useEffect(
    () => () => {
      stopWatching.current?.();
      stopWatching.current = null;
    },
    [],
  );

  const collect = useCallback(async (id: string) => {
    try {
      const body = await results(id);
      setFiles(body.data);
    } catch (err) {
      // The conversion did finish, so this is worth saying but not worth
      // discarding the row for: the links can be asked for again.
      setError(err instanceof ApiError ? err.message : "The result links could not be read.");
    }
  }, []);

  const settle = useCallback(
    async (id: string) => {
      const latest = await show(id).catch(() => null);

      if (latest === null) {
        return;
      }

      setConversion(latest);

      if (latest.status === "finished") {
        await collect(id);
      }
    },
    [collect],
  );

  const onProbe = useCallback(async () => {
    const trimmed = url.trim();

    if (trimmed === "") {
      return;
    }

    setProbing(true);
    setError(null);
    setMedia(null);
    setConversion(null);
    setFiles([]);

    try {
      setMedia(await probe(trimmed));
    } catch (err) {
      // The server's own sentence, and for a 422 the one attached to the
      // field — that is the message naming the four platforms.
      setError(
        err instanceof ApiError ? (err.first("url") ?? err.message) : "That URL could not be read.",
      );
    } finally {
      setProbing(false);
    }
  }, [url]);

  const onChoose = useCallback(
    async (choice: Choice) => {
      setError(null);
      setFiles([]);

      let started: Conversion;

      try {
        started = await submitVideoDownload({ url: url.trim(), ...choice.options });
      } catch (err) {
        setError(
          err instanceof ApiError
            ? (err.first("options.url") ?? err.message)
            : "That download could not be started.",
        );

        return;
      }

      setConversion(started);
      setPhase("");

      stopWatching.current?.();
      stopWatching.current = watch(
        started.id,
        (event) => {
          setPhase(event.phase);
          setConversion((current) =>
            current === null
              ? current
              : { ...current, status: event.status, progress: event.progress },
          );

          if (final(event.status)) {
            void settle(started.id);
          }
        },
        // A stream that gives up is not a failed conversion: ask the server
        // what happened rather than guessing from the connection.
        () => void settle(started.id),
      );
    },
    [settle, url],
  );

  const running = conversion !== null && !final(conversion.status);

  return (
    <>
      <Head title={tool.title} />

      <div className="mx-auto w-full max-w-3xl px-4 py-10">
        <header>
          <h1 className="text-3xl font-semibold tracking-tight">{tool.title}</h1>
          <p className="mt-3 max-w-[70ch] text-ink-soft">{tool.summary}</p>
        </header>

        <form
          className="mt-8"
          onSubmit={(event) => {
            event.preventDefault();
            void onProbe();
          }}
        >
          <label className="label" htmlFor="url">
            Link
          </label>

          <div className="mt-2 flex flex-col gap-2 sm:flex-row">
            <input
              id="url"
              name="url"
              type="url"
              inputMode="url"
              autoComplete="off"
              className={textInput}
              placeholder="https://www.youtube.com/watch?v=…"
              value={url}
              onChange={(event) => setUrl(event.target.value)}
              aria-describedby="platforms"
            />

            <button type="submit" className={primaryButton} disabled={probing || url.trim() === ""}>
              <Icon name="search" className="size-4" />
              {probing ? "Checking…" : "Check"}
            </button>
          </div>

          <p id="platforms" className="mt-2 max-w-[70ch] text-xs text-ink-faint">
            {platforms.map((platform) => platform.label).join(", ")}. Downloading may breach a
            platform&rsquo;s terms of service, and you are responsible for what you download.
          </p>
        </form>

        {error !== null && (
          <p role="alert" className="mt-6 flex items-start gap-1.5 text-sm text-bad">
            <Icon name="alert" className="mt-0.5 size-3.5 shrink-0" />
            <span>{error}</span>
          </p>
        )}

        {media !== null && (
          <section className="mt-6 rounded-panel border border-edge bg-raised">
            <div className="flex items-start gap-4 p-4">
              {media.thumbnail !== "" && (
                <img
                  src={media.thumbnail}
                  alt=""
                  width={160}
                  height={90}
                  loading="lazy"
                  className="hidden w-40 shrink-0 rounded-field object-cover sm:block"
                />
              )}

              <div className="min-w-0">
                <h2 className="truncate font-medium">{media.title}</h2>
                <p className="mt-0.5 text-sm text-ink-soft">{media.uploader}</p>
                <p className="tabular mt-2 text-xs text-ink-faint">
                  {formatDuration(media.duration_seconds)}
                  {media.estimated_bytes > 0 && ` · about ${formatBytes(media.estimated_bytes)}`}
                </p>
              </div>
            </div>

            {media.too_large ? (
              <p
                role="alert"
                className="flex items-start gap-1.5 border-t border-edge px-4 py-4 text-sm text-bad"
              >
                <Icon name="alert" className="mt-0.5 size-3.5 shrink-0" />
                <span>
                  Even the smallest version is larger than this server will download
                  {media.max_bytes > 0 && ` (${formatBytes(media.max_bytes)})`}.
                </span>
              </p>
            ) : (
              <fieldset className="border-t border-edge px-4 py-4" disabled={running}>
                <legend className="label">What would you like?</legend>

                <div className="mt-3 grid gap-2 sm:grid-cols-2">
                  {choices.map((choice) => (
                    <button
                      key={choice.id}
                      type="button"
                      className={
                        "rounded-field border border-edge px-3 py-2 text-left transition-colors " +
                        "duration-100 hover:border-signal hover:bg-sunken " +
                        "disabled:pointer-events-none disabled:opacity-50"
                      }
                      onClick={() => void onChoose(choice)}
                    >
                      <span className="block text-sm font-medium">
                        {choice.id === "clips" ? `${maxClipSeconds}-second clips` : choice.label}
                      </span>
                      <span className="mt-0.5 block text-xs text-ink-faint">{choice.hint}</span>
                    </button>
                  ))}
                </div>
              </fieldset>
            )}
          </section>
        )}

        {conversion !== null && <Progress conversion={conversion} phase={phase} files={files} />}
      </div>
    </>
  );
}

/** Progress is the live state of one download and the links it produced. */
function Progress({
  conversion,
  phase,
  files,
}: {
  conversion: Conversion;
  phase: string;
  files: ResultFile[];
}): React.ReactElement {
  return (
    <section className="mt-8" aria-live="polite">
      <h2 className="label border-b border-edge pb-2">Download</h2>

      {!final(conversion.status) && (
        <div className="py-4">
          <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
            <span className="text-sm text-ink-soft">{phase === "" ? "Starting" : phase}</span>
            <span className="tabular shrink-0 text-xs text-ink-faint">{conversion.progress}%</span>
          </div>

          <div
            className="mt-2 h-1 overflow-hidden rounded-full bg-sunken"
            role="progressbar"
            aria-valuenow={conversion.progress}
            aria-valuemin={0}
            aria-valuemax={100}
          >
            <div
              className="h-full rounded-full bg-signal transition-[width] duration-500 ease-settle"
              style={{ width: `${conversion.progress}%` }}
            />
          </div>
        </div>
      )}

      {conversion.status === "error" && (
        <p role="alert" className="mt-3 flex items-start gap-1.5 text-sm text-bad">
          <Icon name="alert" className="mt-0.5 size-3.5 shrink-0" />
          {/* The server's sentence, from its own taxonomy: the page never
              words a failure itself, so the API, this page and the bot cannot
              describe the same failure three ways. */}
          <span>{conversion.error_message}</span>
        </p>
      )}

      {conversion.status === "cancelled" && (
        <p className="mt-3 text-sm text-ink-faint">That download was cancelled.</p>
      )}

      {files.length > 0 && <ResultLinks files={files} />}
    </section>
  );
}

/** ResultLinks lists the signed links, with the time they have left.
 *
 *  The countdown is shown rather than hidden because five minutes is short
 *  enough to matter: somebody who walks away comes back to a dead link, and
 *  the honest thing is to say so while it is still alive. */
function ResultLinks({ files }: { files: ResultFile[] }): React.ReactElement {
  const expiresAt = files[0].expires_at;
  const [left, setLeft] = useState(() => expiresIn(expiresAt));

  useEffect(() => {
    setLeft(expiresIn(expiresAt));

    const timer = window.setInterval(() => setLeft(expiresIn(expiresAt)), 1000);

    return () => window.clearInterval(timer);
  }, [expiresAt]);

  if (left <= 0) {
    return (
      <p role="alert" className="mt-3 text-sm text-ink-faint">
        These results have expired and been deleted. Download it again for new ones.
      </p>
    );
  }

  return (
    <>
      <ul className="divide-y divide-edge">
        {files.map((file) => (
          <li key={file.url} className="flex items-center justify-between gap-3 py-3">
            <span className="min-w-0 flex-1">
              <span className="block truncate font-medium">{file.name}</span>
              <span className="tabular text-xs text-ink-faint">
                {formatBytes(file.size_bytes)}
              </span>
            </span>

            <a
              className={
                "shrink-0 inline-flex items-center gap-1.5 rounded-field px-2.5 py-1.5 text-sm " +
                "text-ink-soft transition-colors duration-100 hover:bg-sunken hover:text-ink"
              }
              href={file.url}
              download={file.name}
            >
              <Icon name="download" className="size-4" />
              Save
            </a>
          </li>
        ))}
      </ul>

      <p className="tabular mt-3 flex items-center gap-1.5 text-xs text-ink-faint">
        <Icon name="clock" className="size-3.5" />
        <span>
          {Math.floor(left / 60)}:{String(left % 60).padStart(2, "0")} before these links expire and
          the files are deleted.
        </span>
      </p>
    </>
  );
}
