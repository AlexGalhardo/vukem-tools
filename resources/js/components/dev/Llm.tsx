import { useMemo, useState } from "react";

import {
  anthropicToOpenai,
  cleanMarkdown,
  cost,
  defaultPrices,
  estimateTokens,
  extractJSON,
  openaiToAnthropic,
  priceSources,
  type Price,
} from "../../lib/dev/llm";
import { parseJSON } from "../../lib/dev/json";
import { Area, Button, Grid, msg, Notice, Num, Output, Pick, Stack } from "./ui";

const nf = new Intl.NumberFormat("en-US");

export function TokenCount(): React.ReactElement {
  const [text, setText] = useState("");
  const tokens = useMemo(() => estimateTokens(text), [text]);
  const chars = [...text].length;

  return (
    <Stack>
      <Area label="Text" value={text} onChange={setText} rows={12} placeholder="Paste a prompt, a file or a reply" />
      <Grid>
        <Stat label="Estimated tokens" value={nf.format(tokens)} />
        <Stat label="Conservative upper bound" value={nf.format(Math.ceil(tokens * 1.3))} />
        <Stat label="Characters" value={nf.format(chars)} />
        <Stat label="Words" value={nf.format(text.trim() ? text.trim().split(/\s+/).length : 0)} />
      </Grid>
      <Notice kind="warn">
        This is an estimate, not any provider's tokenizer, and it is usually within 10-25% of the real count. Some
        newer tokenizers produce up to about 30% more tokens for the same text, which is what the upper bound allows
        for. For billing, ask the provider's token-counting endpoint.
      </Notice>
    </Stack>
  );
}

function Stat({ label, value }: { label: string; value: string }): React.ReactElement {
  return (
    <div className="rounded-panel border border-edge bg-raised px-4 py-3">
      <p className="label">{label}</p>
      <p className="tabular mt-1 text-2xl font-semibold">{value}</p>
    </div>
  );
}

const sample = `Sure! Here is the data you asked for:

\`\`\`json
{
  "name": "vukem",
  "tags": ["go", "react",],
}
\`\`\`

Let me know if you need anything else!`;

export function LlmClean(): React.ReactElement {
  const [reply, setReply] = useState(sample);
  const found = useMemo(() => extractJSON(reply), [reply]);
  const cleaned = useMemo(() => cleanMarkdown(reply), [reply]);

  return (
    <Stack>
      <Area label="Model reply" value={reply} onChange={setReply} rows={12} />
      <section className="space-y-3">
        <h2 className="text-lg font-medium">
          JSON found <span className="text-ink-faint">({found.length})</span>
        </h2>
        {found.length === 0 ? <Notice kind="warn">No valid JSON object or array in the reply.</Notice> : null}
        {found.map((f, i) => (
          <div key={i} className="space-y-2">
            {f.repaired ? <Notice kind="warn">Repaired: trailing commas were removed.</Notice> : null}
            <Output label={`JSON ${i + 1}`} value={JSON.stringify(f.json, null, 2)} rows={8} />
          </div>
        ))}
      </section>
      <Output label="Reply without fences, lead-in and sign-off" value={cleaned} rows={8} />
    </Stack>
  );
}

export function LlmCost(): React.ReactElement {
  const [prices, setPrices] = useState<Price[]>(defaultPrices);
  const [input, setInput] = useState(2000);
  const [output, setOutput] = useState(500);
  const [calls, setCalls] = useState(1000);

  const edit = (i: number, key: "input" | "output", v: number): void =>
    setPrices(prices.map((p, n) => (n === i ? { ...p, [key]: Number.isFinite(v) ? v : 0 } : p)));
  const usd = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", maximumFractionDigits: 4 });

  return (
    <Stack>
      <Grid>
        <Num label="Input tokens per call" value={input} min={0} onChange={setInput} />
        <Num label="Output tokens per call" value={output} min={0} onChange={setOutput} />
        <Num label="Number of calls" value={calls} min={1} onChange={setCalls} />
      </Grid>
      <div className="overflow-x-auto rounded-panel border border-edge bg-raised">
        <table className="w-full min-w-[34rem] text-sm">
          <thead className="text-left text-ink-soft">
            <tr>
              <th className="px-3 py-2 font-medium">Model</th>
              <th className="px-3 py-2 font-medium">Input $/M</th>
              <th className="px-3 py-2 font-medium">Output $/M</th>
              <th className="px-3 py-2 text-right font-medium">One call</th>
              <th className="px-3 py-2 text-right font-medium">All calls</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-edge">
            {prices.map((p, i) => (
              <tr key={p.model}>
                <td className="px-3 py-2">{p.model}</td>
                {(["input", "output"] as const).map((k) => (
                  <td key={k} className="px-3 py-2">
                    <input
                      aria-label={`${p.model} ${k} price per million tokens`}
                      type="number"
                      min={0}
                      step="any"
                      value={p[k]}
                      onChange={(e) => edit(i, k, e.target.valueAsNumber)}
                      className="tabular w-24 rounded-field border border-edge bg-surface px-2 py-1.5"
                    />
                  </td>
                ))}
                <td className="tabular px-3 py-2 text-right">{usd.format(cost(p, input, output))}</td>
                <td className="tabular px-3 py-2 text-right font-medium">{usd.format(cost(p, input, output, calls))}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <Button variant="quiet" onClick={() => setPrices(defaultPrices)}>
          Reset prices
        </Button>
      </div>
      <p className="text-xs text-ink-faint">
        Standard list prices in USD per million tokens, read from the providers' pricing pages on 2026-10-24. Prices
        change and tiered, cached and batch rates are not included, so edit a row to match your contract. Sources:{" "}
        {priceSources.map((s, i) => (
          <span key={s}>
            {i > 0 ? ", " : ""}
            <a href={s} className="text-signal hover:underline" rel="noreferrer">
              {new URL(s).hostname}
            </a>
          </span>
        ))}
        .
      </p>
    </Stack>
  );
}

type Direction = "openai-anthropic" | "anthropic-openai";

const chatSample = `[
  { "role": "system", "content": "You answer in one sentence." },
  { "role": "user", "content": "What is Go?" }
]`;

export function ChatTemplate(): React.ReactElement {
  const [dir, setDir] = useState<Direction>("openai-anthropic");
  const [text, setText] = useState(chatSample);

  const result = useMemo(() => {
    const p = parseJSON(text);
    if (!p.ok) return { out: "", error: `Line ${p.line}, column ${p.column}: ${p.message}` };
    try {
      const v = dir === "openai-anthropic" ? openaiToAnthropic(p.value) : anthropicToOpenai(p.value);

      return { out: JSON.stringify(v, null, 2), error: "" };
    } catch (e) {
      return { out: "", error: msg(e) };
    }
  }, [dir, text]);

  return (
    <Stack>
      <Pick
        label="Convert"
        value={dir}
        onChange={(d) => {
          setDir(d);
          setText(d === "openai-anthropic" ? chatSample : '{\n  "system": "You answer in one sentence.",\n  "messages": [{ "role": "user", "content": "What is Go?" }]\n}');
        }}
        options={[
          { value: "openai-anthropic", label: "OpenAI messages to Anthropic Messages API" },
          { value: "anthropic-openai", label: "Anthropic Messages API to OpenAI messages" },
        ]}
      />
      <Area label="Input" value={text} onChange={setText} rows={12} />
      {result.error ? <Notice kind="error">{result.error}</Notice> : null}
      <Output label="Output" value={result.out} rows={12} />
      <p className="text-xs text-ink-faint">
        Text, tool calls and tool results are converted. Images and provider-only options are not carried over.
      </p>
    </Stack>
  );
}
