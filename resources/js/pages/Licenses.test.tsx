import { expect, mock, test } from "bun:test";
import { createElement, type ReactNode } from "react";
import { renderToStaticMarkup } from "react-dom/server";

mock.module("@inertiajs/react", () => ({
  Head: ({ children }: { children?: ReactNode }) => createElement("title", null, children),
  usePage: () => ({ url: "/", props: { auth: {}, toolGroups: [] }, flash: {} }),
  useForm: () => ({}),
  router: { on: () => () => undefined },
  Link: ({ children, href }: { children?: ReactNode; href?: string }) => createElement("a", { href }, children),
}));

const { default: Licenses } = await import("./Licenses");

const entry = {
  name: "Ghostscript",
  spdx: "AGPL-3.0-or-later",
  url: "https://example.com",
  role: "Renders PDFs",
  copyleft: true,
};

test("the licence column wraps on a phone instead of pushing the page wider than the screen", () => {
  const html = renderToStaticMarkup(
    createElement(Licenses, { project: { ...entry, name: "Vukem Tools", copyleft: false }, components: [entry], copyleft: [entry] }),
  );

  expect(html).not.toMatch(/(^|[ "])whitespace-nowrap/);
  expect(html).toContain("sm:whitespace-nowrap");
});
