#!/bin/sh
# Sobe o PO Token provider em background (best-effort) e depois o uvicorn.
# Nunca imprime variáveis de ambiente: tokens/cookies vazariam para o log do provedor.
set -eu

if [ "${YTDLP_POT_PROVIDER_ENABLED:-true}" = "true" ] && [ -f /app/pot-provider/build/main.js ]; then
	node /app/pot-provider/build/main.js --port 4416 --host 127.0.0.1 &
	i=0
	until curl -fs --max-time 1 -o /dev/null http://127.0.0.1:4416/ping || [ "$i" -ge 20 ]; do
		i=$((i + 1))
		sleep 0.5
	done
	[ "$i" -lt 20 ] && echo "[entrypoint] PO Token provider pronto." || echo "[entrypoint] PO Token provider não respondeu; seguindo sem ele."
fi

exec uvicorn app.main:app --host 0.0.0.0 --port "${PORT:-8000}" --workers 1
