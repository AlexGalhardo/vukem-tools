#!/usr/bin/env bash
# Ambiente de desenvolvimento nativo (sem Docker) no Windows, rodando no Git Bash.
#   bash setups/setup-windows-local.sh
# Pré-requisitos: Python 3.12+ (py launcher), Bun, Git; opcionais: ffmpeg e Node (winget).
# Cria os venvs do backend e do bot, instala as dependências (incl. frontend e hooks do
# husky) e copia os .env.example que ainda não existem. Idempotente.
set -euo pipefail
cd "$(dirname "$0")/.."

fail() { echo "ERRO: $*" >&2; exit 1; }

if command -v py >/dev/null 2>&1 && py -3.12 --version >/dev/null 2>&1; then
	PYTHON=(py -3.12)
elif command -v python >/dev/null 2>&1 &&
	python -c 'import sys; raise SystemExit(sys.version_info < (3, 12))' 2>/dev/null; then
	PYTHON=(python)
else
	fail "Python 3.12+ não encontrado: winget install Python.Python.3.12"
fi
command -v bun >/dev/null 2>&1 || fail "Bun não encontrado: powershell -c \"irm bun.sh/install.ps1 | iex\""
command -v ffmpeg >/dev/null 2>&1 || echo "AVISO: ffmpeg ausente (winget install Gyan.FFmpeg); Cut & Crop e testes de mídia serão pulados."
command -v node >/dev/null 2>&1 || echo "AVISO: node ausente (winget install OpenJS.NodeJS.LTS); o yt-dlp perde formatos do YouTube."

for workspace in backend telegram; do
	echo "==> $workspace: venv + dependências"
	[[ -x "$workspace/.venv/Scripts/python.exe" ]] || "${PYTHON[@]}" -m venv "$workspace/.venv"
	"$workspace/.venv/Scripts/python.exe" -m pip install -q --upgrade pip
	"$workspace/.venv/Scripts/python.exe" -m pip install -q -r "$workspace/requirements-dev.txt"
done

echo "==> frontend + hooks (husky/commitlint)"
bun install
(cd frontend && bun install)

[[ -f backend/.env ]] || cp backend/.env.example backend/.env
[[ -f telegram/.env ]] || cp telegram/.env.example telegram/.env
[[ -f frontend/.env.local ]] || cp frontend/.env.example frontend/.env.local

echo
echo "Pronto. Em terminais separados (Git Bash):"
echo "  cd backend && .venv/Scripts/uvicorn app.main:app --reload --port 8000"
echo "  cd frontend && bun run dev"
echo "  cd telegram && .venv/Scripts/python -m app.main   # precisa de TELEGRAM_BOT_TOKEN em telegram/.env"
echo "Validação completa (a mesma dos hooks e do CI): bun run check && bun run build"
