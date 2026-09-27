import { forwardDownloadRequest } from "@/lib/download-request";

export function POST(req: Request): Promise<Response> {
	return forwardDownloadRequest(req, "download/start");
}
