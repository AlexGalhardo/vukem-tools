import { expect, test } from "bun:test";

import { resolveGrid, type Grid } from "./spriteGrid";

/**
 * The shared table.
 *
 * Every row is an answer taken from internal/spritesheet/grid.go, and
 * tests/sprite_grid_table_test.go runs the same rows against the Go
 * implementation. The two are therefore pinned to one written-down set of
 * answers rather than to each other, so a change to either that is not matched
 * by the other fails here or there.
 *
 * Keep the two tables identical. The Go one carries the same comment.
 */
const table: { name: string; w: number; h: number; g: Partial<Grid>; frames: number | "error" }[] = [
  { name: "cell size tiles exactly", w: 128, h: 64, g: { cellW: 32, cellH: 32 }, frames: 8 },
  { name: "counts tile exactly", w: 128, h: 64, g: { cols: 4, rows: 2 }, frames: 8 },
  { name: "a margin on every edge", w: 132, h: 68, g: { cellW: 32, cellH: 32, margin: 2 }, frames: 8 },
  { name: "spacing between cells only", w: 134, h: 66, g: { cellW: 32, cellH: 32, spacing: 2 }, frames: 8 },
  { name: "a margin and a spacing", w: 138, h: 70, g: { cellW: 32, cellH: 32, margin: 2, spacing: 2 }, frames: 8 },
  { name: "a cell that leaves a remainder", w: 100, h: 100, g: { cellW: 32, cellH: 32 }, frames: "error" },
  { name: "both a size and a count", w: 128, h: 64, g: { cellW: 32, cellH: 32, cols: 4 }, frames: "error" },
  { name: "neither a size nor a count", w: 128, h: 64, g: {}, frames: "error" },
  { name: "a cell wider than the sheet", w: 128, h: 64, g: { cellW: 200, cellH: 32 }, frames: "error" },
  { name: "a margin that eats the sheet", w: 128, h: 64, g: { cellW: 32, cellH: 32, margin: 70 }, frames: "error" },
  { name: "a thousand frames", w: 4096, h: 4096, g: { cellW: 128, cellH: 128 }, frames: 1024 },
  { name: "past the frame cap", w: 4096, h: 4096, g: { cellW: 64, cellH: 64 }, frames: "error" },
  { name: "a single row", w: 96, h: 32, g: { cols: 3, rows: 1 }, frames: 3 },
  { name: "counts whose spacing leaves a remainder", w: 128, h: 64, g: { cols: 4, rows: 2, spacing: 2 }, frames: "error" },
  { name: "counts with a margin and a spacing", w: 140, h: 72, g: { cols: 4, rows: 2, margin: 2, spacing: 4 }, frames: 8 },
];

const zero: Grid = { cellW: 0, cellH: 0, cols: 0, rows: 0, margin: 0, spacing: 0 };

for (const row of table) {
  test(`resolveGrid: ${row.name}`, () => {
    const got = resolveGrid(row.w, row.h, { ...zero, ...row.g });

    if (row.frames === "error") {
      expect(got.ok).toBe(false);
      expect(got.why ?? "").not.toBe("");

      return;
    }

    expect(got.why ?? "").toBe("");
    expect(got.ok).toBe(true);
    expect(got.frames).toBe(row.frames);
  });
}

test("the counts with a margin and a spacing resolve to the cell Go computes", () => {
  const got = resolveGrid(140, 72, { ...zero, cols: 4, rows: 2, margin: 2, spacing: 4 });

  // Integer division, as Go does it: 124/4 is 31, not 31.
  expect(got.cellW).toBe(31);
  expect(got.cellH).toBe(32);
});
