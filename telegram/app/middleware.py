"""Allowlist de IDs do Telegram: sem ela o bot vira acesso público à banda/CPU do servidor."""

from collections.abc import Awaitable, Callable
from typing import Any

from aiogram import BaseMiddleware
from aiogram.types import Message, TelegramObject, Update, User

from app.config import settings

DENIED_MESSAGE = "🔒 Este bot é restrito. Peça para o administrador liberar seu ID (o @userinfobot mostra o seu)."


def _sender_and_message(event: TelegramObject) -> tuple[User | None, Message | None]:
	if not isinstance(event, Update):
		return None, None
	if event.message:
		return event.message.from_user, event.message
	if event.callback_query:
		message = event.callback_query.message
		return event.callback_query.from_user, message if isinstance(message, Message) else None
	return None, None


class AllowlistMiddleware(BaseMiddleware):
	async def __call__(
		self,
		handler: Callable[[TelegramObject, dict[str, Any]], Awaitable[Any]],
		event: TelegramObject,
		data: dict[str, Any],
	) -> Any:
		user, message = _sender_and_message(event)
		if user is not None and user.id in settings.allowed_user_ids:
			return await handler(event, data)
		# Fail-closed: updates sem remetente (canais, etc.) também são descartados.
		if user is not None and message is not None:
			await message.answer(DENIED_MESSAGE)
		return None
