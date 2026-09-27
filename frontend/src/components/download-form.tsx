"use client";

/**
 * Formulário de download.
 *
 * 1. "Analisar" chama /api/info (proxy -> backend) e mostra os metadados.
 * 2. "Baixar MP4/MP3" inicia um job normal (/api/download/start) e, ao terminar,
 *    dispara o download do arquivo único.
 * 3. "Clipes p/ Stories (60s)" inicia um job de fatiamento
 *    (/api/download/clips/start): o backend baixa o vídeo e o divide em blocos
 *    consecutivos de até 60s. Ao terminar, mostramos a lista dos clipes com
 *    download individual + um botão "Baixar tudo (.zip)".
 *
 * Os dois modos compartilham o mesmo polling de /api/download/status/:jobId.
 */

import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { resolveErrorMessage } from "@/lib/errors";
import { formatEta, formatMB, formatSpeed, formatTimecode } from "@/lib/format";
import { errorMessage, postJson, requestJson, triggerBrowserDownload } from "@/lib/http";
import {
	type DownloadJob,
	detectPlatform,
	downloadFormSchema,
	downloadJobCreatedSchema,
	downloadJobSchema,
	type Platform,
	type VideoInfo,
	videoInfoSchema,
} from "@/lib/schemas";

const PLATFORM_LABEL: Record<Platform, string> = {
	youtube: "YouTube",
	instagram: "Instagram",
	twitter: "X.com",
	tiktok: "TikTok",
};

const POLL_INTERVAL_MS = 700;

// A qualidade "worst" foi removida da UI; todo download usa a melhor disponível.
const QUALITY = "best" as const;

type Mode = "single" | "clips";

export function DownloadForm() {
	const [url, setUrl] = useState("");
	const [format, setFormat] = useState<"mp4" | "mp3">("mp4");
	const [info, setInfo] = useState<VideoInfo | null>(null);
	const [analyzing, setAnalyzing] = useState(false);
	const [busy, setBusy] = useState(false);
	const [job, setJob] = useState<DownloadJob | null>(null);

	const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

	const platform = detectPlatform(url);
	const tooLarge = info?.exceeds_size_limit ?? false;
	const clipsEstimate = info?.duration_seconds
		? Math.max(1, Math.ceil(info.duration_seconds / 60))
		: null;
	const clips = job?.status === "finished" ? (job.clips ?? null) : null;

	const stopPolling = useCallback(() => {
		if (pollRef.current) {
			clearInterval(pollRef.current);
			pollRef.current = null;
		}
	}, []);

	// Encerra o polling se o componente desmontar no meio de um download.
	useEffect(() => stopPolling, [stopPolling]);

	async function analyze() {
		const parsed = downloadFormSchema.safeParse({ url, format, quality: QUALITY });
		if (!parsed.success) {
			toast.error(parsed.error.issues[0]?.message ?? "Formulário inválido.");
			return;
		}
		if (!platform) {
			toast.error("URL não reconhecida. Use YouTube, Instagram, X.com ou TikTok.");
			return;
		}

		setAnalyzing(true);
		setInfo(null);
		try {
			const data = await requestJson(
				"/api/info",
				postJson({ platform, url, format }),
				videoInfoSchema,
				"Falha ao analisar o vídeo.",
			);
			setInfo(data);
			if (data.exceeds_size_limit) {
				toast.warning(
					`O vídeo tem ~${formatMB(data.estimated_filesize_mb)} e passa do limite de ${data.max_file_size_mb} MB.`,
				);
			}
		} catch (e) {
			toast.error(errorMessage(e));
		} finally {
			setAnalyzing(false);
		}
	}

	const poll = useCallback(
		async (jobId: string, mode: Mode) => {
			try {
				const data = await requestJson(
					`/api/download/status/${jobId}`,
					{ cache: "no-store" },
					downloadJobSchema,
					"Falha ao consultar o progresso.",
				);
				setJob(data);

				if (data.status === "finished") {
					stopPolling();
					setBusy(false);
					if (mode === "single") {
						triggerBrowserDownload(`/api/download/file/${jobId}`, data.filename);
						toast.success("Download pronto! Salvando o arquivo…");
					} else {
						toast.success(
							`${data.clip_count ?? data.clips?.length ?? 0} clipes de 60s gerados.`,
						);
					}
				} else if (data.status === "error") {
					stopPolling();
					setBusy(false);
					toast.error(
						resolveErrorMessage(data.error_type, data.detail, "O download falhou."),
					);
				}
			} catch (e) {
				stopPolling();
				setBusy(false);
				setJob(null);
				toast.error(errorMessage(e, "Erro ao acompanhar o download."));
			}
		},
		[stopPolling],
	);

	async function startDownload(mode: Mode) {
		const parsed = downloadFormSchema.safeParse({ url, format, quality: QUALITY });
		if (!parsed.success) {
			toast.error(parsed.error.issues[0]?.message ?? "Formulário inválido.");
			return;
		}
		if (!platform) {
			toast.error("URL não reconhecida.");
			return;
		}

		stopPolling();
		setBusy(true);
		setJob(null);

		const endpoint =
			mode === "clips" ? "/api/download/clips/start" : "/api/download/start";

		try {
			const { job_id } = await requestJson(
				endpoint,
				postJson({ platform, url, format, quality: QUALITY }),
				downloadJobCreatedSchema,
				"Não foi possível iniciar o download.",
			);
			void poll(job_id, mode);
			pollRef.current = setInterval(() => void poll(job_id, mode), POLL_INTERVAL_MS);
		} catch (e) {
			setBusy(false);
			toast.error(errorMessage(e));
		}
	}

	const percent = Math.min(100, Math.max(0, Math.round(job?.percent ?? 0)));
	const showProgress = busy || (job != null && job.status !== "finished");
	const disabled = busy || url.trim() === "" || !platform || tooLarge;

	return (
		<div className="grid gap-4 lg:grid-cols-2">
			<Card>
				<CardHeader>
					<CardTitle>Baixar vídeo</CardTitle>
					<CardDescription>
						Cole o link de um vídeo do YouTube, Instagram, X.com ou TikTok e escolha o
						formato.
					</CardDescription>
				</CardHeader>
				<CardContent className="flex flex-col gap-4">
					<div className="flex flex-col gap-1.5">
						<Label htmlFor="url">URL do vídeo</Label>
						<div className="flex gap-2">
							<Input
								id="url"
								placeholder="Insira a URL do vídeo aqui..."
								value={url}
								onChange={(e) => setUrl(e.target.value)}
								autoComplete="off"
							/>
							<Button
								type="button"
								variant="outline"
								onClick={analyze}
								disabled={analyzing || url.trim() === ""}
							>
								{analyzing ? "Analisando…" : "Analisar"}
							</Button>
						</div>
						<p className="h-4 text-xs text-muted-foreground">
							{url.trim() === ""
								? ""
								: platform
									? `Plataforma detectada: ${PLATFORM_LABEL[platform]}`
									: "Plataforma não reconhecida."}
						</p>
					</div>

					<div className="flex flex-col gap-1.5">
						<Label>Formato</Label>
						<Select
							value={format}
							onValueChange={(v) => setFormat((v as "mp4" | "mp3") ?? "mp4")}
						>
							<SelectTrigger className="w-full">
								<SelectValue />
							</SelectTrigger>
							<SelectContent>
								<SelectItem value="mp4">MP4 — vídeo + áudio</SelectItem>
								<SelectItem value="mp3">MP3 — somente áudio</SelectItem>
							</SelectContent>
						</Select>
					</div>

					<div className="flex flex-col gap-2">
						<Button
							type="button"
							variant="brand"
							className="w-full"
							onClick={() => startDownload("single")}
							disabled={disabled}
						>
							{busy
								? "Processando…"
								: tooLarge
									? "Vídeo acima do limite de 1 GB"
									: `Baixar ${format.toUpperCase()}`}
						</Button>
						<Button
							type="button"
							variant="default"
							className="w-full"
							onClick={() => startDownload("clips")}
							disabled={disabled}
						>
							✂ Clipes p/ Stories (60s)
							{clipsEstimate ? ` · ~${clipsEstimate} partes` : ""}
						</Button>
					</div>

					{showProgress && (
						<div className="flex flex-col gap-1.5" aria-live="polite">
							<Progress value={percent} />
							<div className="flex flex-wrap items-center justify-between gap-x-3 text-xs text-muted-foreground">
								<span>{progressLabel(job)}</span>
								<span className="tabular-nums">
									{percent}%
									{job?.status === "downloading" &&
										formatSpeed(job.speed_bps) &&
										` · ${formatSpeed(job.speed_bps)}`}
									{job?.status === "downloading" &&
										formatEta(job.eta_seconds) &&
										` · faltam ${formatEta(job.eta_seconds)}`}
								</span>
							</div>
						</div>
					)}
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle className="flex items-start gap-2">
						<span className="line-clamp-2 min-w-0 break-words">
							{clips ? "Clipes gerados" : info ? info.title : "Prévia do vídeo"}
						</span>
						{tooLarge && !clips && (
							<Badge variant="destructive" className="shrink-0">
								acima de 1 GB
							</Badge>
						)}
					</CardTitle>
					<CardDescription>
						{clips
							? `${clips.length} arquivos · até 60s cada`
							: info
								? `${PLATFORM_LABEL[info.platform]}${info.uploader ? ` · ${info.uploader}` : ""}`
								: "Clique em Analisar para ver duração, tamanho estimado e thumbnail."}
					</CardDescription>
				</CardHeader>
				<CardContent className="flex flex-col gap-3">
					{clips && job ? (
						<>
							<a
								href={`/api/download/zip/${job.job_id}`}
								download
								className="inline-flex h-9 shrink-0 items-center justify-center rounded-lg bg-primary px-3 text-sm font-semibold text-primary-foreground transition-colors hover:bg-(--primary-hover)"
							>
								⬇ Baixar tudo (.zip)
							</a>
							<ul className="flex flex-col gap-1.5">
								{clips.map((clip) => (
									<li key={clip.index}>
										<a
											href={`/api/download/clips/${job.job_id}/${clip.index}`}
											download
											className="flex items-center justify-between gap-3 rounded-md border border-border px-3 py-2 text-sm transition-colors hover:border-primary hover:bg-muted"
										>
											<span className="min-w-0 break-all">
												Parte {clip.index}
												<span className="text-muted-foreground">
													{" "}
													· {formatTimecode(clip.duration_seconds)}
												</span>
											</span>
											<span className="shrink-0 text-xs text-muted-foreground tabular-nums">
												{formatMB(clip.size_mb)}
											</span>
										</a>
									</li>
								))}
							</ul>
						</>
					) : (
						<>
							{info?.thumbnail && (
								// biome-ignore lint/performance/noImgElement: thumbnail vem de CDNs variados das plataformas; next/image exigiria allowlist de cada host
								<img
									src={info.thumbnail}
									alt=""
									className="aspect-video w-full rounded-md object-cover"
								/>
							)}
							{info ? (
								<dl className="grid grid-cols-2 gap-x-6 gap-y-1 text-sm">
									<dt className="text-muted-foreground">Duração</dt>
									<dd>
										{info.duration_seconds ? formatTimecode(info.duration_seconds) : "—"}
									</dd>
									<dt className="text-muted-foreground">Tamanho estimado</dt>
									<dd>{formatMB(info.estimated_filesize_mb)}</dd>
									<dt className="text-muted-foreground">Clipes de 60s</dt>
									<dd>{clipsEstimate ? `~${clipsEstimate} partes` : "—"}</dd>
									<dt className="text-muted-foreground">Visualizações</dt>
									<dd>{info.view_count?.toLocaleString("pt-BR") ?? "—"}</dd>
								</dl>
							) : (
								<p className="text-sm text-muted-foreground">
									Nenhum vídeo analisado ainda.
								</p>
							)}
						</>
					)}
				</CardContent>
			</Card>
		</div>
	);
}

function progressLabel(job: DownloadJob | null): string {
	if (!job || job.status === "pending") return "Conectando ao vídeo…";
	if (job.status === "downloading") {
		const done = formatMB(job.downloaded_mb);
		const total = job.total_mb != null ? formatMB(job.total_mb) : null;
		return total ? `${job.phase} · ${done} de ${total}` : `${job.phase} · ${done}`;
	}
	if (job.status === "processing") return `${job.phase}…`;
	return job.phase;
}
