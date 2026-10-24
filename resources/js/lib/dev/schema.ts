type Obj = Record<string, unknown>;

export interface Issue {
  path: string;
  message: string;
}

const isObj = (v: unknown): v is Obj => typeof v === "object" && v !== null && !Array.isArray(v);

function typeOf(v: unknown): string {
  if (v === null) return "null";
  if (Array.isArray(v)) return "array";
  if (typeof v === "number") return Number.isInteger(v) ? "integer" : "number";

  return typeof v;
}

const matches = (v: unknown, t: string): boolean => typeOf(v) === t || (t === "number" && typeOf(v) === "integer");

function deepEqual(a: unknown, b: unknown): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

function resolveRef(root: Obj, ref: string): unknown {
  if (!ref.startsWith("#")) throw new Error(`only local $ref values are supported, got ${ref}`);
  let cur: unknown = root;
  for (const seg of ref.slice(1).split("/").filter(Boolean)) {
    cur = isObj(cur) || Array.isArray(cur) ? (cur as Obj)[seg.replace(/~1/g, "/").replace(/~0/g, "~")] : undefined;
  }
  if (cur === undefined) throw new Error(`$ref ${ref} does not resolve`);

  return cur;
}

/** validate checks an instance against the JSON Schema keywords that matter
 *  for data and tool definitions: type, enum, const, properties, required,
 *  additionalProperties, items, length and range limits, pattern, format-free
 *  string checks, allOf, anyOf, oneOf, not and local $ref. */
export function validate(schema: unknown, instance: unknown): Issue[] {
  const issues: Issue[] = [];
  const root = isObj(schema) ? schema : {};
  const walk = (s: unknown, v: unknown, path: string, depth: number): void => {
    if (s === true || s === undefined) return;
    if (s === false) {
      issues.push({ path, message: "no value is allowed here" });

      return;
    }
    if (!isObj(s)) return;
    if (depth > 50) throw new Error("the schema nests too deeply or refers to itself");
    const fail = (message: string): void => void issues.push({ path, message });

    if (typeof s.$ref === "string") walk(resolveRef(root, s.$ref), v, path, depth + 1);
    if (s.type !== undefined) {
      const types = Array.isArray(s.type) ? (s.type as string[]) : [s.type as string];
      if (!types.some((t) => matches(v, t))) fail(`expected ${types.join(" or ")}, got ${typeOf(v)}`);
    }
    if (Array.isArray(s.enum) && !s.enum.some((e) => deepEqual(e, v))) fail(`must be one of ${JSON.stringify(s.enum)}`);
    if ("const" in s && !deepEqual(s.const, v)) fail(`must equal ${JSON.stringify(s.const)}`);

    if (typeof v === "string") {
      if (typeof s.minLength === "number" && [...v].length < s.minLength) fail(`shorter than ${s.minLength} characters`);
      if (typeof s.maxLength === "number" && [...v].length > s.maxLength) fail(`longer than ${s.maxLength} characters`);
      if (typeof s.pattern === "string") {
        try {
          if (!new RegExp(s.pattern, "u").test(v)) fail(`does not match ${s.pattern}`);
        } catch {
          fail(`the schema's pattern ${s.pattern} is not a valid regular expression`);
        }
      }
    }
    if (typeof v === "number") {
      if (typeof s.minimum === "number" && v < s.minimum) fail(`below the minimum ${s.minimum}`);
      if (typeof s.maximum === "number" && v > s.maximum) fail(`above the maximum ${s.maximum}`);
      if (typeof s.exclusiveMinimum === "number" && v <= s.exclusiveMinimum) fail(`must be above ${s.exclusiveMinimum}`);
      if (typeof s.exclusiveMaximum === "number" && v >= s.exclusiveMaximum) fail(`must be below ${s.exclusiveMaximum}`);
      if (typeof s.multipleOf === "number" && v % s.multipleOf !== 0) fail(`not a multiple of ${s.multipleOf}`);
    }
    if (Array.isArray(v)) {
      if (typeof s.minItems === "number" && v.length < s.minItems) fail(`fewer than ${s.minItems} items`);
      if (typeof s.maxItems === "number" && v.length > s.maxItems) fail(`more than ${s.maxItems} items`);
      if (s.uniqueItems === true && new Set(v.map((x) => JSON.stringify(x))).size !== v.length) fail("items are not unique");
      if (s.items !== undefined) v.forEach((x, i) => walk(s.items, x, `${path}[${i}]`, depth + 1));
    }
    if (isObj(v)) {
      const props = isObj(s.properties) ? s.properties : {};
      if (Array.isArray(s.required)) {
        for (const k of s.required as string[]) if (!(k in v)) issues.push({ path: `${path}.${k}`, message: "is required" });
      }
      for (const [k, x] of Object.entries(v)) {
        if (k in props) walk(props[k], x, `${path}.${k}`, depth + 1);
        else if (s.additionalProperties === false) issues.push({ path: `${path}.${k}`, message: "is not allowed" });
        else if (isObj(s.additionalProperties)) walk(s.additionalProperties, x, `${path}.${k}`, depth + 1);
      }
    }
    if (Array.isArray(s.allOf)) s.allOf.forEach((sub) => walk(sub, v, path, depth + 1));
    if (Array.isArray(s.anyOf) && !s.anyOf.some((sub) => validate2(root, sub, v, depth).length === 0)) fail("matches none of the anyOf options");
    if (Array.isArray(s.oneOf)) {
      const n = s.oneOf.filter((sub) => validate2(root, sub, v, depth).length === 0).length;
      if (n !== 1) fail(`matches ${n} of the oneOf options, not exactly one`);
    }
    if (s.not !== undefined && validate2(root, s.not, v, depth).length === 0) fail("matches a schema it must not match");
  };
  walk(root, instance, "$", 0);

  return issues;
}

function validate2(root: Obj, sub: unknown, v: unknown, depth: number): Issue[] {
  if (depth > 50) throw new Error("the schema nests too deeply or refers to itself");

  return validate(isObj(sub) && "$ref" in sub ? { ...root, ...sub } : sub, v);
}

/* ---------- tool definitions ---------- */

export interface ToolCheck {
  format: "openai" | "anthropic" | "bare" | "unknown";
  errors: string[];
  warnings: string[];
}

const NAME = /^[a-zA-Z0-9_-]{1,64}$/;

/** checkToolDefinition looks at an LLM tool definition for what makes a model
 *  call it badly: a name providers reject, a missing description, a parameters
 *  schema that is not an object, required fields that are not declared. */
export function checkToolDefinition(def: unknown): ToolCheck {
  const out: ToolCheck = { format: "unknown", errors: [], warnings: [] };
  if (!isObj(def)) {
    out.errors.push("a tool definition is a JSON object");

    return out;
  }
  let name: unknown;
  let description: unknown;
  let params: unknown;
  if (def.type === "function" && isObj(def.function)) {
    out.format = "openai";
    ({ name, description, parameters: params } = def.function as Obj);
  } else if ("input_schema" in def) {
    out.format = "anthropic";
    ({ name, description, input_schema: params } = def);
  } else {
    out.format = "bare";
    ({ name, description } = def);
    params = def.parameters ?? def.input_schema;
  }
  if (typeof name !== "string" || !NAME.test(name)) out.errors.push("name must be 1-64 characters of letters, digits, _ or -");
  if (typeof description !== "string" || description.trim() === "") {
    out.warnings.push("no description: the model chooses tools by reading it");
  } else if (description.length < 20) {
    out.warnings.push("the description is very short; say when to use the tool and when not to");
  }
  if (!isObj(params)) {
    out.errors.push("no parameters schema; use {\"type\":\"object\",\"properties\":{}} for a tool with no arguments");

    return out;
  }
  if (params.type !== "object") out.errors.push('the parameters schema must have "type": "object" at its root');
  const props = isObj(params.properties) ? params.properties : {};
  if (Array.isArray(params.required)) {
    for (const k of params.required) if (!(k in props)) out.errors.push(`"${String(k)}" is required but not declared in properties`);
  }
  for (const [k, p] of Object.entries(props)) {
    if (!isObj(p)) {
      out.errors.push(`property "${k}" is not a schema object`);
      continue;
    }
    if (p.type === undefined && p.$ref === undefined && p.anyOf === undefined && p.oneOf === undefined && p.enum === undefined) {
      out.warnings.push(`property "${k}" has no type`);
    }
    if (typeof p.description !== "string") out.warnings.push(`property "${k}" has no description`);
  }
  if (params.additionalProperties === undefined && out.format === "openai") {
    out.warnings.push('set "additionalProperties": false (required for OpenAI strict mode)');
  }

  return out;
}
