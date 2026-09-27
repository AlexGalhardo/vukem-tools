import { isJobId, jsonError, proxyStream } from "@/lib/api-proxy";

export const runtime = "nodejs";

export async function GET(
	_req: Request,
	ctx: RouteContext<"/api/download/file/[jobId]">,
): Promise<Response> {
	const { jobId } = await ctx.params;
	if (!isJobId(jobId)) return jsonError("Job inválido.", 400, "INVALID_REQUEST");
	return proxyStream(`/api/v1/downloads/${jobId}/file`);
}
