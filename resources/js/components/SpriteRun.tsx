import { useCallback, useEffect, useRef, useState } from "react";

import ConversionQueue, { type Job } from "./ConversionQueue";
import Icon from "./Icon";
import { ApiError, final, formatBytes, show, uploads, watch, type Conversion } from "../lib/conversions";
import { results, submitSprite, type ResultFile } from "../lib/sprite";
import { upload, UploadError } from "../lib/upload";

/**
 * The upload, submit, watch and download flow both sprite pages share.
 *
 * It is the generic tool page's flow without the generic form: the two sprite
 * pages differ entirely in their controls and not at all in what happens after
 * the button is pressed, so this is the half worth having one of.
 *
 * Nothing here decides anything. Progress, status, every failure message and
 * every download link come from the server, so the page cannot disagree with
 * the API about what happened.
 */
export interface SpriteRun {
  jobs: Job[];
  /** files are the finished outputs of the newest run, newest first. */
  files: Record<string, ResultFile[]>;
  busy: boolean;
  problem?: string;
  /** run uploads the file and submits it with these options. */
  run: (file: File, options: Record<string, unknown>) => Promise<void>;
  patch: (key: string, changes: Partial<Job>) => void;
  remove: (key: string) => void;
}

let nextKey = 0;

/** useSpriteRun wires the flow for one sprite tool. */
export function useSpriteRun(tool: string, tusPath: string): SpriteRun {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [files, setFiles] = useState<Record<string, ResultFile[]>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();

  // The watchers are side effects, not state: putting them in state would
  // re-subscribe on every render.
  const watchers = useRef(new Map<string, () => void>());

  useEffect(
    () => () => {
      for (const stop of watchers.current.values()) {
        stop();
      }

      watchers.current.clear();
    },
    [],
  );

  const patch = useCallback((key: string, changes: Partial<Job>) => {
    setJobs((current) => current.map((job) => (job.key === key ? { ...job, ...changes } : job)));
  }, []);

  const remove = useCallback((key: string) => {
    watchers.current.get(key)?.();
    watchers.current.delete(key);
    setJobs((current) => current.filter((job) => job.key !== key));
    setFiles((current) => {
      const next = { ...current };
      delete next[key];

      return next;
    });
  }, []);

  const follow = useCallback(
    (key: string, conversion: Conversion) => {
      const settle = async (status: Conversion["status"]) => {
        if (status !== "finished") {
          return;
        }

        try {
          const out = await results(conversion.id);

          setFiles((current) => ({ ...current, [key]: out }));
          patch(key, { download: out[0]?.url });
        } catch {
          // The row still says done and a reload fetches the links; a failure
          // here must not claim the conversion failed.
        }
      };

      const stop = watch(
        conversion.id,
        (event) => {
          patch(key, {
            stage: event.status,
            conversion: {
              ...conversion,
              status: event.status,
              progress: event.progress,
              phase: event.phase,
            },
            // Only a progress event's message is kept. The final event says
            // "Done.", which would otherwise overwrite the sentence carrying
            // the frame count and the packing efficiency with nothing worth
            // reading. Omitting the key rather than setting undefined is what
            // leaves the earlier one in place.
            ...(event.status === "running" && event.message ? { note: event.message } : {}),
            error: event.status === "error" ? event.message : undefined,
          });

          if (!final(event.status)) {
            return;
          }

          void show(conversion.id)
            .then((fresh) => {
              patch(key, {
                conversion: fresh,
                stage: fresh.status,
                // The conversion's own sentence, which is authoritative: the
                // stream may have been connected too late to carry it.
                note: fresh.note || undefined,
                error: fresh.error_message || undefined,
              });

              return settle(fresh.status);
            })
            .catch(() => undefined);
        },
        (err) => patch(key, { error: err.message }),
      );

      watchers.current.set(key, stop);
    },
    [patch],
  );

  const run = useCallback(
    async (file: File, options: Record<string, unknown>) => {
      const key = `job-${nextKey++}`;

      setBusy(true);
      setProblem(undefined);
      setJobs((current) => [
        { key, name: file.name, sizeIn: file.size, stage: "uploading", sent: 0 },
        ...current,
      ]);

      try {
        await upload({ endpoint: tusPath, file, onProgress: ({ sent }) => patch(key, { sent }) });
      } catch (err) {
        const message =
          err instanceof UploadError
            ? err.message
            : "The upload did not finish. Check your connection and try again.";

        patch(key, { stage: "error", error: message });
        setProblem(message);
        setBusy(false);

        return;
      }

      patch(key, { stage: "submitting" });

      try {
        // The tus protocol has no response body, so the stored id is read
        // back: the newest unclaimed upload whose size and name stem match.
        const stem = file.name.replace(/\.[^.]+$/, "");
        const mine = (await uploads()).data.find(
          (candidate) =>
            candidate.size_bytes === file.size && candidate.name.replace(/\.[^.]+$/, "") === stem,
        );

        if (!mine) {
          throw new Error("The upload was stored but could not be found again.");
        }

        const conversion = await submitSprite(tool, mine.id, options);

        patch(key, { stage: conversion.status, conversion });
        follow(key, conversion);
      } catch (err) {
        const message = err instanceof ApiError ? spriteFailure(err) : String(err);

        patch(key, { stage: "error", error: message });
        setProblem(message);
      } finally {
        setBusy(false);
      }
    },
    [follow, patch, tool, tusPath],
  );

  return { jobs, files, busy, problem, run, patch, remove };
}

/** spriteFailure finds the server's message for whichever option it refused.
 *
 *  The field message is the useful one: "A CSS sprite sheet has nowhere to
 *  record a frame's trim offset" tells somebody what to do, where "the
 *  conversion could not be started" does not. */
function spriteFailure(err: ApiError): string {
  for (const field of [
    "file_id",
    "options.mode",
    "options.layout",
    "options.cell_w",
    "options.cell_h",
    "options.rows",
    "options.metadata",
    "options.extrude",
    "options.background",
    "options",
  ]) {
    const message = err.first(field);

    if (message) {
      return message;
    }
  }

  return err.message;
}

/** SpriteResults lists a finished conversion's files, and shows the sheet
 *  when one of them is the sheet. */
export function SpriteResults({
  jobs,
  files,
  onChange,
  onRemove,
  preview,
}: {
  jobs: Job[];
  files: Record<string, ResultFile[]>;
  onChange: (key: string, changes: Partial<Job>) => void;
  onRemove: (key: string) => void;
  /** preview shows the first PNG result inline, which is the packed sheet. */
  preview?: boolean;
}): React.ReactElement {
  return (
    <>
      <ConversionQueue jobs={jobs} onChange={onChange} onRemove={onRemove} />

      {jobs.map((job) => {
        const out = files[job.key] ?? [];

        if (out.length === 0) {
          return null;
        }

        const sheet = preview ? out.find((f) => f.mime === "image/png") : undefined;

        return (
          <section key={job.key} className="mt-6">
            <h2 className="text-lg font-medium">Results</h2>
            <p className="mt-1 text-sm text-ink-faint">
              Every file has to be downloaded before the result expires.
            </p>

            {sheet ? (
              <div className="mt-3 overflow-hidden rounded-panel border border-edge bg-sunken p-3">
                <img
                  src={sheet.url}
                  alt="The packed sprite sheet"
                  className="mx-auto block max-h-96 w-auto max-w-full"
                  style={{ imageRendering: "pixelated" }}
                />
              </div>
            ) : null}

            <ul className="mt-3 grid gap-2 sm:grid-cols-2">
              {out.map((file) => (
                <li
                  key={file.url}
                  className="rounded-panel border border-edge bg-raised px-3 py-2 text-sm"
                >
                  <a
                    href={file.url}
                    download
                    className="flex items-center gap-2 truncate font-medium text-signal hover:underline"
                  >
                    <Icon name="download" className="size-4 shrink-0" />
                    {file.name}
                  </a>
                  <span className="tabular block text-xs text-ink-faint">
                    {formatBytes(file.size_bytes)}
                  </span>
                </li>
              ))}
            </ul>
          </section>
        );
      })}
    </>
  );
}
