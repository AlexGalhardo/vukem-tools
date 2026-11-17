import { expect, test } from "bun:test";

import { buttonVariants } from "./button";

test("the default shadcn button uses the app signal colour", () => {
  const classes = buttonVariants();

  expect(classes).toContain("bg-signal");
  expect(classes).toContain("text-raised");
  expect(classes).toContain("rounded-field");
});

test("the ghost shadcn button stays quiet", () => {
  expect(buttonVariants({ variant: "ghost", size: "icon" })).toContain("text-ink-faint");
});
