/**
 * English formatting for a product that has no i18n layer.
 *
 * The reader's zone stays local. Only the words and the digit grouping are
 * pinned, so an operating-system locale cannot render "18 de nov." or "44.100".
 */

/** englishDateTime renders a timestamp in English, in the reader's zone. */
export function englishDateTime(iso: string | null): string {
  if (!iso) {
    return "never";
  }

  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) {
    return "unknown";
  }

  return at.toLocaleString("en", {
    dateStyle: "medium",
    timeStyle: "short",
  });
}

/** englishInteger groups a whole number in English, so 44100 reads as "44,100". */
export function englishInteger(value: number): string {
  return value.toLocaleString("en");
}
