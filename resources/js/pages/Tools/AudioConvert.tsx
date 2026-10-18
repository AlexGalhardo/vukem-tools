import { Head } from "@inertiajs/react";
import { useCallback, useEffect, useMemo, useState } from "react";

import ConversionQueue from "../../components/ConversionQueue";
import DropZone from "../../components/DropZone";
import Icon from "../../components/Icon";
import {
  audioCapabilities,
  submitAudioConvert,
  type AudioCapabilities,
  type AudioConvertOptions,
} from "../../lib/conversions";
import { useToolRuns } from "../../lib/toolRun";

interface Props {
  tool: { name: string; title: string; summary: string; in: string[]; out: string[] };
  maxBytes: number;
  tusPath: string;
}

/**
 * The audio conversion tool.
 *
 * A video input has its soundtrack extracted, which is why the drop zone
 * accepts video too: it is the same conversion with the picture dropped, and a
 * separate "extract audio" tool would be the same work behind a second name.
 */
export default function AudioConvert({ tool, maxBytes, tusPath }: Props): React.ReactElement {
  const [capabilities, setCapabilities] = useState<AudioCapabilities | null>(null);
  const [failed, setFailed] = useState(false);
  const [options, setOptions] = useState<AudioConvertOptions>({ to: "" });

  useEffect(() => {
    let live = true;

    void audioCapabilities()
      .then((caps) => {
        if (!live) {
          return;
        }

        setCapabilities(caps);
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

  const submit = useCallback((fileID: string) => submitAudioConvert(fileID, options), [options]);

  const { jobs, start, patch, remove } = useToolRuns(tusPath, submit, [
    "options.to",
    "options.bitrate",
    "options.sample_rate",
    "options.two_pass",
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
        <p className="mt-3 text-sm text-ink-soft">
          Drop a video to pull its soundtrack out: it is the same conversion with the picture
          dropped.
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
          accept={capabilities?.readable ?? []}
          maxBytes={maxBytes}
          onFiles={(files) => files.forEach((file) => void start(file))}
        />
      </div>

      <Options options={options} onChange={setOptions} capabilities={capabilities} />

      <ConversionQueue jobs={jobs} onChange={patch} onRemove={remove} />
    </>
  );
}

/** The bit rates each lossy format is worth asking for. Opus is transparent
 *  where MP3 is not, so offering one list for both would waste half the file
 *  on Opus and starve MP3. */
const bitrates: Record<string, number[]> = {
  mp3: [128, 192, 256, 320],
  opus: [64, 96, 128, 192],
  aac: [96, 128, 160, 256],
};

function Options({
  options,
  onChange,
  capabilities,
}: {
  options: AudioConvertOptions;
  onChange: (options: AudioConvertOptions) => void;
  capabilities: AudioCapabilities | null;
}): React.ReactElement {
  const writable = useMemo(
    () => capabilities?.formats.filter((format) => format.write) ?? [],
    [capabilities],
  );

  const chosen = writable.find((format) => format.name === options.to);
  const rates = bitrates[options.to] ?? [];

  return (
    <div className="mt-6 space-y-4">
      <div className="rounded-panel border border-edge bg-raised px-4 py-4">
        <label className="label" htmlFor="audio-to">
          Output format
        </label>
        <select
          id="audio-to"
          className="input mt-2"
          value={options.to}
          onChange={(event) =>
            // The bit rate belongs to the format, and a lossless format
            // refuses one outright, so changing the format clears it rather
            // than carrying a value the server would reject.
            onChange({ ...options, to: event.target.value, bitrate: undefined })
          }
        >
          {writable.length === 0 ? <option value="">No audio engine is available</option> : null}
          {writable.map((format) => (
            <option key={format.name} value={format.name}>
              {format.label} ({format.extension})
            </option>
          ))}
        </select>

        {chosen && !chosen.lossy ? (
          <p className="mt-2 text-xs text-ink-faint">
            {chosen.label} is lossless, so it has no bit rate to set. The file will be larger than a
            lossy one and identical to the source when decoded.
          </p>
        ) : null}
      </div>

      <details className="rounded-panel border border-edge bg-raised">
        <summary className="flex cursor-pointer items-center gap-2 px-4 py-3 text-sm font-medium">
          <Icon name="audio" className="size-4 text-ink-faint" />
          Quality and loudness
          <span className="text-ink-faint">— every one has a sensible default</span>
        </summary>

        <div className="grid gap-6 border-t border-edge px-4 py-4 sm:grid-cols-2">
          {chosen?.lossy && rates.length > 0 ? (
            <fieldset>
              <legend className="label">Bit rate</legend>
              <select
                id="audio-bitrate"
                className="input mt-2"
                value={options.bitrate ?? 0}
                onChange={(event) =>
                  onChange({ ...options, bitrate: Number(event.target.value) || undefined })
                }
              >
                <option value={0}>default</option>
                {rates.map((rate) => (
                  <option key={rate} value={rate}>
                    {rate} kbit/s
                  </option>
                ))}
              </select>
            </fieldset>
          ) : null}

          <fieldset>
            <legend className="label">Channels</legend>
            <select
              id="audio-channels"
              className="input mt-2"
              value={options.channels ?? 0}
              onChange={(event) =>
                onChange({
                  ...options,
                  channels: (Number(event.target.value) ||
                    undefined) as AudioConvertOptions["channels"],
                })
              }
            >
              <option value={0}>keep the original</option>
              <option value={1}>mono</option>
              <option value={2}>stereo</option>
            </select>
          </fieldset>

          {capabilities !== null ? (
            <fieldset>
              <legend className="label">Sample rate</legend>
              <select
                id="audio-sample-rate"
                className="input mt-2"
                value={options.sample_rate ?? 0}
                onChange={(event) =>
                  onChange({ ...options, sample_rate: Number(event.target.value) || undefined })
                }
              >
                <option value={0}>keep the original</option>
                {capabilities.sample_rates.map((rate) => (
                  <option key={rate} value={rate}>
                    {rate.toLocaleString()} Hz
                  </option>
                ))}
              </select>
            </fieldset>
          ) : null}

          <fieldset className="sm:col-span-2">
            <legend className="label">Loudness</legend>

            <label className="mt-2 flex items-start gap-2 text-sm">
              <input
                type="checkbox"
                checked={options.normalise ?? false}
                onChange={(event) =>
                  onChange({
                    ...options,
                    normalise: event.target.checked,
                    // Two passes without normalisation does nothing, and the
                    // server refuses the pair, so it is cleared with it.
                    two_pass: event.target.checked ? options.two_pass : undefined,
                  })
                }
                className="mt-1"
              />
              <span>
                <span className="font-medium">
                  Normalise to {capabilities?.loudness_target ?? -16} LUFS
                </span>
                <span className="block text-ink-faint">
                  The figure streaming platforms and podcast players normalise to, so a quiet
                  recording stops being quiet.
                </span>
              </span>
            </label>

            {options.normalise ? (
              <label className="mt-3 flex items-start gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={options.two_pass ?? false}
                  onChange={(event) => onChange({ ...options, two_pass: event.target.checked })}
                  className="mt-1"
                />
                <span>
                  <span className="font-medium">Measure the whole recording first</span>
                  <span className="block text-ink-faint">
                    Costs a second pass over the file and is the only way to actually hit the
                    target: one pass tracks a moving average and undershoots on anything with a
                    quiet opening.
                  </span>
                </span>
              </label>
            ) : null}
          </fieldset>
        </div>
      </details>
    </div>
  );
}
