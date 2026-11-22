import { expect, mock, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

mock.module("@inertiajs/react", () => ({
  Head: () => null,
  useForm: () => ({ data: { email: "" }, errors: {}, processing: false, setData: () => undefined, post: () => undefined }),
  usePage: () => ({ url: "/", props: { auth: {}, toolGroups: [] }, flash: {} }),
  router: { on: () => () => undefined },
  Link: () => null,
}));

const { default: ForgotPassword } = await import("./ForgotPassword");

test("a computer that sends no email does not promise a link", () => {
  const html = renderToStaticMarkup(createElement(ForgotPassword, { local: true }));

  expect(html).toContain("reset code");
  expect(html).not.toContain("link");
});

test("a hosted install still emails a link", () => {
  const html = renderToStaticMarkup(createElement(ForgotPassword, {}));

  expect(html).toContain("Email me a reset link");
});
