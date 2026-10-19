import { useCallback, useEffect, useId, useRef, useState, type ReactNode } from "react";

/**
 * A disclosure menu: a button that opens a panel of commands under itself.
 *
 * Authored rather than installed, which is the same call DESIGN.md records for
 * the icon set and for the same reason. `@base-ui/react` is in package.json
 * and its Menu is correct, but importing it put **46 kB gzipped** on the entry
 * chunk — the header is on every page, so none of it can be split off — and
 * the entry chunk was 4.2 kB. Twelve times the site's own JavaScript, to open
 * two panels on a page whose first viewport is a searchable index, is the
 * wrong trade on a phone. Base UI stays available for a component that earns
 * it: a combobox, a select, a slider.
 *
 * What that leaves this file owing, since a hand-rolled menu is usually where
 * accessibility quietly goes missing:
 *
 *   - `aria-haspopup`, `aria-expanded` and `aria-controls` on the trigger, so
 *     a screen reader announces a menu button rather than a button.
 *   - `role="menu"` on the panel and `role="menuitem"` on each row, applied by
 *     MenuItem rather than left to the caller.
 *   - Arrow keys, Home and End move between items; Escape closes; Tab closes
 *     and lets focus continue past the trigger, which is what a menu does and
 *     a dialog does not.
 *   - Focus returns to the trigger on close, but only when it was inside the
 *     panel: returning it after a click elsewhere on the page would be the
 *     menu taking focus back from whatever the person actually clicked.
 *   - A pointer down outside closes. Pointer down rather than click, so
 *     dragging a selection out of the panel does not leave it open.
 *
 * Deliberately not a dialog: it does not trap focus or block scrolling. A menu
 * hanging off a header button is a transient list of commands, and making it
 * modal would mean the page behind it stops responding to a scroll gesture
 * that a phone user has every reason to expect to work.
 */
export default function Menu({
  label,
  trigger,
  className,
  children,
}: {
  /** label names the button for a screen reader and for a tooltip. */
  label: string;
  /** trigger is what the button shows: an icon, usually. */
  trigger: ReactNode;
  /** className styles the button. */
  className?: string;
  children: ReactNode;
}): React.ReactElement {
  const [open, setOpen] = useState(false);
  const id = useId();

  const button = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);

  /** close puts focus back on the button when it is about to be lost with the
   *  panel, and leaves it alone otherwise. */
  const close = useCallback((restoreFocus: boolean) => {
    setOpen(false);

    if (restoreFocus) {
      button.current?.focus();
    }
  }, []);

  // A pointer down anywhere outside the button and the panel closes it.
  useEffect(() => {
    if (!open) {
      return;
    }

    const onPointerDown = (event: PointerEvent) => {
      const target = event.target as Node | null;

      if (
        target &&
        !panel.current?.contains(target) &&
        !button.current?.contains(target)
      ) {
        close(false);
      }
    };

    document.addEventListener("pointerdown", onPointerDown);

    return () => document.removeEventListener("pointerdown", onPointerDown);
  }, [close, open]);

  // Scrolling or resizing the page moves the header's button but not an
  // absolutely positioned panel's idea of where it should be. The panel is
  // positioned relative to the button, so only a resize can misplace it —
  // closing is both simpler and less surprising than re-measuring.
  useEffect(() => {
    if (!open) {
      return;
    }

    const onResize = () => close(false);

    window.addEventListener("resize", onResize);

    return () => window.removeEventListener("resize", onResize);
  }, [close, open]);

  /** items are the panel's focusable rows, in document order. */
  const items = useCallback(
    (): HTMLElement[] =>
      Array.from(panel.current?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? []),
    [],
  );

  /** focusItem moves focus within the panel, wrapping at both ends. */
  const focusItem = useCallback(
    (to: number | "first" | "last") => {
      const all = items();

      if (all.length === 0) {
        return;
      }

      const index =
        to === "first" ? 0 : to === "last" ? all.length - 1 : (to + all.length) % all.length;

      all[index]?.focus();
    },
    [items],
  );

  const onPanelKeyDown = (event: React.KeyboardEvent) => {
    const all = items();
    const at = all.indexOf(document.activeElement as HTMLElement);

    switch (event.key) {
      case "Escape":
        event.preventDefault();
        close(true);
        break;
      case "ArrowDown":
        event.preventDefault();
        focusItem(at + 1);
        break;
      case "ArrowUp":
        event.preventDefault();
        focusItem(at - 1);
        break;
      case "Home":
        event.preventDefault();
        focusItem("first");
        break;
      case "End":
        event.preventDefault();
        focusItem("last");
        break;
      case "Tab":
        // Not prevented: a menu lets Tab leave, which is the difference
        // between this and a dialog. It closes on the way out.
        close(false);
        break;
      default:
        break;
    }
  };

  const onTriggerKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      // Opening with an arrow key lands on an item, which is what makes the
      // menu usable without a pointer at all.
      event.preventDefault();
      setOpen(true);
      queueMicrotask(() => focusItem(event.key === "ArrowUp" ? "last" : "first"));
    }
  };

  return (
    <div className="relative">
      <button
        ref={button}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        aria-label={label}
        title={label}
        data-open={open ? "" : undefined}
        className={className}
        onClick={() => (open ? close(true) : setOpen(true))}
        onKeyDown={onTriggerKeyDown}
      >
        {trigger}
      </button>

      {open ? (
        <div
          ref={panel}
          id={id}
          role="menu"
          aria-label={label}
          tabIndex={-1}
          onKeyDown={onPanelKeyDown}
          // Right-aligned under the button: both menus live at the right end
          // of the header, so a left-aligned panel would hang off the screen
          // on a phone.
          className="absolute right-0 top-full z-50 mt-2 min-w-56 rounded-panel border border-edge
            bg-raised p-1 shadow-lifted"
        >
          {/* A click anywhere in the panel closes it, which covers every row
              without each one having to remember to. A row that must not
              close the menu — the theme radios — stops the event itself. */}
          <div onClick={() => close(false)}>{children}</div>
        </div>
      ) : null}
    </div>
  );
}

/** MenuItemClass is one row in a menu.
 *
 *  44px tall, which is the number that decides whether a menu works under a
 *  thumb, and the reason the rows look taller than their text. */
export const MenuItemClass =
  "flex min-h-11 w-full cursor-pointer items-center gap-3 rounded-field px-3 text-left text-sm " +
  "text-ink-soft transition-colors duration-100 hover:bg-sunken hover:text-ink " +
  "focus-visible:bg-sunken focus-visible:text-ink";

/** MenuSeparator divides groups of rows. */
export function MenuSeparator(): React.ReactElement {
  // Presentational: a separator announced as an item would be one more thing
  // for a screen reader to walk past on the way to the commands.
  return <div role="presentation" className="my-1 h-px bg-edge" />;
}
