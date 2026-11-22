import { expect, test } from "bun:test";

import { fromCSV, fromXML, fromYaml, parseAs, toCSV, toXML, toYaml, writeAs } from "./convert";
import { decodeJwt } from "./crypto";
import { anthropicToOpenai, extractJSON, openaiToAnthropic } from "./llm";
import { exactOrNumber, ExactNumber, parseJSONDigits, stringifyJSON } from "./json";

const doc = '{"id":12345678901234567890,"price":1.10,"zero":-0.0,"plain":1.5,"n":[7,1E3]}';

function parsed(text: string): unknown {
  const r = parseJSONDigits(text);
  if (!r.ok) throw new Error(r.message);

  return r.value;
}

const b64u = (s: string): string => btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");

test("a number that does not survive a double keeps its digits; others stay numbers", () => {
  expect(exactOrNumber("12345678901234567890")).toEqual(new ExactNumber("12345678901234567890"));
  expect(exactOrNumber("1.10")).toEqual(new ExactNumber("1.10"));
  expect(exactOrNumber("-0.0")).toEqual(new ExactNumber("-0.0"));
  expect(exactOrNumber("1.5")).toBe(1.5);
  expect(exactOrNumber("7")).toBe(7);
});

test("JSON to JSON keeps every digit", () => {
  expect(writeAs("json", parseAs("json", doc))).toBe(
    '{\n  "id": 12345678901234567890,\n  "price": 1.10,\n  "zero": -0.0,\n  "plain": 1.5,\n  "n": [\n    7,\n    1E3\n  ]\n}',
  );
  expect(stringifyJSON(parsed(doc))).toBe(doc);
});

test("JSON to YAML to JSON keeps every digit", () => {
  const yaml = toYaml(parseAs("json", doc));
  expect(yaml).toContain("id: 12345678901234567890\n");
  expect(yaml).toContain("price: 1.10\n");
  expect(yaml).toContain("zero: -0.0\n");
  expect(stringifyJSON(fromYaml(yaml))).toBe(doc);
});

test("JSON to CSV keeps every digit", () => {
  const csv = toCSV(parseAs("json", '[{"id":12345678901234567890,"price":1.10,"zero":-0.0}]'));
  expect(csv).toBe("id,price,zero\n12345678901234567890,1.10,-0.0");
  expect(fromCSV(csv)).toEqual([{ id: "12345678901234567890", price: "1.10", zero: "-0.0" }]);
});

test("JSON to XML keeps every digit, as an element and as an attribute", () => {
  const xml = toXML(parseAs("json", '{"id":12345678901234567890,"price":1.10,"zero":-0.0,"o":{"@v":1.10}}'));
  expect(xml).toContain("<id>12345678901234567890</id>");
  expect(xml).toContain("<price>1.10</price>");
  expect(xml).toContain("<zero>-0.0</zero>");
  expect(xml).toContain('<o v="1.10">');
  expect(fromXML(xml)).toMatchObject({ id: "12345678901234567890", price: "1.10", zero: "-0.0" });
});

test("a JWT payload shows its numbers as written and still reads exp", () => {
  const payload = '{"n":12345678901234567890,"p":1.10,"z":-0.0,"exp":2e9}';
  const jwt = decodeJwt(`${b64u('{"alg":"none"}')}.${b64u(payload)}.`);
  expect(stringifyJSON(jwt.payload)).toBe(payload);
  expect(jwt.expiresAt?.getTime()).toBe(2e12);
});

test("extracting JSON from a model reply keeps the digits", () => {
  const [found] = extractJSON(`Here: ${doc} done`);
  expect(stringifyJSON(found?.json)).toBe(doc);
});

test("converting chat messages keeps the digits in tool arguments", () => {
  const args = '{"id":12345678901234567890,"p":1.10,"z":-0.0}';
  const anthropic = openaiToAnthropic(
    parsed(
      JSON.stringify([
        { role: "assistant", content: null, tool_calls: [{ id: "c1", type: "function", function: { name: "f", arguments: args } }] },
      ]),
    ),
  );
  expect(stringifyJSON(anthropic)).toContain(`"input":${args}`);
  const back = anthropicToOpenai(anthropic);
  expect(back[0]?.tool_calls).toEqual([{ id: "c1", type: "function", function: { name: "f", arguments: args } }]);
});
