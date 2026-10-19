import { Head } from "@inertiajs/react";
import { useEffect, useMemo, useState } from "react";

import DropZone from "../../components/DropZone";
import Icon from "../../components/Icon";
import { SpriteResults, useSpriteRun } from "../../components/SpriteRun";
import { capabilities, zipEntryCount, type Capabilities } from "../../lib/sprite";

interface Props {
  tool: { name: string; title: string; summary: string; in: string[]; out: string[] };
  maxBytes: number;
  tusPath: string;
  layouts: string[];
  limits: Record<string, number>;
}

/**
 * The sprite-build page.
 *
 * Its own page for the same reason the split page is: what somebody is
 * deciding here is not a field but a picture — how tightly the frames pack and
 * whether the gutters are right — and the only honest way to show that is the
 * sheet the server actually produced.
 *
 * So the frame count comes first, from the archive's own directory and without
 * inflating anything, and the packed sheet is shown as the result rather than
 * predicted: predicting it would mean decoding every frame in the browser to
 * run the packer twice, once wrongly.
 */
export default function SpriteBuild({ tool, maxBytes, tusPath, layouts, limits }: Props): React.ReactElement {
  const run = useSpriteRun(tool.name, tusPath);

  const [caps, setCaps] = useState<Capabilities>();
  const [file, setFile] = useState<File>();
  const [frames, setFrames] = useState<number>();

  const [layout, setLayout] = useState(layouts[0] ?? "grid");
  const [margin, setMargin] = useState(0);
  const [padding, setPadding] = useState(0);
  const [extrude, setExtrude] = useState(0);
  const [maxSize, setMaxSize] = useState(limits.maxSheetSize ?? 4096);
  const [pot, setPOT] = useState(false);
  const [formats, setFormats] = useState<string[]>(["json-hash"]);
  const [aseprite, setAseprite] = useState(false);
  const [frameMS, setFrameMS] = useState(100);

  useEffect(() => {
    void capabilities().then(setCaps).catch(() => undefined);
  }, []);

  const maxFormats = limits.maxFormats ?? 4;
  const json = formats.some((name) => name.startsWith("json-"));

  // Extruding into a gutter that does not exist writes nothing, which looks
  // like the option being ignored. The server refuses it; saying so here means
  // nobody has to find out by submitting.
  const extrudeWithoutPadding = extrude > 0 && padding === 0;

  const options = useMemo(() => {
    const out: Record<string, unknown> = {
      layout,
      margin,
      padding,
      extrude,
      max_size: maxSize,
      metadata: formats,
    };

    if (pot) {
      out.pot = true;
    }

    if (aseprite) {
      out.aseprite = true;
      out.frame_ms = frameMS;
    }

    return out;
  }, [aseprite, extrude, formats, frameMS, layout, margin, maxSize, padding, pot]);

  const tooMany = frames !== undefined && frames > (limits.maxFrames ?? 2048);
  const blocked = !file || run.busy || extrudeWithoutPadding || formats.length === 0 || tooMany;

  return (
    <>
      <Head title={tool.title} />

      <header className="max-w-[70ch]">
        <h1 className="text-3xl font-semibold tracking-tight">{tool.title}</h1>
        <p className="mt-3 text-ink-soft">{tool.summary}</p>
        <p className="tabular mt-2 text-xs text-ink-faint">
          {tool.in.join(" · ")} <span aria-hidden="true">&rarr;</span>
          <span className="sr-only">to</span> {tool.out.join(" · ")}
        </p>
      </header>

      <div className="mt-6">
        <DropZone
          accept={["application/zip"]}
          maxBytes={maxBytes}
          disabled={run.busy}
          onFiles={(picked) => {
            const next = picked[0];

            if (!next) {
              return;
            }

            setFile(next);
            setFrames(undefined);

            // Read from the archive's end-of-central-directory record, so the
            // answer arrives at once rather than after inflating a thousand
            // PNGs in the browser.
            void zipEntryCount(next).then(setFrames).catch(() => undefined);
          }}
        />
      </div>

      {file ? (
        <>
          <p className="tabular mt-3 text-sm text-ink-soft">
            {frames === undefined ? (
              "The server will count the frames."
            ) : (
              <>
                <strong className="font-medium text-ink">{frames}</strong> files in this archive
              </>
            )}
          </p>

          {tooMany ? (
            <p role="alert" className="mt-2 flex items-start gap-1.5 text-sm text-bad">
              <Icon name="alert" className="mt-0.5 size-3.5 shrink-0" />
              <span>
                That is more than the {limits.maxFrames} frames one conversion packs. Split the set and
                build more than one sheet.
              </span>
            </p>
          ) : null}

          <div className="mt-6 grid gap-6 lg:grid-cols-2">
            <div className="space-y-5">
              <Field label="Layout">
                <select className={control} value={layout} onChange={(e) => setLayout(e.target.value)}>
                  {layouts.map((l) => (
                    <option key={l} value={l}>
                      {l}
                    </option>
                  ))}
                </select>
                <Help>
                  {layout === "grid"
                    ? "Every frame gets an identical cell, sized to the largest. Looser than compact, and the one Godot 4 and Unity import with no metadata file at all."
                    : "Packs frames of differing sizes tightly, bottom-left. Needs a metadata file, because nothing about the result is uniform."}
                </Help>
              </Field>

              <div className="grid grid-cols-2 gap-3">
                <Field label="Margin">
                  <Number value={margin} min={0} max={limits.maxGutter ?? 256} onChange={setMargin} />
                  <Help>Transparent gutter at the sheet&rsquo;s edges.</Help>
                </Field>
                <Field label="Padding">
                  <Number value={padding} min={0} max={limits.maxGutter ?? 256} onChange={setPadding} />
                  <Help>Between frames. Stops a neighbour bleeding in when the GPU samples wide.</Help>
                </Field>
              </div>

              <Field label="Extrude">
                <Number value={extrude} min={0} max={limits.maxExtrude ?? 8} onChange={setExtrude} />
                <Help>
                  Repeats each frame&rsquo;s own edge pixels into the padding. Padding alone leaves
                  transparent black, which shows as a dark halo on a tileset at non-integer zoom; this is
                  the fix for that.
                </Help>
                {extrudeWithoutPadding ? (
                  <p role="alert" className="mt-1 text-xs text-bad">
                    Extruding repeats edge pixels into the padding, so it needs a padding to repeat them
                    into.
                  </p>
                ) : null}
              </Field>

              <div className="grid grid-cols-2 gap-3">
                <Field label="Largest sheet">
                  <Number value={maxSize} min={16} max={limits.maxSheetSize ? 8192 : 8192} onChange={setMaxSize} />
                  <Help>4096 is what every GPU handles as one texture.</Help>
                </Field>
                <Field label="Power of two">
                  <label className="flex min-h-11 items-center gap-2 text-sm">
                    <input type="checkbox" checked={pot} onChange={(e) => setPOT(e.target.checked)} />
                    Round the sheet up
                  </label>
                  <Help>Some older pipelines need it; most do not.</Help>
                </Field>
              </div>
            </div>

            <div>
              <h2 className="text-lg font-medium">Metadata</h2>
              <p className="mt-1 text-sm text-ink-faint">
                At most {maxFormats} at once, each one a download of its own.
              </p>

              <div className="mt-3 space-y-2">
                {(caps?.formats ?? []).map((format) => {
                  const picked = formats.includes(format.name);

                  return (
                    <label
                      key={format.name}
                      className={`flex items-start gap-2 rounded-panel border px-3 py-2 text-sm ${
                        picked ? "border-signal bg-signal-soft" : "border-edge"
                      }`}
                    >
                      <input
                        type="checkbox"
                        className="mt-1"
                        checked={picked}
                        disabled={!picked && formats.length >= maxFormats}
                        onChange={(e) =>
                          setFormats(
                            e.target.checked
                              ? [...formats, format.name]
                              : formats.filter((name) => name !== format.name),
                          )
                        }
                      />
                      <span>
                        <span className="font-medium">{format.name}</span>
                        <span className="text-ink-faint"> {format.extension}</span>
                        <span className="block text-ink-faint">{format.imports}</span>
                      </span>
                    </label>
                  );
                })}
              </div>

              {formats.length === 0 ? (
                <p role="alert" className="mt-2 text-sm text-bad">
                  Choose at least one, or the sheet comes back with nothing describing it.
                </p>
              ) : null}

              {json ? (
                <div className="mt-4 space-y-3">
                  <label className="flex items-start gap-2 text-sm">
                    <input
                      type="checkbox"
                      className="mt-1"
                      checked={aseprite}
                      onChange={(e) => setAseprite(e.target.checked)}
                    />
                    <span>
                      <span className="font-medium">Aseprite extensions</span>
                      <span className="block text-ink-faint">
                        A per-frame duration and a tag per run of frames sharing a name.
                      </span>
                    </span>
                  </label>

                  {aseprite ? (
                    <Field label="Frame duration (ms)">
                      <Number value={frameMS} min={1} max={60000} onChange={setFrameMS} />
                    </Field>
                  ) : null}
                </div>
              ) : null}
            </div>
          </div>

          {layout === "grid" ? (
            <p className="mt-6 max-w-[70ch] rounded-panel border border-edge bg-raised px-4 py-3 text-sm text-ink-soft">
              <strong className="font-medium text-ink">Godot 4 and Unity need no metadata file for this
              layout.</strong>{" "}
              Import the sheet and give the frame size, or the frame counts, in the importer: Godot slices
              it from <code>AtlasTexture.region</code> or from an <code>AnimatedSprite2D</code>, and
              Unity&rsquo;s sprite editor does the same. Neither has a documented text format this service
              could write, so a file claiming to be one would be a guess.
            </p>
          ) : null}

          <div className="mt-6 flex flex-wrap items-center gap-3">
            <button
              type="button"
              disabled={blocked}
              onClick={() => file && void run.run(file, options)}
              className="flex min-h-11 items-center rounded-field bg-signal px-4 font-medium text-raised
                transition-colors duration-100 hover:brightness-110
                disabled:cursor-not-allowed disabled:opacity-50"
            >
              {run.busy ? "Starting…" : "Build the sheet"}
            </button>

            {run.problem ? (
              <p role="alert" className="text-sm text-bad">
                {run.problem}
              </p>
            ) : null}
          </div>
        </>
      ) : null}

      {/* The packed sheet is shown inline: how much of it is used, and whether
          the gutters are right, are things you look at rather than read. The
          percentage used is on the row above, from the server's own figure. */}
      <SpriteResults
        jobs={run.jobs}
        files={run.files}
        onChange={run.patch}
        onRemove={run.remove}
        preview
      />
    </>
  );
}

const control =
  "min-h-11 w-full rounded-field border border-edge bg-surface px-3 focus:border-signal focus:outline-none";

function Field({ label, children }: { label: string; children: React.ReactNode }): React.ReactElement {
  return (
    <div>
      <label className="label block">{label}</label>
      <div className="mt-1">{children}</div>
    </div>
  );
}

function Help({ children }: { children: React.ReactNode }): React.ReactElement {
  return <p className="mt-1 text-xs text-ink-faint">{children}</p>;
}

/** Number is a bounded integer input that never reports NaN. */
function Number({
  value,
  min,
  max,
  onChange,
}: {
  value: number;
  min: number;
  max: number;
  onChange: (v: number) => void;
}): React.ReactElement {
  return (
    <input
      type="number"
      inputMode="numeric"
      className={control}
      value={value}
      min={min}
      max={max}
      onChange={(e) => {
        const parsed = globalThis.Number(e.target.value);
        onChange(globalThis.Number.isFinite(parsed) ? parsed : min);
      }}
    />
  );
}
