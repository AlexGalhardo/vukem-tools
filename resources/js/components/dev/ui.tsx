import { useId, useState } from "react";
import type { ReactNode } from "react";

import Icon from "../Icon";

const field =
  "w-full rounded-field border border-edge bg-raised px-3 py-2 text-ink placeholder:text-ink-faint " +
  "focus:border-signal focus:outline-none";

export function Label({ children, htmlFor }: { children: ReactNode; htmlFor?: string }): React.ReactElement {
  return (
    <label htmlFor={htmlFor} className="label block">
      {children}
    </label>
  );
}

interface AreaProps {
  label: string;
  value: string;
  onChange: (value: string) => void;
  rows?: number;
  placeholder?: string;
}

/** Area is a monospace multi-line input. Spellcheck is off: this is code. */
export function Area({ label, value, onChange, rows = 10, placeholder }: AreaProps): React.ReactElement {
  const id = useId();

  return (
    <div>
      <Label htmlFor={id}>{label}</Label>
      <textarea
        id={id}
        value={value}
        rows={rows}
        placeholder={placeholder}
        spellCheck={false}
        autoCapitalize="off"
        autoCorrect="off"
        onChange={(e) => onChange(e.target.value)}
        className={`${field} mt-2 resize-y font-mono text-sm`}
      />
    </div>
  );
}

export function Text({
  label,
  value,
  onChange,
  placeholder,
  mono = true,
  inputMode,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  mono?: boolean;
  inputMode?: "numeric" | "text";
}): React.ReactElement {
  const id = useId();

  return (
    <div>
      <Label htmlFor={id}>{label}</Label>
      <input
        id={id}
        value={value}
        placeholder={placeholder}
        inputMode={inputMode}
        spellCheck={false}
        autoComplete="off"
        onChange={(e) => onChange(e.target.value)}
        className={`${field} mt-2 ${mono ? "font-mono text-sm" : ""}`}
      />
    </div>
  );
}

export function Pick<T extends string>({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: T;
  options: { value: T; label: string }[];
  onChange: (value: T) => void;
}): React.ReactElement {
  const id = useId();

  return (
    <div>
      <Label htmlFor={id}>{label}</Label>
      <select
        id={id}
        value={value}
        onChange={(e) => onChange(e.target.value as T)}
        className={`${field} mt-2 min-h-11`}
      >
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </div>
  );
}

export function Num({
  label,
  value,
  onChange,
  min,
  max,
  step,
}: {
  label: string;
  value: number;
  onChange: (value: number) => void;
  min?: number;
  max?: number;
  step?: number;
}): React.ReactElement {
  const id = useId();

  return (
    <div>
      <Label htmlFor={id}>{label}</Label>
      <input
        id={id}
        type="number"
        value={Number.isFinite(value) ? value : ""}
        min={min}
        max={max}
        step={step}
        onChange={(e) => onChange(e.target.valueAsNumber)}
        className={`${field} tabular mt-2 min-h-11`}
      />
    </div>
  );
}

export function Check({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: (value: boolean) => void;
}): React.ReactElement {
  return (
    <label className="flex min-h-11 items-center gap-2 text-sm text-ink">
      <input
        type="checkbox"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
        className="size-4 accent-[var(--color-signal)]"
      />
      {label}
    </label>
  );
}

export function Button({
  children,
  onClick,
  variant = "primary",
}: {
  children: ReactNode;
  onClick: () => void;
  variant?: "primary" | "quiet";
}): React.ReactElement {
  const look =
    variant === "primary"
      ? "bg-signal text-raised hover:opacity-90 dark:text-surface"
      : "border border-edge bg-raised text-ink hover:bg-sunken";

  return (
    <button
      type="button"
      onClick={onClick}
      className={`min-h-11 rounded-field px-4 text-sm font-medium transition-colors duration-100 ${look}`}
    >
      {children}
    </button>
  );
}

/** Copy writes text to the clipboard and says so. The clipboard API is absent
 *  on an insecure origin, so a failure shows rather than doing nothing. */
export function Copy({ text }: { text: string }): React.ReactElement {
  const [state, setState] = useState<"idle" | "done" | "failed">("idle");

  const copy = async (): Promise<void> => {
    try {
      await navigator.clipboard.writeText(text);
      setState("done");
    } catch {
      setState("failed");
    }
    setTimeout(() => setState("idle"), 1500);
  };

  return (
    <button
      type="button"
      onClick={() => void copy()}
      disabled={text === ""}
      className="inline-flex min-h-9 items-center gap-1.5 rounded-field border border-edge bg-raised px-3 text-xs
        font-medium text-ink-soft hover:bg-sunken hover:text-ink disabled:opacity-50"
    >
      <Icon name={state === "done" ? "check" : "document"} className="size-3.5" />
      {state === "done" ? "Copied" : state === "failed" ? "Copy failed" : "Copy"}
    </button>
  );
}

/** Output is a read-only result with a copy button. */
export function Output({ label, value, rows = 10 }: { label: string; value: string; rows?: number }): React.ReactElement {
  const id = useId();

  return (
    <div>
      <div className="flex items-end justify-between gap-2">
        <Label htmlFor={id}>{label}</Label>
        <Copy text={value} />
      </div>
      <textarea
        id={id}
        readOnly
        value={value}
        rows={rows}
        spellCheck={false}
        className={`${field} mt-2 resize-y bg-sunken font-mono text-sm`}
      />
    </div>
  );
}

/** Notice is a one-line status; an error is announced to screen readers. */
export function Notice({ kind, children }: { kind: "error" | "ok" | "warn"; children: ReactNode }): React.ReactElement {
  const look = {
    error: "border-bad/30 bg-bad-soft text-bad",
    ok: "border-ok/30 bg-ok-soft text-ok",
    warn: "border-warn/30 bg-warn-soft text-warn",
  }[kind];

  return (
    <p role={kind === "error" ? "alert" : "status"} className={`rounded-panel border px-4 py-2.5 text-sm ${look}`}>
      {children}
    </p>
  );
}

export function Grid({ children }: { children: ReactNode }): React.ReactElement {
  return <div className="grid gap-4 sm:grid-cols-2">{children}</div>;
}

export function Stack({ children }: { children: ReactNode }): React.ReactElement {
  return <div className="space-y-5">{children}</div>;
}

/** msg turns anything thrown into the sentence to show. */
export function msg(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
