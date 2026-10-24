import { expect, test } from "bun:test";

import { cepNormalize, cnpjFormat, cnpjGenerate, cnpjValid, cpfFormat, cpfGenerate, cpfValid, fakePerson } from "./brazil";

test("CPF check digits", () => {
  expect(cpfValid("529.982.247-25")).toBe(true);
  expect(cpfValid("529.982.247-24")).toBe(false);
  expect(cpfValid("111.111.111-11")).toBe(false);
  expect(cpfValid("123")).toBe(false);
  expect(cpfFormat("52998224725")).toBe("529.982.247-25");
});

test("generated CPFs validate", () => {
  for (let i = 0; i < 200; i++) expect(cpfValid(cpfGenerate())).toBe(true);
});

test("CNPJ check digits, numeric and alphanumeric", () => {
  expect(cnpjValid("11.222.333/0001-81")).toBe(true);
  expect(cnpjValid("11.222.333/0001-82")).toBe(false);
  // The example the Receita Federal publishes for the alphanumeric format.
  expect(cnpjValid("12.ABC.345/01DE-35")).toBe(true);
  expect(cnpjValid("00000000000000")).toBe(false);
  expect(cnpjFormat("11222333000181")).toBe("11.222.333/0001-81");
});

test("generated CNPJs validate", () => {
  for (let i = 0; i < 200; i++) {
    expect(cnpjValid(cnpjGenerate(false))).toBe(true);
    expect(cnpjValid(cnpjGenerate(true))).toBe(true);
  }
});

test("a CEP is eight digits", () => {
  expect(cepNormalize("01310-100")).toBe("01310100");
  expect(cepNormalize("1234")).toBeNull();
});

test("a fake person has a valid CPF and a documentation-only email", () => {
  const p = fakePerson();
  expect(cpfValid(p.cpf)).toBe(true);
  expect(p.email.endsWith("@example.com")).toBe(true);
});
