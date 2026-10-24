import { useEffect, useMemo, useState } from "react";

import {
  cepNormalize,
  cnpjFormat,
  cnpjGenerate,
  cnpjValid,
  cpfFormat,
  cpfGenerate,
  cpfValid,
  fakePerson,
  type Person,
} from "../../lib/dev/brazil";
import { toCSV } from "../../lib/dev/convert";
import { Button, Check, Copy, msg, Notice, Num, Output, Stack, Text } from "./ui";

export function CpfCnpj(): React.ReactElement {
  const [cpf, setCpf] = useState("");
  const [cnpj, setCnpj] = useState("");
  const [masked, setMasked] = useState(true);
  const [alnum, setAlnum] = useState(false);
  const [generated, setGenerated] = useState<{ cpf: string; cnpj: string }>({ cpf: "", cnpj: "" });

  const make = (): void => {
    const c = cpfGenerate();
    const j = cnpjGenerate(alnum);
    setGenerated({ cpf: masked ? cpfFormat(c) : c, cnpj: masked ? cnpjFormat(j) : j });
  };

  const verdict = (ok: boolean, text: string): React.ReactElement | null =>
    text.trim() ? <Notice kind={ok ? "ok" : "error"}>{ok ? "The check digits are correct." : "The check digits are wrong."}</Notice> : null;

  return (
    <Stack>
      <section className="space-y-4">
        <h2 className="text-lg font-medium">Generate</h2>
        <div className="flex flex-wrap items-center gap-4">
          <Check label="With punctuation" checked={masked} onChange={setMasked} />
          <Check label="Alphanumeric CNPJ" checked={alnum} onChange={setAlnum} />
          <Button onClick={make}>Generate a pair</Button>
        </div>
        {generated.cpf ? (
          <dl className="divide-y divide-edge rounded-panel border border-edge bg-raised">
            {[
              ["CPF", generated.cpf],
              ["CNPJ", generated.cnpj],
            ].map(([k, v]) => (
              <div key={k} className="flex items-center gap-3 px-3 py-3">
                <dt className="w-14 text-sm font-medium">{k}</dt>
                <dd className="flex-1 font-mono text-sm">{v}</dd>
                <Copy text={v!} />
              </div>
            ))}
          </dl>
        ) : null}
        <p className="text-xs text-ink-faint">
          These numbers pass the check-digit rule and belong to nobody in particular. Use them in tests, never to
          register or impersonate.
        </p>
      </section>
      <section className="space-y-4">
        <h2 className="text-lg font-medium">Validate</h2>
        <Text label="CPF" value={cpf} onChange={setCpf} placeholder="000.000.000-00" inputMode="numeric" />
        {verdict(cpfValid(cpf), cpf)}
        <Text label="CNPJ" value={cnpj} onChange={setCnpj} placeholder="00.000.000/0000-00 or 12.ABC.345/01DE-35" />
        {verdict(cnpjValid(cnpj), cnpj)}
        <p className="text-xs text-ink-faint">
          Check digits only prove the number is well formed. They do not prove it was ever issued.
        </p>
      </section>
    </Stack>
  );
}

interface Lookup<T> {
  state: "idle" | "loading" | "done" | "error";
  data?: T;
  error?: string;
}

/** useLookup fetches /lookup/<kind>/<value>; the server holds the provider's
 *  address, so the browser never talks to a third party. */
function useLookup<T>(kind: "cep" | "cnpj", value: string | null): Lookup<T> {
  const [s, setS] = useState<Lookup<T>>({ state: "idle" });

  useEffect(() => {
    if (!value) {
      setS({ state: "idle" });

      return;
    }
    const ctl = new AbortController();
    setS({ state: "loading" });
    fetch(`/lookup/${kind}/${value}`, { signal: ctl.signal, headers: { Accept: "application/json" } })
      .then(async (r) => {
        const body = (await r.json().catch(() => ({}))) as T & { error?: string };
        if (!r.ok) throw new Error(body.error ?? `The lookup failed (${r.status}).`);
        setS({ state: "done", data: body });
      })
      .catch((e: unknown) => {
        if (!ctl.signal.aborted) setS({ state: "error", error: msg(e) });
      });

    return () => ctl.abort();
  }, [kind, value]);

  return s;
}

function Record({ rows }: { rows: [string, unknown][] }): React.ReactElement {
  return (
    <dl className="divide-y divide-edge rounded-panel border border-edge bg-raised text-sm">
      {rows
        .filter(([, v]) => v !== undefined && v !== null && v !== "")
        .map(([k, v]) => (
          <div key={k} className="flex flex-col gap-0.5 px-3 py-2 sm:flex-row sm:gap-4">
            <dt className="w-44 shrink-0 text-ink-soft">{k}</dt>
            <dd className="min-w-0 break-words text-ink">{String(v)}</dd>
          </div>
        ))}
    </dl>
  );
}

interface CepData {
  cep: string;
  logradouro: string;
  complemento: string;
  bairro: string;
  localidade: string;
  uf: string;
  ibge: string;
  ddd: string;
}

export function CepLookup(): React.ReactElement {
  const [input, setInput] = useState("");
  const cep = cepNormalize(input);
  const r = useLookup<CepData>("cep", cep);

  return (
    <Stack>
      <Text label="CEP" value={input} onChange={setInput} placeholder="01310-100" inputMode="numeric" />
      {input && !cep ? <Notice kind="warn">A CEP has eight digits.</Notice> : null}
      {r.state === "loading" ? <p className="text-sm text-ink-soft">Looking up…</p> : null}
      {r.state === "error" ? <Notice kind="error">{r.error}</Notice> : null}
      {r.data ? (
        <Record
          rows={[
            ["CEP", r.data.cep],
            ["Street", r.data.logradouro],
            ["Complement", r.data.complemento],
            ["District", r.data.bairro],
            ["City", r.data.localidade],
            ["State", r.data.uf],
            ["IBGE code", r.data.ibge],
            ["Area code (DDD)", r.data.ddd],
          ]}
        />
      ) : null}
      <p className="text-xs text-ink-faint">Data from ViaCEP, fetched by this server. Results are cached for an hour.</p>
    </Stack>
  );
}

interface CnpjData {
  cnpj: string;
  razao_social: string;
  nome_fantasia: string;
  descricao_situacao_cadastral: string;
  data_inicio_atividade: string;
  cnae_fiscal_descricao: string;
  natureza_juridica: string;
  porte: string;
  logradouro: string;
  numero: string;
  bairro: string;
  municipio: string;
  uf: string;
  cep: string;
  ddd_telefone_1: string;
}

export function CnpjLookup(): React.ReactElement {
  const [input, setInput] = useState("");
  const clean = input.toUpperCase().replace(/[^0-9A-Z]/g, "");
  const valid = cnpjValid(clean);
  const r = useLookup<CnpjData>("cnpj", valid ? clean : null);

  return (
    <Stack>
      <Text label="CNPJ" value={input} onChange={setInput} placeholder="00.000.000/0000-00" />
      {clean.length >= 14 && !valid ? <Notice kind="error">The check digits are wrong, so nothing was looked up.</Notice> : null}
      {r.state === "loading" ? <p className="text-sm text-ink-soft">Looking up…</p> : null}
      {r.state === "error" ? <Notice kind="error">{r.error}</Notice> : null}
      {r.data ? (
        <Record
          rows={[
            ["CNPJ", cnpjFormat(r.data.cnpj)],
            ["Legal name", r.data.razao_social],
            ["Trade name", r.data.nome_fantasia],
            ["Status", r.data.descricao_situacao_cadastral],
            ["Opened", r.data.data_inicio_atividade],
            ["Main activity", r.data.cnae_fiscal_descricao],
            ["Legal nature", r.data.natureza_juridica],
            ["Size", r.data.porte],
            ["Address", [r.data.logradouro, r.data.numero, r.data.bairro].filter(Boolean).join(", ")],
            ["City", r.data.municipio && `${r.data.municipio}/${r.data.uf}`],
            ["CEP", r.data.cep],
            ["Phone", r.data.ddd_telefone_1],
          ]}
        />
      ) : null}
      <p className="text-xs text-ink-faint">
        Data from BrasilAPI, a free community service in beta with no uptime promise. A company's registration data
        is public.
      </p>
    </Stack>
  );
}

export function FakePerson(): React.ReactElement {
  const [count, setCount] = useState(5);
  const [format, setFormat] = useState<"json" | "csv">("json");
  const [seed, setSeed] = useState(0);

  const people = useMemo<Person[]>(() => {
    void seed;

    return Array.from({ length: Math.min(200, Math.max(1, count || 1)) }, () => fakePerson());
  }, [count, seed]);
  const out = useMemo(
    () =>
      format === "json"
        ? JSON.stringify(people, null, 2)
        : toCSV(people.map(({ address, ...p }) => ({ ...p, ...Object.fromEntries(Object.entries(address).map(([k, v]) => [`address_${k}`, v])) }))),
    [people, format],
  );

  return (
    <Stack>
      <div className="flex flex-wrap items-end gap-4">
        <Num label="How many" value={count} min={1} max={200} onChange={setCount} />
        <div className="flex gap-2">
          <Button variant={format === "json" ? "primary" : "quiet"} onClick={() => setFormat("json")}>
            JSON
          </Button>
          <Button variant={format === "csv" ? "primary" : "quiet"} onClick={() => setFormat("csv")}>
            CSV
          </Button>
        </div>
        <Button onClick={() => setSeed(seed + 1)}>Generate</Button>
      </div>
      <Output label="People" value={out} rows={16} />
      <p className="text-xs text-ink-faint">
        Every person is invented. Emails use example.com, which is reserved for documentation, and the CPFs are random
        numbers that pass the check-digit rule.
      </p>
    </Stack>
  );
}
