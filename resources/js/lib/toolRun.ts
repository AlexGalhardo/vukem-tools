import { useCallback, useEffect, useRef, useState } from "react";

import { type Job } from "../components/ConversionQueue";
import { ApiError, final, show, uploads, watch, type Conversion, type Status } from "./conversions";
import { upload, UploadError } from "./upload";

/**
 * useToolRuns is the upload, submit, watch and download flow every conversion
 * tool has.
 *
 * It exists because that flow is about a hundred and fifty lines and is
 * identical for every tool: only the submit call and the option type differ.
 * The S4 media domain adds four pages at once, and four copies of this would
 * be four places for the "find the upload id again" workaround to drift, four
 * places to fix when the stream protocol changes, and four chances to forget
 * to tear a watcher down.
 *
 * The caller supplies one function, `submit`, which turns an upload id into a
 * conversion. Everything else — the rows, the progress, the error wording and
 * the signed download link — comes from the server, so a page cannot disagree
 * with the API about what happened.
 *
 * Tools/PixelsToSvg.tsx predates this and still has its own copy. It is not
 * refactored here because it belongs to another phase; adopting this would be
 * a small, separate change.
 */
export interface ToolRuns {
  /** jobs are the rows to render, newest first. */
  jobs: Job[];
  /** start uploads one file and submits it. */
  start: (file: File) => Promise<void>;
  /** patch updates one row, which ConversionQueue needs for its own controls. */
  patch: (key: string, changes: Partial<Job>) => void;
  /** remove drops a row and stops watching it. */
  remove: (key: string) => void;
}

/** nextKey gives each run its own row, so converting the same file twice is
 *  two rows rather than one that overwrites itself. */
let nextKey = 0;

/**
 * useToolRuns wires the flow for one tool.
 *
 * `submit` is called with the id of the stored upload and must return the
 * conversion the server created. `fields` names the option fields whose
 * validation errors should be surfaced on the row: a 422 carries them per
 * field, and showing "That is not a format this server can write" beats
 * showing "the conversion could not be started".
 */
export function useToolRuns(
  tusPath: string,
  submit: (fileID: string) => Promise<Conversion>,
  fields: string[] = [],
): ToolRuns {
  const [jobs, setJobs] = useState<Job[]>([]);

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
  }, []);

  /** settle fetches the signed download link once a conversion has finished.
   *  The link can only be signed by the server, and it is signed when asked,
   *  so it is never older than the request. */
  const settle = useCallback(
    async (key: string, id: string, status: Status) => {
      if (status !== "finished") {
        return;
      }

      try {
        const response = await fetch(`/api/v1/conversions/${encodeURIComponent(id)}/result`, {
          headers: { Accept: "application/json" },
          credentials: "same-origin",
        });

        if (response.ok) {
          const body = (await response.json()) as { data: { url: string }[] };
          patch(key, { download: body.data[0]?.url });
        }
      } catch {
        // The row still says "done" and a reload will fetch the link. A
        // failure here must not claim the conversion failed.
      }
    },
    [patch],
  );

  /** follow subscribes to a conversion and keeps its row in step. */
  const follow = useCallback(
    (key: string, conversion: Conversion) => {
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

          if (!final(event.status)) {
            return;
          }

          // The stream carries progress, not sizes: re-read the conversion so
          // the before and after figures are the server's.
          void show(conversion.id)
            .then((fresh) => {
              patch(key, {
                conversion: fresh,
                stage: fresh.status,
                error: fresh.error_message || undefined,
              });

              return settle(key, fresh.id, fresh.status);
            })
            .catch(() => undefined);
        },
        (err) => patch(key, { error: err.message }),
      );

      watchers.current.set(key, stop);
    },
    [patch, settle],
  );

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

      if (!(await send(file, tusPath, controller, key, patch))) {
        return;
      }

      patch(key, { stage: "submitting" });

      try {
        const stored = await findUpload(file);

        if (!stored) {
          patch(key, {
            stage: "error",
            error: "The upload was stored but could not be found again.",
          });

          return;
        }

        const conversion = await submit(stored);

        patch(key, { stage: conversion.status, conversion });
        follow(key, conversion);
      } catch (err) {
        patch(key, { stage: "error", error: startFailure(err, fields) });
      }
    },
    [fields, follow, patch, submit, tusPath],
  );

  return { jobs, start, patch, remove };
}

/** send uploads the file and reports whether it got there. */
async function send(
  file: File,
  tusPath: string,
  controller: AbortController,
  key: string,
  patch: (key: string, changes: Partial<Job>) => void,
): Promise<boolean> {
  try {
    await upload({
      endpoint: tusPath,
      file,
      signal: controller.signal,
      onProgress: ({ sent }) => patch(key, { sent }),
    });

    return true;
  } catch (err) {
    if (err instanceof DOMException && err.name === "AbortError") {
      patch(key, { stage: "cancelled" });

      return false;
    }

    patch(key, {
      stage: "error",
      error:
        err instanceof UploadError
          ? err.message
          : "The upload did not finish. Check your connection and try again.",
    });

    return false;
  }
}

/**
 * findUpload reads back the id of the upload just sent.
 *
 * The tus protocol has no response body, so the id has to be found: the
 * newest unclaimed upload whose size and name stem match what was sent. The
 * listing is newest first, and the server sanitises the name and forces the
 * extension to the type it detected, which is why the stem is compared rather
 * than the whole name.
 */
async function findUpload(file: File): Promise<string | undefined> {
  const stem = stemOf(file.name);

  const mine = (await uploads()).data.find(
    (candidate) => candidate.size_bytes === file.size && stemOf(candidate.name) === stem,
  );

  return mine?.id;
}

/** stemOf drops a filename's extension. */
function stemOf(name: string): string {
  return name.replace(/\.[^.]+$/, "");
}

/**
 * startFailure is the sentence to show when a submission is refused.
 *
 * A 422 carries the message per field, and the field message is the useful
 * one: "This server cannot write avif. It can write: jpeg, png, …" tells
 * somebody what to do, where "the conversion could not be started" does not.
 * The fields are tried in the order the caller listed them, so a page names
 * its own most-likely-wrong option first.
 */
function startFailure(err: unknown, fields: string[]): string {
  if (!(err instanceof ApiError)) {
    return "The conversion could not be started.";
  }

  for (const field of ["file_id", ...fields]) {
    const message = err.first(field);

    if (message) {
      return message;
    }
  }

  return err.message;
}
