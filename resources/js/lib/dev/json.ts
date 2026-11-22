/** ParseResult is either the parsed value or where parsing failed. */
export type ParseResult =
  | { ok: true; value: unknown }
  | { ok: false; message: string; line: number; column: number };

/** A parsed document that keeps every number and string as it was written, so
 *  formatting never rounds 12345678901234567890 or turns 1.10 into 1.1. */
type Node =
  | { kind: "raw"; text: string }
  | { kind: "array"; items: Node[] }
  | { kind: "object"; entries: { key: string; raw: string; value: Node }[] };

class SyntaxFailure extends Error {
  constructor(
    message: string,
    readonly offset: number,
  ) {
    super(message);
  }
}

const maxDepth = 2000;
const numberPattern = /-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?/y;

class Scanner {
  private at = 0;

  constructor(private readonly text: string) {}

  document(): Node {
    this.space();
    const node = this.value(0);
    this.space();
    if (this.at < this.text.length) {
      this.fail("Unexpected token");
    }

    return node;
  }

  private fail(what: string): never {
    if (this.at >= this.text.length) {
      throw new SyntaxFailure("Unexpected end of JSON input", this.text.length);
    }

    throw new SyntaxFailure(`${what} '${this.text[this.at]}'`, this.at);
  }

  private space(): void {
    while (this.at < this.text.length && " \t\n\r".includes(this.text[this.at] as string)) {
      this.at++;
    }
  }

  private value(depth: number): Node {
    if (depth > maxDepth) {
      throw new SyntaxFailure("Nested too deeply", this.at);
    }
    const c = this.text[this.at];
    if (c === "{") return this.object(depth);
    if (c === "[") return this.array(depth);
    if (c === '"') return { kind: "raw", text: this.string() };
    for (const word of ["true", "false", "null"]) {
      if (this.text.startsWith(word, this.at)) {
        this.at += word.length;

        return { kind: "raw", text: word };
      }
    }
    numberPattern.lastIndex = this.at;
    const found = numberPattern.exec(this.text);
    if (found) {
      this.at += found[0].length;

      return { kind: "raw", text: found[0] };
    }

    return this.fail("Unexpected token");
  }

  private string(): string {
    const start = this.at;
    this.at++;
    while (this.at < this.text.length) {
      const c = this.text[this.at] as string;
      if (c === '"') {
        this.at++;

        return this.text.slice(start, this.at);
      }
      if (c < " ") {
        this.fail("Bad control character");
      }
      if (c === "\\") {
        this.at++;
        const e = this.text[this.at];
        if (e === "u") {
          if (!/^[0-9a-fA-F]{4}$/.test(this.text.slice(this.at + 1, this.at + 5))) {
            this.at++;
            this.fail("Bad Unicode escape at");
          }
          this.at += 4;
        } else if (e === undefined || !'"\\/bfnrt'.includes(e)) {
          this.fail("Bad escaped character");
        }
      }
      this.at++;
    }

    throw new SyntaxFailure("Unterminated string in JSON", this.text.length);
  }

  private array(depth: number): Node {
    this.at++;
    const items: Node[] = [];
    this.space();
    if (this.text[this.at] === "]") {
      this.at++;

      return { kind: "array", items };
    }
    for (;;) {
      this.space();
      items.push(this.value(depth + 1));
      this.space();
      const c = this.text[this.at];
      this.at++;
      if (c === "]") return { kind: "array", items };
      if (c !== ",") {
        this.at--;
        this.fail("Expected ',' or ']' but found");
      }
    }
  }

  private object(depth: number): Node {
    this.at++;
    const entries: { key: string; raw: string; value: Node }[] = [];
    this.space();
    if (this.text[this.at] === "}") {
      this.at++;

      return { kind: "object", entries };
    }
    for (;;) {
      this.space();
      if (this.text[this.at] !== '"') {
        this.fail("Expected a property name but found");
      }
      const raw = this.string();
      this.space();
      if (this.text[this.at] !== ":") {
        this.fail("Expected ':' but found");
      }
      this.at++;
      this.space();
      entries.push({ key: JSON.parse(raw) as string, raw, value: this.value(depth + 1) });
      this.space();
      const c = this.text[this.at];
      this.at++;
      if (c === "}") return { kind: "object", entries };
      if (c !== ",") {
        this.at--;
        this.fail("Expected ',' or '}' but found");
      }
    }
  }
}

function locate(text: string, offset: number): { line: number; column: number } {
  const before = text.slice(0, offset);

  return { line: before.split("\n").length, column: offset - (before.lastIndexOf("\n") + 1) + 1 };
}

function scan(text: string): { node: Node } | { failure: Extract<ParseResult, { ok: false }> } {
  try {
    return { node: new Scanner(text).document() };
  } catch (error) {
    if (error instanceof SyntaxFailure) {
      return { failure: { ok: false, message: error.message, ...locate(text, error.offset) } };
    }
    if (error instanceof RangeError) {
      return { failure: { ok: false, message: "Nested too deeply", line: 1, column: 1 } };
    }
    throw error;
  }
}

/** parseJSON parses text and, on failure, says where: the line and column of
 *  the first offending character, which is what a person fixing the text needs. */
export function parseJSON(text: string): ParseResult {
  const scanned = scan(text);
  if ("failure" in scanned) {
    return scanned.failure;
  }

  return { ok: true, value: JSON.parse(text) as unknown };
}

function byKey(a: { key: string }, b: { key: string }): number {
  return a.key < b.key ? -1 : a.key > b.key ? 1 : 0;
}

function enclose(open: string, items: string[], close: string, indent: number, level: number): string {
  if (items.length === 0) {
    return open + close;
  }
  if (!indent) {
    return open + items.join(",") + close;
  }
  const inner = " ".repeat(indent * (level + 1));

  return `${open}\n${inner}${items.join(`,\n${inner}`)}\n${" ".repeat(indent * level)}${close}`;
}

function write(node: Node, indent: number, sort: boolean, level: number): string {
  if (node.kind === "raw") {
    return node.text;
  }
  if (node.kind === "array") {
    return enclose("[", node.items.map((item) => write(item, indent, sort, level + 1)), "]", indent, level);
  }
  const entries = sort ? [...node.entries].sort(byKey) : node.entries;
  const items = entries.map((entry) => `${entry.raw}:${indent ? " " : ""}${write(entry.value, indent, sort, level + 1)}`);

  return enclose("{", items, "}", indent, level);
}

/** formatJSON re-serialises text with the given indent (0 minifies), keeping
 *  every number and string exactly as it was written. */
export function formatJSON(text: string, indent: number, sort = false): ParseResult & { text?: string } {
  const scanned = scan(text);
  if ("failure" in scanned) {
    return scanned.failure;
  }

  return { ok: true, value: undefined, text: write(scanned.node, indent, sort, 0) };
}

/** sortKeys returns a deep copy with every object's keys in order. */
export function sortKeys(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map(sortKeys);
  }
  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value as Record<string, unknown>)
        .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
        .map(([k, v]) => [k, sortKeys(v)]),
    );
  }

  return value;
}

function toValue(node: Node): unknown {
  if (node.kind === "array") {
    return node.items.map(toValue);
  }
  if (node.kind === "object") {
    return Object.fromEntries(node.entries.map((entry) => [entry.key, toValue(entry.value)]));
  }
  if (/^-?\d+$/.test(node.text) && !Number.isSafeInteger(Number(node.text))) {
    return BigInt(node.text);
  }

  return JSON.parse(node.text) as unknown;
}

/** parseJSONExact is parseJSON for comparing: an integer too large for a
 *  double stays a bigint, so two different IDs are never reported as equal. */
export function parseJSONExact(text: string): ParseResult {
  const scanned = scan(text);
  if ("failure" in scanned) {
    return scanned.failure;
  }

  return { ok: true, value: toValue(scanned.node) };
}

/** ExactNumber is a number kept as the digits it was written with, because a
 *  double would change them: 12345678901234567890, 1.10 and -0.0 do not
 *  survive Number(). Converters carry it through and print `raw`. */
export class ExactNumber {
  constructor(readonly raw: string) {}

  toString(): string {
    return this.raw;
  }
}

const jsonNumber = /^-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?$/;

/** exactOrNumber is a plain number when printing it gives the same digits
 *  back, and an ExactNumber when it would not. Text that is not a JSON number
 *  (a YAML `+5` or `.5`) is a plain number, since its raw form is not valid
 *  JSON to print. */
export function exactOrNumber(raw: string): number | ExactNumber {
  const n = Number(raw);

  return jsonNumber.test(raw) && String(n) !== raw ? new ExactNumber(raw) : n;
}

function toDigits(node: Node): unknown {
  if (node.kind === "array") {
    return node.items.map(toDigits);
  }
  if (node.kind === "object") {
    return Object.fromEntries(node.entries.map((entry) => [entry.key, toDigits(entry.value)]));
  }
  if (node.text === "true" || node.text === "false" || node.text === "null" || node.text.startsWith('"')) {
    return JSON.parse(node.text) as unknown;
  }

  return exactOrNumber(node.text);
}

/** parseJSONDigits is parseJSON for converters: a number that a double would
 *  change becomes an ExactNumber, so stringifyJSON prints it as written. */
export function parseJSONDigits(text: string): ParseResult {
  const scanned = scan(text);
  if ("failure" in scanned) {
    return scanned.failure;
  }

  return { ok: true, value: toDigits(scanned.node) };
}

function plainJSON(value: unknown, indent: number, level: number): string {
  if (value instanceof ExactNumber) {
    return value.raw;
  }
  if (Array.isArray(value)) {
    return enclose("[", value.map((item) => plainJSON(item ?? null, indent, level + 1)), "]", indent, level);
  }
  if (value !== null && typeof value === "object") {
    const items = Object.entries(value)
      .filter(([, v]) => v !== undefined)
      .map(([k, v]) => `${JSON.stringify(k)}:${indent ? " " : ""}${plainJSON(v, indent, level + 1)}`);

    return enclose("{", items, "}", indent, level);
  }

  return JSON.stringify(value) ?? "null";
}

/** stringifyJSON is JSON.stringify(value, null, indent) that prints an
 *  ExactNumber as the digits it was written with. */
export function stringifyJSON(value: unknown, indent = 0): string {
  return plainJSON(value, indent, 0);
}
