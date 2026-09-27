/** Helpers de formatação de tempo/tamanho reutilizados nas duas telas. */

/** 83.4 -> "1:23.4" ; 3723 -> "1:02:03.0" */
export function formatTimecode(totalSeconds: number): string {
	if (!Number.isFinite(totalSeconds) || totalSeconds < 0) totalSeconds = 0;
	const h = Math.floor(totalSeconds / 3600);
	const m = Math.floor((totalSeconds % 3600) / 60);
	const s = totalSeconds % 60;
	const ss = s.toFixed(1).padStart(4, "0");
	return h > 0
		? `${h}:${String(m).padStart(2, "0")}:${ss.padStart(4, "0")}`
		: `${m}:${ss}`;
}

export function formatMB(mb?: number | null): string {
	if (mb == null) return "desconhecido";
	if (mb >= 1024) return `${(mb / 1024).toFixed(2)} GB`;
	return `${mb.toFixed(1)} MB`;
}

/** bytes/s -> "2,4 MB/s". */
export function formatSpeed(bps?: number | null): string | null {
	if (!bps || bps <= 0) return null;
	const mbps = bps / (1024 * 1024);
	if (mbps >= 1) return `${mbps.toFixed(1)} MB/s`;
	return `${(bps / 1024).toFixed(0)} KB/s`;
}

/** segundos restantes -> "12s" / "1m 03s". */
export function formatEta(seconds?: number | null): string | null {
	if (seconds == null || seconds < 0) return null;
	if (seconds < 60) return `${Math.round(seconds)}s`;
	const m = Math.floor(seconds / 60);
	const s = Math.round(seconds % 60);
	return `${m}m ${String(s).padStart(2, "0")}s`;
}
