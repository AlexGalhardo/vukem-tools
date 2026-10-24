import { expect, test } from "bun:test";

import { anthropicToOpenai, cleanMarkdown, cost, defaultPrices, estimateTokens, extractJSON, openaiToAnthropic } from "./llm";

test("the token estimate grows with the text and is zero for nothing", () => {
  expect(estimateTokens("")).toBe(0);
  expect(estimateTokens("hello world")).toBe(estimateTokens("hello") + estimateTokens("world"));
  expect(estimateTokens("日本語")).toBe(3);
  expect(estimateTokens("1234567")).toBe(3);
});

test("cost is per million tokens", () => {
  expect(cost({ model: "m", input: 2, output: 10 }, 1_000_000, 500_000)).toBe(7);
  expect(cost({ model: "m", input: 2, output: 10 }, 1000, 0, 10)).toBeCloseTo(0.02);
  expect(defaultPrices.every((p) => p.input > 0 && p.output >= p.input)).toBe(true);
});

test("JSON is found in fences, in prose, and with a trailing comma", () => {
  const reply = 'Sure! Here you go:\n```json\n{"a": [1, 2,],}\n```\nand also {"b": "}"} inline.';
  const found = extractJSON(reply);
  expect(found.map((f) => f.json)).toEqual([{ a: [1, 2] }, { b: "}" }]);
  expect(found[0]!.repaired).toBe(true);
  expect(extractJSON("no json here {oops")).toEqual([]);
});

test("markdown loses its wrapper and keeps its content", () => {
  expect(cleanMarkdown("Here is the file:\n\n```md\n# Title\n\ntext\n```\nLet me know if you need more!")).toBe(
    "# Title\n\ntext",
  );
  expect(cleanMarkdown("Sure!\n\nA\n\n\n\nB")).toBe("A\n\nB");
  expect(cleanMarkdown("```js\nlet a\n```\ntext\n```js\nlet b\n```")).toContain("text");
});

test("OpenAI messages become Anthropic messages and back", () => {
  const openai = [
    { role: "system", content: "Be brief." },
    { role: "user", content: "Weather in Rio?" },
    {
      role: "assistant",
      content: null,
      tool_calls: [{ id: "c1", type: "function", function: { name: "weather", arguments: '{"city":"Rio"}' } }],
    },
    { role: "tool", tool_call_id: "c1", content: "28C" },
    { role: "assistant", content: "It is 28C." },
  ];
  const a = openaiToAnthropic(openai);
  expect(a.system).toBe("Be brief.");
  const msgs = a.messages as { role: string; content: { type: string }[] }[];
  expect(msgs.map((m) => m.role)).toEqual(["user", "assistant", "user", "assistant"]);
  expect(msgs[1]!.content[0]).toMatchObject({ type: "tool_use", name: "weather", input: { city: "Rio" } });
  expect(msgs[2]!.content[0]).toMatchObject({ type: "tool_result", tool_use_id: "c1" });
  expect(anthropicToOpenai(a)).toEqual(openai);
});

test("bad arguments are reported, not swallowed", () => {
  expect(() =>
    openaiToAnthropic([{ role: "assistant", tool_calls: [{ id: "x", function: { name: "f", arguments: "{" } }] }]),
  ).toThrow("not valid JSON");
});
