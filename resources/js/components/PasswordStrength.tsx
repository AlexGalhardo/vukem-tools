// These mirror the server policy in app/requests/password.go exactly: length
// from PasswordMinLength to PasswordMaxLength, plus the four character
// classes. A form that promises different rules than the server enforces is
// worse than no hint at all, so a change there is a change here.
const MIN_LENGTH = 8;
const MAX_LENGTH = 64;

// The server counts runes: z.String().Min/.Max use utf8.RuneCountInString,
// while JavaScript's String.length counts UTF-16 code units. They disagree for
// anything outside the Basic Multilingual Plane — an emoji is one rune and two
// code units — so a naive mirror would tell someone their 8-emoji password is
// long enough when the server sees 8 runes, or refuse one the server accepts.
// Counting code points is what makes the two agree.
const runes = (value: string) => Array.from(value).length;

// A special character is anything that is not a letter, not a digit and not
// whitespace — the same definition the server uses. Unicode-aware, so an
// accented letter counts as a letter rather than as a symbol.
const hasUpper = (v: string) => /\p{Lu}/u.test(v);
const hasLower = (v: string) => /\p{Ll}/u.test(v);
const hasDigit = (v: string) => /\p{Nd}/u.test(v);
const hasSpecial = (v: string) => /[^\p{L}\p{N}\s]/u.test(v);

interface Props {
  password: string;
  confirmation?: string;
}

interface Rule {
  label: string;
  ok: boolean;
}

function requiredRules(password: string, confirmation?: string): Rule[] {
  const rules: Rule[] = [
    {
      label: `Between ${MIN_LENGTH} and ${MAX_LENGTH} characters`,
      ok: runes(password) >= MIN_LENGTH && runes(password) <= MAX_LENGTH,
    },
    { label: "An upper-case letter", ok: hasUpper(password) },
    { label: "A lower-case letter", ok: hasLower(password) },
    { label: "A digit", ok: hasDigit(password) },
    { label: "A special character", ok: hasSpecial(password) },
  ];

  // Only worth showing once the user has started confirming.
  if (confirmation) {
    rules.push({ label: "Both passwords match", ok: password === confirmation });
  }

  return rules;
}

// A longer password beats a clever one, so length carries three of the four
// points. This is advice, never a gate: a password that satisfies the
// checklist is accepted whatever the meter says, and a meter that refused what
// the server allows would simply be wrong.
function strength(password: string): number {
  if (!password) return 0;

  let score = 0;

  const length = runes(password);

  if (length >= MIN_LENGTH) score++;
  if (length >= 16) score++;
  if (length >= 24) score++;
  if (hasUpper(password) && hasLower(password) && hasDigit(password) && hasSpecial(password)) {
    score++;
  }

  return Math.min(score, 4);
}

const labels = ["", "Weak", "Fair", "Good", "Strong"];
const filled = ["", "bg-red-500", "bg-amber-500", "bg-lime-500", "bg-emerald-500"];
const empty = "bg-zinc-200 dark:bg-zinc-800";

function Mark({ ok }: { ok: boolean }) {
  return (
    <svg
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      className="mt-0.5 size-4 shrink-0"
      aria-hidden="true"
    >
      {ok ? <path d="M3 8.5 6.5 12 13 4.5" /> : <circle cx="8" cy="8" r="5.5" />}
    </svg>
  );
}

// PasswordStrength shows, while the user types, what the server requires and
// how strong the password is.
//
// The two are deliberately separate: the checklist is what will be accepted,
// the meter is advice. Conflating them is how a form ends up refusing a
// password the API would have taken.
export default function PasswordStrength({ password, confirmation }: Props) {
  const rules = requiredRules(password, confirmation);
  const score = strength(password);

  return (
    <div className="mt-2 space-y-2 text-sm">
      <div>
        <div className="flex items-center justify-between text-zinc-600 dark:text-zinc-400">
          <span>Strength</span>
          <span aria-live="polite">{labels[score]}</span>
        </div>
        <div className="mt-1 flex gap-1" aria-hidden="true">
          {[1, 2, 3, 4].map((step) => (
            <div
              key={step}
              className={`h-1 flex-1 rounded-full ${step <= score ? filled[score] : empty}`}
            />
          ))}
        </div>
      </div>
      <ul className="space-y-1">
        {rules.map((rule) => (
          <li
            key={rule.label}
            className={`flex gap-1.5 ${
              rule.ok
                ? "text-emerald-700 dark:text-emerald-400"
                : "text-zinc-600 dark:text-zinc-400"
            }`}
          >
            <Mark ok={rule.ok} />
            <span>{rule.label}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
