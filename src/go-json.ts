/**
 * Compact JSON matching Go's encoding/json defaults so content-addressed
 * hashes of existing ~/.vox runs stay stable across the rewrite.
 *
 * Go HTML-escapes &, <, >. Field order is the caller's responsibility.
 */
export function goJSON(value: unknown): string {
  return JSON.stringify(value)
    .replaceAll("&", "\\u0026")
    .replaceAll("<", "\\u003c")
    .replaceAll(">", "\\u003e");
}

/** Byte-wise UTF-8 order, matching Go's sort.Strings. */
export function sortUtf8(values: string[]): string[] {
  return [...values].sort((left, right) =>
    Buffer.compare(Buffer.from(left, "utf8"), Buffer.from(right, "utf8")),
  );
}
