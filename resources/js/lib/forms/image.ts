import { type ToolForm } from "../toolForm";

/**
 * The image tools' forms, for the ones whose interface is a field list.
 *
 * image-convert is not here: it has its own page, because choosing a format
 * means reading what this build can actually write from the capability
 * endpoint, and a static choice list cannot do that.
 *
 * The modes here are the server's own, from internal/image. A Go test asserts
 * the two lists agree, so a mode added to the library without being offered —
 * or offered and not implemented — fails CI rather than shipping a select
 * whose values are refused.
 */
export const imageForms: Record<string, ToolForm> = {
  "image-compress": {
    files: "one",
    accept: ["image/png", "image/jpeg", "image/webp", "image/gif"],
    fields: [
      {
        name: "mode",
        label: "Mode",
        kind: "choice",
        choices: ["lossless", "lossy"],
        help: "Lossless keeps every pixel. Lossy trades some of them for a smaller file. Left empty: lossless for PNG and GIF, lossy for JPEG and WebP.",
      },
      {
        name: "quality",
        label: "Quality",
        kind: "number",
        min: 1,
        max: 100,
        placeholder: "the format's default",
        help: "Lossy only. Lower is smaller and softer; 80 is the default and is usually indistinguishable from the original.",
      },
      {
        name: "effort",
        label: "Effort",
        kind: "number",
        min: 1,
        max: 10,
        placeholder: "the encoder's highest",
        help: "How long the encoder may search for a smaller file. Higher is slower and smaller.",
      },
    ],
    note: "The format never changes — use Convert an image for that. Every setting worth trying is run and the smallest file wins, so a lossy compression can come back lossless when that was smaller. A file this encoder cannot beat comes back unchanged, and the conversion says so rather than handing you something larger. Metadata is always removed.",
  },
};
