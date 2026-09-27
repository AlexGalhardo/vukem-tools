import { proxyJson } from "@/lib/api-proxy";

/** Healthcheck do backend; o badge da navbar lê os headers de deploy desta resposta. */
export function GET(): Promise<Response> {
	return proxyJson("/health");
}
