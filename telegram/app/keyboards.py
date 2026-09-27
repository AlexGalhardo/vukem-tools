"""
keyboards.py
------------
Teclados inline (botões) do fluxo de download.
"""

from aiogram.types import InlineKeyboardButton, InlineKeyboardMarkup


def format_keyboard() -> InlineKeyboardMarkup:
	"""VIDEO MP4 ou AUDIO MP3."""
	return InlineKeyboardMarkup(
		inline_keyboard=[
			[
				InlineKeyboardButton(text="🎬 VIDEO MP4", callback_data="format:mp4"),
				InlineKeyboardButton(text="🎵 AUDIO MP3", callback_data="format:mp3"),
			]
		]
	)


def mode_keyboard() -> InlineKeyboardMarkup:
	"""Vídeo completo ou padrão Stories (clipes de 60s) — só quando format=mp4."""
	return InlineKeyboardMarkup(
		inline_keyboard=[
			[
				InlineKeyboardButton(text="📼 Vídeo completo", callback_data="mode:full"),
				InlineKeyboardButton(text="📱 Padrão Stories (clipes 60s)", callback_data="mode:clips"),
			]
		]
	)
