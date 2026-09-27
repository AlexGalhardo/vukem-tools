import { afterEach, expect, mock, test } from "bun:test";
import { z } from "zod";
import { ERROR_COPY } from "@/lib/errors";
import { errorMessage, postJson, request, requestJson } from "@/lib/http";

const realFetch = globalThis.fetch;

afterEach(() => {
	globalThis.fetch = realFetch;
});

function respondWith(response: Response): void {
	globalThis.fetch = mock(() => Promise.resolve(response)) as unknown as typeof fetch;
}

test("erro do backend vira a mensagem curada do error_type", async () => {
	respondWith(Response.json({ detail: "x", error_type: "SERVER_BUSY" }, { status: 503 }));
	await expect(request("/api/info", undefined, "fallback")).rejects.toThrow(
		ERROR_COPY.SERVER_BUSY.title,
	);
});

test("erro sem JSON usa o fallback", async () => {
	respondWith(new Response("<html>", { status: 502 }));
	await expect(request("/api/info", undefined, "fallback")).rejects.toThrow("fallback");
});

test("requestJson valida o corpo com o schema", async () => {
	respondWith(Response.json({ job_id: "abc" }));
	const body = await requestJson(
		"/api/download/start",
		postJson({ a: 1 }),
		z.object({ job_id: z.string() }),
		"fallback",
	);
	expect(body.job_id).toBe("abc");
});

test("errorMessage aceita qualquer valor lançado", () => {
	expect(errorMessage(new Error("boom"))).toBe("boom");
	expect(errorMessage("texto", "padrão")).toBe("padrão");
});
