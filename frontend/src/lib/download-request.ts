import { jsonError, proxyJson } from "@/lib/api-proxy";
import { PLATFORM_PREFIX } from "@/lib/config";
import { proxyDownloadRequestSchema } from "@/lib/schemas";

type Endpoint = "info" | "download/start" | "download/clips/start";

/** Valida o JSON do browser e repassa para `{prefixo da plataforma}/{endpoint}`. */
export async function forwardDownloadRequest(
	req: Request,
	endpoint: Endpoint,
): Promise<Response> {
	const parsed = proxyDownloadRequestSchema.safeParse(await req.json().catch(() => null));
	if (!parsed.success) {
		return jsonError(
			"Requisição inválida: use uma URL do YouTube, Instagram, X ou TikTok.",
			400,
			"INVALID_REQUEST",
		);
	}
	const { platform, url, format, quality } = parsed.data;
	return proxyJson(`${PLATFORM_PREFIX[platform]}/${endpoint}`, {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		// O download simples sai comprimido; os clipes já são reencodados no fatiamento.
		body: JSON.stringify({
			url,
			format,
			quality,
			compress: endpoint === "download/start",
		}),
	});
}
