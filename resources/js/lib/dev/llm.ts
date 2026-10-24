type Obj = Record<string, unknown>;

/* ---------- tokens ---------- */

/** estimateTokens approximates a BPE tokenizer without shipping one: a Latin
 *  word costs about one token per four characters, a digit run one per three,
 *  a CJK character one, and each symbol or line break one. Real counts differ
 *  by model and by language, typically by 10-25%, which is why the page says
 *  "estimate". */
export function estimateTokens(text: string): number {
  let n = 0;
  for (const m of text.matchAll(/[\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}]|\p{L}+|\p{N}+|\n|[^\s\p{L}\p{N}]/gu)) {
    const t = m[0];
    if (/^\p{Script=Han}$|^[\p{Script=Hiragana}\p{Script=Katakana}]$/u.test(t)) n += 1;
    else if (/^\p{N}+$/u.test(t)) n += Math.ceil(t.length / 3);
    else if (/^\p{L}+$/u.test(t)) n += Math.max(1, Math.ceil(t.length / 4));
    else n += 1;
  }

  return n;
}

/* ---------- cost ---------- */

export interface Price {
  model: string;
  /** USD per million input tokens. */
  input: number;
  /** USD per million output tokens. */
  output: number;
}

/** defaultPrices are the standard-rate list prices read from each provider's
 *  pricing page on 2026-10-24. Prices change; the page lets the visitor edit
 *  them, and tiered or dated prices are left out rather than guessed. */
export const defaultPrices: Price[] = [
  { model: "Claude Fable 5.1", input: 10, output: 50 },
  { model: "Claude Opus 5.5", input: 4, output: 20 },
  { model: "Claude Sonnet 5.5", input: 2, output: 10 },
  { model: "Claude Haiku 5.5 (up to 100k tokens)", input: 0.1, output: 0.5 },
  { model: "GPT-6 Astra", input: 10, output: 50 },
  { model: "GPT-6.1 Sol", input: 2, output: 10 },
  { model: "GPT-6 Luna", input: 0.1, output: 0.5 },
  { model: "Gemini 3.5 Flash-Lite", input: 0.3, output: 2.5 },
];

export const priceSources = [
  "https://platform.claude.com/docs/en/about-claude/pricing",
  "https://developers.openai.com/api/docs/pricing",
  "https://ai.google.dev/gemini-api/docs/pricing",
];

/** cost is the price in USD of a call, times the number of calls. */
export function cost(p: Price, inputTokens: number, outputTokens: number, calls = 1): number {
  return ((inputTokens * p.input + outputTokens * p.output) / 1_000_000) * calls;
}

/* ---------- cleaning model output ---------- */

/** balancedAt returns the end (exclusive) of the JSON value that starts at
 *  index i, or -1. It counts brackets outside strings. */
function balancedAt(s: string, i: number): number {
  const open = s[i];
  const close = open === "{" ? "}" : "]";
  let depth = 0;
  let inStr = false;
  for (let j = i; j < s.length; j++) {
    const c = s[j]!;
    if (inStr) {
      if (c === "\\") j++;
      else if (c === '"') inStr = false;
    } else if (c === '"') inStr = true;
    else if (c === open) depth++;
    else if (c === close && --depth === 0) return j + 1;
  }

  return -1;
}

function tryParse(s: string): unknown {
  try {
    return { value: JSON.parse(s) as unknown };
  } catch {
    // Models leave a trailing comma more than any other mistake.
    try {
      return { value: JSON.parse(s.replace(/,\s*([}\]])/g, "$1")) as unknown, repaired: true };
    } catch {
      return null;
    }
  }
}

export interface Extracted {
  json: unknown;
  text: string;
  repaired: boolean;
}

/** extractJSON finds every JSON object or array in a model's reply, fenced or
 *  not, and returns those that parse, largest first. */
export function extractJSON(reply: string): Extracted[] {
  const found: Extracted[] = [];
  const seen = new Set<string>();
  const add = (candidate: string): void => {
    const t = candidate.trim();
    if (!/^[[{]/.test(t) || seen.has(t)) return;
    const r = tryParse(t) as { value: unknown; repaired?: boolean } | null;
    if (r && typeof r.value === "object" && r.value !== null) {
      seen.add(t);
      found.push({ json: r.value, text: t, repaired: r.repaired === true });
    }
  };
  for (const m of reply.matchAll(/```[\w-]*\n([\s\S]*?)```/g)) add(m[1]!);
  for (let i = 0; i < reply.length; i++) {
    if (reply[i] === "{" || reply[i] === "[") {
      const end = balancedAt(reply, i);
      if (end > 0) {
        add(reply.slice(i, end));
        i = end - 1;
      }
    }
  }

  return found.sort((a, b) => b.text.length - a.text.length);
}

/** cleanMarkdown removes what a chat reply wraps around its payload: a lead-in
 *  sentence ending in a colon, an outer code fence, a sign-off, and runs of
 *  blank lines. */
export function cleanMarkdown(reply: string): string {
  let s = reply.replace(/\r\n/g, "\n").trim();
  const fence = /^(?:[^\n`]*:\n\s*)?```[\w-]*\n([\s\S]*?)\n```\s*(?:\n[^\n]*)?$/.exec(s);
  if (fence && !fence[1]!.includes("```")) s = fence[1]!;
  s = s
    .replace(/^(sure|certainly|of course|here(?:'s| is| are)[^\n]*?)[!:.]?\s*\n+/i, "")
    .replace(/\n+(let me know[^\n]*|i hope this helps[^\n]*|feel free to[^\n]*)$/i, "")
    .replace(/\n{3,}/g, "\n\n");

  return s.trim();
}

/* ---------- chat formats ---------- */

export type Provider = "openai" | "anthropic";

const isObj = (v: unknown): v is Obj => typeof v === "object" && v !== null && !Array.isArray(v);

function textOf(content: unknown): string {
  if (typeof content === "string") return content;
  if (Array.isArray(content)) {
    return content.map((p) => (isObj(p) && typeof p.text === "string" ? p.text : "")).join("");
  }

  return "";
}

/** openaiToAnthropic converts a Chat Completions message list into the shape of
 *  the Messages API: the system prompt moves out of the list, assistant tool
 *  calls become tool_use blocks and tool replies become tool_result blocks. */
export function openaiToAnthropic(input: unknown): Obj {
  const list = Array.isArray(input) ? input : isObj(input) && Array.isArray(input.messages) ? input.messages : null;
  if (!list) throw new Error("expected an array of messages, or an object with a messages array");
  const system: string[] = [];
  const messages: Obj[] = [];
  const push = (role: string, blocks: Obj[]): void => {
    const last = messages[messages.length - 1];
    // The Messages API wants roles to alternate; consecutive tool results are
    // one user turn.
    if (last && last.role === role) (last.content as Obj[]).push(...blocks);
    else messages.push({ role, content: blocks });
  };
  for (const m of list) {
    if (!isObj(m) || typeof m.role !== "string") throw new Error("every message needs a role");
    if (m.role === "system" || m.role === "developer") {
      system.push(textOf(m.content));
    } else if (m.role === "tool") {
      push("user", [{ type: "tool_result", tool_use_id: m.tool_call_id, content: textOf(m.content) }]);
    } else if (m.role === "assistant") {
      const blocks: Obj[] = [];
      const t = textOf(m.content);
      if (t) blocks.push({ type: "text", text: t });
      for (const c of Array.isArray(m.tool_calls) ? m.tool_calls : []) {
        const fn = isObj(c) && isObj(c.function) ? c.function : {};
        let args: unknown = {};
        try {
          args = JSON.parse(typeof fn.arguments === "string" ? fn.arguments : "{}") as unknown;
        } catch {
          throw new Error(`the arguments of tool call ${String((c as Obj).id)} are not valid JSON`);
        }
        blocks.push({ type: "tool_use", id: (c as Obj).id, name: fn.name, input: args });
      }
      push("assistant", blocks);
    } else {
      push("user", [{ type: "text", text: textOf(m.content) }]);
    }
  }
  const out: Obj = {};
  if (system.length) out.system = system.join("\n\n");
  out.messages = messages;

  return out;
}

/** anthropicToOpenai is the reverse of openaiToAnthropic. */
export function anthropicToOpenai(input: unknown): Obj[] {
  if (!isObj(input) || !Array.isArray(input.messages)) throw new Error("expected an object with a messages array");
  const out: Obj[] = [];
  const sys = textOf(input.system);
  if (sys) out.push({ role: "system", content: sys });
  for (const m of input.messages) {
    if (!isObj(m) || typeof m.role !== "string") throw new Error("every message needs a role");
    if (typeof m.content === "string") {
      out.push({ role: m.role, content: m.content });
      continue;
    }
    const blocks = Array.isArray(m.content) ? m.content.filter(isObj) : [];
    const text = blocks.filter((b) => b.type === "text").map((b) => String(b.text ?? "")).join("");
    for (const b of blocks.filter((x) => x.type === "tool_result")) {
      out.push({ role: "tool", tool_call_id: b.tool_use_id, content: textOf(b.content) });
    }
    const uses = blocks.filter((b) => b.type === "tool_use");
    if (text || uses.length || !blocks.some((b) => b.type === "tool_result")) {
      const msg: Obj = { role: m.role, content: text || null };
      if (uses.length) {
        msg.tool_calls = uses.map((b) => ({
          id: b.id,
          type: "function",
          function: { name: b.name, arguments: JSON.stringify(b.input ?? {}) },
        }));
      }
      out.push(msg);
    }
  }

  return out;
}
