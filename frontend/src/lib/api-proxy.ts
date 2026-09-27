/**
 * Proxy server-side para o backend FastAPI. O browser só fala com `/api/*` do Next:
 * a URL e a `API_KEY` do backend nunca chegam ao cliente.
 */
import {
	API_BASE_URL,
	API_KEY,
	FRONTEND_DEPLOY,
	sanitizeDeployLabel,
} from "@/lib/config";
import {
	BACKEND_DEPLOY_HEADER,
	BACKEND_UNREACHABLE,
	FRONTEND_DEPLOY_HEADER,
} from "@/lib/deploy-info";

const PASSTHROUGH_HEADERS = [
	"content-type",
	"content-disposition",
	"content-length",
	"x-clip-count",
];

const JOB_ID = /^[0-9a-f]{32}$/;
const CLIP_INDEX = /^[1-9]\d{0,3}$/;

export function isJobId(value: string): boolean {
	return JOB_ID.test(value);
}

export function isClipIndex(value: string): boolean {
	return CLIP_INDEX.test(value);
}

/**
 * Identifica quem respondeu: o deploy deste frontend e o do backend (header
 * `X-Deploy-Target` do FastAPI). `upstream` nulo = o backend não chegou a responder.
 */
export function deployHeaders(upstream?: Response | null): Headers {
	const headers = new Headers({ [FRONTEND_DEPLOY_HEADER]: FRONTEND_DEPLOY });
	if (upstream !== undefined) {
		headers.set(
			BACKEND_DEPLOY_HEADER,
			upstream
				? sanitizeDeployLabel(
						upstream.headers.get("x-deploy-target") ?? "",
						"desconhecido",
					)
				: BACKEND_UNREACHABLE,
		);
	}
	return headers;
}

export function jsonError(
	detail: string,
	status: number,
	errorType?: string,
	headers: Headers = deployHeaders(),
): Response {
	return Response.json(errorType ? { detail, error_type: errorType } : { detail }, {
		status,
		headers,
	});
}

const UNREACHABLE = () =>
	jsonError(
		"O backend está indisponível no momento.",
		502,
		"NETWORK_ERROR",
		deployHeaders(null),
	);

function backendHeaders(extra: HeadersInit = {}): Headers {
	const headers = new Headers(extra);
	if (API_KEY) headers.set("X-API-Key", API_KEY);
	return headers;
}

/** Lê o corpo como texto antes do JSON.parse para relatar respostas não-JSON com contexto. */
export async function readUpstreamJson(upstream: Response): Promise<unknown> {
	const raw = await upstream.text();
	if (!raw.trim()) return { detail: `O backend respondeu ${upstream.status} sem corpo.` };
	try {
		return JSON.parse(raw);
	} catch {
		return {
			detail: `O backend respondeu ${upstream.status} com um corpo inválido.`,
		};
	}
}

async function callBackend(
	path: string,
	init: RequestInit = {},
): Promise<Response | null> {
	try {
		return await fetch(`${API_BASE_URL}${path}`, {
			cache: "no-store",
			...init,
			headers: backendHeaders(init.headers),
		});
	} catch {
		return null;
	}
}

/** Repassa uma resposta JSON (sucesso ou `{detail, error_type}`) com o mesmo status. */
export async function proxyJson(path: string, init?: RequestInit): Promise<Response> {
	const upstream = await callBackend(path, init);
	if (!upstream) return UNREACHABLE();
	return Response.json(await readUpstreamJson(upstream), {
		status: upstream.status,
		headers: deployHeaders(upstream),
	});
}

/**
 * Repassa um arquivo em streaming nos dois sentidos (upload multipart e download),
 * sem carregar o conteúdo inteiro em memória.
 */
export async function proxyStream(
	path: string,
	init: RequestInit & { duplex?: "half" } = {},
): Promise<Response> {
	const upstream = await callBackend(path, init);
	if (!upstream) return UNREACHABLE();
	if (!upstream.ok || !upstream.body) {
		return Response.json(await readUpstreamJson(upstream), {
			status: upstream.status || 502,
			headers: deployHeaders(upstream),
		});
	}

	const headers = deployHeaders(upstream);
	headers.set("Cache-Control", "no-store");
	for (const name of PASSTHROUGH_HEADERS) {
		const value = upstream.headers.get(name);
		if (value) headers.set(name, value);
	}
	return new Response(upstream.body, { status: 200, headers });
}

/** Upload multipart repassado sem bufferizar (`duplex: "half"` é exigido pelo undici). */
export function proxyUpload(req: Request, path: string): Promise<Response> {
	return proxyStream(path, {
		method: "POST",
		headers: { "Content-Type": req.headers.get("content-type") ?? "multipart/form-data" },
		body: req.body,
		duplex: "half",
	});
}
