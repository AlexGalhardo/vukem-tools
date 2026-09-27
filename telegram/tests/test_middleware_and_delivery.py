from datetime import datetime
from types import SimpleNamespace
from unittest.mock import AsyncMock

from aiogram.types import Chat, Message, Update, User

from app import handlers
from app.api_client import Clip, DownloadedFile, JobStatus
from app.middleware import DENIED_MESSAGE, AllowlistMiddleware


def _update(user_id: int | None) -> Update:
	user = User(id=user_id, is_bot=False, first_name="t") if user_id is not None else None
	message = Message(message_id=1, date=datetime.now(), chat=Chat(id=9, type="private"), from_user=user, text="hi")
	return Update(update_id=1, message=message)


async def test_allowlisted_user_reaches_handler():
	handler = AsyncMock(return_value="ok")
	assert await AllowlistMiddleware()(handler, _update(111), {}) == "ok"


async def test_unknown_user_is_denied(monkeypatch):
	handler = AsyncMock()
	answer = AsyncMock()
	monkeypatch.setattr(Message, "answer", answer)
	assert await AllowlistMiddleware()(handler, _update(999), {}) is None
	handler.assert_not_awaited()
	assert answer.await_args.args[0] == DENIED_MESSAGE


async def test_update_without_sender_is_dropped():
	handler = AsyncMock()
	assert await AllowlistMiddleware()(handler, _update(None), {}) is None
	handler.assert_not_awaited()


def _job(clips: list[Clip]) -> JobStatus:
	return JobStatus("c" * 32, "finished", 100, "Concluído", 10, None, None, clips)


async def test_clips_are_sent_as_individual_videos(monkeypatch):
	monkeypatch.setattr(handlers.settings, "MAX_DIRECT_UPLOAD_MB", 50.0)
	message = SimpleNamespace(answer_video=AsyncMock(), answer_audio=AsyncMock())
	progress = SimpleNamespace(edit_text=AsyncMock())
	client = SimpleNamespace(
		download_clip=AsyncMock(side_effect=lambda job_id, i: DownloadedFile(b"v", f"p{i}.mp4", "video/mp4")),
		last_deploy_target="Koyeb (fra)",
	)
	clips = [Clip(1, "p1.mp4", 10.0), Clip(2, "p2.mp4", 80.0), Clip(3, "p3.mp4", 3.0)]

	await handlers._deliver_clips(message, progress, client, _job(clips))

	captions = [call.kwargs["caption"] for call in message.answer_video.await_args_list]
	assert captions == ["🎬 Parte 1/3", "🎬 Parte 3/3"]
	summary = progress.edit_text.await_args_list[-1].args[0]
	assert "3 clipes enviados" in summary
	assert f"/api/download/clips/{'c' * 32}/2" in summary
	assert "API: Koyeb (fra)" in summary


async def test_single_file_too_big_sends_link(monkeypatch):
	monkeypatch.setattr(handlers.settings, "MAX_DIRECT_UPLOAD_MB", 1.0)
	progress = SimpleNamespace(edit_text=AsyncMock())
	job = JobStatus("d" * 32, "finished", 100, "ok", 5 * 1024 * 1024, None, None)

	await handlers._deliver_single(
		SimpleNamespace(), progress, SimpleNamespace(last_deploy_target="VPS Hostinger"), job
	)

	text = progress.edit_text.await_args.args[0]
	assert f"/api/download/file/{'d' * 32}" in text
	assert "API: VPS Hostinger" in text
