import { Head } from "@inertiajs/react";
import { useCallback, useEffect, useState } from "react";

import ConversionQueue from "../../components/ConversionQueue";
import DropZone from "../../components/DropZone";
import Icon from "../../components/Icon";
import {
  submitVideoToGif,
  videoCapabilities,
  type VideoCapabilities,
  type VideoToGifOptions,
} from "../../lib/conversions";
import { useToolRuns } from "../../lib/toolRun";

interface Props {
  tool: { name: string; title: string; summary: string; in: string[]; out: string[] };
  maxBytes: number;
  tusPath: string;
}

const accept = [
  "video/mp4",
  "video/webm",
  "video/x-matroska",
  "video/quicktime",
  "video/x-msvideo",
];

/**
 * The video to GIF tool.
 *
 * The length limit is the interesting constraint and it is the server's, so
 * this page reads it rather than repeating it: a GIF has no interframe
 * compression worth the name, so the file grows linearly with the duration and
 * a one-minute GIF at any useful size is tens of megabytes — a worse result
 * than the video it came from.
 */
export default function VideoToGif({ tool, maxBytes, tusPath }: Props): React.ReactElement {
  const [capabilities, setCapabilities] = useState<VideoCapabilities | null>(null);
  const [options, setOptions] = useState<VideoToGifOptions>({});

  useEffect(() => {
    let live = true;

    void videoCapabilities()
      .then((caps) => {
        if (live) {
          setCapabilities(caps);
        }
      })
      // A failure here is not fatal: every option has a server-side default,
      // so the tool still works and only the dithering list is missing.
      .catch(() => undefined);

    return () => {
      live = false;
    };
  }, []);

  const submit = useCallback((fileID: string) => submitVideoToGif(fileID, options), [options]);

  const { jobs, start, patch, remove } = useToolRuns(tusPath, submit, [
    "options.duration_seconds",
    "options.start_seconds",
    "options.dither",
    "options.width",
    "options.fps",
  ]);

  const limit = capabilities?.max_gif_seconds ?? 15;

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
        <p className="mt-3 text-sm text-ink-soft">
          At most {limit} seconds. A GIF stores every frame almost whole, so its size grows with its
          length: a long one would be bigger than the video it came from.
        </p>
      </div>

      <div className="mt-8">
        <DropZone
          accept={accept}
          maxBytes={maxBytes}
          onFiles={(files) => files.forEach((file) => void start(file))}
        />
      </div>

      <Options options={options} onChange={setOptions} capabilities={capabilities} limit={limit} />

      <ConversionQueue jobs={jobs} onChange={patch} onRemove={remove} />
    </>
  );
}

function Options({
  options,
  onChange,
  capabilities,
  limit,
}: {
  options: VideoToGifOptions;
  onChange: (options: VideoToGifOptions) => void;
  capabilities: VideoCapabilities | null;
  limit: number;
}): React.ReactElement {
  return (
    <details className="mt-6 rounded-panel border border-edge bg-raised" open>
      <summary className="flex cursor-pointer items-center gap-2 px-4 py-3 text-sm font-medium">
        <Icon name="video" className="size-4 text-ink-faint" />
        Which part, and how
        <span className="text-ink-faint">— every one has a sensible default</span>
      </summary>

      <div className="grid gap-6 border-t border-edge px-4 py-4 sm:grid-cols-2">
        <fieldset>
          <legend className="label">Range</legend>

          <div className="mt-2 grid grid-cols-2 gap-2">
            <div>
              <label className="label block text-xs" htmlFor="gif-start">
                Start (seconds)
              </label>
              <input
                id="gif-start"
                type="number"
                min={0}
                step={0.5}
                className="input mt-1"
                value={options.start_seconds ?? ""}
                onChange={(event) =>
                  onChange({
                    ...options,
                    start_seconds:
                      event.target.value === "" ? undefined : Number(event.target.value),
                  })
                }
              />
            </div>

            <div>
              <label className="label block text-xs" htmlFor="gif-duration">
                Length (seconds)
              </label>
              <input
                id="gif-duration"
                type="number"
                min={0}
                max={limit}
                step={0.5}
                className="input mt-1"
                value={options.duration_seconds ?? ""}
                onChange={(event) =>
                  onChange({
                    ...options,
                    duration_seconds:
                      event.target.value === "" ? undefined : Number(event.target.value),
                  })
                }
              />
            </div>
          </div>

          <p className="mt-2 text-xs text-ink-faint">
            Leave the length empty to take the rest of the video, up to {limit} seconds.
          </p>
        </fieldset>

        <fieldset>
          <legend className="label">Size and smoothness</legend>

          <label className="label mt-2 block text-xs" htmlFor="gif-width">
            Width
          </label>
          <select
            id="gif-width"
            className="input mt-1"
            value={options.width ?? 480}
            onChange={(event) => onChange({ ...options, width: Number(event.target.value) })}
          >
            {[240, 320, 480, 640, 800].map((width) => (
              <option key={width} value={width}>
                {width} px
              </option>
            ))}
          </select>

          <label className="label mt-3 block text-xs" htmlFor="gif-fps">
            Frames per second
          </label>
          <select
            id="gif-fps"
            className="input mt-1"
            value={options.fps ?? 12}
            onChange={(event) => onChange({ ...options, fps: Number(event.target.value) })}
          >
            {[5, 10, 12, 20, 25].map((fps) => (
              <option key={fps} value={fps}>
                {fps}
              </option>
            ))}
          </select>
          <p className="mt-1 text-xs text-ink-faint">
            A GIF stores each frame&rsquo;s delay in hundredths of a second, so rates that divide
            100 evenly play evenly. 12 is both smooth and small.
          </p>
        </fieldset>

        {capabilities !== null && capabilities.dithers.length > 0 ? (
          <fieldset className="sm:col-span-2">
            <legend className="label">Dithering</legend>
            <select
              id="gif-dither"
              className="input mt-2"
              value={options.dither ?? ""}
              onChange={(event) =>
                onChange({ ...options, dither: event.target.value || undefined })
              }
            >
              <option value="">default (sierra2_4a)</option>
              {capabilities.dithers.map((dither) => (
                <option key={dither} value={dither}>
                  {dither}
                </option>
              ))}
            </select>
            <p className="mt-1 text-xs text-ink-faint">
              A GIF holds 256 colours. The palette is computed from the frames in your clip rather
              than a fixed one, and the dithering decides how the frames are mapped onto it:
              &ldquo;none&rdquo; gives flat bands, the error-diffusion modes trade a little noise
              for smoother gradients.
            </p>
          </fieldset>
        ) : null}
      </div>
    </details>
  );
}
