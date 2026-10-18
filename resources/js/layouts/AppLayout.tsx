import { Link, usePage } from "@inertiajs/react";
import type { ReactNode } from "react";

import Icon from "../components/Icon";
import ThemeToggle from "../components/ThemeToggle";
import type { SharedProps } from "../types";

/**
 * The shell every page sits in.
 *
 * It is deliberately thin: this is an Operate surface, so the chrome gets out
 * of the way of the task. The header holds the mark, the account, and the
 * theme; the footer holds the one thing a visitor legally needs to find, which
 * is the licence notice.
 */
export default function AppLayout({ children }: { children: ReactNode }): React.ReactElement {
  const page = usePage<SharedProps>();
  const { auth } = page.props;
  const message = page.flash?.success ?? page.flash?.status;

  return (
    <div className="flex min-h-dvh flex-col">
      {/* Keyboard users reach the content without walking the header. */}
      <a
        href="#main"
        className="sr-only focus-visible:not-sr-only focus-visible:absolute focus-visible:left-4 focus-visible:top-4 focus-visible:z-50 focus-visible:rounded-field focus-visible:bg-raised focus-visible:px-3 focus-visible:py-2 focus-visible:shadow-lifted"
      >
        Skip to content
      </a>

      <header className="sticky top-0 z-40 border-b border-edge bg-surface/85 backdrop-blur-sm">
        <div className="mx-auto flex h-14 max-w-5xl items-center gap-4 px-4">
          <Link
            href="/"
            className="flex items-center gap-2 font-semibold tracking-tight text-ink"
            aria-label={`${page.props.appName} home`}
          >
            <Icon name="trace" className="size-5 text-signal" />
            <span>{page.props.appName}</span>
          </Link>

          <div className="flex-1" />

          <nav className="flex items-center gap-1 text-sm">
            {auth?.user ? (
              <>
                <Link href="/dashboard" className={navLink}>
                  {auth.user.name}
                </Link>
                <Link href="/logout" method="post" as="button" className={navLink}>
                  Log out
                </Link>
              </>
            ) : auth ? (
              <>
                <Link href="/login" className={navLink}>
                  Log in
                </Link>
                <Link
                  href="/register"
                  className="rounded-field bg-signal px-3 py-1.5 font-medium text-raised transition-colors duration-100 hover:opacity-90 dark:text-surface"
                >
                  Create an account
                </Link>
              </>
            ) : null}
          </nav>

          <ThemeToggle />
        </div>
      </header>

      <main id="main" className="mx-auto w-full max-w-5xl flex-1 px-4 py-10">
        {typeof message === "string" && (
          <p
            role="status"
            className="mb-8 flex items-start gap-2 rounded-panel border border-ok/30 bg-ok-soft px-4 py-3 text-sm text-ink"
          >
            <Icon name="check" className="mt-0.5 size-4 shrink-0 text-ok" />
            <span>{message}</span>
          </p>
        )}
        {children}
      </main>

      <footer className="border-t border-edge">
        <div className="mx-auto flex max-w-5xl flex-col gap-3 px-4 py-6 text-sm text-ink-soft sm:flex-row sm:items-center sm:justify-between">
          <p>
            Open source, self-hosted, MIT.{" "}
            <Link href="/licenses" className="text-signal hover:underline">
              Every engine&rsquo;s licence
            </Link>{" "}
            is declared &mdash; some are copyleft.
          </p>
          <a
            href="https://github.com/AlexGalhardo/vukem-tools"
            className="flex items-center gap-1.5 text-signal hover:underline"
          >
            Source
            <Icon name="external" className="size-3.5" />
          </a>
        </div>
      </footer>
    </div>
  );
}

const navLink =
  "rounded-field px-3 py-1.5 text-ink-soft transition-colors duration-100 hover:bg-sunken hover:text-ink";
