import { isJobId, jsonError, proxyJson } from "@/lib/api-proxy";

export async function GET(
	_req: Request,
	ctx: RouteContext<"/api/download/status/[jobId]">,
): Promise<Response> {
	const { jobId } = await ctx.params;
	if (!isJobId(jobId)) return jsonError("Job inválido.", 400, "INVALID_REQUEST");
	return proxyJson(`/api/v1/downloads/${jobId}`);
}
