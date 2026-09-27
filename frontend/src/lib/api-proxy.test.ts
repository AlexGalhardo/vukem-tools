import { afterEach, describe, expect, mock, test } from "bun:test";
import {
	isClipIndex,
	isJobId,
	proxyJson,
	proxyStream,
	readUpstreamJson,
} from "@/lib/api-proxy";

const realFetch = globalThis.fetch;

afterEach(() => {
	globalThis.fetch = realFetch;
});

describe("identificadores", () => {
	test("aceita só job ids hex de 32 caracteres", () => {
		expect(isJobId("a".repeat(32))).toBe(true);
		expect(isJobId("../../etc/passwd")).toBe(false);
		expect(isJobId("A".repeat(32))).toBe(false);
	});

	test("índice de clipe é inteiro positivo", () => {
		expect(isClipIndex("1")).toBe(true);
		expect(isClipIndex("0")).toBe(false);
		expect(isClipIndex("1/../2")).toBe(false);
	});
});

describe("readUpstreamJson", () => {
	test("corpo vazio e não-JSON viram detail legível", async () => {
		expect(await readUpstreamJson(new Response("", { status: 502 }))).toEqual({
			detail: "O backend respondeu 502 sem corpo.",
		});
		const body = (await readUpstreamJson(new Response("<html>", { status: 500 }))) as {
			detail: string;
		};
		expect(body.detail).toContain("500");
	});
});

describe("proxy", () => {
	test("backend fora do ar não vaza a URL interna", async () => {
		globalThis.fetch = mock(() =>
			Promise.reject(new Error("ECONNREFUSED")),
		) as unknown as typeof fetch;
		const res = await proxyJson("/api/v1/detect");
		expect(res.status).toBe(502);
		const body = (await res.json()) as { detail: string; error_type: string };
		expect(body.error_type).toBe("NETWORK_ERROR");
		expect(body.detail).not.toContain("localhost");
	});

	test("stream repassa só headers permitidos", async () => {
		globalThis.fetch = mock(() =>
			Promise.resolve(
				new Response("bytes", {
					headers: {
						"content-type": "video/mp4",
						"content-disposition": 'attachment; filename="a.mp4"',
						"set-cookie": "x=1",
					},
				}),
			),
		) as unknown as typeof fetch;
		const res = await proxyStream("/api/v1/downloads/x/file");
		expect(res.headers.get("content-type")).toBe("video/mp4");
		expect(res.headers.get("set-cookie")).toBeNull();
		expect(res.headers.get("cache-control")).toBe("no-store");
		expect(await res.text()).toBe("bytes");
	});

	test("erro do backend preserva status e error_type", async () => {
		globalThis.fetch = mock(() =>
			Promise.resolve(
				Response.json({ detail: "ocupado", error_type: "SERVER_BUSY" }, { status: 503 }),
			),
		) as unknown as typeof fetch;
		const res = await proxyStream("/api/v1/downloads/x/file");
		expect(res.status).toBe(503);
		expect(await res.json()).toEqual({ detail: "ocupado", error_type: "SERVER_BUSY" });
	});
});
