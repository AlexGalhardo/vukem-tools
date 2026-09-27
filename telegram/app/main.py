"""Ponto de entrada do bot (``python -m app.main``).

Long polling (não webhook): sem domínio/HTTPS obrigatório e sem porta pública extra.
"""

import asyncio
import logging

from aiogram import Bot, Dispatcher
from aiogram.client.default import DefaultBotProperties
from aiogram.client.session.aiohttp import AiohttpSession

from app.api_client import BackendClient
from app.config import settings
from app.handlers import on_dispatcher_error, router
from app.middleware import AllowlistMiddleware

logging.basicConfig(level=logging.INFO, format="%(asctime)s | %(levelname)s | %(name)s | %(message)s")
logger = logging.getLogger("telegram-bot")


async def main() -> None:
	if not settings.TELEGRAM_BOT_TOKEN:
		raise SystemExit("TELEGRAM_BOT_TOKEN não definido (ver telegram/.env.example).")
	if not settings.allowed_user_ids:
		logger.warning("ALLOWED_TELEGRAM_USER_IDS vazio: ninguém consegue usar o bot (fail-closed).")

	bot = Bot(
		token=settings.TELEGRAM_BOT_TOKEN,
		default=DefaultBotProperties(parse_mode=None),
		# O timeout padrão (60s) derruba o upload de vídeos grandes.
		session=AiohttpSession(timeout=settings.REQUEST_TIMEOUT_SECONDS),
	)
	backend = BackendClient(
		settings.API_BASE_URL, api_key=settings.API_KEY, file_timeout=settings.REQUEST_TIMEOUT_SECONDS
	)
	bot.backend_client = backend  # type: ignore[attr-defined]  # lido pelos handlers via message.bot
	logger.info("Backend: %s", settings.API_BASE_URL)

	dispatcher = Dispatcher()
	dispatcher.update.outer_middleware(AllowlistMiddleware())
	dispatcher.include_router(router)
	dispatcher.errors.register(on_dispatcher_error)

	try:
		await bot.delete_webhook(drop_pending_updates=True)
		await dispatcher.start_polling(bot)
	finally:
		await backend.aclose()
		await bot.session.close()


if __name__ == "__main__":
	asyncio.run(main())
