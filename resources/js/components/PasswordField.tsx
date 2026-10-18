import { type InputHTMLAttributes, useId, useState } from "react";

interface Props extends Omit<InputHTMLAttributes<HTMLInputElement>, "type" | "id"> {
  label: string;
  error?: string;
}

// An eye, and an eye with a line through it. Inline so the project does not
// pull an icon package in for two glyphs.
function EyeIcon({ hidden }: { hidden: boolean }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      className="size-5"
      aria-hidden="true"
    >
      <path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12Z" />
      <circle cx="12" cy="12" r="3" />
      {hidden && <path d="M4 20 20 4" />}
    </svg>
  );
}

// PasswordField is a labelled password input with a show/hide toggle inside
// the field, on the right.
//
// The label is bound with htmlFor rather than by wrapping, because wrapping
// puts the toggle button inside the label and its text joins the input's
// accessible name: the field then announces as "Password Show password".
//
// The toggle is a real button, so it is reachable by keyboard and announced;
// aria-pressed carries its state. Revealing a password is a deliberate act, so
// it never persists: every mount starts hidden.
export default function PasswordField({ label, error, ...input }: Props) {
  const [visible, setVisible] = useState(false);
  const id = useId();
  const errorId = `${id}-error`;

  return (
    <div className="block">
      <label htmlFor={id} className="font-medium">
        {label}
      </label>
      <div className="relative mt-1">
        <input
          {...input}
          id={id}
          type={visible ? "text" : "password"}
          aria-invalid={error ? true : undefined}
          aria-describedby={error ? errorId : undefined}
          className="block w-full rounded-md border border-zinc-300 bg-transparent px-3 py-2 pr-11 focus:border-indigo-500 focus:outline-none dark:border-zinc-700"
        />
        <button
          type="button"
          onClick={() => setVisible((v) => !v)}
          aria-label={visible ? "Hide password" : "Show password"}
          aria-pressed={visible}
          aria-controls={id}
          className="absolute inset-y-0 right-0 flex items-center rounded-r-md px-3 text-zinc-500 hover:text-zinc-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:hover:text-zinc-100"
        >
          <EyeIcon hidden={visible} />
        </button>
      </div>
      {error && (
        <p id={errorId} className="mt-1 text-sm text-red-600">
          {error}
        </p>
      )}
    </div>
  );
}
