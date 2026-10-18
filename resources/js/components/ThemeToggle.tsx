import { useEffect, useState } from "react";

import Icon, { type IconName } from "./Icon";
import { apply, read, resolve, watchSystem, type Choice } from "../lib/theme";

const options: { value: Choice; icon: IconName; label: string }[] = [
  { value: "system", icon: "monitor", label: "Follow the system theme" },
  { value: "light", icon: "sun", label: "Light theme" },
  { value: "dark", icon: "moon", label: "Dark theme" },
];

/**
 * ThemeToggle offers the three real states: follow the system, or override it
 * in either direction.
 *
 * It is a radio group rather than a cycling button, because a cycling button
 * never tells you which of the three you are in and takes up to two presses to
 * reach the one you want. Each option is a labelled radio, so a screen reader
 * announces the group, the current choice and the alternatives.
 */
export default function ThemeToggle(): React.ReactElement {
  // Starts as "system" on the server and on the first client render, which is
  // what the markup must say for hydration to match; the effect below reads
  // the real choice immediately afterwards. The document class is already
  // correct either way, applied before paint, so nothing flashes.
  const [choice, setChoice] = useState<Choice>("system");

  useEffect(() => setChoice(read()), []);

  // While following the system, a change to the system preference has to move
  // the document class: the pre-paint script only runs on a full load, and an
  // Inertia app can live for hours without one.
  useEffect(() => {
    if (choice !== "system") {
      return;
    }

    return watchSystem(() => {
      document.documentElement.classList.toggle("dark", resolve("system"));
    });
  }, [choice]);

  return (
    <fieldset className="flex items-center rounded-field border border-edge p-0.5">
      <legend className="sr-only">Theme</legend>
      {options.map((option) => {
        const active = choice === option.value;

        return (
          <label
            key={option.value}
            title={option.label}
            className={`flex size-7 cursor-pointer items-center justify-center rounded-[0.3rem] transition-colors duration-100 ${
              active ? "bg-signal-soft text-signal" : "text-ink-faint hover:text-ink"
            }`}
          >
            <input
              type="radio"
              name="theme"
              value={option.value}
              checked={active}
              onChange={() => setChoice(apply(option.value))}
              className="sr-only"
            />
            <Icon name={option.icon} className="size-4" label={option.label} />
          </label>
        );
      })}
    </fieldset>
  );
}
