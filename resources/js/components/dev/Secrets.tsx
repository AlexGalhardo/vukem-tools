import { useEffect, useMemo, useState } from "react";

import {
  algorithms,
  decodeId,
  decodeJwt,
  digest,
  entropyBits,
  generatePassword,
  hmac,
  toBase64,
  toHex,
  ulid,
  utf8,
  uuidV4,
  uuidV7,
  type Algorithm,
  type PasswordOptions,
} from "../../lib/dev/crypto";
import { Area, Button, Check, Copy, Grid, msg, Notice, Num, Output, Pick, Stack, Text } from "./ui";

export function HashTool(): React.ReactElement {
  const [input, setInput] = useState<"text" | "file">("text");
  const [text, setText] = useState("hello");
  const [file, setFile] = useState<File | null>(null);
  const [key, setKey] = useState("");
  const [rows, setRows] = useState<{ alg: Algorithm; hex: string; b64: string }[]>([]);
  const [error, setError] = useState("");

  useEffect(() => {
    let live = true;
    void (async () => {
      try {
        // A file is read whole into memory: Web Crypto has no streaming digest,
        // so a very large file is refused rather than freezing the tab.
        if (input === "file" && file && file.size > 256 * 1024 * 1024) {
          throw new Error("Files over 256 MB are too big to hash in the browser.");
        }
        const data = input === "file" ? (file ? new Uint8Array(await file.arrayBuffer()) : null) : utf8(text);
        if (!data) {
          if (live) setRows([]);

          return;
        }
        const k = utf8(key);
        const out = await Promise.all(
          algorithms.map(async (alg) => {
            const bytes = key ? await hmac(alg, k, data) : await digest(alg, data);

            return { alg, hex: toHex(bytes), b64: toBase64(bytes) };
          }),
        );
        if (live) {
          setRows(out);
          setError("");
        }
      } catch (e) {
        if (live) setError(msg(e));
      }
    })();

    return () => {
      live = false;
    };
  }, [input, text, file, key]);

  return (
    <Stack>
      <Pick
        label="Input"
        value={input}
        onChange={setInput}
        options={[
          { value: "text", label: "Text" },
          { value: "file", label: "File" },
        ]}
      />
      {input === "text" ? (
        <Area label="Text" value={text} onChange={setText} rows={5} />
      ) : (
        <div>
          <label htmlFor="hash-file" className="label block">
            File
          </label>
          <input
            id="hash-file"
            type="file"
            onChange={(e) => setFile(e.target.files?.[0] ?? null)}
            className="mt-2 block w-full text-sm text-ink-soft"
          />
          <p className="mt-1 text-xs text-ink-faint">The file is hashed in your browser and never uploaded.</p>
        </div>
      )}
      <Text label="HMAC key (optional; leave empty for a plain hash)" value={key} onChange={setKey} />
      {error ? <Notice kind="error">{error}</Notice> : null}
      <div className="divide-y divide-edge rounded-panel border border-edge bg-raised">
        {rows.map((r) => (
          <div key={r.alg} className="flex flex-col gap-1 px-3 py-3 sm:flex-row sm:items-center sm:gap-3">
            <span className="w-20 shrink-0 text-sm font-medium text-ink">{r.alg}</span>
            <code className="min-w-0 flex-1 break-all font-mono text-xs text-ink-soft">{r.hex}</code>
            <Copy text={r.hex} />
          </div>
        ))}
        {rows.length === 0 ? <p className="px-3 py-3 text-sm text-ink-faint">Nothing to hash yet.</p> : null}
      </div>
      {rows[0] ? <p className="text-xs text-ink-faint">Base64 of {rows[0].alg}: {rows[0].b64}</p> : null}
      <p className="text-xs text-ink-faint">
        MD5 and SHA-1 are broken for security. Use them to check a download, not to protect anything.
      </p>
    </Stack>
  );
}

export function PasswordTool(): React.ReactElement {
  const [o, setO] = useState<PasswordOptions>({
    length: 20,
    lower: true,
    upper: true,
    digits: true,
    symbols: true,
    noAmbiguous: false,
  });
  const [seed, setSeed] = useState(0);

  const result = useMemo(() => {
    void seed;
    try {
      return { password: generatePassword(o), error: "" };
    } catch (e) {
      return { password: "", error: msg(e) };
    }
  }, [o, seed]);
  const bits = Math.round(entropyBits(o));
  const set = <K extends keyof PasswordOptions>(k: K, v: PasswordOptions[K]): void => setO({ ...o, [k]: v });

  return (
    <Stack>
      <div className="rounded-panel border border-edge bg-raised p-4">
        <code className="block break-all font-mono text-lg text-ink">{result.password || " "}</code>
        <div className="mt-3 flex flex-wrap items-center gap-3">
          <Copy text={result.password} />
          <Button variant="quiet" onClick={() => setSeed(seed + 1)}>
            Generate another
          </Button>
          <span className="text-sm text-ink-soft">
            {bits} bits of entropy &middot; {bits >= 100 ? "very strong" : bits >= 70 ? "strong" : bits >= 50 ? "fair" : "weak"}
          </span>
        </div>
      </div>
      {result.error ? <Notice kind="error">{result.error}</Notice> : null}
      <Grid>
        <Num label="Length" value={o.length} min={4} max={128} onChange={(v) => set("length", Math.min(128, Math.max(4, v || 4)))} />
        <div>
          <Check label="Lowercase (a-z)" checked={o.lower} onChange={(v) => set("lower", v)} />
          <Check label="Uppercase (A-Z)" checked={o.upper} onChange={(v) => set("upper", v)} />
          <Check label="Digits (0-9)" checked={o.digits} onChange={(v) => set("digits", v)} />
          <Check label="Symbols" checked={o.symbols} onChange={(v) => set("symbols", v)} />
          <Check label="Avoid look-alikes (O 0 l 1 I)" checked={o.noAmbiguous} onChange={(v) => set("noAmbiguous", v)} />
        </div>
      </Grid>
      <p className="text-xs text-ink-faint">Generated in your browser from the secure random source. Nothing is sent anywhere.</p>
    </Stack>
  );
}

export function UuidTool(): React.ReactElement {
  const [kind, setKind] = useState<"v4" | "v7" | "ulid">("v7");
  const [count, setCount] = useState(5);
  const [seed, setSeed] = useState(0);
  const [probe, setProbe] = useState("");

  const ids = useMemo(() => {
    void seed;
    const make = kind === "v4" ? uuidV4 : kind === "v7" ? () => uuidV7() : () => ulid();

    return Array.from({ length: Math.min(100, Math.max(1, count || 1)) }, make);
  }, [kind, count, seed]);
  const decoded = probe.trim() ? decodeId(probe) : null;

  return (
    <Stack>
      <Grid>
        <Pick
          label="Kind"
          value={kind}
          onChange={setKind}
          options={[
            { value: "v7", label: "UUID v7 (time-ordered)" },
            { value: "v4", label: "UUID v4 (random)" },
            { value: "ulid", label: "ULID" },
          ]}
        />
        <Num label="How many" value={count} min={1} max={100} onChange={setCount} />
      </Grid>
      <div className="flex gap-3">
        <Button onClick={() => setSeed(seed + 1)}>Generate</Button>
        <Copy text={ids.join("\n")} />
      </div>
      <Output label="Identifiers" value={ids.join("\n")} rows={Math.min(10, ids.length + 1)} />
      <Text label="Read the time out of a UUID v7 or ULID" value={probe} onChange={setProbe} placeholder="Paste an identifier" />
      {probe.trim() ? (
        decoded ? (
          <Notice kind="ok">
            {decoded.kind} made at {decoded.time.toISOString()}
          </Notice>
        ) : (
          <Notice kind="warn">That is not a UUID v7 or a ULID, so it carries no time.</Notice>
        )
      ) : null}
    </Stack>
  );
}

export function JwtTool(): React.ReactElement {
  const [token, setToken] = useState("");

  const result = useMemo(() => {
    if (!token.trim()) return null;
    try {
      return { jwt: decodeJwt(token) };
    } catch (e) {
      return { error: msg(e) };
    }
  }, [token]);
  const jwt = result && "jwt" in result ? result.jwt : null;
  const expired = jwt?.expiresAt ? jwt.expiresAt.getTime() < Date.now() : null;

  return (
    <Stack>
      <Area label="Token" value={token} onChange={setToken} rows={6} placeholder="eyJhbGciOi…" />
      <Notice kind="warn">
        This only decodes the token. It does not check the signature, so a decoded token is not a trusted one.
      </Notice>
      {result && "error" in result ? <Notice kind="error">{result.error}</Notice> : null}
      {jwt ? (
        <>
          {expired !== null ? (
            <Notice kind={expired ? "error" : "ok"}>
              {expired ? "Expired" : "Not expired"}: exp {jwt.expiresAt?.toISOString()}
            </Notice>
          ) : null}
          {jwt.issuedAt ? <p className="text-sm text-ink-soft">Issued at {jwt.issuedAt.toISOString()}</p> : null}
          {jwt.notBefore ? <p className="text-sm text-ink-soft">Not valid before {jwt.notBefore.toISOString()}</p> : null}
          {!jwt.signaturePresent ? <Notice kind="error">The token has an empty signature.</Notice> : null}
          <Output label="Header" value={JSON.stringify(jwt.header, null, 2)} rows={5} />
          <Output label="Payload" value={JSON.stringify(jwt.payload, null, 2)} rows={10} />
        </>
      ) : null}
    </Stack>
  );
}
