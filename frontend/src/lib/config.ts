/** Configuração server-side (nunca `NEXT_PUBLIC_*`: segredo não vai para o bundle do browser). */
import type { Platform } from "@/lib/schemas";

export const API_BASE_URL = process.env.API_BASE_URL ?? "http://localhost:8000";

export const API_KEY = process.env.API_KEY ?? "";

/** Rótulo do deploy do frontend vai em header HTTP: só ASCII seguro, sem quebra de linha. */
export function sanitizeDeployLabel(value: string, fallback: string): string {
	return (
		value
			.replace(/[^A-Za-z0-9 ._()/:-]/g, "")
			.slice(0, 64)
			.trim() || fallback
	);
}

/** `DEPLOY_TARGET` explícito; senão detecta a Vercel (que injeta `VERCEL` e `VERCEL_REGION`). */
export function resolveFrontendDeploy(env: Record<string, string | undefined>): string {
	const explicit = env.DEPLOY_TARGET?.trim();
	if (explicit) return sanitizeDeployLabel(explicit, "self-hosted");
	if (env.VERCEL) {
		return sanitizeDeployLabel(
			env.VERCEL_REGION ? `Vercel (${env.VERCEL_REGION})` : "Vercel",
			"Vercel",
		);
	}
	return "self-hosted";
}

export const FRONTEND_DEPLOY = resolveFrontendDeploy(process.env);

export const PLATFORM_PREFIX: Record<Platform, string> = {
	youtube: "/api/v1/youtube",
	instagram: "/api/v1/instagram",
	twitter: "/api/v1/twitter",
	tiktok: "/api/v1/tiktok",
};
