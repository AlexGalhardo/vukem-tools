import { Head, Link, useForm, usePage } from "@inertiajs/react";

import type { SharedProps } from "../../types";

interface Props {
  found: boolean;
  code: string;
  client?: string;
  device_name?: string;
  abilities?: string[];
  expires_at?: string;
}

/**
 * The page a person lands on from the Telegram bot's "Open the sign-in page"
 * button. It is where an account is connected to the bot: the bot holds a
 * secret that is worth nothing until somebody signed in says yes here, and the
 * short code is how they check this is the request in their own chat.
 */
export default function LinkPage({ found, code, device_name, abilities = [] }: Props): React.ReactElement {
  const { errors } = usePage<SharedProps>().props;
  const form = useForm({ code, decision: "" });

  const decide = (decision: "approve" | "deny"): void => {
    form.transform((data) => ({ ...data, decision }));
    form.post("/account/link");
  };

  if (!found) {
    return (
      <>
        <Head title="Connect Telegram" />
        <div className="max-w-[60ch]">
          <h1 className="text-3xl font-semibold tracking-tight">This code does not work</h1>
          <p className="mt-3 text-ink-soft">
            It is wrong, it expired, or somebody already used it. Codes last ten minutes. Go back to the chat and
            press the sign-in button again to get a new one.
          </p>
          <Link href="/account" className="mt-6 inline-block text-signal hover:underline">
            Back to your account
          </Link>
        </div>
      </>
    );
  }

  return (
    <>
      <Head title="Connect Telegram" />
      <div className="max-w-[60ch]">
        <h1 className="text-3xl font-semibold tracking-tight">Connect Telegram to your account</h1>
        <p className="mt-3 text-ink-soft">
          The bot is asking to act as you. Check that the code below is the one the bot showed you in the chat. If it
          is not, or you did not ask for this, choose Refuse.
        </p>

        <p className="mt-6 text-sm text-ink-faint">Code in your chat</p>
        <p className="tabular mt-1 text-4xl font-semibold tracking-[0.12em]" aria-label={`Code ${code}`}>
          {code}
        </p>
        {device_name ? <p className="mt-2 text-sm text-ink-soft">Asked by: {device_name}</p> : null}

        <h2 className="mt-8 text-lg font-medium">What it may do</h2>
        <ul className="mt-2 list-disc space-y-1 pl-5 text-sm text-ink-soft">
          {abilities.map((a) => (
            <li key={a}>
              <code className="font-mono">{a}</code>
            </li>
          ))}
        </ul>
        <p className="mt-3 text-sm text-ink-soft">
          Nothing else: it cannot change your password or your profile. The key lasts 30 days, and you can revoke it
          under API keys in your account at any time.
        </p>

        {errors?.code ? (
          <p role="alert" className="mt-4 rounded-panel border border-bad/30 bg-bad-soft px-4 py-2.5 text-sm text-bad">
            {errors.code}
          </p>
        ) : null}

        <div className="mt-8 flex flex-wrap gap-3">
          <button
            type="button"
            disabled={form.processing}
            onClick={() => decide("approve")}
            className="min-h-11 rounded-field bg-signal px-5 text-sm font-medium text-raised hover:opacity-90 disabled:opacity-60 dark:text-surface"
          >
            Connect
          </button>
          <button
            type="button"
            disabled={form.processing}
            onClick={() => decide("deny")}
            className="min-h-11 rounded-field border border-edge bg-raised px-5 text-sm font-medium text-ink hover:bg-sunken disabled:opacity-60"
          >
            Refuse
          </button>
        </div>
      </div>
    </>
  );
}
