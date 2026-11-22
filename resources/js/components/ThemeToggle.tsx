import { useEffect, useState } from "react";

import Icon, { type IconName } from "./Icon";
import { apply, read, resolve, watchSystem, type Choice } from "../lib/theme";

const options: { value: "light" | "dark"; icon: IconName; label: string }[] = [
  { value: "light", icon: "sun", label: "Light theme" },
  { value: "dark", icon: "moon", label: "Dark theme" },
];

/**
 * ThemeToggle offers the two themes.
 *
 * Following the system is still what an untouched install does — the module
 * below defaults to it, and resources/views/app.html applies it before first
 * paint — but it is no longer a button. Three controls where two will do was
 * the header's own clutter, and the one most people never press was the one
 * that describes the state they are already in: the control shows which theme
 * is on, and picking either one makes that a decision that sticks.
 *
 * It is a radio group rather than a cycling button, because a cycling button
 * never tells you which state you are in. Each option is a labelled radio, so
 * a screen reader announces the group, the current theme and the alternative.
 *
 * The two options are the sun and the moon, side by side in one row. The name
 * stays in the markup, hidden for sighted users, so a screen reader still
 * hears "Light theme" or "Dark theme"; a pointer also gets it as a tooltip.
 * Each icon is a 44px square, which is what a thumb needs.
 */
export default function ThemeToggle(): React.ReactElement {
  // Both start empty, which is what the markup must say for hydration to
  // match: the stored choice and the system preference are client-only. The
  // effect below reads them immediately afterwards, and the document class is
  // already correct — applied before paint — so the page does not flash.
  // Only this control is briefly unmarked.
  const [choice, setChoice] = useState<Choice | null>(null);
  const [dark, setDark] = useState<boolean | null>(null);

  useEffect(() => {
    const stored = read();

    setChoice(stored);
    setDark(resolve(stored));
  }, []);

  // While nothing is stored, a change to the system preference has to move
  // the document class and this control with it: the pre-paint script only
  // runs on a full load, and an Inertia app can live for hours without one.
  useEffect(() => {
    if (choice !== "system") {
      return;
    }

    return watchSystem(() => {
      const next = resolve("system");

      document.documentElement.classList.toggle("dark", next);
      setDark(next);
    });
  }, [choice]);

  function pick(value: "light" | "dark"): void {
    setChoice(apply(value));
    setDark(value === "dark");
  }

  /** on reports whether this option is the theme currently showing. */
  function on(value: "light" | "dark"): boolean {
    return dark !== null && dark === (value === "dark");
  }

  return (
    <fieldset className="flex items-center gap-1">
      <legend className="sr-only">Theme</legend>
      {options.map((option) => {
        const active = on(option.value);

        return (
          <label
            key={option.value}
            title={option.label}
            className={`flex size-11 cursor-pointer items-center justify-center rounded-field
              transition-colors duration-100 focus-within:outline-2 focus-within:outline-offset-2
              focus-within:outline-signal ${
                active ? "bg-signal-soft text-signal" : "text-ink-soft hover:bg-sunken hover:text-ink"
              }`}
          >
            <input
              type="radio"
              name="theme"
              value={option.value}
              checked={active}
              onChange={() => pick(option.value)}
              className="sr-only"
            />
            <Icon name={option.icon} className="size-5 shrink-0" />
            <span className="sr-only">{option.label}</span>
          </label>
        );
      })}
    </fieldset>
  );
}
