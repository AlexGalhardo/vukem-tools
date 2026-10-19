import type { SVGProps } from "react";

/**
 * The icon set, authored rather than installed.
 *
 * There are few enough icons that a package would cost more than it saves, and
 * DESIGN.md asks for one consistent stroke: every path below is drawn on the
 * same 24px grid at 1.75 stroke with round caps and joins, so nothing is
 * heavier or lighter than its neighbour. No emoji and no icon font stand in
 * for one of these.
 */
const paths = {
  /** The project mark: a trace stepping up, like a signal on an instrument. */
  trace: "M3 17.5 8 17.5 8 11 13 11 13 6.5 21 6.5",
  search: "M10.5 17a6.5 6.5 0 1 0 0-13 6.5 6.5 0 0 0 0 13ZM15.2 15.2 20 20",
  /** Pixel art to vector: a stepped edge becoming a curve. */
  vector: "M4 20V13h7V6h9M4 20h7M11 13h9",
  image: "M4 5h16v14H4zM4 15l4.5-4.5L13 15M14.5 12.5 20 18M15.5 9h.01",
  video: "M4 6h11v12H4zM15 10.5 20 7.5v9l-5-3z",
  audio: "M4 14V10h3l4-3.5v11L7 14H4zM15 9.5a4 4 0 0 1 0 5M18 7a7.5 7.5 0 0 1 0 10",
  document: "M6 3h7l5 5v13H6zM13 3v5h5",
  archive: "M4 7h16v13H4zM4 7l2-3h12l2 3M10 11h4",
  /** A sprite sheet: a frame grid. Three by two rather than a square, so it
   *  reads as a sheet of frames rather than as a layout or a table. */
  grid: "M3.5 5h17v14h-17zM9.17 5v14M14.83 5v14M3.5 12h17",
  download: "M12 4v11M7.5 10.5 12 15l4.5-4.5M5 19h14",
  upload: "M12 16V5M7.5 9.5 12 5l4.5 4.5M5 19h14",
  close: "M6 6l12 12M18 6 6 18",
  check: "M5 12.5 9.5 17 19 7",
  alert: "M12 4 2.5 20h19L12 4ZM12 10v4.5M12 17.5h.01",
  clock: "M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18ZM12 7.5V12l3.5 2",
  sun: "M12 16.5a4.5 4.5 0 1 0 0-9 4.5 4.5 0 0 0 0 9ZM12 2v2M12 20v2M4.2 4.2l1.4 1.4M18.4 18.4l1.4 1.4M2 12h2M20 12h2M4.2 19.8l1.4-1.4M18.4 5.6l1.4-1.4",
  moon: "M20 14.5A8.5 8.5 0 0 1 9.5 4a8.5 8.5 0 1 0 10.5 10.5Z",
  monitor: "M3 5h18v11H3zM8.5 20h7M12 16v4",
  key: "M15.5 3a5.5 5.5 0 0 1 2.9 10.2L17 14.6l-1.5-1.5-2 2-2-2-7 7v-3l7-7A5.5 5.5 0 0 1 15.5 3Z",
  /** Compression: two edges pressed toward a line.
   *
   *  This replaces `scale`, which the three compression tools used and which
   *  was drawn as a waste bin — so every "compress" tool on the index showed
   *  a delete icon. One name was carrying two meanings and was drawn as
   *  neither; this is one of them and `sliders` is the other. */
  compress: "M12 3v6M9 6.5 12 9.5 15 6.5M12 21v-6M9 17.5 12 14.5 15 17.5M3.5 12h17",
  /** A panel of settings: two tracks with a handle on each. The other half
   *  of what `scale` was being asked to mean, on the option panels. */
  sliders: "M4 8h7M15 8h5M4 16h3M11 16h9M13 5.5v5M9 13.5v5",
  external: "M14 4h6v6M20 4l-8 8M18 14v6H4V6h6",
  /** The navigation menu, at the one weight the rest of the set uses: three
   *  rules on the 24px grid rather than the heavier three-bar glyph an icon
   *  font would supply. */
  menu: "M4 7h16M4 12h16M4 17h16",
  /** A person, for the signed-in account. Head and shoulders, which is the
   *  plain reading of "the account" and needs no avatar image to stand in
   *  for a name. */
  user: "M12 11.5a3.75 3.75 0 1 0 0-7.5 3.75 3.75 0 0 0 0 7.5ZM4.75 20a7.25 7.25 0 0 1 14.5 0",
  /** A disclosure chevron, pointing down: the menu opens below its trigger. */
  chevron: "M7.5 10.5 12 15l4.5-4.5",
  /** Leaving: a door with the way out drawn through it. Its own glyph rather
   *  than `external`, which is an arrow leaving the page and reads as "opens
   *  in a new tab" — the wrong promise on the control that ends a session. */
  signout: "M14 4h4a1 1 0 0 1 1 1v14a1 1 0 0 1-1 1h-4M10.5 15.5 14 12l-3.5-3.5M14 12H4",
} as const;

/** Name is an icon in the set. */
export type IconName = keyof typeof paths;

interface Props extends Omit<SVGProps<SVGSVGElement>, "children"> {
  name: IconName;
  /** Label makes the icon meaningful on its own, for an icon-only control.
   *  Leaving it out marks the icon decorative, which is right beside text. */
  label?: string;
}

/** Icon draws one icon from the set. */
export default function Icon({ name, label, className, ...rest }: Props): React.ReactElement {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className ?? "size-5"}
      role={label ? "img" : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
      {...rest}
    >
      <path d={paths[name]} />
    </svg>
  );
}

/** Names returns every icon in the set, for a test that asserts each one is
 *  drawn on the same grid. */
export function iconNames(): IconName[] {
  return Object.keys(paths) as IconName[];
}
