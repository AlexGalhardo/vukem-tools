#!/usr/bin/env bash
# Ambiente de desenvolvimento nativo (sem Docker) em Linux/macOS.
#   bash setups/setup-unix-local.sh
# Cria os venvs do backend e do bot, instala as dependências (incl. frontend e hooks do
# husky) e copia os .env.example que ainda não existem. Idempotente.
set -euo pipefail
cd "$(dirname "$0")/.."

fail() { echo "ERRO: $*" >&2; exit 1; }

PYTHON="${PYTHON:-}"
for candidate in python3.12 python3.13 python3; do
	[[ -n "$PYTHON" ]] && break
	if command -v "$candidate" >/dev/null 2>&1 &&
		"$candidate" -c 'import sys; raise SystemExit(sys.version_info < (3, 12))'; then
		PYTHON="$candidate"
	fi
done
[[ -n "$PYTHON" ]] || fail "Python 3.12+ não encontrado (defina PYTHON=/caminho/python)."
command -v bun >/dev/null 2>&1 || fail "Bun não encontrado: curl -fsSL https://bun.sh/install | bash"
command -v ffmpeg >/dev/null 2>&1 || echo "AVISO: ffmpeg/ffprobe ausentes; Cut & Crop e os testes de mídia serão pulados."
command -v node >/dev/null 2>&1 || echo "AVISO: node ausente; o yt-dlp perde formatos do YouTube (YTDLP_JS_RUNTIME=node)."

for workspace in backend telegram; do
	echo "==> $workspace: venv + dependências"
	[[ -x "$workspace/.venv/bin/python" ]] || "$PYTHON" -m venv "$workspace/.venv"
	"$workspace/.venv/bin/python" -m pip install -q --upgrade pip
	"$workspace/.venv/bin/python" -m pip install -q -r "$workspace/requirements-dev.txt"
done

echo "==> frontend + hooks (husky/commitlint)"
bun install
(cd frontend && bun install)

[[ -f backend/.env ]] || cp backend/.env.example backend/.env
[[ -f telegram/.env ]] || cp telegram/.env.example telegram/.env
[[ -f frontend/.env.local ]] || cp frontend/.env.example frontend/.env.local

echo
echo "Pronto. Em terminais separados:"
echo "  cd backend && .venv/bin/uvicorn app.main:app --reload --port 8000"
echo "  cd frontend && bun run dev"
echo "  cd telegram && .venv/bin/python -m app.main   # precisa de TELEGRAM_BOT_TOKEN em telegram/.env"
echo "Validação completa (a mesma dos hooks e do CI): bun run check && bun run build"
