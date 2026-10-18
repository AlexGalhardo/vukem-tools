/**
 * The theme override.
 *
 * Three states, not two: "system" follows the operating system and keeps
 * following it when the user changes it, while "light" and "dark" are
 * decisions that stick. A two-state toggle cannot express "go back to
 * following my system", which is the state most people actually want.
 *
 * resources/views/app.html applies the stored choice before first paint; this
 * module only changes it afterwards, so the two must agree on the key and on
 * the default. They do: "theme", and dark when nothing is stored and the
 * system does not ask for light.
 */

/** Choice is what the user picked. */
export type Choice = "system" | "light" | "dark";

const KEY = "theme";

/** read returns the stored choice, defaulting to following the system. */
export function read(): Choice {
  try {
    const stored = localStorage.getItem(KEY);

    return stored === "light" || stored === "dark" ? stored : "system";
  } catch {
    // Private mode, or storage blocked. Following the system is the honest
    // answer when the override cannot be read.
    return "system";
  }
}

/** prefersDark reports what the operating system asks for. */
export function prefersDark(): boolean {
  return !window.matchMedia("(prefers-color-scheme: light)").matches;
}

/** resolve turns a choice into whether dark is applied. */
export function resolve(choice: Choice): boolean {
  return choice === "system" ? prefersDark() : choice === "dark";
}

/** apply writes the choice, reflects it on the document, and returns it.
 *  "system" removes the stored value rather than storing the current system
 *  answer, so a later change of the system preference is still followed. */
export function apply(choice: Choice): Choice {
  try {
    if (choice === "system") {
      localStorage.removeItem(KEY);
    } else {
      localStorage.setItem(KEY, choice);
    }
  } catch {
    // The class below still takes effect for this page; only persistence is
    // lost, which is the right thing to degrade.
  }

  document.documentElement.classList.toggle("dark", resolve(choice));

  return choice;
}

/** watchSystem calls fn whenever the system preference changes, and returns a
 *  function that stops watching. It is what makes "system" stay live rather
 *  than being read once at load. */
export function watchSystem(fn: () => void): () => void {
  const query = window.matchMedia("(prefers-color-scheme: light)");

  query.addEventListener("change", fn);

  return () => query.removeEventListener("change", fn);
}
