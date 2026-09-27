import { describe, expect, test } from "bun:test";
import { ERROR_COPY, resolveErrorMessage } from "@/lib/errors";
import { formatEta, formatMB, formatSpeed, formatTimecode } from "@/lib/format";
import { cutCropSchema, detectPlatform, proxyDownloadRequestSchema } from "@/lib/schemas";

describe("detectPlatform", () => {
	test.each([
		["https://youtu.be/abc", "youtube"],
		["https://www.instagram.com/reel/x/", "instagram"],
		["https://x.com/a/status/1", "twitter"],
		["https://vm.tiktok.com/abc/", "tiktok"],
		["https://evil-youtube.com/watch", null],
		["not a url", null],
	])("%s -> %s", (url, expected) => {
		expect(detectPlatform(url)).toBe(expected as ReturnType<typeof detectPlatform>);
	});
});

describe("resolveErrorMessage", () => {
	test("usa a copy curada, depois o detail, depois o fallback", () => {
		expect(resolveErrorMessage("SERVER_BUSY")).toContain(ERROR_COPY.SERVER_BUSY.title);
		expect(resolveErrorMessage("DESCONHECIDO", "detalhe do backend")).toBe(
			"detalhe do backend",
		);
		expect(resolveErrorMessage(null, "  ", "fallback")).toBe("fallback");
	});
});

describe("format", () => {
	test("timecode, MB, velocidade e ETA", () => {
		expect(formatTimecode(83.4)).toBe("1:23.4");
		expect(formatTimecode(3723)).toBe("1:02:03.0");
		expect(formatMB(null)).toBe("desconhecido");
		expect(formatMB(2048)).toBe("2.00 GB");
		expect(formatSpeed(2 * 1024 * 1024)).toBe("2.0 MB/s");
		expect(formatSpeed(0)).toBeNull();
		expect(formatEta(63)).toBe("1m 03s");
	});
});

describe("schemas", () => {
	test("proxyDownloadRequestSchema rejeita plataforma desconhecida", () => {
		const parsed = proxyDownloadRequestSchema.safeParse({
			platform: "vimeo",
			url: "https://vimeo.com/1",
		});
		expect(parsed.success).toBe(false);
	});

	test("cutCropSchema valida limites do crop", () => {
		const base = {
			start: 0,
			end: 5,
			outputFormat: "mp4" as const,
			videoWidth: 100,
			videoHeight: 100,
			duration: 10,
		};
		expect(cutCropSchema.safeParse({ ...base, crop: null }).success).toBe(true);
		expect(
			cutCropSchema.safeParse({ ...base, crop: { x: 50, y: 0, w: 80, h: 10 } }).success,
		).toBe(false);
	});
});
