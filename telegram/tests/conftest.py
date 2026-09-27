import os

# Antes de importar app.*: não herdar o .env local nem exigir token real.
os.environ["ENV_FILE"] = ""
os.environ.setdefault("TELEGRAM_BOT_TOKEN", "123456:TEST")
os.environ.setdefault("ALLOWED_TELEGRAM_USER_IDS", "111,222")
