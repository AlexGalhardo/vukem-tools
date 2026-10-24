import { expect, test } from "bun:test";

import { forms, optionsFrom } from "./toolForm";

test("the forms tool's lines become name/value pairs, split at the first =", () => {
  const form = forms["pdf-forms"]!;
  const options = optionsFrom(form, { action: "fill", values: "full_name = Ana Souza\n\nnote=a=b\nflag" });

  expect(options).toEqual({
    action: "fill",
    values: [
      { name: "full_name", value: "Ana Souza" },
      { name: "note", value: "a=b" },
      { name: "flag", value: "" },
    ],
  });
});

test("the OCR language choice becomes the list the API takes", () => {
  expect(optionsFrom(forms["pdf-ocr"]!, { languages: "eng+por", deskew: true })).toEqual({
    languages: ["eng", "por"],
    deskew: true,
  });
});

test("every Office converter's picker names the types the server reads", () => {
  for (const tool of ["word-to-pdf", "powerpoint-to-pdf", "excel-to-pdf"]) {
    expect(forms[tool]!.accept).toHaveLength(3);
  }
});
