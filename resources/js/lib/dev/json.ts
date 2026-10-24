/** ParseResult is either the parsed value or where parsing failed. */
export type ParseResult =
  | { ok: true; value: unknown }
  | { ok: false; message: string; line: number; column: number };

/** parseJSON parses text and, on failure, turns the engine's character offset
 *  into a line and column, which is what a person fixing the text needs. */
export function parseJSON(text: string): ParseResult {
  try {
    return { ok: true, value: JSON.parse(text) as unknown };
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    const at = /position (\d+)/.exec(message);
    const offset = at ? Number(at[1]) : text.length;
    const before = text.slice(0, offset);
    const line = before.split("\n").length;
    const column = offset - (before.lastIndexOf("\n") + 1) + 1;

    return { ok: false, message, line, column };
  }
}

/** formatJSON re-serialises text with the given indent; 0 minifies. */
export function formatJSON(text: string, indent: number): ParseResult & { text?: string } {
  const result = parseJSON(text);
  if (!result.ok) {
    return result;
  }

  return { ...result, text: JSON.stringify(result.value, null, indent || undefined) };
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
