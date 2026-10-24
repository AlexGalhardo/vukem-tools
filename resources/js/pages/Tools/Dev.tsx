import { Head } from "@inertiajs/react";

import { CepLookup, CnpjLookup, CpfCnpj, FakePerson } from "../../components/dev/Brazil";
import { Base64Tool, DataConvert, DiffTool, JsonFormat, JsonSchema } from "../../components/dev/Data";
import { ChatTemplate, LlmClean, LlmCost, TokenCount } from "../../components/dev/Llm";
import { HashTool, JwtTool, PasswordTool, UuidTool } from "../../components/dev/Secrets";

interface Props {
  tool: { name: string; title: string; summary: string };
}

/** tools maps a catalogue name to its interface. A client tool in the
 *  catalogue without an entry here is a bug that tools/dev_test.go catches. */
export const tools: Record<string, () => React.ReactElement> = {
  "json-format": JsonFormat,
  "json-schema": JsonSchema,
  "data-convert": DataConvert,
  diff: DiffTool,
  hash: HashTool,
  password: PasswordTool,
  uuid: UuidTool,
  base64: Base64Tool,
  "jwt-decode": JwtTool,
  "cpf-cnpj": CpfCnpj,
  "cep-lookup": CepLookup,
  "cnpj-lookup": CnpjLookup,
  "fake-person": FakePerson,
  "token-count": TokenCount,
  "llm-clean": LlmClean,
  "llm-cost": LlmCost,
  "chat-template": ChatTemplate,
};

/**
 * The page of every developer tool. They run in the browser, so there is no
 * upload, no queue and no result to download: the interface is the tool.
 */
export default function Dev({ tool }: Props): React.ReactElement {
  const Tool = tools[tool.name];

  return (
    <>
      <Head title={tool.title} />
      <div className="max-w-[70ch]">
        <h1 className="text-3xl font-semibold tracking-tight">{tool.title}</h1>
        <p className="mt-3 text-ink-soft">{tool.summary}</p>
      </div>
      <div className="mt-8 max-w-3xl">
        {Tool ? <Tool /> : <p role="alert">This tool has no interface yet.</p>}
      </div>
    </>
  );
}
