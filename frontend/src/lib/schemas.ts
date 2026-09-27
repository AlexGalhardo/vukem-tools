/**
 * schemas.ts
 * ----------
 * Schemas Zod usados para (1) validar os formulários e (2) validar as respostas
 * da API antes de confiar nelas no front-end.
 */
import { z } from "zod";

export const PLATFORMS = ["youtube", "instagram", "twitter", "tiktok"] as const;
export type Platform = (typeof PLATFORMS)[number];

const YT = /(^|\.)(youtube\.com|youtu\.be)$/i;
const IG = /(^|\.)instagram\.com$/i;
const TW = /(^|\.)(twitter\.com|x\.com)$/i;
const TT = /(^|\.)tiktok\.com$/i;

export function detectPlatform(rawUrl: string): Platform | null {
	try {
		const host = new URL(rawUrl).hostname.toLowerCase();
		if (YT.test(host)) return "youtube";
		if (IG.test(host)) return "instagram";
		if (TW.test(host)) return "twitter";
		if (TT.test(host)) return "tiktok";
		return null;
	} catch {
		return null;
	}
}

/** Formulário de download. */
export const downloadFormSchema = z.object({
	url: z.string().trim().url("Informe uma URL válida."),
	format: z.enum(["mp4", "mp3"]),
	quality: z.enum(["best", "worst"]),
});
export type DownloadForm = z.infer<typeof downloadFormSchema>;

/**
 * Corpo aceito pelas route handlers de proxy que iniciam um download
 * (`/api/info`, `/api/download/start`, `/api/download/clips/start`) — valida
 * o JSON recebido do browser antes de repassá-lo ao backend.
 */
export const proxyDownloadRequestSchema = z.object({
	platform: z.enum(PLATFORMS),
	url: z.string().trim().url(),
	format: z.enum(["mp4", "mp3"]).default("mp4"),
	quality: z.enum(["best", "worst"]).default("best"),
});
export type ProxyDownloadRequest = z.infer<typeof proxyDownloadRequestSchema>;

/** Resposta do endpoint /info do backend. */
export const videoInfoSchema = z.object({
	platform: z.enum(PLATFORMS),
	id: z.string(),
	title: z.string(),
	uploader: z.string().nullish(),
	duration_seconds: z.number().nullish(),
	thumbnail: z.string().nullish(),
	view_count: z.number().nullish(),
	upload_date: z.string().nullish(),
	webpage_url: z.string(),
	estimated_filesize_mb: z.number().nullish(),
	exceeds_size_limit: z.boolean(),
	max_file_size_mb: z.number(),
});
export type VideoInfo = z.infer<typeof videoInfoSchema>;

export const apiErrorSchema = z.object({
	detail: z.string(),
	error_type: z.string().nullish(),
});

/** Resposta de `POST {prefix}/download/start`. */
export const downloadJobCreatedSchema = z.object({ job_id: z.string() });

/** Um clipe de 60s gerado pelo modo "Clipes p/ Stories". */
export const clipInfoSchema = z.object({
	index: z.number(),
	filename: z.string(),
	duration_seconds: z.number(),
	size_mb: z.number().nullish(),
});
export type ClipInfo = z.infer<typeof clipInfoSchema>;

/** Progresso de um job de download (polling). */
export const downloadJobSchema = z.object({
	job_id: z.string(),
	status: z.enum(["pending", "downloading", "processing", "finished", "error"]),
	percent: z.number(),
	downloaded_bytes: z.number().default(0),
	total_bytes: z.number().nullish(),
	downloaded_mb: z.number().nullish(),
	total_mb: z.number().nullish(),
	speed_bps: z.number().nullish(),
	eta_seconds: z.number().nullish(),
	phase: z.string(),
	filename: z.string().nullish(),
	detail: z.string().nullish(),
	// Código estável do erro quando status === "error" (ver src/lib/errors.ts).
	error_type: z.string().nullish(),
	// Preenchidos só quando o job é do modo "Clipes p/ Stories" e terminou.
	clips: z.array(clipInfoSchema).nullish(),
	clip_count: z.number().nullish(),
});
export type DownloadJob = z.infer<typeof downloadJobSchema>;

/** Parâmetros do corte + crop (validados contra as dimensões reais do vídeo). */
export const cutCropSchema = z
	.object({
		start: z.number().min(0),
		end: z.number().positive().nullable(),
		outputFormat: z.enum(["mp4", "mp3"]),
		crop: z
			.object({
				x: z.number().min(0),
				y: z.number().min(0),
				w: z.number().positive(),
				h: z.number().positive(),
			})
			.nullable(),
		videoWidth: z.number().positive(),
		videoHeight: z.number().positive(),
		duration: z.number().positive(),
	})
	.refine((v) => v.end === null || v.end > v.start, {
		message: "O fim do corte deve ser maior que o início.",
		path: ["end"],
	})
	.refine((v) => v.start < v.duration, {
		message: "O início do corte está além da duração do vídeo.",
		path: ["start"],
	})
	.refine(
		(v) =>
			!v.crop ||
			(v.crop.x + v.crop.w <= v.videoWidth + 1 &&
				v.crop.y + v.crop.h <= v.videoHeight + 1),
		{ message: "A área de recorte sai dos limites do vídeo.", path: ["crop"] },
	);
export type CutCropInput = z.infer<typeof cutCropSchema>;

export const capabilitiesSchema = z.object({
	ffmpeg_available: z.boolean(),
	max_upload_size_mb: z.number(),
	output_formats: z.array(z.string()),
});
