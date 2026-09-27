import { proxyJson } from "@/lib/api-proxy";

export function GET(): Promise<Response> {
	return proxyJson("/api/v1/edit/capabilities");
}
