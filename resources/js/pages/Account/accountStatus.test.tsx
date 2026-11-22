import { expect, mock, test } from "bun:test";
import { createElement, type ReactNode } from "react";
import { renderToStaticMarkup } from "react-dom/server";

const page = {
  url: "/account",
  props: { auth: { user: { name: "QA", email: "qa@example.com" } }, toolGroups: [] as unknown[] },
  flash: { status: "Your name has been updated." } as Record<string, string>,
};

const form = (initial: Record<string, unknown>): Record<string, unknown> =>
  new Proxy(
    { data: initial, errors: {}, processing: false, isDirty: false, recentlySuccessful: false },
    { get: (target, key: string) => (key in target ? target[key as keyof typeof target] : () => undefined) },
  );

mock.module("@inertiajs/react", () => ({
  usePage: () => page,
  useForm: form,
  router: { on: () => () => undefined },
  Head: () => null,
  Link: ({ children, href }: { children?: ReactNode; href?: string }) => createElement("a", { href }, children),
}));

const { default: AppLayout } = await import("../../layouts/AppLayout");
const { default: AccountIndex } = await import("./Index");

test("a flashed status is announced once on the account page, not twice", () => {
  const profile = { name: "QA", email: "qa@example.com", verified: true, joined_at: "2026-11-22T00:00:00Z" };
  const html = renderToStaticMarkup(
    createElement(AppLayout, null, createElement(AccountIndex, { profile, keys: [], abilities: [] })),
  );

  expect(html.split("Your name has been updated.").length - 1).toBe(1);
});
