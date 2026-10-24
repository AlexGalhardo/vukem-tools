export type Format = "json" | "yaml" | "csv" | "xml";

type Obj = Record<string, unknown>;

/* ---------- YAML ---------- */

const plain = /^[A-Za-z_][\w .\-/]*$/;
const reserved = /^(true|false|null|yes|no|on|off|~)$/i;

function yamlScalar(v: unknown): string {
  if (v === null || v === undefined) return "null";
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  const s = String(v);
  if (s === "" || reserved.test(s) || !plain.test(s) || /^\s|\s$/.test(s) || !Number.isNaN(Number(s))) {
    return JSON.stringify(s);
  }

  return s;
}

/** toYaml writes a JSON-compatible value as block-style YAML. */
export function toYaml(value: unknown, depth = 0): string {
  const pad = "  ".repeat(depth);
  if (Array.isArray(value)) {
    if (value.length === 0) return `${pad}[]\n`;

    return value
      .map((item) => {
        if (item && typeof item === "object" && !(Array.isArray(item) && item.length === 0)) {
          const inner = toYaml(item, depth + 1);
          const trimmed = inner.slice(2 * (depth + 1));

          return `${pad}- ${trimmed}`;
        }

        return `${pad}- ${Array.isArray(item) ? "[]" : yamlScalar(item)}\n`;
      })
      .join("");
  }
  if (value && typeof value === "object") {
    const entries = Object.entries(value as Obj);
    if (entries.length === 0) return `${pad}{}\n`;

    return entries
      .map(([k, v]) => {
        const key = yamlScalar(k);
        if (v && typeof v === "object" && !(Array.isArray(v) ? v.length === 0 : Object.keys(v).length === 0)) {
          return `${pad}${key}:\n${toYaml(v, depth + 1)}`;
        }
        if (Array.isArray(v)) return `${pad}${key}: []\n`;
        if (v && typeof v === "object") return `${pad}${key}: {}\n`;

        return `${pad}${key}: ${yamlScalar(v)}\n`;
      })
      .join("");
  }

  return `${pad}${yamlScalar(value)}\n`;
}

function parseYamlScalar(raw: string): unknown {
  const s = raw.trim();
  if (s === "" || s === "~" || /^null$/i.test(s)) return null;
  if (/^(true|false)$/i.test(s)) return s.toLowerCase() === "true";
  if (/^[-+]?(\d+\.?\d*|\.\d+)([eE][-+]?\d+)?$/.test(s)) return Number(s);
  if (s.startsWith('"') || s.startsWith("[") || s.startsWith("{")) {
    try {
      return JSON.parse(s) as unknown;
    } catch {
      throw new Error(`cannot read the value ${s}`);
    }
  }
  if (s.startsWith("'") && s.endsWith("'")) return s.slice(1, -1).replace(/''/g, "'");

  return s;
}

interface YLine {
  indent: number;
  text: string;
}

/** fromYaml reads the block-style YAML subset configuration files use:
 *  mappings, sequences, comments, and quoted, flow or plain scalars. Anchors,
 *  tags, multi-line scalars and multiple documents are refused. */
export function fromYaml(text: string): unknown {
  const lines: YLine[] = [];
  for (const rawLine of text.replace(/\r\n/g, "\n").split("\n")) {
    if (/^\s*(#.*)?$/.test(rawLine) || rawLine.trim() === "---") continue;
    if (/[|>][+-]?\s*$/.test(rawLine) || /(^|\s)[&*!]\w/.test(rawLine.replace(/"[^"]*"/g, ""))) {
      throw new Error("anchors, tags and multi-line scalars are not supported");
    }
    const stripped = rawLine.replace(/\s+#.*$/, (m, off: number) => (rawLine.slice(0, off).includes('"') ? m : ""));
    lines.push({ indent: stripped.length - stripped.trimStart().length, text: stripped.trim() });
  }
  if (lines.length === 0) return null;

  let i = 0;
  const block = (indent: number): unknown => {
    if (lines[i]!.text.startsWith("- ") || lines[i]!.text === "-") return seq(indent);
    if (/^("[^"]*"|[^:]+?):(\s|$)/.test(lines[i]!.text)) return map(indent);

    return parseYamlScalar(lines[i++]!.text);
  };
  const child = (parent: number): unknown => {
    const next = lines[i];
    if (!next || next.indent <= parent) return null;

    return block(next.indent);
  };
  const seq = (indent: number): unknown[] => {
    const out: unknown[] = [];
    while (i < lines.length && lines[i]!.indent === indent && lines[i]!.text.startsWith("-")) {
      const rest = lines[i]!.text.slice(1).trim();
      if (rest === "") {
        i++;
        out.push(child(indent));
      } else if (/^("[^"]*"|[^:{["]+?):(\s|$)/.test(rest)) {
        lines[i] = { indent: indent + 2, text: rest };
        out.push(map(indent + 2));
      } else {
        i++;
        out.push(parseYamlScalar(rest));
      }
    }

    return out;
  };
  const map = (indent: number): Obj => {
    const out: Obj = {};
    while (i < lines.length && lines[i]!.indent === indent) {
      const m = /^("[^"]*"|[^:]+?):(?:\s+(.*))?$/.exec(lines[i]!.text);
      if (!m) throw new Error(`cannot read the line "${lines[i]!.text}"`);
      const key = m[1]!.startsWith('"') ? (JSON.parse(m[1]!) as string) : m[1]!;
      const rest = m[2];
      i++;
      if (rest === undefined || rest === "") {
        const next = lines[i];
        // A sequence may sit at the same indent as its key.
        if (next && next.indent === indent && next.text.startsWith("-")) out[key] = seq(indent);
        else out[key] = child(indent);
      } else {
        out[key] = parseYamlScalar(rest);
      }
    }

    return out;
  };

  const value = block(lines[0]!.indent);
  if (i < lines.length) throw new Error(`unexpected indentation near "${lines[i]!.text}"`);

  return value;
}

/* ---------- CSV ---------- */

/** parseCSV reads RFC 4180 CSV into rows; delimiter is one character. */
export function parseCSV(text: string, delimiter = ","): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let field = "";
  let quoted = false;
  const src = text.replace(/^﻿/, "");
  for (let i = 0; i < src.length; i++) {
    const c = src[i]!;
    if (quoted) {
      if (c === '"' && src[i + 1] === '"') {
        field += '"';
        i++;
      } else if (c === '"') {
        quoted = false;
      } else {
        field += c;
      }
    } else if (c === '"') {
      quoted = true;
    } else if (c === delimiter) {
      row.push(field);
      field = "";
    } else if (c === "\n" || c === "\r") {
      if (c === "\r" && src[i + 1] === "\n") i++;
      row.push(field);
      rows.push(row);
      row = [];
      field = "";
    } else {
      field += c;
    }
  }
  if (quoted) throw new Error("a quoted field is never closed");
  if (field !== "" || row.length > 0) {
    row.push(field);
    rows.push(row);
  }

  return rows;
}

function csvCell(v: unknown, delimiter: string): string {
  const s = v === null || v === undefined ? "" : typeof v === "object" ? JSON.stringify(v) : String(v);

  return s.includes('"') || s.includes("\n") || s.includes("\r") || s.includes(delimiter)
    ? `"${s.replace(/"/g, '""')}"`
    : s;
}

/** toCSV writes an array of objects as CSV, with the union of keys as header. */
export function toCSV(value: unknown, delimiter = ","): string {
  const rows = Array.isArray(value) ? value : [value];
  const header: string[] = [];
  for (const r of rows) {
    if (!r || typeof r !== "object" || Array.isArray(r)) throw new Error("CSV needs an array of objects");
    for (const k of Object.keys(r)) if (!header.includes(k)) header.push(k);
  }

  return [header, ...rows.map((r) => header.map((k) => (r as Obj)[k]))]
    .map((r) => r.map((c) => csvCell(c, delimiter)).join(delimiter))
    .join("\n");
}

/** fromCSV reads CSV with a header row into an array of objects. */
export function fromCSV(text: string, delimiter = ","): Obj[] {
  const [header, ...rows] = parseCSV(text, delimiter);
  if (!header) return [];

  return rows.map((r) => Object.fromEntries(header.map((h, i) => [h, r[i] ?? ""])));
}

/* ---------- XML ---------- */

const esc = (s: string): string =>
  s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");

const tag = (name: string): string => (/^[A-Za-z_][\w.-]*$/.test(name) ? name : "item");

function xmlNode(name: string, v: unknown, depth: number): string {
  const pad = "  ".repeat(depth);
  const n = tag(name);
  if (Array.isArray(v)) return v.map((x) => xmlNode(name, x, depth)).join("");
  if (v && typeof v === "object") {
    const o = v as Obj;
    const attrs = Object.entries(o)
      .filter(([k, x]) => k.startsWith("@") && typeof x !== "object")
      .map(([k, x]) => ` ${tag(k.slice(1))}="${esc(String(x))}"`)
      .join("");
    const kids = Object.entries(o).filter(([k]) => !k.startsWith("@") && k !== "#text");
    const text = o["#text"] === undefined ? "" : esc(String(o["#text"]));
    if (kids.length === 0) return `${pad}<${n}${attrs}>${text}</${n}>\n`;

    return `${pad}<${n}${attrs}>${text}\n${kids.map(([k, x]) => xmlNode(k, x, depth + 1)).join("")}${pad}</${n}>\n`;
  }

  return `${pad}<${n}>${v === null || v === undefined ? "" : esc(String(v))}</${n}>\n`;
}

/** toXML writes a value as XML. Keys starting with "@" become attributes and
 *  "#text" is the element's text, the convention fromXML reads back. */
export function toXML(value: unknown, root = "root"): string {
  return `<?xml version="1.0" encoding="UTF-8"?>\n${xmlNode(root, value, 0)}`;
}

const unesc = (s: string): string =>
  s.replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&quot;/g, '"').replace(/&apos;/g, "'").replace(/&amp;/g, "&");

/** fromXML reads XML into a value: repeated siblings become an array,
 *  attributes get an "@" prefix and mixed text is "#text". DTDs are refused. */
export function fromXML(text: string): unknown {
  if (/<!DOCTYPE|<!ENTITY/i.test(text)) throw new Error("DOCTYPE and entity declarations are not supported");
  const src = text.replace(/<\?[\s\S]*?\?>/g, "").replace(/<!--[\s\S]*?-->/g, "");
  const re = /<(\/?)([A-Za-z_][\w.:-]*)([^>]*?)(\/?)>|([^<]+)/g;
  const root: Obj = {};
  const stack: { name: string; node: Obj; text: string }[] = [{ name: "", node: root, text: "" }];
  let m: RegExpExecArray | null;
  while ((m = re.exec(src))) {
    const top = stack[stack.length - 1]!;
    if (m[5] !== undefined) {
      top.text += m[5];
      continue;
    }
    const [, closing, name, attrText, selfClosing] = m as unknown as [string, string, string, string, string];
    if (closing) {
      if (name !== top.name) throw new Error(`</${name}> does not close <${top.name}>`);
      stack.pop();
      const t = unesc(top.text.trim());
      const keys = Object.keys(top.node);
      const value: unknown = keys.length === 0 ? t : t ? { ...top.node, "#text": t } : top.node;
      attach(stack[stack.length - 1]!.node, name, value);
      continue;
    }
    const node: Obj = {};
    for (const a of attrText.matchAll(/([\w:.-]+)\s*=\s*("([^"]*)"|'([^']*)')/g)) {
      node[`@${a[1]}`] = unesc(a[3] ?? a[4] ?? "");
    }
    if (selfClosing) {
      attach(top.node, name, Object.keys(node).length ? node : "");
    } else {
      stack.push({ name, node, text: "" });
    }
  }
  if (stack.length !== 1) throw new Error(`<${stack[stack.length - 1]!.name}> is never closed`);

  const keys = Object.keys(root);
  if (keys.length !== 1) throw new Error("an XML document needs exactly one root element");

  return root[keys[0]!];
}

function attach(parent: Obj, name: string, value: unknown): void {
  if (!(name in parent)) parent[name] = value;
  else if (Array.isArray(parent[name])) (parent[name] as unknown[]).push(value);
  else parent[name] = [parent[name], value];
}

/* ---------- entry points ---------- */

/** parseAs reads text in a format into a JSON-compatible value. */
export function parseAs(format: Format, text: string): unknown {
  switch (format) {
    case "json":
      return JSON.parse(text) as unknown;
    case "yaml":
      return fromYaml(text);
    case "csv":
      return fromCSV(text);
    case "xml":
      return fromXML(text);
  }
}

/** writeAs serialises a value in a format. */
export function writeAs(format: Format, value: unknown): string {
  switch (format) {
    case "json":
      return JSON.stringify(value, null, 2);
    case "yaml":
      return toYaml(value);
    case "csv":
      return toCSV(value);
    case "xml":
      return toXML(value);
  }
}
