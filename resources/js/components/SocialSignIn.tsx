import { usePage } from "@inertiajs/react";
import type { ReactNode } from "react";

import type { SharedProps } from "../types";

// Marks for the providers this app offers. Each provider the server reports as
// configured must have one here, or the button renders without its logo.
const marks: Record<string, { label: string; icon: ReactNode }> = {
  google: {
    label: "Google",
    icon: (
      <svg viewBox="0 0 18 18" className="size-5" aria-hidden="true">
        <path
          fill="#4285F4"
          d="M17.64 9.2c0-.64-.06-1.25-.16-1.84H9v3.48h4.84a4.14 4.14 0 0 1-1.8 2.72v2.26h2.92c1.7-1.57 2.68-3.88 2.68-6.62Z"
        />
        <path
          fill="#34A853"
          d="M9 18c2.43 0 4.47-.8 5.96-2.18l-2.92-2.26c-.8.54-1.84.86-3.04.86-2.34 0-4.32-1.58-5.03-3.7H.96v2.33A9 9 0 0 0 9 18Z"
        />
        <path
          fill="#FBBC05"
          d="M3.97 10.72a5.4 5.4 0 0 1 0-3.44V4.95H.96a9 9 0 0 0 0 8.1l3.01-2.33Z"
        />
        <path
          fill="#EA4335"
          d="M9 3.58c1.32 0 2.5.45 3.44 1.35l2.58-2.59C13.46.89 11.43 0 9 0A9 9 0 0 0 .96 4.95l3.01 2.33C4.68 5.16 6.66 3.58 9 3.58Z"
        />
      </svg>
    ),
  },
};

// SocialSignIn renders a button per configured provider, and nothing at all
// when none is configured.
//
// The list comes from the server, which reports only the providers that have
// credentials, so a deploy without SOCIAL_GOOGLE_CLIENT_ID shows no button
// rather than one that fails on click.
export default function SocialSignIn({ action }: { action: string }) {
  const { socialProviders, passwordAuth } = usePage<SharedProps>().props;
  const providers = (socialProviders ?? []).filter((name) => name in marks);

  if (providers.length === 0) return null;

  return (
    <>
      {/* The divider separates this from the password form. With password
          sign-in off there is nothing to separate it from. */}
      {passwordAuth !== false && (
        <div className="my-6 flex items-center gap-3 text-sm text-zinc-500">
          <span className="h-px flex-1 bg-zinc-200 dark:bg-zinc-800" />
          <span>or</span>
          <span className="h-px flex-1 bg-zinc-200 dark:bg-zinc-800" />
        </div>
      )}
      <div className="space-y-2">
        {providers.map((name) => (
          <a
            key={name}
            href={`/auth/${name}/redirect`}
            className="flex w-full items-center justify-center gap-2 rounded-md border border-zinc-300 px-4 py-2 font-medium hover:bg-zinc-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-500 dark:border-zinc-700 dark:hover:bg-zinc-900"
          >
            {marks[name].icon}
            <span>
              {action} with {marks[name].label}
            </span>
          </a>
        ))}
      </div>
    </>
  );
}
