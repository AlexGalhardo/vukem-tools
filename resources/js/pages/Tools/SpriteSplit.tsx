import { Head } from "@inertiajs/react";
import { useEffect, useMemo, useRef, useState } from "react";

import DropZone from "../../components/DropZone";
import Icon from "../../components/Icon";
import { SpriteResults, useSpriteRun } from "../../components/SpriteRun";
import { capabilities, sheetSize, type Capabilities } from "../../lib/sprite";
import { gridRects, resolveGrid, type Grid } from "../../lib/spriteGrid";

interface Props {
  tool: { name: string; title: string; summary: string; in: string[]; out: string[] };
  maxBytes: number;
  tusPath: string;
  modes: string[];
  limits: Record<string, number>;
}

/**
 * The sprite-split page.
 *
 * Its own page rather than an entry in the generic form table, because the
 * control that matters here is not a field: it is the grid drawn over the
 * sheet. Somebody arriving with a sheet does not know its cell size — they
 * know what the frames look like — so the job of this page is to let them try
 * numbers and see immediately whether the lines land on the frames.
 *
 * Which is why the overlay and the frame count are computed in the browser,
 * from the same arithmetic the server uses (lib/spriteGrid.ts, pinned against
 * the Go implementation by a shared table). A round trip per keystroke would
 * make it a form again.
 */
export default function SpriteSplit({ tool, maxBytes, tusPath, modes, limits }: Props): React.ReactElement {
  const run = useSpriteRun(tool.name, tusPath);

  const [caps, setCaps] = useState<Capabilities>();
  const [file, setFile] = useState<File>();
  const [sheet, setSheet] = useState<{ width: number; height: number; url: string }>();
  const [sheetError, setSheetError] = useState<string>();

  const [mode, setMode] = useState(modes[0] ?? "grid");
  const [by, setBy] = useState<"size" | "counts">("size");
  const [grid, setGrid] = useState<Grid>({ cellW: 32, cellH: 32, cols: 4, rows: 4, margin: 0, spacing: 0 });
  const [trim, setTrim] = useState(false);
  const [prefix, setPrefix] = useState("");
  const [background, setBackground] = useState("");
  const [tolerance, setTolerance] = useState(0);
  const [minFrame, setMinFrame] = useState(0);
  const [formats, setFormats] = useState<string[]>(["json-hash"]);
  const [aseprite, setAseprite] = useState(false);
  const [frameMS, setFrameMS] = useState(100);

  useEffect(() => {
    void capabilities().then(setCaps).catch(() => undefined);
  }, []);

  // The object URL is this component's to release: left alone it keeps the
  // decoded sheet alive for the tab's lifetime, which on a 4096x4096 sheet is
  // 64 MB of somebody else's memory.
  useEffect(() => {
    if (!sheet) {
      return;
    }

    return () => URL.revokeObjectURL(sheet.url);
  }, [sheet]);

  // Only the cell size or the counts is sent, never both: the server refuses
  // a request carrying the two, and rightly — they are two ways of saying one
  // thing and these two would disagree.
  const effective: Grid = useMemo(
    () =>
      by === "size"
        ? { ...grid, cols: 0, rows: 0 }
        : { ...grid, cellW: 0, cellH: 0 },
    [by, grid],
  );

  const resolved = useMemo(
    () => (sheet ? resolveGrid(sheet.width, sheet.height, effective) : undefined),
    [sheet, effective],
  );

  const cssChosen = formats.includes("css");

  const options = useMemo(() => {
    const out: Record<string, unknown> = { mode, metadata: formats };

    if (mode === "grid") {
      if (by === "size") {
        out.cell_w = grid.cellW;
        out.cell_h = grid.cellH;
      } else {
        out.cols = grid.cols;
        out.rows = grid.rows;
      }

      out.margin = grid.margin;
      out.spacing = grid.spacing;
    } else {
      if (background) {
        out.background = background;
        out.background_tolerance = tolerance;
      }

      if (minFrame > 0) {
        out.min_frame = minFrame;
      }
    }

    if (trim) {
      out.trim = true;
    }

    if (prefix) {
      out.prefix = prefix;
    }

    if (aseprite) {
      out.aseprite = true;
      out.frame_ms = frameMS;
    }

    return out;
  }, [aseprite, background, by, formats, frameMS, grid, minFrame, mode, prefix, tolerance, trim]);

  const blocked =
    !file ||
    run.busy ||
    (mode === "grid" && resolved !== undefined && !resolved.ok) ||
    (trim && cssChosen) ||
    formats.length === 0;

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
          accept={["image/png", "image/gif", "image/jpeg"]}
          maxBytes={maxBytes}
          disabled={run.busy}
          onFiles={(picked) => {
            const next = picked[0];

            if (!next) {
              return;
            }

            setFile(next);
            setSheetError(undefined);
            setSheet(undefined);

            // The dimensions land before the picture does, so the frame count
            // is readable while a large sheet is still decoding.
            void sheetSize(next)
              .then(setSheet)
              .catch((err: Error) => setSheetError(err.message));
          }}
        />
      </div>

      {sheetError ? (
        <p role="alert" className="mt-3 text-sm text-bad">
          {sheetError}
        </p>
      ) : null}

      {sheet ? (
        <>
          <div className="mt-6 grid gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]">
            <Preview sheet={sheet} grid={effective} resolved={resolved} mode={mode} />

            <div className="space-y-5">
              <Field label="How to find the frames">
                <select className={control} value={mode} onChange={(e) => setMode(e.target.value)}>
                  {modes.map((m) => (
                    <option key={m} value={m}>
                      {m}
                    </option>
                  ))}
                </select>
                <Help>
                  {mode === "grid"
                    ? "Exact, and what the overwhelming majority of sheets are: you know your own cell size."
                    : "Finds each frame's bounding box from the sheet's own content, for a sheet whose frames are not a uniform grid."}
                </Help>
              </Field>

              {mode === "grid" ? (
                <GridControls by={by} setBy={setBy} grid={grid} setGrid={setGrid} limits={limits} />
              ) : (
                <AutoControls
                  background={background}
                  setBackground={setBackground}
                  tolerance={tolerance}
                  setTolerance={setTolerance}
                  minFrame={minFrame}
                  setMinFrame={setMinFrame}
                />
              )}

              <Field label="Frame name prefix">
                <input
                  className={control}
                  value={prefix}
                  placeholder="frame"
                  onChange={(e) => setPrefix(e.target.value)}
                />
                <Help>The nth frame is this plus a zero-padded index, so they sort in order everywhere.</Help>
              </Field>

              <label className="flex items-start gap-2 text-sm">
                <input
                  type="checkbox"
                  className="mt-1"
                  checked={trim}
                  onChange={(e) => setTrim(e.target.checked)}
                />
                <span>
                  <span className="font-medium">Trim each frame</span>
                  <span className="block text-ink-faint">
                    Shrinks each frame to its opaque box and records the offset, so an importer can put
                    it back. Safe only because the metadata carries that offset &mdash; which is why CSS
                    refuses it.
                  </span>
                </span>
              </label>
            </div>
          </div>

          <Formats
            caps={caps}
            chosen={formats}
            setChosen={setFormats}
            trim={trim}
            aseprite={aseprite}
            setAseprite={setAseprite}
            frameMS={frameMS}
            setFrameMS={setFrameMS}
            maxFormats={limits.maxFormats ?? 4}
          />

          <div className="mt-6 flex flex-wrap items-center gap-3">
            <button
              type="button"
              disabled={blocked}
              onClick={() => file && void run.run(file, options)}
              className="flex min-h-11 items-center rounded-field bg-signal px-4 font-medium text-raised
                transition-colors duration-100 hover:brightness-110
                disabled:cursor-not-allowed disabled:opacity-50"
            >
              {run.busy ? "Starting…" : "Split the sheet"}
            </button>

            {run.problem ? (
              <p role="alert" className="text-sm text-bad">
                {run.problem}
              </p>
            ) : null}
          </div>
        </>
      ) : null}

      <SpriteResults jobs={run.jobs} files={run.files} onChange={run.patch} onRemove={run.remove} />
    </>
  );
}

/** Preview draws the sheet with the grid over it.
 *
 *  On a scaled canvas, never at full size: a 4096x4096 sheet painted at one
 *  pixel per pixel is sixteen megapixels of layout work on every redraw, and
 *  the lines would be invisible anyway. */
function Preview({
  sheet,
  grid,
  resolved,
  mode,
}: {
  sheet: { width: number; height: number; url: string };
  grid: Grid;
  resolved: ReturnType<typeof resolveGrid> | undefined;
  mode: string;
}): React.ReactElement {
  const canvas = useRef<HTMLCanvasElement>(null);
  const [drawn, setDrawn] = useState(false);

  // The longest side of the canvas. Big enough to judge whether the lines sit
  // on the frames, small enough to redraw on a keystroke.
  const maxSide = 640;
  const scale = Math.min(1, maxSide / Math.max(sheet.width, sheet.height));
  const width = Math.max(1, Math.round(sheet.width * scale));
  const height = Math.max(1, Math.round(sheet.height * scale));

  useEffect(() => {
    const element = canvas.current;

    if (!element) {
      return;
    }

    const context = element.getContext("2d");

    if (!context) {
      return;
    }

    const img = new Image();

    img.onload = () => {
      context.clearRect(0, 0, width, height);
      // Nearest-neighbour, because this is pixel art: smoothing a sprite
      // sheet preview makes it impossible to see where a frame ends.
      context.imageSmoothingEnabled = false;
      context.drawImage(img, 0, 0, width, height);
      setDrawn(true);

      if (mode !== "grid" || !resolved?.ok) {
        return;
      }

      context.lineWidth = 1;

      for (const rect of gridRects(resolved, grid)) {
        // Two strokes, light over dark, so the lines are visible on a sheet
        // of any colour without a tint over the artwork.
        context.strokeStyle = "rgba(0,0,0,0.55)";
        context.strokeRect(rect.x * scale + 0.5, rect.y * scale + 0.5, rect.w * scale, rect.h * scale);
        context.strokeStyle = "rgba(255,255,255,0.55)";
        context.strokeRect(rect.x * scale - 0.5, rect.y * scale - 0.5, rect.w * scale, rect.h * scale);
      }
    };

    img.src = sheet.url;
  }, [grid, height, mode, resolved, scale, sheet.url, width]);

  return (
    <div>
      {/* The numbers come first and do not wait for the picture. */}
      <p className="tabular text-sm text-ink-soft">
        {sheet.width} &times; {sheet.height}
        {mode === "grid" && resolved?.ok ? (
          <>
            {" "}
            &middot; <strong className="font-medium text-ink">{resolved.frames} frames</strong> of{" "}
            {resolved.cellW} &times; {resolved.cellH}
          </>
        ) : null}
        {mode !== "grid" ? " · the frame count is found by the server from the sheet's content" : null}
      </p>

      {mode === "grid" && resolved && !resolved.ok ? (
        <p role="alert" className="mt-2 flex items-start gap-1.5 text-sm text-bad">
          <Icon name="alert" className="mt-0.5 size-3.5 shrink-0" />
          <span>{resolved.why}</span>
        </p>
      ) : null}

      <div className="mt-3 overflow-auto rounded-panel border border-edge bg-sunken p-3">
        <canvas
          ref={canvas}
          width={width}
          height={height}
          className="mx-auto block max-w-full"
          style={{ imageRendering: "pixelated" }}
          aria-label={`The sheet, ${sheet.width} by ${sheet.height} pixels, with the grid drawn over it`}
          role="img"
        />
        {!drawn ? <p className="mt-2 text-center text-xs text-ink-faint">Drawing the sheet&hellip;</p> : null}
      </div>
    </div>
  );
}

/** GridControls is the cell size or the counts, plus the gutters. */
function GridControls({
  by,
  setBy,
  grid,
  setGrid,
  limits,
}: {
  by: "size" | "counts";
  setBy: (v: "size" | "counts") => void;
  grid: Grid;
  setGrid: (g: Grid) => void;
  limits: Record<string, number>;
}): React.ReactElement {
  const gutter = limits.maxGutter ?? 256;

  return (
    <>
      <Field label="Describe the grid by">
        <select className={control} value={by} onChange={(e) => setBy(e.target.value as "size" | "counts")}>
          <option value="size">cell size</option>
          <option value="counts">column and row counts</option>
        </select>
        <Help>Two ways of saying one thing, so only one is sent.</Help>
      </Field>

      {by === "size" ? (
        <div className="grid grid-cols-2 gap-3">
          <Field label="Cell width">
            <Number value={grid.cellW} min={1} max={8192} onChange={(v) => setGrid({ ...grid, cellW: v })} />
          </Field>
          <Field label="Cell height">
            <Number value={grid.cellH} min={1} max={8192} onChange={(v) => setGrid({ ...grid, cellH: v })} />
          </Field>
        </div>
      ) : (
        <div className="grid grid-cols-2 gap-3">
          <Field label="Columns">
            <Number value={grid.cols} min={1} max={2048} onChange={(v) => setGrid({ ...grid, cols: v })} />
          </Field>
          <Field label="Rows">
            <Number value={grid.rows} min={1} max={2048} onChange={(v) => setGrid({ ...grid, rows: v })} />
          </Field>
        </div>
      )}

      <div className="grid grid-cols-2 gap-3">
        <Field label="Margin">
          <Number value={grid.margin} min={0} max={gutter} onChange={(v) => setGrid({ ...grid, margin: v })} />
          <Help>Outside the cells, at every edge.</Help>
        </Field>
        <Field label="Spacing">
          <Number value={grid.spacing} min={0} max={gutter} onChange={(v) => setGrid({ ...grid, spacing: v })} />
          <Help>Between cells only, never after the last one.</Help>
        </Field>
      </div>
    </>
  );
}

/** AutoControls is the background and the noise floor. */
function AutoControls({
  background,
  setBackground,
  tolerance,
  setTolerance,
  minFrame,
  setMinFrame,
}: {
  background: string;
  setBackground: (v: string) => void;
  tolerance: number;
  setTolerance: (v: number) => void;
  minFrame: number;
  setMinFrame: (v: number) => void;
}): React.ReactElement {
  return (
    <>
      <Field label="Background colour">
        <div className="flex gap-2">
          <input
            type="color"
            className="h-11 w-14 shrink-0 cursor-pointer rounded-field border border-edge bg-surface p-1"
            value={background || "#ff00ff"}
            onChange={(e) => setBackground(e.target.value)}
            aria-label="Pick the background colour"
          />
          <input
            className={control}
            value={background}
            placeholder="transparency"
            onChange={(e) => setBackground(e.target.value)}
          />
          {background ? (
            <button
              type="button"
              className="min-h-11 shrink-0 rounded-field px-2 text-sm text-ink-faint hover:text-ink"
              onClick={() => setBackground("")}
            >
              Clear
            </button>
          ) : null}
        </div>
        <Help>
          Leave it empty for transparency, which is what almost every sheet uses. A fully opaque sheet
          with no colour named is refused rather than guessed at &mdash; frames that are subtly wrong are
          worse than frames that were never produced.
        </Help>
      </Field>

      {background ? (
        <Field label="Colour tolerance">
          <Number value={tolerance} min={0} max={255} onChange={setTolerance} />
          <Help>How far a pixel may be from that colour and still count as background.</Help>
        </Field>
      ) : null}

      <Field label="Smallest frame">
        <Number value={minFrame} min={0} max={1000000} onChange={setMinFrame} />
        <Help>Drop anything smaller than this many pixels as noise. The count dropped is reported.</Help>
      </Field>
    </>
  );
}

/** Formats is the metadata choice, with the sentence the server publishes for
 *  each one. */
function Formats({
  caps,
  chosen,
  setChosen,
  trim,
  aseprite,
  setAseprite,
  frameMS,
  setFrameMS,
  maxFormats,
}: {
  caps: Capabilities | undefined;
  chosen: string[];
  setChosen: (v: string[]) => void;
  trim: boolean;
  aseprite: boolean;
  setAseprite: (v: boolean) => void;
  frameMS: number;
  setFrameMS: (v: number) => void;
  maxFormats: number;
}): React.ReactElement {
  const formats = caps?.formats ?? [];
  const json = chosen.some((name) => name.startsWith("json-"));

  return (
    <section className="mt-6">
      <h2 className="text-lg font-medium">Metadata</h2>
      <p className="mt-1 max-w-[70ch] text-sm text-ink-faint">
        What an engine reads to find the frames in the sheet. At most {maxFormats} at once, each one a
        download of its own &mdash; and all of them are inside the frames archive too.
      </p>

      <div className="mt-3 grid gap-2 sm:grid-cols-2">
        {formats.map((format) => {
          const picked = chosen.includes(format.name);
          const refused = trim && !format.trim;

          return (
            <label
              key={format.name}
              className={`flex items-start gap-2 rounded-panel border px-3 py-2 text-sm ${
                refused ? "border-edge opacity-50" : picked ? "border-signal bg-signal-soft" : "border-edge"
              }`}
            >
              <input
                type="checkbox"
                className="mt-1"
                checked={picked}
                disabled={refused || (!picked && chosen.length >= maxFormats)}
                onChange={(e) =>
                  setChosen(
                    e.target.checked
                      ? [...chosen, format.name]
                      : chosen.filter((name) => name !== format.name),
                  )
                }
              />
              <span>
                <span className="font-medium">{format.name}</span>
                <span className="text-ink-faint"> {format.extension}</span>
                <span className="block text-ink-faint">{format.imports}</span>
                {refused ? (
                  <span className="block text-bad">Not available with trimming on.</span>
                ) : null}
              </span>
            </label>
          );
        })}
      </div>

      {chosen.length === 0 ? (
        <p role="alert" className="mt-2 text-sm text-bad">
          Choose at least one metadata format, or the frames come back with nothing describing them.
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
                A per-frame duration and a tag per run of frames sharing a name. A flag on the JSON
                formats rather than a format of its own, because that is what it is.
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
    </section>
  );
}

const control =
  "min-h-11 w-full rounded-field border border-edge bg-surface px-3 focus:border-signal focus:outline-none";

/** Field is a label and its control. */
function Field({ label, children }: { label: string; children: React.ReactNode }): React.ReactElement {
  return (
    <div>
      <label className="label block">{label}</label>
      <div className="mt-1">{children}</div>
    </div>
  );
}

/** Help is the one line under a control. */
function Help({ children }: { children: React.ReactNode }): React.ReactElement {
  return <p className="mt-1 text-xs text-ink-faint">{children}</p>;
}

/** Number is a bounded integer input that never reports NaN. */
function Number_({
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

        // An empty field parses as 0, which is a legitimate value for a
        // gutter and not for a cell size; the server bounds both, and the
        // overlay says so before anything is sent.
        onChange(globalThis.Number.isFinite(parsed) ? parsed : min);
      }}
    />
  );
}

const Number = Number_;
