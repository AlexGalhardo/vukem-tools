import { Head } from "@inertiajs/react";
import { useCallback, useEffect, useRef, useState } from "react";

import ConversionQueue, { type Job } from "../../components/ConversionQueue";
import DropZone from "../../components/DropZone";
import Icon from "../../components/Icon";
import {
  ApiError,
  final,
  show,
  submitPixelSVG,
  uploads,
  watch,
  type Conversion,
  type PixelSVGOptions,
  type Status,
} from "../../lib/conversions";
import { upload, UploadError } from "../../lib/upload";

interface Props {
  tool: { name: string; title: string; summary: string; in: string[]; out: string[] };
  maxBytes: number;
  tusPath: string;
  modes: string[];
  alphas: string[];
}

/** Each run gets its own key so a repeat of the same file is a new row. */
let nextKey = 0;

/**
 * The pixel-art to SVG tool.
 *
 * The shape of the flow is upload, submit, watch, download — and every step is
 * the server's to decide. The page holds the list of rows and nothing else:
 * progress, status and the failure message all come from the conversion the
 * server reports, so the page cannot disagree with the API about what
 * happened.
 */
export default function PixelsToSvg({
  tool,
  maxBytes,
  tusPath,
  modes,
  alphas,
}: Props): React.ReactElement {
  const [options, setOptions] = useState<PixelSVGOptions>({ mode: "rect", alpha: "opacity" });
  const [jobs, setJobs] = useState<Job[]>([]);

  // The watchers are kept out of state: they are side effects, and putting
  // them in state would re-subscribe on every render.
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
  }, []);

  /** follow subscribes to a conversion and keeps the row in step with it. */
  const follow = useCallback(
    (key: string, conversion: Conversion) => {
      const settle = async (status: Status) => {
        if (status !== "finished") {
          return;
        }

        // The link can only be signed by the server, and it is signed when
        // asked so it is never older than the request.
        try {
          const result = await fetch(`/api/v1/conversions/${conversion.id}/result`, {
            headers: { Accept: "application/json" },
            credentials: "same-origin",
          });

          if (result.ok) {
            const body = (await result.json()) as { data: { url: string }[] };
            patch(key, { download: body.data[0]?.url });
          }
        } catch {
          // The row still says "done" and a reload will fetch the link; a
          // failure here must not claim the conversion failed.
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
            error: event.status === "error" ? event.message : undefined,
          });

          if (final(event.status)) {
            // The stream carries progress, not sizes: re-read the conversion
            // so the before/after figures are the server's.
            void show(conversion.id)
              .then((fresh) => {
                patch(key, { conversion: fresh, stage: fresh.status, error: fresh.error_message || undefined });

                return settle(fresh.status);
              })
              .catch(() => undefined);
          }
        },
        (err) => patch(key, { error: err.message }),
      );

      watchers.current.set(key, stop);
    },
    [patch],
  );

  /** start uploads one file and submits it. */
  const start = useCallback(
    async (file: File) => {
      const key = `job-${nextKey++}`;
      const controller = new AbortController();

      setJobs((current) => [
        {
          key,
          name: file.name,
          sizeIn: file.size,
          stage: "uploading",
          sent: 0,
          cancelUpload: () => controller.abort(),
        },
        ...current,
      ]);

      try {
        await upload({
          endpoint: tusPath,
          file,
          signal: controller.signal,
          onProgress: ({ sent }) => patch(key, { sent }),
        });
      } catch (err) {
        if (err instanceof DOMException && err.name === "AbortError") {
          patch(key, { stage: "cancelled" });

          return;
        }

        patch(key, {
          stage: "error",
          error:
            err instanceof UploadError
              ? err.message
              : "The upload did not finish. Check your connection and try again.",
        });

        return;
      }

      patch(key, { stage: "submitting" });

      try {
        // The tus protocol has no response body, so the stored id is read
        // back: the newest unclaimed upload whose size and name stem match
        // what was just sent. The listing is newest first, and the server
        // sanitises the name and forces the extension to the detected type,
        // which is why the stem is compared rather than the whole name.
        const stem = file.name.replace(/\.[^.]+$/, "");
        const mine = (await uploads()).data.find(
          (candidate) =>
            candidate.size_bytes === file.size && candidate.name.replace(/\.[^.]+$/, "") === stem,
        );

        if (!mine) {
          patch(key, { stage: "error", error: "The upload was stored but could not be found again." });

          return;
        }

        const conversion = await submitPixelSVG(mine.id, options);

        patch(key, { stage: conversion.status, conversion });
        follow(key, conversion);
      } catch (err) {
        patch(key, {
          stage: "error",
          error:
            err instanceof ApiError
              ? (err.first("file_id") ?? err.first("options.mode") ?? err.message)
              : "The conversion could not be started.",
        });
      }
    },
    [follow, options, patch, tusPath],
  );

  return (
    <>
      <Head title={tool.title} />

      <div className="max-w-[70ch]">
        <h1 className="text-3xl font-semibold tracking-tight">{tool.title}</h1>
        <p className="mt-3 text-ink-soft">{tool.summary}</p>
        <p className="tabular mt-2 text-xs text-ink-faint">
          {tool.in.join(" · ")} <span aria-hidden="true">&rarr;</span>
          <span className="sr-only">to</span> {tool.out.join(" · ")}
        </p>
      </div>

      <div className="mt-8">
        <DropZone
          accept={["image/png", "image/gif", "image/bmp", "image/jpeg"]}
          maxBytes={maxBytes}
          onFiles={(files) => files.forEach((file) => void start(file))}
        />
      </div>

      <Options options={options} onChange={setOptions} modes={modes} alphas={alphas} />

      <ConversionQueue jobs={jobs} onChange={patch} onRemove={remove} />
    </>
  );
}

/** Options is the tool's own settings. Every one has a server-side default, so
 *  nothing here is required and the panel starts collapsed: the common case is
 *  dropping a sprite and getting an SVG. */
function Options({
  options,
  onChange,
  modes,
  alphas,
}: {
  options: PixelSVGOptions;
  onChange: (options: PixelSVGOptions) => void;
  modes: string[];
  alphas: string[];
}): React.ReactElement {
  const describe: Record<string, string> = {
    square: "One shape per pixel. Largest file, exact.",
    rect: "Merged rectangles. Smallest and fastest.",
    contour: "Traced outlines. Fewest shapes, smooth edges.",
    opacity: "Keep partial transparency.",
    drop: "Remove anything not fully opaque.",
  };

  return (
    <details className="mt-6 rounded-panel border border-edge bg-raised">
      <summary className="flex cursor-pointer items-center gap-2 px-4 py-3 text-sm font-medium">
        <Icon name="scale" className="size-4 text-ink-faint" />
        Options
        <span className="text-ink-faint">— every one has a sensible default</span>
      </summary>

      <div className="grid gap-6 border-t border-edge px-4 py-4 sm:grid-cols-2">
        <fieldset>
          <legend className="label">Geometry</legend>
          <div className="mt-2 space-y-2">
            {modes.map((mode) => (
              <label key={mode} className="flex items-start gap-2 text-sm">
                <input
                  type="radio"
                  name="mode"
                  value={mode}
                  checked={options.mode === mode}
                  onChange={() => onChange({ ...options, mode: mode as PixelSVGOptions["mode"] })}
                  className="mt-1"
                />
                <span>
                  <span className="font-medium">{mode}</span>
                  <span className="block text-ink-faint">{describe[mode]}</span>
                </span>
              </label>
            ))}
          </div>
        </fieldset>

        <fieldset>
          <legend className="label">Transparency</legend>
          <div className="mt-2 space-y-2">
            {alphas.map((alpha) => (
              <label key={alpha} className="flex items-start gap-2 text-sm">
                <input
                  type="radio"
                  name="alpha"
                  value={alpha}
                  checked={options.alpha === alpha}
                  onChange={() => onChange({ ...options, alpha: alpha as PixelSVGOptions["alpha"] })}
                  className="mt-1"
                />
                <span>
                  <span className="font-medium">{alpha}</span>
                  <span className="block text-ink-faint">{describe[alpha]}</span>
                </span>
              </label>
            ))}
          </div>
        </fieldset>
      </div>
    </details>
  );
}
