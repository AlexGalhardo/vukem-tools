import { useEffect, useState } from "react";

import Icon, { type IconName } from "./Icon";
import {
  cancel as cancelConversion,
  expiresIn,
  final,
  formatBytes,
  savedPercent,
  type Conversion,
  type Status,
} from "../lib/conversions";

/** Job is one row: a local upload, or a conversion the server owns. */
export interface Job {
  /** key is stable for the row's whole life, including before the server has
   *  given it an id, so React does not remount the row mid-upload. */
  key: string;
  name: string;
  sizeIn: number;
  /** stage is where the row is. "uploading" is local; the rest mirror the
   *  conversion's status. */
  stage: "uploading" | "submitting" | Status;
  /** sent is upload progress in bytes, for the "uploading" stage. */
  sent?: number;
  conversion?: Conversion;
  /** error is a sentence to show. It comes from the server wherever the
   *  server had one. */
  error?: string;
  /** download is the signed link, once the result exists. */
  download?: string;
  /** cancel stops the upload; a conversion is cancelled through the API. */
  cancelUpload?: () => void;
}

interface Props {
  jobs: Job[];
  /** onChange reports a row the queue itself changed — a cancellation, or an
   *  expiry it noticed — so the page holding the list stays the one truth. */
  onChange: (key: string, patch: Partial<Job>) => void;
  onRemove: (key: string) => void;
}

/**
 * The job queue: one row per file, with the progress the server reports.
 *
 * This is the only genuinely live thing on the page, so it gets the motion
 * budget: the bar eases, the percentage counts with tabular figures so it does
 * not jitter, and a finished row settles rather than snapping. Everything else
 * in the interface transitions at 120ms or not at all.
 */
export default function ConversionQueue({ jobs, onChange, onRemove }: Props): React.ReactElement | null {
  if (jobs.length === 0) {
    return null;
  }

  return (
    <section className="mt-8">
      <h2 className="label border-b border-edge pb-2">
        {jobs.length} file{jobs.length === 1 ? "" : "s"}
      </h2>
      <ul className="divide-y divide-edge">
        {jobs.map((job) => (
          <li key={job.key}>
            <Row job={job} onChange={onChange} onRemove={onRemove} />
          </li>
        ))}
      </ul>
    </section>
  );
}

function Row({ job, onChange, onRemove }: { job: Job } & Omit<Props, "jobs">): React.ReactElement {
  const percent = progressOf(job);
  const done = job.stage === "finished";

  return (
    <div className="py-4">
      <div className="flex items-start gap-3">
        <Icon name={iconOf(job.stage)} className={`mt-0.5 size-5 shrink-0 ${toneOf(job.stage)}`} />

        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
            <p className="truncate font-medium" title={job.name}>
              {job.name}
            </p>
            <p className="tabular shrink-0 text-xs text-ink-faint">
              {formatBytes(job.sizeIn)}
              {done && job.conversion && job.conversion.bytes_out > 0 && (
                <>
                  {" → "}
                  {formatBytes(job.conversion.bytes_out)}
                  {savedPercent(job.sizeIn, job.conversion.bytes_out) !== null && (
                    <span className="ml-2 text-ok">
                      {savedPercent(job.sizeIn, job.conversion.bytes_out)}% smaller
                    </span>
                  )}
                </>
              )}
            </p>
          </div>

          <p className="mt-0.5 text-sm text-ink-soft">
            <StageText job={job} />
          </p>

          {!final(job.stage as Status) || job.stage === "uploading" ? (
            <div
              role="progressbar"
              aria-valuenow={percent}
              aria-valuemin={0}
              aria-valuemax={100}
              aria-label={`${job.name}: ${percent}%`}
              className="mt-2 h-1 overflow-hidden rounded-full bg-sunken"
            >
              <div
                className="h-full rounded-full bg-signal transition-[width] duration-500 ease-settle"
                style={{ width: `${percent}%` }}
              />
            </div>
          ) : null}

          {job.error && (
            <p className="mt-2 flex items-start gap-1.5 text-sm text-bad">
              <Icon name="alert" className="mt-0.5 size-3.5 shrink-0" />
              <span>{job.error}</span>
            </p>
          )}

          {done && job.download && job.conversion && (
            <Result conversion={job.conversion} href={job.download} name={job.name} />
          )}
        </div>

        <RowAction job={job} onChange={onChange} onRemove={onRemove} />
      </div>
    </div>
  );
}

/** Result is the download, with the five-minute lifetime counting down beside
 *  it. The countdown is the honest way to show a promise that short: a static
 *  "expires in 5 minutes" would be wrong within a minute of rendering. */
function Result({
  conversion,
  href,
  name,
}: {
  conversion: Conversion;
  href: string;
  name: string;
}): React.ReactElement {
  const [left, setLeft] = useState(() => expiresIn(conversion.expires_at));

  useEffect(() => {
    if (left <= 0) {
      return;
    }

    const timer = setInterval(() => setLeft(expiresIn(conversion.expires_at)), 1000);

    return () => clearInterval(timer);
  }, [conversion.expires_at, left]);

  if (left <= 0) {
    return (
      <p className="mt-3 text-sm text-ink-faint">
        This result has expired and been deleted. Convert the file again for a new one.
      </p>
    );
  }

  return (
    <div className="mt-3 flex flex-wrap items-center gap-3">
      <a
        href={href}
        download={name.replace(/\.[^.]+$/, "") + ".svg"}
        className="inline-flex items-center gap-1.5 rounded-field bg-signal px-3 py-1.5 text-sm font-medium text-raised transition-opacity duration-100 hover:opacity-90 dark:text-surface"
      >
        <Icon name="download" className="size-4" />
        Download
      </a>
      <span className="tabular flex items-center gap-1.5 text-xs text-ink-faint">
        <Icon name="clock" className="size-3.5" />
        deleted in {Math.floor(left / 60)}:{String(left % 60).padStart(2, "0")}
      </span>
    </div>
  );
}

/** RowAction is cancel while there is something to cancel, and dismiss once
 *  there is not. One button in one place, so the row does not reflow. */
function RowAction({ job, onChange, onRemove }: { job: Job } & Omit<Props, "jobs">): React.ReactElement {
  const running = job.stage === "uploading" || job.stage === "pending" || job.stage === "running";

  if (!running) {
    return (
      <button
        type="button"
        onClick={() => onRemove(job.key)}
        aria-label={`Remove ${job.name} from the list`}
        className="shrink-0 rounded-field p-1.5 text-ink-faint transition-colors duration-100 hover:bg-sunken hover:text-ink"
      >
        <Icon name="close" className="size-4" />
      </button>
    );
  }

  return (
    <button
      type="button"
      onClick={async () => {
        // The upload is local, so it stops immediately. A conversion is the
        // server's, so it is asked and the stream reports the result.
        job.cancelUpload?.();

        if (job.conversion) {
          try {
            await cancelConversion(job.conversion.id);
          } catch {
            // Already ended, or the request failed. Either way the stream or
            // the next poll carries the real state, so nothing is claimed
            // here.
            return;
          }
        }

        onChange(job.key, { stage: "cancelled" });
      }}
      className="shrink-0 rounded-field px-2.5 py-1.5 text-sm text-ink-soft transition-colors duration-100 hover:bg-sunken hover:text-ink"
    >
      Cancel
    </button>
  );
}

function StageText({ job }: { job: Job }): React.ReactElement {
  switch (job.stage) {
    case "uploading":
      return (
        <>
          Uploading <span className="tabular">{progressOf(job)}%</span>
        </>
      );
    case "submitting":
      return <>Queueing</>;
    case "pending":
      return <>Waiting for a worker</>;
    case "running":
      return (
        <>
          {job.conversion?.phase ? `${job.conversion.phase} ` : "Converting "}
          <span className="tabular">{progressOf(job)}%</span>
        </>
      );
    case "finished":
      return <>Done</>;
    case "cancelled":
      return <>Cancelled</>;
    case "error":
      return <>Failed</>;
    default:
      return <>{job.stage}</>;
  }
}

function progressOf(job: Job): number {
  if (job.stage === "uploading") {
    return job.sizeIn > 0 ? Math.round(((job.sent ?? 0) / job.sizeIn) * 100) : 0;
  }

  if (job.stage === "finished") {
    return 100;
  }

  return job.conversion?.progress ?? 0;
}

function iconOf(stage: Job["stage"]): IconName {
  switch (stage) {
    case "uploading":
      return "upload";
    case "finished":
      return "check";
    case "error":
      return "alert";
    case "cancelled":
      return "close";
    default:
      return "clock";
  }
}

function toneOf(stage: Job["stage"]): string {
  switch (stage) {
    case "finished":
      return "text-ok";
    case "error":
      return "text-bad";
    case "cancelled":
      return "text-ink-faint";
    default:
      return "text-signal";
  }
}
