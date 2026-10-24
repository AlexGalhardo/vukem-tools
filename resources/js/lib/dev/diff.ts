export type Op = "same" | "add" | "del";

export interface Line {
  op: Op;
  text: string;
}

/** diffLines is a line diff by longest common subsequence. The table is
 *  O(n·m), so inputs beyond maxCells fall back to "everything changed" rather
 *  than freezing the tab. */
export function diffLines(a: string, b: string, maxCells = 4_000_000): Line[] {
  const x = a === "" ? [] : a.split("\n");
  const y = b === "" ? [] : b.split("\n");
  // Trimming the shared head and tail keeps the table small for the common
  // case of a few edits in a long text.
  let head = 0;
  while (head < x.length && head < y.length && x[head] === y[head]) head++;
  let tail = 0;
  while (tail < x.length - head && tail < y.length - head && x[x.length - 1 - tail] === y[y.length - 1 - tail]) tail++;
  const xm = x.slice(head, x.length - tail);
  const ym = y.slice(head, y.length - tail);
  const out: Line[] = x.slice(0, head).map((text) => ({ op: "same", text }));

  if ((xm.length + 1) * (ym.length + 1) > maxCells) {
    out.push(...xm.map((text): Line => ({ op: "del", text })), ...ym.map((text): Line => ({ op: "add", text })));
  } else {
    const w = ym.length + 1;
    const t = new Uint32Array((xm.length + 1) * w);
    for (let i = xm.length - 1; i >= 0; i--) {
      for (let j = ym.length - 1; j >= 0; j--) {
        t[i * w + j] = xm[i] === ym[j] ? t[(i + 1) * w + j + 1]! + 1 : Math.max(t[(i + 1) * w + j]!, t[i * w + j + 1]!);
      }
    }
    let i = 0;
    let j = 0;
    while (i < xm.length && j < ym.length) {
      if (xm[i] === ym[j]) {
        out.push({ op: "same", text: xm[i]! });
        i++;
        j++;
      } else if (t[(i + 1) * w + j]! >= t[i * w + j + 1]!) {
        out.push({ op: "del", text: xm[i++]! });
      } else {
        out.push({ op: "add", text: ym[j++]! });
      }
    }
    while (i < xm.length) out.push({ op: "del", text: xm[i++]! });
    while (j < ym.length) out.push({ op: "add", text: ym[j++]! });
  }
  out.push(...x.slice(x.length - tail).map((text): Line => ({ op: "same", text })));

  return out;
}

export interface Change {
  path: string;
  kind: "added" | "removed" | "changed";
  before?: unknown;
  after?: unknown;
}

/** diffJSON compares two values key by key, so a reordered object is not a
 *  change and a moved line is not a rewrite. Arrays compare by index. */
export function diffJSON(a: unknown, b: unknown, path = "$"): Change[] {
  const obj = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);
  if (obj(a) && obj(b)) {
    const keys = [...new Set([...Object.keys(a), ...Object.keys(b)])];

    return keys.flatMap((k) => {
      const p = `${path}.${k}`;
      if (!(k in a)) return [{ path: p, kind: "added" as const, after: b[k] }];
      if (!(k in b)) return [{ path: p, kind: "removed" as const, before: a[k] }];

      return diffJSON(a[k], b[k], p);
    });
  }
  if (Array.isArray(a) && Array.isArray(b)) {
    const n = Math.max(a.length, b.length);

    return Array.from({ length: n }, (_, i) => {
      const p = `${path}[${i}]`;
      if (i >= a.length) return [{ path: p, kind: "added" as const, after: b[i] }];
      if (i >= b.length) return [{ path: p, kind: "removed" as const, before: a[i] }];

      return diffJSON(a[i], b[i], p);
    }).flat();
  }

  return JSON.stringify(a) === JSON.stringify(b) ? [] : [{ path, kind: "changed", before: a, after: b }];
}
