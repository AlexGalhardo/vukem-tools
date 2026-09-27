/**
 * Qual deploy respondeu a última chamada `/api/*` (Vercel + Koyeb, VPS Hostinger...).
 * Os route handlers carimbam os headers abaixo; o browser guarda o último valor visto
 * num store mínimo, lido pelo badge da navbar.
 */

export const FRONTEND_DEPLOY_HEADER = "X-Frontend-Deploy";
export const BACKEND_DEPLOY_HEADER = "X-Backend-Deploy";
export const BACKEND_UNREACHABLE = "unreachable";

export type DeployInfo = { frontend: string; backend: string | null };

let current: DeployInfo | null = null;
const listeners = new Set<() => void>();

export function readDeployHeaders(headers: Headers): DeployInfo | null {
	const frontend = headers.get(FRONTEND_DEPLOY_HEADER);
	if (!frontend) return null;
	return { frontend, backend: headers.get(BACKEND_DEPLOY_HEADER) };
}

export function recordDeploy(headers: Headers): void {
	const next = readDeployHeaders(headers);
	// Chamadas que não passam pelo backend não trazem o header dele: mantém o último conhecido.
	if (!next || (next.backend === null && current?.frontend === next.frontend)) return;
	if (current?.frontend === next.frontend && current.backend === next.backend) return;
	if (current) {
		console.warn(
			`[deploy] resposta veio de outro deploy: ${formatDeploy(current)} → ${formatDeploy(next)}`,
		);
	} else {
		console.info(`[deploy] ${formatDeploy(next)}`);
	}
	current = next;
	for (const listener of listeners) listener();
}

export function formatDeploy(info: DeployInfo): string {
	return `front: ${info.frontend} · api: ${info.backend ?? "?"}`;
}

export function subscribeDeploy(listener: () => void): () => void {
	listeners.add(listener);
	return () => listeners.delete(listener);
}

export function getDeploy(): DeployInfo | null {
	return current;
}

/** Só para testes. */
export function resetDeploy(): void {
	current = null;
}
