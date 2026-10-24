import { Head } from "@inertiajs/react";
import { useCallback, useEffect, useRef, useState } from "react";

import ConversionQueue, { type Job } from "../../components/ConversionQueue";
import DropZone from "../../components/DropZone";
import Icon from "../../components/Icon";
import {
  ApiError,
  final,
  formatBytes,
  show,
  uploads,
  watch,
  type Conversion,
} from "../../lib/conversions";
import {
  forms,
  optionsFrom,
  results,
  submitMany,
  submitOne,
  type Field,
  type ResultFile,
  type ToolForm,
} from "../../lib/toolForm";
import { upload, UploadError } from "../../lib/upload";

interface Props {
  tool: { name: string; title: string; summary: string; in: string[]; out: string[] };
  maxBytes: number;
  tusPath: string;
}

/** Each run gets its own key so a repeat of the same file is a new row. */
let nextKey = 0;

/** A staged file: uploaded and waiting to be converted. */
interface Staged {
  key: string;
  name: string;
  sizeBytes: number;
  /** id is the stored upload's id, set once the upload finishes. */
  id?: string;
  error?: string;
}

/**
 * The page of every tool whose interface is a file picker, a field list and a
 * download.
 *
 * One page for twenty tools, driven by the form description in lib/toolForm.ts.
 * The alternative was twenty pages that differed in their fields and agreed in
 * everything else — the upload, the submit, the progress stream, the
 * multi-file result list, the way a server-side refusal is shown — and the
 * parts they would have agreed on are exactly the parts worth having one of.
 * A tool that needs a genuinely different interaction, rather than different
 * fields, gets its own page: the sprite tools draw a grid over the sheet,
 * which is not something a field list can express.
 *
 * Nothing here decides anything. Progress, status, every failure message and
 * every download link come from the server, so the page cannot disagree with
 * the API about what happened.
 */
export default function ToolForm({ tool, maxBytes, tusPath }: Props): React.ReactElement {
  // An unknown tool falls back to one file and no options, which renders a
  // usable page rather than a crash. It cannot happen: app/controllers/pages.go
  // answers a 500 for a live tool with no page, and a tool with a page always
  // has a form.
  const form: ToolForm = forms[tool.name] ?? { files: "one", accept: [], fields: [] };

  const [values, setValues] = useState<Record<string, string | boolean>>({});
  const [staged, setStaged] = useState<Staged[]>([]);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();
  const [downloads, setDownloads] = useState<Record<string, ResultFile[]>>({});

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

  /** follow keeps a row in step with its conversion and collects its results.
   *
   *  The results are collected as a list rather than a single link, because a
   *  PDF tool can produce many files: a split into twenty parts, an image per
   *  page. Each one has its own expiry and each one has to be downloaded. */
  const follow = useCallback(
    (key: string, conversion: Conversion) => {
      const settle = async (status: Conversion["status"]) => {
        if (status !== "finished") {
          return;
        }

        try {
          const files = await results(conversion.id);

          setDownloads((current) => ({ ...current, [key]: files }));
          patch(key, { download: files[0]?.url });
        } catch {
          // The row still says "done" and a reload will fetch the links; a
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
            // The server's own account of what it did, where it had one: a
            // compression that could not beat the file says so here, and that
            // sentence is the whole answer for that case. Only a progress
            // event's message is kept — the final event says "Done.", which
            // would otherwise overwrite it.
            ...(event.status === "running" && event.message ? { note: event.message } : {}),
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
                  note: fresh.note || undefined,
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

  /** stage uploads one file and holds its id until the conversion is started.
   *
   *  Uploading and converting are separate steps here, unlike the single-file
   *  tools: a merge needs every file before it can start, and the order they
   *  were added is the order of the pages. */
  const stage = useCallback(
    async (file: File) => {
      const key = `file-${nextKey++}`;

      setStaged((current) => [...current, { key, name: file.name, sizeBytes: file.size }]);
      setProblem(undefined);

      try {
        await upload({ endpoint: tusPath, file });
      } catch (err) {
        setStaged((current) =>
          current.map((item) =>
            item.key === key
              ? {
                  ...item,
                  error:
                    err instanceof UploadError
                      ? err.message
                      : "The upload did not finish. Check your connection and try again.",
                }
              : item,
          ),
        );

        return;
      }

      // The tus protocol has no response body, so the stored id is read back:
      // the newest unclaimed upload whose size and name stem match what was
      // just sent. The server sanitises the name and forces the extension to
      // the detected type, which is why the stem is compared.
      const stem = file.name.replace(/\.[^.]+$/, "");
      const mine = (await uploads()).data.find(
        (candidate) =>
          candidate.size_bytes === file.size && candidate.name.replace(/\.[^.]+$/, "") === stem,
      );

      setStaged((current) =>
        current.map((item) =>
          item.key === key
            ? {
                ...item,
                id: mine?.id,
                error: mine ? undefined : "The upload was stored but could not be found again.",
              }
            : item,
        ),
      );
    },
    [tusPath],
  );

  const unstage = useCallback((key: string) => {
    setStaged((current) => current.filter((item) => item.key !== key));
  }, []);

  /** convert submits the staged uploads. */
  const convert = useCallback(async () => {
    const ready = staged.filter((item) => item.id);

    if (ready.length === 0) {
      setProblem("Add a file first.");

      return;
    }

    setBusy(true);
    setProblem(undefined);

    const key = `job-${nextKey++}`;
    const options = optionsFrom(form, values);

    setJobs((current) => [
      {
        key,
        name: ready.length === 1 ? ready[0].name : `${ready.length} files`,
        sizeIn: ready.reduce((total, item) => total + item.sizeBytes, 0),
        stage: "submitting",
      },
      ...current,
    ]);

    try {
      const ids = ready.map((item) => item.id as string);
      const conversion =
        form.files === "many"
          ? await submitMany(tool.name, ids, options)
          : await submitOne(tool.name, ids[0], options);

      patch(key, { stage: conversion.status, conversion });
      follow(key, conversion);

      // An upload belongs to exactly one conversion, so the staged list is
      // cleared: submitting the same ids again would be refused, and leaving
      // them on screen would invite it.
      setStaged([]);
    } catch (err) {
      const message =
        err instanceof ApiError
          ? (err.first("file_id") ?? err.first("file_ids") ?? firstOptionError(err, form) ?? err.message)
          : "The conversion could not be started.";

      patch(key, { stage: "error", error: message });
      setProblem(message);
    } finally {
      setBusy(false);
    }
  }, [follow, form, patch, staged, tool.name, values]);

  const multiple = form.files === "many";

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

      {form.note ? (
        <p className="mt-6 max-w-[70ch] rounded-panel border border-edge bg-raised px-4 py-3 text-sm text-ink-soft">
          {form.note}
        </p>
      ) : null}

      <div className="mt-6">
        <DropZone
          accept={form.accept}
          maxBytes={maxBytes}
          disabled={busy}
          onFiles={(files) => files.forEach((file) => void stage(file))}
        />
      </div>

      {staged.length > 0 ? (
        <StagedList items={staged} multiple={multiple} onRemove={unstage} />
      ) : null}

      {form.fields.length > 0 ? <Options form={form} values={values} onChange={setValues} /> : null}

      <div className="mt-6 flex items-center gap-3">
        <button
          type="button"
          disabled={busy || staged.length === 0}
          onClick={() => void convert()}
          className="rounded-field bg-signal px-4 py-2 font-medium text-raised transition-colors duration-100
            hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {busy ? "Starting…" : tool.title}
        </button>

        {problem ? (
          <p role="alert" className="text-sm text-bad">
            {problem}
          </p>
        ) : null}
      </div>

      <ConversionQueue jobs={jobs} onChange={patch} onRemove={remove} />

      <Results downloads={downloads} jobs={jobs} />
    </>
  );
}

/** firstOptionError finds the server's message for whichever option it
 *  rejected, so a 422 says which field is wrong rather than "that did not
 *  work". */
function firstOptionError(err: ApiError, form: ToolForm): string | undefined {
  for (const field of form.fields) {
    const message = err.first(`options.${field.name}`);

    if (message) {
      return `${field.label}: ${message}`;
    }
  }

  return err.first("options");
}

/** StagedList shows the uploads waiting to be converted, in order. */
function StagedList({
  items,
  multiple,
  onRemove,
}: {
  items: Staged[];
  multiple: boolean;
  onRemove: (key: string) => void;
}): React.ReactElement {
  return (
    <ol className="mt-4 space-y-2">
      {items.map((item, index) => (
        <li
          key={item.key}
          className="flex items-center gap-3 rounded-panel border border-edge bg-raised px-3 py-2 text-sm"
        >
          {multiple ? <span className="tabular w-5 text-ink-faint">{index + 1}</span> : null}

          <span className="min-w-0 flex-1 truncate">{item.name}</span>

          <span className="tabular text-xs text-ink-faint">{formatBytes(item.sizeBytes)}</span>

          {item.error ? (
            <span className="text-xs text-bad">{item.error}</span>
          ) : item.id ? (
            <span className="text-xs text-ink-faint">ready</span>
          ) : (
            <span className="text-xs text-ink-faint">uploading…</span>
          )}

          <button
            type="button"
            className="text-ink-faint hover:text-ink"
            aria-label={`Remove ${item.name}`}
            onClick={() => onRemove(item.key)}
          >
            <Icon name="close" className="size-4" />
          </button>
        </li>
      ))}
    </ol>
  );
}

/** Results lists every file a finished conversion produced.
 *
 *  Separate from the queue because the queue has room for one link and a tool
 *  can produce sixty-four. The expiry is repeated here on purpose: a user with
 *  sixty-four links has five minutes for all of them. */
function Results({
  downloads,
  jobs,
}: {
  downloads: Record<string, ResultFile[]>;
  jobs: Job[];
}): React.ReactElement | null {
  const rows = jobs
    .map((job) => ({ job, files: downloads[job.key] ?? [] }))
    .filter((row) => row.files.length > 1);

  if (rows.length === 0) {
    return null;
  }

  return (
    <section className="mt-8">
      <h2 className="text-lg font-medium">Results</h2>
      <p className="mt-1 text-sm text-ink-faint">
        Every file has to be downloaded before the result expires.
      </p>

      {rows.map(({ job, files }) => (
        <ul key={job.key} className="mt-3 grid gap-2 sm:grid-cols-2">
          {files.map((file) => (
            <li key={file.url} className="rounded-panel border border-edge bg-raised px-3 py-2 text-sm">
              <a href={file.url} className="block truncate font-medium text-signal hover:underline" download>
                {file.name}
              </a>
              <span className="tabular block text-xs text-ink-faint">{formatBytes(file.size_bytes)}</span>
            </li>
          ))}
        </ul>
      ))}
    </section>
  );
}

/** Options is the tool's own settings, generated from its field list.
 *
 *  The panel starts collapsed unless a field is required: the common case is
 *  dropping a file and taking the defaults, and a tool that cannot run
 *  without an option should not hide it. */
function Options({
  form,
  values,
  onChange,
}: {
  form: ToolForm;
  values: Record<string, string | boolean>;
  onChange: (values: Record<string, string | boolean>) => void;
}): React.ReactElement {
  const hasRequired = form.fields.some((field) => field.required);

  const set = (name: string, value: string | boolean) => onChange({ ...values, [name]: value });

  return (
    <details className="mt-6 rounded-panel border border-edge bg-raised" open={hasRequired}>
      <summary className="flex cursor-pointer items-center gap-2 px-4 py-3 text-sm font-medium">
        <Icon name="sliders" className="size-4 text-ink-faint" />
        Options
        <span className="text-ink-faint">
          {hasRequired ? "— this tool needs at least one of these" : "— every one has a sensible default"}
        </span>
      </summary>

      <div className="grid gap-5 border-t border-edge px-4 py-4 sm:grid-cols-2">
        {form.fields.map((field) => (
          <FieldInput key={field.name} field={field} value={values[field.name]} onChange={set} />
        ))}
      </div>
    </details>
  );
}

/** controlClass is the shared look of a text input and a select, so the
 *  generated form does not look like a different form from the rest of the
 *  site. */
const controlClass =
  "mt-1 block w-full rounded-field border border-edge bg-surface px-3 py-2 " +
  "focus:border-signal focus:outline-none";

/** FieldInput renders one option. */
function FieldInput({
  field,
  value,
  onChange,
}: {
  field: Field;
  value: string | boolean | undefined;
  onChange: (name: string, value: string | boolean) => void;
}): React.ReactElement {
  const id = `option-${field.name}`;

  if (field.kind === "toggle") {
    return (
      <div>
        <label className="flex items-start gap-2 text-sm" htmlFor={id}>
          <input
            id={id}
            type="checkbox"
            className="mt-1"
            checked={value === true}
            onChange={(event) => onChange(field.name, event.target.checked)}
          />
          <span>
            <span className="font-medium">{field.label}</span>
            {field.help ? <span className="block text-ink-faint">{field.help}</span> : null}
          </span>
        </label>
      </div>
    );
  }

  return (
    <div>
      <label className="label" htmlFor={id}>
        {field.label}
        {field.required ? <span className="text-bad" aria-hidden="true"> *</span> : null}
      </label>

      {field.kind === "choice" ? (
        <select
          id={id}
          className={controlClass}
          value={typeof value === "string" ? value : ""}
          onChange={(event) => onChange(field.name, event.target.value)}
        >
          <option value="">default</option>
          {field.choices?.map((choice) => (
            <option key={choice} value={choice}>
              {choice}
            </option>
          ))}
        </select>
      ) : field.kind === "textarea" ? (
        <textarea
          id={id}
          className={`${controlClass} font-mono text-sm`}
          rows={6}
          placeholder={field.placeholder}
          spellCheck={false}
          value={typeof value === "string" ? value : ""}
          onChange={(event) => onChange(field.name, event.target.value)}
        />
      ) : (
        <input
          id={id}
          className={controlClass}
          type={field.kind === "password" ? "password" : field.kind === "number" ? "number" : "text"}
          inputMode={field.kind === "number" ? "decimal" : undefined}
          min={field.min}
          max={field.max}
          step={field.step}
          placeholder={field.placeholder}
          required={field.required}
          autoComplete={field.kind === "password" ? "new-password" : "off"}
          value={typeof value === "string" ? value : ""}
          onChange={(event) => onChange(field.name, event.target.value)}
        />
      )}

      {field.help ? <p className="mt-1 text-xs text-ink-faint">{field.help}</p> : null}
    </div>
  );
}
