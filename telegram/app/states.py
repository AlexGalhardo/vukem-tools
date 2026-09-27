"""
states.py
---------
Estados da conversa (FSM do aiogram) do fluxo de download:

  URL recebida -> [choosing_format] -> escolheu MP4 -> [choosing_mode] -> baixa
                                     -> escolheu MP3 -> baixa direto (sem modo)
"""

from aiogram.fsm.state import State, StatesGroup


class DownloadFlow(StatesGroup):
	choosing_format = State()
	choosing_mode = State()
