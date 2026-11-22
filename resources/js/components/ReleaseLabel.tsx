import { release } from "../release";

/**
 * The release being run, fixed in the bottom-right corner of every screen so a
 * tester always knows which build they are looking at.
 *
 * It takes no pointer events, so the control under it stays clickable, and it
 * sits below the menu, the drawer and the search (z-30 and up) so it never
 * covers them. The padding follows the safe-area insets for a phone with a
 * home bar or a notch.
 */
export default function ReleaseLabel(): React.ReactElement {
  return (
    <aside
      data-release-label
      className="pointer-events-none fixed right-0 bottom-0 z-20 select-none rounded-tl-field bg-surface/90 px-2.5 py-1 text-xs tabular-nums text-ink-soft print:hidden"
      style={{
        paddingBottom: "max(0.25rem, env(safe-area-inset-bottom, 0px))",
        paddingRight: "max(0.625rem, env(safe-area-inset-right, 0px))",
      }}
    >
      <span className="sr-only">Release </span>
      {`v${release}`}
    </aside>
  );
}
