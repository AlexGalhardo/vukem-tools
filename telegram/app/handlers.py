"""Fluxo de conversa do bot.

1. URL -> ``/api/v1/detect`` -> preview via ``/{plataforma}/info``.
2. Botões: MP4 ou MP3; se MP4 (e o vídeo passa de 60s), vídeo completo ou clipes de 60s.
3. Inicia o job, edita uma mensagem de progresso e entrega o resultado: arquivo no chat
   quando cabe no limite do Telegram, senão um link. No modo clipes, cada parte vai como um
   vídeo separado (mais fácil de salvar no celular do que um .zip).
"""

import asyncio
import contextlib
import logging
import time

from aiogram import F, Router
from aiogram.filters import Command, CommandStart
from aiogram.fsm.context import FSMContext
from aiogram.types import BufferedInputFile, CallbackQuery, ErrorEvent, Message

from app.api_client import BackendClient, BackendError, DownloadedFile, JobMode, JobStatus, OutputFormat
from app.config import settings
from app.errors import FALLBACK_MESSAGE, resolve_error_message
from app.keyboards import format_keyboard, mode_keyboard
from app.presenters import START_TEXT, clip_caption, progress_text, video_caption
from app.states import DownloadFlow

router = Router(name="download-flow")
logger = logging.getLogger("telegram-bot")


def _backend(event: Message | CallbackQuery) -> BackendClient:
	return event.bot.backend_client  # type: ignore[union-attr]  # anexado em main.py


def _download_link(path: str) -> str:
	return f"{settings.PUBLIC_WEB_BASE_URL.rstrip('/')}/api/download/{path}"


@router.message(CommandStart())
@router.message(Command("help"))
async def cmd_start(message: Message, state: FSMContext) -> None:
	await state.clear()
	await message.answer(START_TEXT, parse_mode="HTML")


@router.message(F.text.startswith("http"))
async def on_url(message: Message, state: FSMContext) -> None:
	url = (message.text or "").strip()
	client = _backend(message)
	await state.clear()

	try:
		platform = await client.detect_platform(url)
	except BackendError as e:
		await message.answer(resolve_error_message(e.error_type, e.detail))
		return
	if platform is None:
		await message.answer("❌ URL inválida. Envie um link do YouTube, Instagram, X (Twitter) ou TikTok.")
		return

	status_msg = await message.answer("🔎 Buscando informações do vídeo...")
	try:
		info = await client.get_info(platform, url)
	except BackendError as e:
		await status_msg.edit_text(resolve_error_message(e.error_type, e.detail))
		return

	await state.update_data(url=url, platform=platform, duration=info.duration_seconds)
	await state.set_state(DownloadFlow.choosing_format)

	caption = video_caption(platform, info)
	if info.thumbnail:
		await message.answer_photo(info.thumbnail, caption=caption, parse_mode="HTML", reply_markup=format_keyboard())
		await status_msg.delete()
	else:
		await status_msg.edit_text(caption, parse_mode="HTML", reply_markup=format_keyboard())


@router.message(DownloadFlow.choosing_format)
@router.message(DownloadFlow.choosing_mode)
async def on_unexpected_text(message: Message) -> None:
	await message.answer("Use os botões acima 👆 (ou mande uma nova URL para recomeçar).")


@router.callback_query(DownloadFlow.choosing_format, F.data.in_({"format:mp4", "format:mp3"}))
async def on_format_chosen(callback: CallbackQuery, state: FSMContext) -> None:
	fmt: OutputFormat = callback.data.split(":", 1)[1]  # type: ignore[union-attr,assignment]
	await callback.answer()
	await state.update_data(format=fmt)
	duration = (await state.get_data()).get("duration")

	is_short = duration is not None and duration < settings.MIN_CLIPS_DURATION_SECONDS
	if fmt == "mp3" or is_short:
		await state.update_data(mode="full")
		await callback.message.edit_reply_markup(reply_markup=None)  # type: ignore[union-attr]
		await _start_download(callback.message, state)  # type: ignore[arg-type]
		return

	await state.set_state(DownloadFlow.choosing_mode)
	await callback.message.edit_reply_markup(reply_markup=mode_keyboard())  # type: ignore[union-attr]


@router.callback_query(DownloadFlow.choosing_mode, F.data.in_({"mode:full", "mode:clips"}))
async def on_mode_chosen(callback: CallbackQuery, state: FSMContext) -> None:
	mode: JobMode = callback.data.split(":", 1)[1]  # type: ignore[union-attr,assignment]
	await callback.answer()
	await state.update_data(mode=mode)
	await callback.message.edit_reply_markup(reply_markup=None)  # type: ignore[union-attr]
	await _start_download(callback.message, state)  # type: ignore[arg-type]


async def _start_download(message: Message, state: FSMContext) -> None:
	data = await state.get_data()
	await state.clear()
	client = _backend(message)
	progress_msg = await message.answer("⏳ Iniciando o download...")
	try:
		job_id = await client.start_job(data["platform"], data["url"], data["format"], data["mode"])
		job = await _watch_progress(client, job_id, progress_msg)
	except BackendError as e:
		await progress_msg.edit_text(resolve_error_message(e.error_type, e.detail))
		return

	if job.status == "error":
		await progress_msg.edit_text(resolve_error_message(job.error_type, job.detail))
		return

	try:
		if data["mode"] == "clips":
			await _deliver_clips(message, progress_msg, client, job)
		else:
			await _deliver_single(message, progress_msg, client, job)
	except BackendError as e:
		await progress_msg.edit_text(resolve_error_message(e.error_type, e.detail))
	except Exception:  # noqa: BLE001 - timeout/rede do Telegram não pode deixar o chat sem resposta
		logger.exception("Falha ao entregar o job %s", job.job_id)
		await progress_msg.edit_text(FALLBACK_MESSAGE)


async def _watch_progress(client: BackendClient, job_id: str, progress_msg: Message) -> JobStatus:
	last_edit, last_text = 0.0, ""
	while True:
		job = await client.get_job_status(job_id)
		if job.status in ("finished", "error"):
			return job
		text = progress_text(job.status, job.percent, job.phase)
		now = time.monotonic()
		if text != last_text and now - last_edit >= settings.PROGRESS_EDIT_INTERVAL_SECONDS:
			with contextlib.suppress(Exception):  # "message is not modified" e afins
				await progress_msg.edit_text(text)
			last_edit, last_text = now, text
		await asyncio.sleep(settings.POLL_INTERVAL_SECONDS)


async def _send_media(message: Message, file: DownloadedFile, caption: str) -> None:
	payload = BufferedInputFile(file.content, filename=file.filename)
	send = message.answer_audio if file.media_type.startswith("audio") else message.answer_video
	await asyncio.wait_for(send(payload, caption=caption), timeout=settings.REQUEST_TIMEOUT_SECONDS)


async def _deliver_single(message: Message, progress_msg: Message, client: BackendClient, job: JobStatus) -> None:
	size = job.total_bytes or 0
	if size > settings.max_direct_upload_bytes:
		await progress_msg.edit_text(
			f"✅ Pronto! ({size / 1024 / 1024:.1f} MB — grande demais pro Telegram)\n"
			f"⬇️ Baixe aqui: {_download_link(f'file/{job.job_id}')}"
		)
		return
	await progress_msg.edit_text("✅ Pronto! Enviando o arquivo...")
	file = await asyncio.wait_for(client.download_file(job.job_id), timeout=settings.REQUEST_TIMEOUT_SECONDS)
	await _send_media(message, file, "✅ Pronto!")
	await progress_msg.delete()


async def _deliver_clips(message: Message, progress_msg: Message, client: BackendClient, job: JobStatus) -> None:
	total = len(job.clips)
	too_big: list[str] = []
	for clip in job.clips:
		await progress_msg.edit_text(f"✅ Pronto! Enviando parte {clip.index}/{total}...")
		size_bytes = (clip.size_mb or 0) * 1024 * 1024
		if size_bytes > settings.max_direct_upload_bytes:
			too_big.append(f"Parte {clip.index}: {_download_link(f'clips/{job.job_id}/{clip.index}')}")
			continue
		file = await asyncio.wait_for(
			client.download_clip(job.job_id, clip.index), timeout=settings.REQUEST_TIMEOUT_SECONDS
		)
		await _send_media(message, file, clip_caption(clip.index, total))

	summary = f"🎬 {total} clipes enviados."
	if too_big:
		summary += "\n\nGrandes demais pro Telegram, baixe pelo link:\n" + "\n".join(too_big)
	await progress_msg.edit_text(summary)


async def on_dispatcher_error(event: ErrorEvent) -> None:
	"""Rede de segurança: toda exceção não tratada ainda gera uma resposta no chat."""
	logger.exception("Exceção não tratada processando update: %s", event.exception)
	update = event.update
	message = update.message or (update.callback_query.message if update.callback_query else None)
	if message is not None and message.bot is not None:
		with contextlib.suppress(Exception):
			await message.bot.send_message(message.chat.id, FALLBACK_MESSAGE)
