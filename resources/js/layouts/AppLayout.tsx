import { Link, usePage } from "@inertiajs/react";
import type { ReactNode } from "react";

import Icon from "../components/Icon";
import Navbar from "../components/Navbar";
import type { SharedProps } from "../types";

/**
 * The shell every page sits in.
 *
 * It is deliberately thin: this is an Operate surface, so the chrome gets out
 * of the way of the task. The header is its own component because it now has
 * two layouts and two menus; the footer holds the one thing a visitor legally
 * needs to find, which is the licence notice.
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

      <Navbar />

      <main id="main" className="edge-safe mx-auto w-full max-w-5xl flex-1 py-10">
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
        <div className="edge-safe mx-auto flex max-w-5xl flex-col gap-3 py-6 text-sm text-ink-soft sm:flex-row sm:items-center sm:justify-between">
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
