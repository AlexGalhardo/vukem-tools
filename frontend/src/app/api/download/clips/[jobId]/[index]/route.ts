import { isClipIndex, isJobId, jsonError, proxyStream } from "@/lib/api-proxy";

export const runtime = "nodejs";

export async function GET(
	_req: Request,
	ctx: RouteContext<"/api/download/clips/[jobId]/[index]">,
): Promise<Response> {
	const { jobId, index } = await ctx.params;
	if (!isJobId(jobId) || !isClipIndex(index)) {
		return jsonError("Clipe inválido.", 400, "INVALID_REQUEST");
	}
	return proxyStream(`/api/v1/downloads/${jobId}/clips/${index}/file`);
}
