import { Head } from "@inertiajs/react";
import { useCallback, useEffect, useMemo, useState } from "react";

import ConversionQueue from "../../components/ConversionQueue";
import DropZone from "../../components/DropZone";
import Icon from "../../components/Icon";
import {
  submitVideoConvert,
  videoCapabilities,
  type VideoCapabilities,
  type VideoConvertOptions,
} from "../../lib/conversions";
import { useToolRuns } from "../../lib/toolRun";

interface Props {
  tool: { name: string; title: string; summary: string; in: string[]; out: string[] };
  maxBytes: number;
  tusPath: string;
}

/** The media types the video tools accept, from internal/detect. */
const accept = [
  "video/mp4",
  "video/webm",
  "video/x-matroska",
  "video/quicktime",
  "video/x-msvideo",
];

/**
 * The video conversion tool.
 *
 * Containers and codecs are fetched, and the licence is the reason. H.264 and
 * H.265 come from x264 and x265, which are GPL-only, so an ffmpeg built
 * without GPL support has neither — and this page says so in words rather
 * than silently offering a codec that would be refused.
 */
export default function VideoConvert({ tool, maxBytes, tusPath }: Props): React.ReactElement {
  const [capabilities, setCapabilities] = useState<VideoCapabilities | null>(null);
  const [failed, setFailed] = useState(false);
  const [options, setOptions] = useState<VideoConvertOptions>({ container: "" });

  useEffect(() => {
    let live = true;

    void videoCapabilities()
      .then((caps) => {
        if (!live) {
          return;
        }

        setCapabilities(caps);
        setOptions((current) =>
          current.container ? current : { container: caps.containers[0]?.name ?? "" },
        );
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
    (fileID: string) => submitVideoConvert(fileID, options),
    [options],
  );

  const { jobs, start, patch, remove } = useToolRuns(tusPath, submit, [
    "options.container",
    "options.video",
    "options.audio",
    "options.crf",
    "options.height",
  ]);

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
          The list of codecs could not be read, so this tool cannot say what it would produce.
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

      <Options options={options} onChange={setOptions} capabilities={capabilities} />

      <ConversionQueue jobs={jobs} onChange={patch} onRemove={remove} />
    </>
  );
}

/** describeCodec is what each codec is for, in one line, because "av1" tells
 *  somebody choosing nothing at all. */
const describeCodec: Record<string, string> = {
  h264: "Plays everywhere. Largest file of the four.",
  h265: "About half the size of H.264. Not supported everywhere.",
  vp9: "Royalty-free, well supported in browsers.",
  av1: "Smallest file. Royalty-free. Slow to encode.",
};

const describeAudio: Record<string, string> = {
  aac: "Plays everywhere.",
  opus: "Best quality per byte.",
  mp3: "Oldest and most compatible.",
  flac: "Lossless, much larger.",
  vorbis: "Older royalty-free option.",
  copy: "Keep the original track untouched.",
  mute: "Remove the sound.",
};

function Options({
  options,
  onChange,
  capabilities,
}: {
  options: VideoConvertOptions;
  onChange: (options: VideoConvertOptions) => void;
  capabilities: VideoCapabilities | null;
}): React.ReactElement {
  const container = useMemo(
    () => capabilities?.containers.find((entry) => entry.name === options.container),
    [capabilities, options.container],
  );

  // The codecs the chosen container carries *and* this build can encode: the
  // server publishes the intersection, so the page does not work it out.
  const videoCodecs = container?.video_codecs ?? [];
  const audioCodecs = container?.audio_codecs ?? [];

  return (
    <div className="mt-6 space-y-4">
      <div className="rounded-panel border border-edge bg-raised px-4 py-4">
        <label className="label" htmlFor="video-container">
          Container
        </label>
        <select
          id="video-container"
          className="input mt-2"
          value={options.container}
          onChange={(event) =>
            // The codecs belong to the container, so changing it clears them
            // rather than leaving a pair the server would refuse.
            onChange({ container: event.target.value, crf: options.crf, height: options.height })
          }
        >
          {capabilities === null ? <option value="">Loading…</option> : null}
          {capabilities?.containers.map((entry) => (
            <option key={entry.name} value={entry.name}>
              {entry.label} ({entry.extension})
            </option>
          ))}
        </select>

        {capabilities !== null && !capabilities.gpl_build ? (
          <p className="mt-2 text-xs text-ink-faint">
            This server&rsquo;s video engine is not built with GPL support, so H.264 and H.265 are
            not available. VP9 and AV1 are, and both are royalty-free.
          </p>
        ) : null}
      </div>

      <details className="rounded-panel border border-edge bg-raised">
        <summary className="flex cursor-pointer items-center gap-2 px-4 py-3 text-sm font-medium">
          <Icon name="video" className="size-4 text-ink-faint" />
          Codecs, quality and size
          <span className="text-ink-faint">— every one has a sensible default</span>
        </summary>

        <div className="grid gap-6 border-t border-edge px-4 py-4 sm:grid-cols-2">
          <fieldset>
            <legend className="label">Video codec</legend>
            <div className="mt-2 space-y-2">
              <Choice
                name="video-codec"
                value=""
                label="default"
                hint="The one this container plays everywhere."
                checked={!options.video}
                onSelect={() => onChange({ ...options, video: undefined })}
              />
              {videoCodecs.map((codec) => (
                <Choice
                  key={codec}
                  name="video-codec"
                  value={codec}
                  label={codec}
                  hint={describeCodec[codec]}
                  checked={options.video === codec}
                  onSelect={() => onChange({ ...options, video: codec })}
                />
              ))}
            </div>
          </fieldset>

          <fieldset>
            <legend className="label">Audio</legend>
            <div className="mt-2 space-y-2">
              <Choice
                name="video-audio"
                value=""
                label="default"
                hint="The one this container plays everywhere."
                checked={!options.audio}
                onSelect={() => onChange({ ...options, audio: undefined })}
              />
              {audioCodecs.map((codec) => (
                <Choice
                  key={codec}
                  name="video-audio"
                  value={codec}
                  label={codec}
                  hint={describeAudio[codec]}
                  checked={options.audio === codec}
                  onSelect={() => onChange({ ...options, audio: codec })}
                />
              ))}
            </div>
          </fieldset>

          <fieldset>
            <legend className="label">Quality</legend>
            <input
              id="video-crf"
              type="range"
              min={14}
              max={50}
              value={options.crf ?? 23}
              onChange={(event) => onChange({ ...options, crf: Number(event.target.value) })}
              className="mt-2 w-full"
            />
            <p className="tabular mt-1 text-xs text-ink-faint">
              CRF {options.crf ?? 23} — lower is better and larger. The scale is not comparable
              between codecs, and a value outside the chosen codec&rsquo;s range is pulled into it.
            </p>
          </fieldset>

          <fieldset>
            <legend className="label">Resolution</legend>
            <label className="label mt-2 block text-xs" htmlFor="video-height">
              Scale to height
            </label>
            <select
              id="video-height"
              className="input mt-1"
              value={options.height ?? 0}
              onChange={(event) =>
                onChange({ ...options, height: Number(event.target.value) || undefined })
              }
            >
              <option value={0}>keep the original</option>
              {[2160, 1440, 1080, 720, 480, 360].map((height) => (
                <option key={height} value={height}>
                  {height}p
                </option>
              ))}
            </select>
            <p className="mt-1 text-xs text-ink-faint">
              Only downwards: a smaller video is never upscaled, because the result would be larger
              and look worse.
            </p>
          </fieldset>
        </div>
      </details>
    </div>
  );
}

/** Choice is one radio with a label and a one-line explanation. */
function Choice({
  name,
  value,
  label,
  hint,
  checked,
  onSelect,
}: {
  name: string;
  value: string;
  label: string;
  hint?: string;
  checked: boolean;
  onSelect: () => void;
}): React.ReactElement {
  return (
    <label className="flex items-start gap-2 text-sm">
      <input
        type="radio"
        name={name}
        value={value}
        checked={checked}
        onChange={onSelect}
        className="mt-1"
      />
      <span>
        <span className="font-medium">{label}</span>
        {hint ? <span className="block text-ink-faint">{hint}</span> : null}
      </span>
    </label>
  );
}
