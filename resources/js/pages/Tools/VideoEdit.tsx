import { Head } from "@inertiajs/react";
import { useCallback, useEffect, useRef, useState } from "react";

import ConversionQueue, { type Job } from "../../components/ConversionQueue";
import DropZone from "../../components/DropZone";
import Icon from "../../components/Icon";
import {
  ApiError,
  final,
  formatBytes,
  results,
  show,
  submitVideoClip,
  submitVideoCompress,
  uploads,
  watch,
  type ClipOptions,
  type Conversion,
  type ResultFile,
  type Status,
} from "../../lib/conversions";
import { upload, UploadError } from "../../lib/upload";

interface Props {
  tool: { name: string; title: string; summary: string; in: string[]; out: string[] };
  maxBytes: number;
  tusPath: string;
  /** mode picks which tool this page drives. The two share everything except
   *  the options panel and the submit call, so one component serves both
   *  rather than two that drift apart. */
  mode: "clip" | "compress";
  clipModes: string[];
  maxClipSeconds: number;
  maxOutputSeconds: number;
}

/** Each run gets its own key so a repeat of the same file is a new row. */
let nextKey = 0;

/**
 * The cut, crop and compress tools.
 *
 * The flow is upload, submit, watch, download, the same as every other tool
 * that takes a file. What is particular here is that the page deliberately
 * does *not* validate the interesting things: a crop is checked against the
 * real stream dimensions by ffprobe, and the start of a cut against the real
 * duration, so both refusals arrive on the conversion with the real numbers
 * in them. Guessing at those in the browser would mean guessing, and the only
 * thing worse than a refusal is a wrong one.
 */
export default function VideoEdit({
  tool,
  maxBytes,
  tusPath,
  mode,
  clipModes,
  maxClipSeconds,
  maxOutputSeconds,
}: Props): React.ReactElement {
  const [options, setOptions] = useState<ClipOptions>({ mode: "trim" });
  const [jobs, setJobs] = useState<Job[]>([]);
  const [extra, setExtra] = useState<Record<string, ResultFile[]>>({});

  // The watchers are side effects, not state: holding them in state would
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
    setExtra((current) => {
      const { [key]: _dropped, ...rest } = current;

      return rest;
    });
  }, []);

  /** follow subscribes to a conversion and keeps the row in step with it. */
  const follow = useCallback(
    (key: string, conversion: Conversion) => {
      const settle = async (status: Status) => {
        if (status !== "finished") {
          return;
        }

        try {
          const body = await results(conversion.id);

          // The queue row carries one link. A split produces many, so the
          // rest are listed below it rather than hidden.
          patch(key, { download: body.data[0]?.url });

          if (body.data.length > 1) {
            setExtra((current) => ({ ...current, [key]: body.data }));
          }
        } catch {
          // The row still says done and a reload fetches the links; a
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
            // so the before and after figures are the server's.
            void show(conversion.id)
              .then((fresh) => {
                patch(key, {
                  conversion: fresh,
                  stage: fresh.status,
                  error: fresh.error_message || undefined,
                });

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
        // what was just sent. The server sanitises the name and forces the
        // extension to the detected type, which is why the stem is compared
        // rather than the whole name.
        const stem = file.name.replace(/\.[^.]+$/, "");
        const mine = (await uploads()).data.find(
          (candidate) =>
            candidate.size_bytes === file.size && candidate.name.replace(/\.[^.]+$/, "") === stem,
        );

        if (!mine) {
          patch(key, {
            stage: "error",
            error: "The upload was stored but could not be found again.",
          });

          return;
        }

        const conversion =
          mode === "compress"
            ? await submitVideoCompress(mine.id)
            : await submitVideoClip(mine.id, options);

        patch(key, { stage: conversion.status, conversion });
        follow(key, conversion);
      } catch (err) {
        patch(key, {
          stage: "error",
          error:
            err instanceof ApiError
              ? (err.first("file_id") ??
                err.first("options.crop") ??
                err.first("options.mode") ??
                err.message)
              : "The conversion could not be started.",
        });
      }
    },
    [follow, mode, options, patch, tusPath],
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
          accept={[
            "video/mp4",
            "video/webm",
            "video/x-matroska",
            "video/quicktime",
            "video/x-msvideo",
            ...(mode === "compress"
              ? ["audio/mpeg", "audio/wav", "audio/flac", "audio/ogg", "audio/aac"]
              : []),
          ]}
          maxBytes={maxBytes}
          onFiles={(files) => files.forEach((file) => void start(file))}
        />
      </div>

      {mode === "clip" ? (
        <ClipSettings
          options={options}
          onChange={setOptions}
          modes={clipModes}
          maxClipSeconds={maxClipSeconds}
          maxOutputSeconds={maxOutputSeconds}
        />
      ) : (
        <p className="mt-6 max-w-[70ch] text-sm text-ink-soft">
          There is nothing to choose. The file is re-encoded and you get back whichever is smaller,
          the result or the original — and if the original wins, the conversion still succeeds and
          says so, because a file that is already optimal is not a failure.
        </p>
      )}

      <ConversionQueue jobs={jobs} onChange={patch} onRemove={remove} />

      {Object.entries(extra).map(([key, files]) => (
        <ClipList key={key} files={files} />
      ))}
    </>
  );
}

/** ClipList lists every output of a conversion that produced more than one. */
function ClipList({ files }: { files: ResultFile[] }): React.ReactElement {
  return (
    <section className="mt-6">
      <h2 className="label border-b border-edge pb-2">
        {files.length} file{files.length === 1 ? "" : "s"}
      </h2>

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
                "inline-flex shrink-0 items-center gap-1.5 rounded-field px-2.5 py-1.5 text-sm " +
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
    </section>
  );
}

const numberInput =
  "mt-1 w-full rounded-field border border-edge bg-raised px-3 py-1.5 text-sm text-ink " +
  "transition-colors duration-100 focus:border-signal focus:outline-none";

/** ClipSettings is the cut's own settings.
 *
 *  Every one has a server-side default, so nothing here is required: the
 *  common case is dropping a recording and taking the first minute. The crop
 *  is four numbers rather than a dragged rectangle because the dimensions are
 *  not known until ffprobe has read the file, and a rectangle dragged over a
 *  guess is worse than four numbers the server checks. */
function ClipSettings({
  options,
  onChange,
  modes,
  maxClipSeconds,
  maxOutputSeconds,
}: {
  options: ClipOptions;
  onChange: (options: ClipOptions) => void;
  modes: string[];
  maxClipSeconds: number;
  maxOutputSeconds: number;
}): React.ReactElement {
  const describe: Record<string, string> = {
    trim: "Keep one range, optionally cropped.",
    split: `Cut the whole video into clips of up to ${maxClipSeconds} seconds.`,
  };

  const crop = options.crop ?? { x: 0, y: 0, width: 0, height: 0 };
  const cropping = options.crop !== undefined;

  const setCrop = (changes: Partial<typeof crop>) =>
    onChange({ ...options, crop: { ...crop, ...changes } });

  return (
    <section className="mt-6 rounded-panel border border-edge bg-raised">
      <fieldset className="px-4 py-4">
        <legend className="label">Mode</legend>

        <div className="mt-2 space-y-2">
          {modes.map((name) => (
            <label key={name} className="flex cursor-pointer items-start gap-2 text-sm">
              <input
                type="radio"
                name="mode"
                value={name}
                checked={(options.mode ?? "trim") === name}
                onChange={() => onChange({ ...options, mode: name as ClipOptions["mode"] })}
                className="mt-0.5"
              />
              <span>
                <span className="block font-medium capitalize">{name}</span>
                <span className="block text-ink-faint">{describe[name] ?? ""}</span>
              </span>
            </label>
          ))}
        </div>
      </fieldset>

      {(options.mode ?? "trim") === "trim" ? (
        <div className="grid gap-6 border-t border-edge px-4 py-4 sm:grid-cols-2">
          <label className="block text-sm">
            <span className="font-medium">Start, seconds</span>
            <input
              type="number"
              min={0}
              step="0.1"
              className={numberInput}
              value={options.start_seconds ?? 0}
              onChange={(event) =>
                onChange({ ...options, start_seconds: Number(event.target.value) })
              }
            />
          </label>

          <label className="block text-sm">
            <span className="font-medium">Length, seconds</span>
            <input
              type="number"
              min={0}
              max={maxOutputSeconds > 0 ? maxOutputSeconds : undefined}
              step="0.1"
              className={numberInput}
              value={options.duration_seconds ?? 0}
              onChange={(event) =>
                onChange({ ...options, duration_seconds: Number(event.target.value) })
              }
            />
            <span className="mt-1 block text-xs text-ink-faint">
              0 means to the end
              {maxOutputSeconds > 0 && `, up to ${maxOutputSeconds} seconds`}.
            </span>
          </label>

          <label className="flex cursor-pointer items-start gap-2 text-sm sm:col-span-2">
            <input
              type="checkbox"
              checked={options.frame_accurate ?? false}
              onChange={(event) => onChange({ ...options, frame_accurate: event.target.checked })}
              className="mt-0.5"
            />
            <span>
              <span className="block font-medium">Cut exactly</span>
              <span className="block text-ink-faint">
                Re-encodes, which is an order of magnitude slower. Off, the cut lands on the
                nearest keyframe and the streams are copied.
              </span>
            </span>
          </label>

          <label className="flex cursor-pointer items-start gap-2 text-sm sm:col-span-2">
            <input
              type="checkbox"
              checked={cropping}
              onChange={(event) =>
                onChange({
                  ...options,
                  crop: event.target.checked ? { x: 0, y: 0, width: 640, height: 360 } : undefined,
                })
              }
              className="mt-0.5"
            />
            <span>
              <span className="block font-medium">Crop the frame</span>
              <span className="block text-ink-faint">
                Checked against the video&rsquo;s real dimensions, so an area outside it is
                refused with those dimensions named.
              </span>
            </span>
          </label>

          {cropping && (
            <div className="grid grid-cols-2 gap-4 sm:col-span-2 sm:grid-cols-4">
              {(["x", "y", "width", "height"] as const).map((field) => (
                <label key={field} className="block text-sm">
                  <span className="font-medium capitalize">{field}</span>
                  <input
                    type="number"
                    min={0}
                    className={numberInput}
                    value={crop[field]}
                    onChange={(event) => setCrop({ [field]: Number(event.target.value) })}
                  />
                </label>
              ))}
            </div>
          )}
        </div>
      ) : (
        <div className="border-t border-edge px-4 py-4">
          <label className="block text-sm">
            <span className="font-medium">Clip length, seconds</span>
            <input
              type="number"
              min={1}
              max={maxClipSeconds}
              className={numberInput}
              value={options.clip_seconds ?? maxClipSeconds}
              onChange={(event) =>
                onChange({ ...options, clip_seconds: Number(event.target.value) })
              }
            />
            <span className="mt-1 block text-xs text-ink-faint">
              1 to {maxClipSeconds}. Clips are cut on keyframes where that fits the length, and
              re-encoded where it does not, so none is ever longer than you asked for.
            </span>
          </label>
        </div>
      )}
    </section>
  );
}
