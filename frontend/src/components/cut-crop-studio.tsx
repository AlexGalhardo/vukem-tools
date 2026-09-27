"use client";

/**
 * Estúdio de Cut & Crop.
 *
 * O usuário envia um vídeo; ele é renderizado num <video>. Sobre o vídeo há
 * um retângulo arrastável/redimensionável (crop) e, abaixo, um slider de dois
 * pontos para o intervalo de tempo. Ao confirmar, os parâmetros + o arquivo
 * são enviados (multipart) para /api/edit, que faz proxy em streaming para o
 * backend (ffmpeg). O resultado volta como blob e é pré-visualizado.
 *
 * O retângulo de crop é guardado em coordenadas NORMALIZADAS (0..1), então
 * funciona igual seja qual for o tamanho de exibição; na hora de enviar,
 * multiplicamos pelas dimensões reais do vídeo.
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Rnd } from "react-rnd";
import { toast } from "sonner";
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
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { Slider } from "@/components/ui/slider";
import { formatTimecode } from "@/lib/format";
import { errorMessage, request, triggerBrowserDownload } from "@/lib/http";
import { cutCropSchema } from "@/lib/schemas";

type Meta = { width: number; height: number; duration: number };
type BoxN = { x: number; y: number; w: number; h: number };

const FULL_BOX: BoxN = { x: 0, y: 0, w: 1, h: 1 };
const MAX_W = 720;

function filenameFromDisposition(cd: string | null, fallback: string): string {
	if (!cd) return fallback;
	const m = /filename="?([^";]+)"?/i.exec(cd);
	return m?.[1] ?? fallback;
}

export function CutCropStudio() {
	const [file, setFile] = useState<File | null>(null);
	const [objectUrl, setObjectUrl] = useState<string | null>(null);
	const [meta, setMeta] = useState<Meta | null>(null);
	const [range, setRange] = useState<[number, number]>([0, 0]);
	const [cropEnabled, setCropEnabled] = useState(true);
	const [box, setBox] = useState<BoxN>(FULL_BOX);
	const [display, setDisplay] = useState<{ w: number; h: number }>({ w: 0, h: 0 });
	const [outputFormat, setOutputFormat] = useState<"mp4" | "mp3">("mp4");
	const [processing, setProcessing] = useState(false);
	const [splitting, setSplitting] = useState(false);
	const [result, setResult] = useState<{
		url: string;
		name: string;
		isAudio: boolean;
	} | null>(null);

	const videoRef = useRef<HTMLVideoElement>(null);
	const lastRange = useRef<[number, number]>([0, 0]);
	const roRef = useRef<ResizeObserver | null>(null);

	// Object URLs precisam ser revogados para não vazar memória.
	useEffect(() => {
		return () => {
			if (objectUrl) URL.revokeObjectURL(objectUrl);
		};
	}, [objectUrl]);
	useEffect(() => {
		return () => {
			if (result) URL.revokeObjectURL(result.url);
		};
	}, [result]);

	// Ref callback: observa o tamanho real de exibição do vídeo (para posicionar
	// o retângulo de crop). Usar ref callback em vez de useEffect evita depender
	// do timing de renderização do <video>.
	const setStageRef = useCallback((node: HTMLDivElement | null) => {
		roRef.current?.disconnect();
		if (!node) return;
		const measure = () => {
			const r = node.getBoundingClientRect();
			setDisplay({ w: r.width, h: r.height });
		};
		measure();
		const ro = new ResizeObserver(measure);
		ro.observe(node);
		roRef.current = ro;
	}, []);

	function onPickFile(f: File | null) {
		if (result) URL.revokeObjectURL(result.url);
		setResult(null);
		setMeta(null);
		setBox(FULL_BOX);
		if (objectUrl) URL.revokeObjectURL(objectUrl);
		if (!f) {
			setFile(null);
			setObjectUrl(null);
			return;
		}
		setFile(f);
		setObjectUrl(URL.createObjectURL(f));
	}

	function onLoadedMetadata() {
		const v = videoRef.current;
		if (!v) return;
		const m: Meta = {
			width: v.videoWidth,
			height: v.videoHeight,
			duration: v.duration,
		};
		setMeta(m);
		setRange([0, m.duration]);
		lastRange.current = [0, m.duration];
		setBox(FULL_BOX);
	}

	function onRangeChange(value: number | readonly number[]) {
		const arr = Array.isArray(value) ? value : [value, value];
		const next: [number, number] = [arr[0] as number, arr[1] as number];
		const v = videoRef.current;
		if (v) {
			// Move o playhead para a alça que mudou.
			const movedEnd =
				Math.abs(next[1] - lastRange.current[1]) >
				Math.abs(next[0] - lastRange.current[0]);
			v.currentTime = movedEnd ? next[1] : next[0];
		}
		lastRange.current = next;
		setRange(next);
	}

	function previewSegment() {
		const v = videoRef.current;
		if (!v) return;
		v.currentTime = range[0];
		void v.play();
	}
	function onTimeUpdate() {
		const v = videoRef.current;
		if (v && !v.paused && v.currentTime >= range[1]) v.pause();
	}

	const cropNatural = useMemo(() => {
		if (!meta) return null;
		return {
			x: Math.round(box.x * meta.width),
			y: Math.round(box.y * meta.height),
			w: Math.round(box.w * meta.width),
			h: Math.round(box.h * meta.height),
		};
	}, [box, meta]);

	async function process() {
		if (!file || !meta) return;
		const cropPx =
			cropEnabled &&
			cropNatural &&
			(cropNatural.w < meta.width || cropNatural.h < meta.height)
				? cropNatural
				: null;

		const parsed = cutCropSchema.safeParse({
			start: range[0],
			end: range[1] < meta.duration - 0.05 ? range[1] : null,
			outputFormat,
			crop: cropPx,
			videoWidth: meta.width,
			videoHeight: meta.height,
			duration: meta.duration,
		});
		if (!parsed.success) {
			toast.error(parsed.error.issues[0]?.message ?? "Parâmetros inválidos.");
			return;
		}

		const fd = new FormData();
		fd.set("file", file);
		fd.set("start", String(parsed.data.start));
		if (parsed.data.end != null) fd.set("end", String(parsed.data.end));
		fd.set("output_format", outputFormat);
		if (cropPx) {
			fd.set("crop_x", String(cropPx.x));
			fd.set("crop_y", String(cropPx.y));
			fd.set("crop_w", String(cropPx.w));
			fd.set("crop_h", String(cropPx.h));
		}

		setProcessing(true);
		if (result) URL.revokeObjectURL(result.url);
		setResult(null);
		try {
			const res = await request(
				"/api/edit",
				{ method: "POST", body: fd },
				"Falha ao processar o vídeo.",
			);
			const blob = await res.blob();
			const name = filenameFromDisposition(
				res.headers.get("content-disposition"),
				`${file.name.replace(/\.[^.]+$/, "")}_edit.${outputFormat}`,
			);
			setResult({
				url: URL.createObjectURL(blob),
				name,
				isAudio: outputFormat === "mp3",
			});
			toast.success("Vídeo processado!");
		} catch (e) {
			toast.error(errorMessage(e));
		} finally {
			setProcessing(false);
		}
	}

	async function splitIntoClips() {
		if (!file) return;
		const fd = new FormData();
		fd.set("file", file);
		fd.set("segment_seconds", "60");

		setSplitting(true);
		try {
			const res = await request(
				"/api/edit/split",
				{ method: "POST", body: fd },
				"Falha ao gerar os clipes.",
			);
			const count = res.headers.get("x-clip-count");
			const blob = await res.blob();
			const objUrl = URL.createObjectURL(blob);
			triggerBrowserDownload(objUrl, `${file.name.replace(/\.[^.]+$/, "")}_clipes.zip`);
			URL.revokeObjectURL(objUrl);
			toast.success(
				count
					? `${count} clipes de 60s gerados — baixando o .zip…`
					: "Clipes gerados — baixando o .zip…",
			);
		} catch (e) {
			toast.error(errorMessage(e));
		} finally {
			setSplitting(false);
		}
	}

	const clipsEstimate = meta ? Math.max(1, Math.ceil(meta.duration / 60)) : null;
	const clipsButtonLabel = clipsEstimate
		? `✂ Clipes p/ Stories (60s) · ~${clipsEstimate} partes`
		: "✂ Clipes p/ Stories (60s)";

	const stageStyle = meta
		? {
				aspectRatio: `${meta.width} / ${meta.height}`,
				maxWidth: MAX_W,
				maxHeight: "80vh",
			}
		: undefined;

	return (
		<div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_340px]">
			{/* Área de mídia: stage de edição ou prévia do resultado. */}
			<Card>
				<CardHeader>
					<CardTitle>{result ? "Resultado" : "Prévia"}</CardTitle>
					<CardDescription>
						{result
							? "Trecho processado com ffmpeg no backend."
							: objectUrl
								? "Arraste o retângulo para o recorte (crop) e use o slider para o intervalo."
								: "Envie um vídeo para começar."}
					</CardDescription>
				</CardHeader>
				<CardContent className="flex items-center justify-center">
					{result ? (
						result.isAudio ? (
							<audio src={result.url} controls className="w-full" />
						) : (
							<video
								src={result.url}
								controls
								className="max-h-[80vh] w-full rounded-md bg-black"
							/>
						)
					) : objectUrl ? (
						<div
							ref={setStageRef}
							className="relative mx-auto w-full overflow-hidden rounded-md bg-black"
							style={stageStyle}
						>
							<video
								ref={videoRef}
								src={objectUrl}
								controls
								onLoadedMetadata={onLoadedMetadata}
								onTimeUpdate={onTimeUpdate}
								className="absolute inset-0 h-full w-full"
							/>
							{cropEnabled && meta && display.w > 0 && (
								<Rnd
									bounds="parent"
									size={{ width: box.w * display.w, height: box.h * display.h }}
									position={{ x: box.x * display.w, y: box.y * display.h }}
									onDragStop={(_e, d) => {
										setBox((b) => ({
											...b,
											x: clamp01(d.x / display.w),
											y: clamp01(d.y / display.h),
										}));
									}}
									onResizeStop={(_e, _dir, ref, _delta, pos) => {
										setBox({
											x: clamp01(pos.x / display.w),
											y: clamp01(pos.y / display.h),
											w: clamp01(ref.offsetWidth / display.w),
											h: clamp01(ref.offsetHeight / display.h),
										});
									}}
									className="z-10 border-2 border-primary shadow-[0_0_0_9999px_rgba(0,0,0,0.45)]"
								/>
							)}
						</div>
					) : (
						<p className="text-sm text-muted-foreground">Nenhum vídeo carregado.</p>
					)}
				</CardContent>
			</Card>

			{/* Controles. */}
			<Card>
				<CardHeader>
					<CardTitle>Ajustes</CardTitle>
					<CardDescription>Corte por tempo e recorte por área.</CardDescription>
				</CardHeader>
				<CardContent className="flex flex-col gap-4">
					<div className="flex flex-col gap-1.5">
						<Label htmlFor="cc-file">Vídeo</Label>
						<Input
							id="cc-file"
							type="file"
							accept="video/*"
							onChange={(e) => onPickFile(e.target.files?.[0] ?? null)}
						/>
					</div>

					{result ? (
						<div className="flex flex-col gap-2">
							<a
								href={result.url}
								download={result.name}
								className="flex min-h-8 flex-col items-center justify-center gap-0.5 rounded-lg bg-primary px-3 py-1.5 text-center text-sm font-medium leading-tight text-primary-foreground transition-colors hover:bg-(--primary-hover)"
							>
								<span className="shrink-0 opacity-90">Baixar</span>
								<span className="w-full break-all">{result.name}</span>
							</a>
							<Button type="button" variant="outline" onClick={() => setResult(null)}>
								Novo corte
							</Button>
						</div>
					) : (
						<>
							<label className="flex flex-wrap items-center gap-2 text-sm">
								<input
									type="checkbox"
									checked={cropEnabled}
									onChange={(e) => setCropEnabled(e.target.checked)}
								/>
								Aplicar recorte (crop)
								{cropEnabled && (
									<Button
										type="button"
										size="xs"
										variant="ghost"
										onClick={() => setBox(FULL_BOX)}
									>
										resetar
									</Button>
								)}
								{cropEnabled && cropNatural && (
									<span className="w-full text-xs text-muted-foreground">
										{cropNatural.w}×{cropNatural.h}px a partir de ({cropNatural.x},{" "}
										{cropNatural.y})
									</span>
								)}
							</label>

							{meta && (
								<div className="flex flex-col gap-2">
									<div className="flex flex-col gap-1 text-sm">
										<span>
											Trecho:{" "}
											<span className="font-mono">{formatTimecode(range[0])}</span> →{" "}
											<span className="font-mono">{formatTimecode(range[1])}</span>{" "}
											<span className="text-muted-foreground">
												({formatTimecode(Math.max(0, range[1] - range[0]))})
											</span>
										</span>
									</div>
									<Slider
										value={range}
										min={0}
										max={meta.duration}
										step={0.1}
										onValueChange={onRangeChange}
									/>
									<div className="grid grid-cols-2 gap-2">
										<Button
											type="button"
											size="sm"
											variant="secondary"
											onClick={() => {
												const t = videoRef.current?.currentTime ?? 0;
												onRangeChange([Math.min(t, range[1] - 0.1), range[1]]);
											}}
										>
											◁ Início = atual
										</Button>
										<Button
											type="button"
											size="sm"
											variant="secondary"
											onClick={() => {
												const t = videoRef.current?.currentTime ?? meta.duration;
												onRangeChange([range[0], Math.max(t, range[0] + 0.1)]);
											}}
										>
											Fim = atual ▷
										</Button>
									</div>
									<Button
										type="button"
										size="sm"
										variant="outline"
										onClick={previewSegment}
									>
										▶ Pré-visualizar trecho
									</Button>
								</div>
							)}

							<div className="flex flex-col gap-1.5">
								<Label>Formato de saída</Label>
								<Select
									value={outputFormat}
									onValueChange={(v) => setOutputFormat((v as "mp4" | "mp3") ?? "mp4")}
								>
									<SelectTrigger className="w-full">
										<SelectValue />
									</SelectTrigger>
									<SelectContent>
										<SelectItem value="mp4">MP4 — vídeo (com o crop)</SelectItem>
										<SelectItem value="mp3">MP3 — só o áudio do trecho</SelectItem>
									</SelectContent>
								</Select>
							</div>

							<Button
								type="button"
								variant="brand"
								className="w-full"
								onClick={process}
								disabled={processing || splitting || !meta}
							>
								{processing ? "Processando com ffmpeg…" : "Processar corte + crop"}
							</Button>

							<div className="flex flex-col gap-1.5 border-t border-border pt-3">
								<span className="text-xs text-muted-foreground">
									Ou divida o vídeo inteiro em blocos de 60s (para Stories):
								</span>
								<Button
									type="button"
									variant="default"
									className="w-full"
									onClick={splitIntoClips}
									disabled={splitting || processing || !file}
								>
									{splitting ? "Fatiando…" : clipsButtonLabel}
								</Button>
							</div>
						</>
					)}
				</CardContent>
			</Card>
		</div>
	);
}

function clamp01(n: number): number {
	return Math.max(0, Math.min(1, n));
}
