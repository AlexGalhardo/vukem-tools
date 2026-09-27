import { proxyUpload } from "@/lib/api-proxy";

export const runtime = "nodejs";

export function POST(req: Request): Promise<Response> {
	return proxyUpload(req, "/api/v1/edit/split");
}
