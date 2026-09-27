/** Chamadas do browser para as rotas `/api/*` do próprio Next. */
import type { z } from "zod";
import { resolveErrorMessage } from "@/lib/errors";
import { apiErrorSchema } from "@/lib/schemas";

/** `fetch` que lança `Error` com a mensagem curada do backend quando a resposta não é 2xx. */
export async function request(
	input: string,
	init: RequestInit | undefined,
	fallback: string,
): Promise<Response> {
	const res = await fetch(input, init);
	if (res.ok) return res;
	const parsed = apiErrorSchema.safeParse(await res.json().catch(() => null));
	throw new Error(
		resolveErrorMessage(
			parsed.success ? parsed.data.error_type : null,
			parsed.success ? parsed.data.detail : null,
			fallback,
		),
	);
}

export async function requestJson<T>(
	input: string,
	init: RequestInit | undefined,
	schema: z.ZodType<T>,
	fallback: string,
): Promise<T> {
	const res = await request(input, init, fallback);
	return schema.parse(await res.json());
}

export function postJson(body: unknown): RequestInit {
	return {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify(body),
	};
}

export function errorMessage(error: unknown, fallback = "Erro inesperado."): string {
	return error instanceof Error ? error.message : fallback;
}

export function triggerBrowserDownload(href: string, filename?: string | null): void {
	const a = document.createElement("a");
	a.href = href;
	if (filename) a.download = filename;
	document.body.appendChild(a);
	a.click();
	a.remove();
}
