import { expect, test } from "bun:test";

import { headerPages } from "./nav";

/**
 * The pages this app renders itself, as Inertia components under
 * resources/js/pages. Anything the header links to that is not one of them
 * leaves the app, and leaving the app by an Inertia visit is the bug this
 * table exists to catch: the client asks by XHR, gets HTML with no Inertia
 * header back, and renders it in a full-screen error iframe. The owner saw a
 * white panel over the home page and no navigation.
 *
 * Add a page here when the app starts rendering it; mark it `document`
 * otherwise.
 */
const inertiaPages = new Set(["/", "/licenses", "/account", "/login", "/register"]);

test("a header link that leaves the app is a document link", () => {
  for (const page of headerPages) {
    const leavesTheApp = !inertiaPages.has(page.href);

    expect(
      { href: page.href, document: page.document === true },
      `${page.href} ${leavesTheApp ? "is not an Inertia page" : "is an Inertia page"}`,
    ).toEqual({ href: page.href, document: leavesTheApp });
  }
});

test("Documentation opens the documentation site in a new tab by a document link", () => {
  // Named on its own because leaving the app by an Inertia visit is the bug
  // above: the documentation site is another origin and plain HTML.
  const docs = headerPages.find((page) => page.label === "Documentation");

  expect(docs).toBeDefined();
  expect(docs?.href).toBe("https://tools.vukem.dev/docs/");
  expect(docs?.document).toBe(true);
  expect(docs?.external).toBe(true);
});

test("the sidebar has no link called API reference any more", () => {
  expect(headerPages.some((page) => page.label === "API reference")).toBe(false);
});
