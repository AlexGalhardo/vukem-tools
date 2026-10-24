import { useMemo, useState } from "react";

import { parseAs, writeAs, type Format } from "../../lib/dev/convert";
import { diffJSON, diffLines } from "../../lib/dev/diff";
import { parseJSON, sortKeys } from "../../lib/dev/json";
import { checkToolDefinition, validate } from "../../lib/dev/schema";
import { fromBase64, toBase64, utf8 } from "../../lib/dev/crypto";
import { Area, Button, Check, Grid, msg, Notice, Output, Pick, Stack } from "./ui";

export function JsonFormat(): React.ReactElement {
  const [text, setText] = useState('{"name":"vukem","tags":["a","b"],"nested":{"ok":true}}');
  const [indent, setIndent] = useState<"2" | "4" | "0">("2");
  const [sort, setSort] = useState(false);

  const result = useMemo(() => {
    const parsed = parseJSON(text);
    if (!parsed.ok) return { error: `Line ${parsed.line}, column ${parsed.column}: ${parsed.message}`, out: "" };
    const value = sort ? sortKeys(parsed.value) : parsed.value;

    return { error: "", out: JSON.stringify(value, null, Number(indent) || undefined) };
  }, [text, indent, sort]);

  return (
    <Stack>
      <Area label="JSON" value={text} onChange={setText} />
      <div className="flex flex-wrap items-end gap-4">
        <Pick
          label="Indent"
          value={indent}
          onChange={setIndent}
          options={[
            { value: "2", label: "2 spaces" },
            { value: "4", label: "4 spaces" },
            { value: "0", label: "Minified" },
          ]}
        />
        <Check label="Sort keys" checked={sort} onChange={setSort} />
      </div>
      {result.error ? <Notice kind="error">{result.error}</Notice> : <Notice kind="ok">Valid JSON.</Notice>}
      <Output label="Result" value={result.out} />
    </Stack>
  );
}

const formats: { value: Format; label: string }[] = [
  { value: "json", label: "JSON" },
  { value: "yaml", label: "YAML" },
  { value: "csv", label: "CSV" },
  { value: "xml", label: "XML" },
];

export function DataConvert(): React.ReactElement {
  const [from, setFrom] = useState<Format>("json");
  const [to, setTo] = useState<Format>("yaml");
  const [text, setText] = useState('{"name":"vukem","tags":["a","b"],"nested":{"ok":true}}');

  const result = useMemo(() => {
    try {
      return { out: text.trim() ? writeAs(to, parseAs(from, text)) : "", error: "" };
    } catch (e) {
      return { out: "", error: msg(e) };
    }
  }, [from, to, text]);

  return (
    <Stack>
      <Grid>
        <Pick label="From" value={from} onChange={setFrom} options={formats} />
        <Pick label="To" value={to} onChange={setTo} options={formats} />
      </Grid>
      <Area label="Input" value={text} onChange={setText} />
      {result.error ? <Notice kind="error">{result.error}</Notice> : null}
      <Output label="Output" value={result.out} />
    </Stack>
  );
}

const sampleSchema = `{
  "type": "object",
  "required": ["name", "age"],
  "properties": {
    "name": { "type": "string", "minLength": 2 },
    "age": { "type": "integer", "minimum": 0 }
  }
}`;

const sampleTool = `{
  "name": "get_weather",
  "description": "Get the current weather for a city. Use when asked about weather.",
  "input_schema": {
    "type": "object",
    "properties": { "city": { "type": "string", "description": "City name" } },
    "required": ["city"]
  }
}`;

export function JsonSchema(): React.ReactElement {
  const [mode, setMode] = useState<"data" | "tool">("data");
  const [schema, setSchema] = useState(sampleSchema);
  const [data, setData] = useState('{"name": "A", "age": -1}');
  const [tool, setTool] = useState(sampleTool);

  const report = useMemo(() => {
    if (mode === "tool") {
      const p = parseJSON(tool);
      if (!p.ok) return { error: `Line ${p.line}, column ${p.column}: ${p.message}` };

      return { tool: checkToolDefinition(p.value) };
    }
    const s = parseJSON(schema);
    if (!s.ok) return { error: `Schema, line ${s.line}: ${s.message}` };
    const d = parseJSON(data);
    if (!d.ok) return { error: `Data, line ${d.line}: ${d.message}` };
    try {
      return { issues: validate(s.value, d.value) };
    } catch (e) {
      return { error: msg(e) };
    }
  }, [mode, schema, data, tool]);

  return (
    <Stack>
      <Pick
        label="Check"
        value={mode}
        onChange={setMode}
        options={[
          { value: "data", label: "A document against a schema" },
          { value: "tool", label: "An LLM tool definition" },
        ]}
      />
      {mode === "data" ? (
        <Grid>
          <Area label="JSON Schema" value={schema} onChange={setSchema} rows={14} />
          <Area label="Document" value={data} onChange={setData} rows={14} />
        </Grid>
      ) : (
        <Area label="Tool definition (OpenAI or Anthropic format)" value={tool} onChange={setTool} rows={14} />
      )}

      {"error" in report && report.error ? <Notice kind="error">{report.error}</Notice> : null}
      {"issues" in report && report.issues ? (
        report.issues.length === 0 ? (
          <Notice kind="ok">The document is valid.</Notice>
        ) : (
          <ul className="space-y-2">
            {report.issues.map((i, n) => (
              <li key={n}>
                <Notice kind="error">
                  <code className="font-mono">{i.path}</code> {i.message}
                </Notice>
              </li>
            ))}
          </ul>
        )
      ) : null}
      {"tool" in report && report.tool ? (
        <div className="space-y-2">
          <p className="text-sm text-ink-soft">Detected format: {report.tool.format}</p>
          {report.tool.errors.map((e) => (
            <Notice key={e} kind="error">
              {e}
            </Notice>
          ))}
          {report.tool.warnings.map((w) => (
            <Notice key={w} kind="warn">
              {w}
            </Notice>
          ))}
          {report.tool.errors.length === 0 && report.tool.warnings.length === 0 ? (
            <Notice kind="ok">Nothing to fix.</Notice>
          ) : null}
        </div>
      ) : null}
    </Stack>
  );
}

const rowLook = { same: "", add: "bg-ok-soft text-ok", del: "bg-bad-soft text-bad" } as const;
const mark = { same: " ", add: "+", del: "-" } as const;

export function DiffTool(): React.ReactElement {
  const [mode, setMode] = useState<"text" | "json">("text");
  const [a, setA] = useState("one\ntwo\nthree");
  const [b, setB] = useState("one\n2\nthree\nfour");

  const result = useMemo(() => {
    if (mode === "text") return { lines: diffLines(a, b) };
    const x = parseJSON(a);
    const y = parseJSON(b);
    if (!x.ok) return { error: `Left, line ${x.line}: ${x.message}` };
    if (!y.ok) return { error: `Right, line ${y.line}: ${y.message}` };

    return { changes: diffJSON(x.value, y.value) };
  }, [mode, a, b]);

  return (
    <Stack>
      <Pick
        label="Compare"
        value={mode}
        onChange={setMode}
        options={[
          { value: "text", label: "Text, line by line" },
          { value: "json", label: "JSON, key by key" },
        ]}
      />
      <Grid>
        <Area label="Left" value={a} onChange={setA} />
        <Area label="Right" value={b} onChange={setB} />
      </Grid>
      {"error" in result && result.error ? <Notice kind="error">{result.error}</Notice> : null}
      {"lines" in result && result.lines ? (
        <pre
          aria-label="Differences"
          className="overflow-x-auto rounded-panel border border-edge bg-raised py-2 font-mono text-sm"
        >
          {result.lines.map((l, i) => (
            <div key={i} className={`px-3 ${rowLook[l.op]}`}>
              <span aria-hidden="true" className="mr-2 select-none">
                {mark[l.op]}
              </span>
              <span className="sr-only">{l.op === "add" ? "added " : l.op === "del" ? "removed " : ""}</span>
              {l.text || " "}
            </div>
          ))}
        </pre>
      ) : null}
      {"changes" in result && result.changes ? (
        result.changes.length === 0 ? (
          <Notice kind="ok">The documents are equal. Key order does not count.</Notice>
        ) : (
          <ul className="divide-y divide-edge rounded-panel border border-edge bg-raised text-sm">
            {result.changes.map((c) => (
              <li key={c.path} className="px-3 py-2">
                <code className="font-mono text-ink">{c.path}</code>{" "}
                <span className="text-ink-faint">{c.kind}</span>
                {c.kind !== "added" ? <div className="font-mono text-bad">- {JSON.stringify(c.before)}</div> : null}
                {c.kind !== "removed" ? <div className="font-mono text-ok">+ {JSON.stringify(c.after)}</div> : null}
              </li>
            ))}
          </ul>
        )
      ) : null}
    </Stack>
  );
}

export function Base64Tool(): React.ReactElement {
  const [mode, setMode] = useState<"encode" | "decode">("encode");
  const [urlSafe, setUrlSafe] = useState(false);
  const [text, setText] = useState("Hello, vukem!");

  const result = useMemo(() => {
    try {
      if (mode === "encode") {
        const b = toBase64(utf8(text));

        return { out: urlSafe ? b.replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "") : b, error: "" };
      }

      return { out: new TextDecoder("utf-8", { fatal: true }).decode(fromBase64(text)), error: "" };
    } catch {
      return { out: "", error: "That is not valid Base64, or it does not decode to UTF-8 text." };
    }
  }, [mode, urlSafe, text]);

  return (
    <Stack>
      <div className="flex flex-wrap items-end gap-4">
        <Pick
          label="Direction"
          value={mode}
          onChange={setMode}
          options={[
            { value: "encode", label: "Text to Base64" },
            { value: "decode", label: "Base64 to text" },
          ]}
        />
        {mode === "encode" ? <Check label="URL-safe, no padding" checked={urlSafe} onChange={setUrlSafe} /> : null}
        <Button variant="quiet" onClick={() => setText(result.out)}>
          Use result as input
        </Button>
      </div>
      <Area label="Input" value={text} onChange={setText} rows={6} />
      {result.error ? <Notice kind="error">{result.error}</Notice> : null}
      <Output label="Output" value={result.out} rows={6} />
    </Stack>
  );
}
