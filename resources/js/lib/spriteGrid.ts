/**
 * The grid arithmetic, mirrored from internal/spritesheet/grid.go.
 *
 * It is here so the sprite-split page can redraw its overlay and show the
 * frame count as a control moves, with no round trip — which is what makes
 * choosing a cell size feel like adjusting something rather than submitting a
 * form. The server does the same arithmetic again and is the authority; this
 * is a prediction, and the only thing that would make it worse than useless is
 * being wrong.
 *
 * So the two are pinned against one written-down table of cases rather than
 * against each other: resources/js/lib/spriteGrid.test.ts and
 * tests/sprite_grid_table_test.go run the same rows. A change to either
 * implementation that is not matched by the other fails one of them.
 *
 * The off-by-one this file exists to get right: n cells of size c separated by
 * s occupy n*c + (n-1)*s. The spacing goes between cells and never after the
 * last one.
 */

/** Grid is the layout, described either by cell size or by counts. */
export interface Grid {
  cellW: number;
  cellH: number;
  cols: number;
  rows: number;
  margin: number;
  spacing: number;
}

/** Rect is one frame's rectangle in the sheet. */
export interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

/** Resolved is a grid that tiles the sheet, with its frames. */
export interface Resolved {
  /** ok is false when the grid cannot tile this sheet; why says what is wrong. */
  ok: boolean;
  why?: string;
  cols: number;
  rows: number;
  cellW: number;
  cellH: number;
  /** frames is how many frames the server will produce. */
  frames: number;
}

/** maxFrames mirrors spritesheet.MaxFrames. */
export const maxFrames = 2048;

/** maxGutter mirrors spritesheet.MaxGutter. */
export const maxGutter = 256;

/** resolveGrid works out the cell size or the counts, whichever was not
 *  given, and reports whether the result tiles the sheet exactly. */
export function resolveGrid(w: number, h: number, g: Grid): Resolved {
  const fail = (why: string): Resolved => ({ ok: false, why, cols: 0, rows: 0, cellW: 0, cellH: 0, frames: 0 });

  if (w <= 0 || h <= 0) {
    return fail("The sheet is empty.");
  }

  const bySize = g.cellW > 0 || g.cellH > 0;
  const byCount = g.cols > 0 || g.rows > 0;

  if (!bySize && !byCount) {
    return fail("Give a cell size, or a column and row count.");
  }

  if (bySize && byCount) {
    return fail("Give either a cell size or a column and row count, not both.");
  }

  if (bySize && (g.cellW <= 0 || g.cellH <= 0)) {
    return fail("A cell size needs both a width and a height.");
  }

  if (byCount && (g.cols <= 0 || g.rows <= 0)) {
    return fail("A grid by counts needs both a column count and a row count.");
  }

  if (g.margin < 0 || g.margin > maxGutter || g.spacing < 0 || g.spacing > maxGutter) {
    return fail(`The margin and the spacing must each be between 0 and ${maxGutter}.`);
  }

  const usableW = w - 2 * g.margin;
  const usableH = h - 2 * g.margin;

  if (usableW <= 0 || usableH <= 0) {
    return fail(`A margin of ${g.margin} leaves nothing of a ${w}x${h} sheet.`);
  }

  let { cols, rows, cellW, cellH } = g;

  if (bySize) {
    if (g.cellW > usableW || g.cellH > usableH) {
      return fail(`A ${g.cellW}x${g.cellH} cell does not fit the ${usableW}x${usableH} usable area.`);
    }

    cols = countFor(usableW, g.cellW, g.spacing);
    rows = countFor(usableH, g.cellH, g.spacing);
  } else {
    const spanW = usableW - (g.cols - 1) * g.spacing;
    const spanH = usableH - (g.rows - 1) * g.spacing;

    if (spanW <= 0 || spanH <= 0) {
      return fail(
        `${g.cols} columns and ${g.rows} rows with a spacing of ${g.spacing} leave no room for a cell.`,
      );
    }

    // Integer division, matching Go's: the remainder is what the exact-tiling
    // check below then refuses.
    cellW = Math.floor(spanW / g.cols);
    cellH = Math.floor(spanH / g.rows);
  }

  if (cellW <= 0 || cellH <= 0) {
    return fail(`The cell size works out as ${cellW}x${cellH}, which is not a cell.`);
  }

  const usedW = cols * cellW + (cols - 1) * g.spacing;
  const usedH = rows * cellH + (rows - 1) * g.spacing;

  const remX = usableW - usedW;
  const remY = usableH - usedH;

  if (remX !== 0 || remY !== 0) {
    return fail(
      `A ${cellW}x${cellH} cell does not tile a ${w}x${h} sheet with a margin of ${g.margin} ` +
        `and a spacing of ${g.spacing}: ${remX} pixels are left over across and ${remY} down.`,
    );
  }

  const frames = cols * rows;

  if (frames > maxFrames) {
    return fail(`That grid is ${frames} frames, and the most one conversion produces is ${maxFrames}.`);
  }

  return { ok: true, cols, rows, cellW, cellH, frames };
}

/** countFor is how many cells of size cell, separated by spacing, fit in
 *  span. It rounds down, so a span that does not tile exactly reports the
 *  count that does fit and resolveGrid then refuses the remainder. */
function countFor(span: number, cell: number, spacing: number): number {
  const n = Math.floor((span + spacing) / (cell + spacing));

  return n > 0 ? n : 1;
}

/** gridRects returns every frame's rectangle, row-major, for the overlay.
 *
 *  Capped: an overlay of two thousand rectangles is two thousand canvas
 *  operations on every keystroke, and past a few hundred the lines are closer
 *  together than a pixel anyway. The count shown beside the preview is the
 *  real one. */
export function gridRects(resolved: Resolved, g: Grid, limit = 400): Rect[] {
  if (!resolved.ok) {
    return [];
  }

  const out: Rect[] = [];

  for (let row = 0; row < resolved.rows; row++) {
    for (let col = 0; col < resolved.cols; col++) {
      if (out.length >= limit) {
        return out;
      }

      out.push({
        x: g.margin + col * (resolved.cellW + g.spacing),
        y: g.margin + row * (resolved.cellH + g.spacing),
        w: resolved.cellW,
        h: resolved.cellH,
      });
    }
  }

  return out;
}
