import { Link, usePage } from "@inertiajs/react";

import Icon from "./Icon";
import Menu, { MenuItemClass, MenuSeparator } from "./Menu";
import ThemeToggle from "./ThemeToggle";
import type { SharedProps } from "../types";

/**
 * The site header.
 *
 * Two layouts rather than one that shrinks. On a wide screen the navigation is
 * inline, which is the fastest thing to scan with a mouse; on a phone it
 * collapses into one menu button, because four 28px links in a row is the
 * shape that makes a header unusable with a thumb — and that was the previous
 * header's real failing, not its width.
 *
 * Every control a finger has to hit is at least 44px square. That is the one
 * number that decides whether a header works on a phone, and it is why the
 * icon buttons here look larger than their icons.
 */
export default function Navbar(): React.ReactElement {
  const page = usePage<SharedProps>();
  const { auth, appName } = page.props;

  return (
    <header className="sticky top-0 z-40 border-b border-edge bg-surface/85 backdrop-blur-sm">
      <div className="edge-safe mx-auto flex h-14 max-w-5xl items-center gap-1">
        <Link
          href="/"
          className="-ml-2 flex min-h-11 items-center gap-2 rounded-field px-2 font-semibold
            tracking-tight text-ink"
          aria-label={`${appName} home`}
        >
          <Icon name="trace" className="size-5 shrink-0 text-signal" />
          {/* The configured name on a wide screen; on a phone, "Tools".
              The header there has room for one word beside the menu button,
              and the brand is already being carried by the mark to its left —
              so the word is better spent naming the place than repeating the
              brand. Deriving it from appName was tried and dropped: the last
              word of a one-word APP_NAME is that word, so the phone header
              read "Vukem", which names the company and not the page. */}
          <span className="hidden sm:inline">{appName}</span>
          <span className="sm:hidden">Tools</span>
        </Link>

        <div className="flex-1" />

        {/* The wide layout: the pages inline, then the theme, then the
            account. All of it hidden on a phone, where the menu button below
            carries the same things. */}
        <nav aria-label="Main" className="hidden items-center gap-1 text-sm sm:flex">
          {pages.map((item) => (
            <Link key={item.href} href={item.href} className={deskLink}>
              {item.label}
            </Link>
          ))}
        </nav>

        <div className="mx-1 hidden sm:block">
          <ThemeToggle />
        </div>

        {auth?.user ? <AccountMenu name={auth.user.name} email={auth.user.email} /> : null}

        {/* One button, not two. Signing up was the second one, and it is a
            step past signing in rather than an alternative to it: the sign-in
            page already carries the link, so the header spends its last
            44px on the control that the people who come back every day
            need. */}
        {!auth?.user && auth ? (
          <Link
            href="/login"
            className="hidden min-h-11 items-center rounded-field bg-signal px-4 text-sm font-medium
              text-raised transition-colors duration-100 hover:opacity-90 sm:flex dark:text-surface"
          >
            Log in
          </Link>
        ) : null}

        <div className="sm:hidden">
          <MobileMenu signedIn={Boolean(auth?.user)} canSignIn={Boolean(auth)} />
        </div>
      </div>
    </header>
  );
}

/** The site's own pages, in the order the header lists them.
 *
 *  Three, and each one already existed and was reachable only from the footer
 *  or from nowhere at all: the API reference had no link in the chrome. */
const pages = [
  { href: "/", label: "Tools" },
  { href: "/docs", label: "API" },
  { href: "/licenses", label: "Licences" },
];

const deskLink =
  "flex min-h-11 items-center rounded-field px-3 text-ink-soft transition-colors duration-100 " +
  "hover:bg-sunken hover:text-ink";

/** iconButton is the look of the two menu triggers: a 44px square that shows
 *  it is pressed while its panel is open. */
const iconButton =
  "flex size-11 items-center justify-center rounded-field text-ink-soft " +
  "transition-colors duration-100 hover:bg-sunken hover:text-ink " +
  "data-open:bg-sunken data-open:text-ink";

/**
 * AccountMenu is the signed-in account: a plain face icon that opens the
 * profile and the way out.
 *
 * The icon replaces the account's name, which used to be the link. A name is
 * a link whose width changes with the person using the site — "Jo" and
 * "Alexandre Galhardo" are not the same header — and on a phone the longer one
 * pushed everything else off the row. A face is the same size for everyone,
 * and the name is still the first thing in the menu, where there is room for
 * it and for the address that tells two accounts apart.
 */
function AccountMenu({ name, email }: { name: string; email: string }): React.ReactElement {
  return (
    <Menu label="Your account" className={iconButton} trigger={<Icon name="user" className="size-5" />}>
      {/* Not a menuitem: it is the heading saying which account this is, and
          making it focusable would put a row in the keyboard order that does
          nothing when chosen. */}
      <div className="px-3 py-2">
        <p className="truncate text-sm font-medium text-ink">{name}</p>
        <p className="truncate text-xs text-ink-faint">{email}</p>
      </div>

      <MenuSeparator />

      {/* Three rows, and each one is a destination: the account, the API
          reference, and the way out. "Profile and API keys" was one row
          naming two things, and the API reference — the page a developer
          here actually wants — was reachable only from the header's wide
          layout, which a phone never shows. */}
      <Link href="/account" role="menuitem" className={MenuItemClass}>
        <Icon name="user" className="size-4 shrink-0" />
        Profile
      </Link>

      <Link href="/docs" role="menuitem" className={MenuItemClass}>
        <Icon name="document" className="size-4 shrink-0" />
        API
      </Link>

      <Link href="/logout" method="post" as="button" role="menuitem" className={MenuItemClass}>
        <Icon name="signout" className="size-4 shrink-0" />
        Log out
      </Link>
    </Menu>
  );
}

/**
 * MobileMenu is the header's whole navigation behind one button, on a phone.
 *
 * It carries the theme as well as the pages, because the compact theme control
 * is two 28px squares with no words and has no business on a touch screen.
 */
function MobileMenu({
  signedIn,
  canSignIn,
}: {
  signedIn: boolean;
  canSignIn: boolean;
}): React.ReactElement {
  return (
    <Menu label="Menu" className={iconButton} trigger={<Icon name="menu" className="size-5" />}>
      {pages.map((item) => (
        <Link key={item.href} href={item.href} role="menuitem" className={MenuItemClass}>
          {item.label}
        </Link>
      ))}

      {!signedIn && canSignIn ? (
        <>
          <MenuSeparator />

          <Link href="/login" role="menuitem" className={`${MenuItemClass} font-medium text-signal`}>
            Log in
          </Link>
        </>
      ) : null}

      <MenuSeparator />

      {/* The click that chooses a theme is stopped here rather than allowed to
          bubble: the panel closes on any click inside it, and closing the menu
          the instant a theme is picked would hide the control that just
          changed. Choosing a theme is the one thing in here that is a setting
          rather than a destination. */}
      <div className="py-1" onClick={(event) => event.stopPropagation()}>
        <ThemeToggle roomy />
      </div>
    </Menu>
  );
}
