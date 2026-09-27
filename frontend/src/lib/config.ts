/** Configuração server-side (nunca `NEXT_PUBLIC_*`: segredo não vai para o bundle do browser). */
import type { Platform } from "@/lib/schemas";

export const API_BASE_URL = process.env.API_BASE_URL ?? "http://localhost:8000";

export const API_KEY = process.env.API_KEY ?? "";

export const PLATFORM_PREFIX: Record<Platform, string> = {
	youtube: "/api/v1/youtube",
	instagram: "/api/v1/instagram",
	twitter: "/api/v1/twitter",
	tiktok: "/api/v1/tiktok",
};
