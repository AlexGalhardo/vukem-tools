/** typedNumber is the value a half-typed number field may pass on: a finite
 *  number already inside [min, max], else null. Anything else stays a draft,
 *  because clamping "2" up to a minimum of 4 on the first keystroke makes
 *  "24" impossible to type. */
export function typedNumber(raw: string, min?: number, max?: number): number | null {
  const n = Number(raw);
  if (raw.trim() === "" || !Number.isFinite(n)) return null;
  if ((min !== undefined && n < min) || (max !== undefined && n > max)) return null;

  return n;
}

/** settledNumber is the value a number field takes when the person leaves it:
 *  the draft clamped into [min, max], or the lower bound when it is empty. */
export function settledNumber(raw: string, min?: number, max?: number): number {
  const n = Number(raw);
  const base = raw.trim() === "" || !Number.isFinite(n) ? (min ?? 0) : n;

  return Math.min(max ?? Infinity, Math.max(min ?? -Infinity, base));
}
