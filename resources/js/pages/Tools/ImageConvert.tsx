import { Head } from "@inertiajs/react";
import { useCallback, useEffect, useState } from "react";

import ConversionQueue from "../../components/ConversionQueue";
import DropZone from "../../components/DropZone";
import Icon from "../../components/Icon";
import {
  imageCapabilities,
  submitImageConvert,
  type ImageConvertOptions,
  type MediaCapabilities,
} from "../../lib/conversions";
import { useToolRuns } from "../../lib/toolRun";

interface Props {
  tool: { name: string; title: string; summary: string; in: string[]; out: string[] };
  maxBytes: number;
  tusPath: string;
  resizeModes: string[];
  flips: string[];
  rotations: number[];
}

/**
 * The image conversion tool.
 *
 * The formats come from the server, not from this file, and that is the whole
 * point of the capability endpoint: Debian's libvips has `heifsave` while
 * libheif ships no encoder, so AVIF is readable and not writable. A list
 * written here would offer a conversion the server refuses, and the person
 * would read the refusal as something they did wrong.
 */
export default function ImageConvert({
  tool,
  maxBytes,
  tusPath,
  resizeModes,
  flips,
  rotations,
}: Props): React.ReactElement {
  const [capabilities, setCapabilities] = useState<MediaCapabilities | null>(null);
  const [failed, setFailed] = useState(false);
  const [options, setOptions] = useState<ImageConvertOptions>({ to: "" });

  useEffect(() => {
    let live = true;

    void imageCapabilities()
      .then((caps) => {
        if (!live) {
          return;
        }

        setCapabilities(caps);
        // The first writable format is the default, so the form is usable
        // without a choice. There is no sensible fallback if the list is
        // empty: that means no image engine, and nothing can be converted.
        setOptions((current) => (current.to ? current : { to: caps.writable[0] ?? "" }));
      })
      .catch(() => {
        if (live) {
          setFailed(true);
        }
      });

    return () => {
      live = false;
    };
  }, []);

  const submit = useCallback(
    (fileID: string) => submitImageConvert(fileID, options),
    [options],
  );

  const { jobs, start, patch, remove } = useToolRuns(tusPath, submit, [
    "options.to",
    "options.resize",
    "options.width",
    "options.height",
    "options.crop_width",
  ]);

  const accept = capabilities?.readable ?? [];

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

      {failed ? (
        <p className="mt-8 rounded-panel border border-edge bg-raised px-4 py-3 text-sm text-ink-soft">
          The list of formats could not be read, so this tool cannot say what it would produce.
          Reload the page to try again.
        </p>
      ) : null}

      <div className="mt-8">
        <DropZone
          accept={accept}
          maxBytes={maxBytes}
          onFiles={(files) => files.forEach((file) => void start(file))}
        />
      </div>

      <Options
        options={options}
        onChange={setOptions}
        capabilities={capabilities}
        resizeModes={resizeModes}
        flips={flips}
        rotations={rotations}
      />

      <ConversionQueue jobs={jobs} onChange={patch} onRemove={remove} />
    </>
  );
}

/** Options is the tool's settings. The output format is the only one without a
 *  server-side default, so it is the only one shown unfolded. */
function Options({
  options,
  onChange,
  capabilities,
  resizeModes,
  flips,
  rotations,
}: {
  options: ImageConvertOptions;
  onChange: (options: ImageConvertOptions) => void;
  capabilities: MediaCapabilities | null;
  resizeModes: string[];
  flips: string[];
  rotations: number[];
}): React.ReactElement {
  const writable = capabilities?.formats.filter((format) => format.write) ?? [];
  const lossy = new Set(["jpeg", "webp", "avif", "heic", "jxl", "tiff"]);

  const describeResize: Record<string, string> = {
    fit: "Fits inside the box. Nothing is cropped.",
    fill: "Covers the box and crops the overflow.",
    exact: "Stretches to the box. Changes the proportions.",
  };

  return (
    <div className="mt-6 space-y-4">
      <div className="rounded-panel border border-edge bg-raised px-4 py-4">
        <label className="label" htmlFor="image-to">
          Output format
        </label>
        <select
          id="image-to"
          className="input mt-2"
          value={options.to}
          onChange={(event) => onChange({ ...options, to: event.target.value })}
        >
          {writable.length === 0 ? <option value="">No image engine is available</option> : null}
          {writable.map((format) => (
            <option key={format.name} value={format.name}>
              {format.label}
            </option>
          ))}
        </select>
        <p className="mt-2 text-xs text-ink-faint">
          Only the formats this server can actually produce are listed.
        </p>
      </div>

      <details className="rounded-panel border border-edge bg-raised">
        <summary className="flex cursor-pointer items-center gap-2 px-4 py-3 text-sm font-medium">
          <Icon name="sliders" className="size-4 text-ink-faint" />
          Size, rotation and quality
          <span className="text-ink-faint">— every one has a sensible default</span>
        </summary>

        <div className="grid gap-6 border-t border-edge px-4 py-4 sm:grid-cols-2">
          <fieldset>
            <legend className="label">Resize</legend>
            <div className="mt-2 space-y-2">
              <label className="flex items-start gap-2 text-sm">
                <input
                  type="radio"
                  name="resize"
                  checked={!options.resize}
                  onChange={() =>
                    onChange({ ...options, resize: undefined, width: undefined, height: undefined })
                  }
                  className="mt-1"
                />
                <span>
                  <span className="font-medium">none</span>
                  <span className="block text-ink-faint">Keep the original size.</span>
                </span>
              </label>

              {resizeModes.map((mode) => (
                <label key={mode} className="flex items-start gap-2 text-sm">
                  <input
                    type="radio"
                    name="resize"
                    value={mode}
                    checked={options.resize === mode}
                    onChange={() =>
                      onChange({ ...options, resize: mode as ImageConvertOptions["resize"] })
                    }
                    className="mt-1"
                  />
                  <span>
                    <span className="font-medium">{mode}</span>
                    <span className="block text-ink-faint">{describeResize[mode]}</span>
                  </span>
                </label>
              ))}
            </div>

            {options.resize ? (
              <div className="mt-3 grid grid-cols-2 gap-2">
                <NumberField
                  id="image-width"
                  label="Width"
                  value={options.width}
                  onChange={(width) => onChange({ ...options, width })}
                />
                <NumberField
                  id="image-height"
                  label="Height"
                  value={options.height}
                  onChange={(height) => onChange({ ...options, height })}
                />
              </div>
            ) : null}

            {options.resize ? (
              <label className="mt-3 flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={options.upscale ?? false}
                  onChange={(event) => onChange({ ...options, upscale: event.target.checked })}
                />
                Allow making it larger
              </label>
            ) : null}
          </fieldset>

          <fieldset>
            <legend className="label">Orientation</legend>

            <label className="label mt-2 block text-xs" htmlFor="image-rotate">
              Rotate
            </label>
            <select
              id="image-rotate"
              className="input mt-1"
              value={options.rotate ?? 0}
              onChange={(event) =>
                onChange({
                  ...options,
                  rotate: Number(event.target.value) as ImageConvertOptions["rotate"],
                })
              }
            >
              {rotations.map((degrees) => (
                <option key={degrees} value={degrees}>
                  {degrees === 0 ? "none" : `${degrees}° clockwise`}
                </option>
              ))}
            </select>

            <label className="label mt-3 block text-xs" htmlFor="image-flip">
              Flip
            </label>
            <select
              id="image-flip"
              className="input mt-1"
              value={options.flip ?? ""}
              onChange={(event) =>
                onChange({
                  ...options,
                  flip: (event.target.value || undefined) as ImageConvertOptions["flip"],
                })
              }
            >
              <option value="">none</option>
              {flips.map((flip) => (
                <option key={flip} value={flip}>
                  {flip}
                </option>
              ))}
            </select>
          </fieldset>

          {lossy.has(options.to) ? (
            <fieldset>
              <legend className="label">Quality</legend>
              <input
                id="image-quality"
                type="range"
                min={1}
                max={100}
                value={options.quality ?? 82}
                onChange={(event) => onChange({ ...options, quality: Number(event.target.value) })}
                className="mt-2 w-full"
              />
              <p className="tabular mt-1 text-xs text-ink-faint">{options.quality ?? 82} of 100</p>
            </fieldset>
          ) : null}

          <fieldset>
            <legend className="label">Metadata</legend>
            <label className="mt-2 flex items-start gap-2 text-sm">
              <input
                type="checkbox"
                checked={options.keep_metadata ?? false}
                onChange={(event) => onChange({ ...options, keep_metadata: event.target.checked })}
                className="mt-1"
              />
              <span>
                <span className="font-medium">Keep EXIF</span>
                <span className="block text-ink-faint">
                  Off by default. A photograph&rsquo;s EXIF often carries GPS coordinates, a device
                  serial number and the time it was taken, and keeping it republishes all three.
                  The colour profile is applied either way, so the colours do not change.
                </span>
              </span>
            </label>
          </fieldset>
        </div>
      </details>
    </div>
  );
}

/** NumberField is a small labelled number input. An empty box means "not set"
 *  rather than zero, because zero means "leave this axis open" to the API. */
export function NumberField({
  id,
  label,
  value,
  onChange,
}: {
  id: string;
  label: string;
  value?: number;
  onChange: (value: number | undefined) => void;
}): React.ReactElement {
  return (
    <div>
      <label className="label block text-xs" htmlFor={id}>
        {label}
      </label>
      <input
        id={id}
        type="number"
        min={0}
        max={20000}
        className="input mt-1"
        value={value ?? ""}
        onChange={(event) =>
          onChange(event.target.value === "" ? undefined : Number(event.target.value))
        }
      />
    </div>
  );
}
