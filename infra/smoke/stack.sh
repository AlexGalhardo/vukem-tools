#!/usr/bin/env bash
# =============================================================================
# Teste de fumaça END-TO-END da stack já rodando via docker-compose.
#
# Valida, de forma automatizada:
#   1. Containers estão UP e HEALTHY (healthcheck real, não só "running")
#   2. Endpoints principais respondem (backend direto + via proxy do frontend)
#   3. Fluxo completo dentro do container:
#        gera um vídeo -> /api/v1/edit/cut-crop (corte + crop) -> confere a saída
#        gera um vídeo -> /api/v1/edit/split (clipes 60s)     -> confere <= 60.5s
#   4. Erros estruturados: um upload inválido devolve {detail, error_type}
#
# Uso:
#   docker compose -f infra/docker-compose.prod.yml up -d --build
#   API_KEY=<a do backend/.env> bash infra/smoke/stack.sh      (rodar da raiz do repo)
#
# Códigos de saída: 0 = tudo passou; 1 = alguma verificação falhou.
# =============================================================================
set -u

COMPOSE_FILE="${COMPOSE_FILE:-infra/docker-compose.prod.yml}"
API="${API:-http://localhost:8000}"
WEB="${WEB:-http://localhost:3000}"
BACKEND_SVC="${BACKEND_SVC:-backend}"
# Diretório RELATIVO de propósito: no Git Bash (Windows) o curl.exe nativo não
# entende caminhos MSYS absolutos como /tmp/xxx.
TMP=".smoke-tmp/stack"
PASS=0
FAIL=0

rm -rf "$TMP"; mkdir -p "$TMP/clips"
trap 'rm -rf "$TMP"' EXIT

ok()   { echo "  PASS  $1"; PASS=$((PASS+1)); }
bad()  { echo "  FAIL  $1"; FAIL=$((FAIL+1)); }
head() { echo; echo "=== $1 ==="; }

dc() { docker compose -f "$COMPOSE_FILE" "$@"; }

# -----------------------------------------------------------------------------
head "1. Containers UP + HEALTHY"
# -----------------------------------------------------------------------------
for svc in "$BACKEND_SVC" frontend; do
  cid="$(dc ps -q "$svc" 2>/dev/null)"
  if [ -z "$cid" ]; then bad "$svc: container não encontrado"; continue; fi
  state="$(docker inspect -f '{{.State.Status}}' "$cid")"
  health="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}no-healthcheck{{end}}' "$cid")"
  if [ "$state" = "running" ] && [ "$health" = "healthy" ]; then
    ok "$svc: running + healthy"
  else
    bad "$svc: state=$state health=$health"
  fi
done

# -----------------------------------------------------------------------------
head "2. Endpoints principais"
# -----------------------------------------------------------------------------
code="$(curl -s -H "X-API-Key: ${API_KEY:-}" -o "$TMP/health.json" -w '%{http_code}' "$API/health")"
if [ "$code" = "200" ] && grep -q '"status":"ok"' "$TMP/health.json"; then
  ok "GET $API/health -> 200 ($(cat "$TMP/health.json"))"
else
  bad "GET $API/health -> $code"
fi

code="$(curl -s -H "X-API-Key: ${API_KEY:-}" -o /dev/null -w '%{http_code}' "$WEB/")"
if [ "$code" = "200" ]; then ok "GET $WEB/ (frontend) -> 200"; else bad "GET $WEB/ -> $code"; fi

code="$(curl -s -H "X-API-Key: ${API_KEY:-}" -o "$TMP/cap.json" -w '%{http_code}' "$WEB/api/capabilities")"
if [ "$code" = "200" ] && grep -q '"ffmpeg_available":true' "$TMP/cap.json"; then
  ok "GET $WEB/api/capabilities (proxy -> backend) -> ffmpeg_available:true"
else
  bad "GET $WEB/api/capabilities -> $code ($(cat "$TMP/cap.json" 2>/dev/null))"
fi

code="$(curl -s -H "X-API-Key: ${API_KEY:-}" -o "$TMP/detect.json" -w '%{http_code}' "$API/api/v1/detect?url=https://www.youtube.com/watch?v=dQw4w9WgXcQ")"
if [ "$code" = "200" ] && grep -q '"platform":"youtube"' "$TMP/detect.json"; then
  ok "GET /api/v1/detect -> youtube"
else
  bad "GET /api/v1/detect -> $code ($(cat "$TMP/detect.json" 2>/dev/null))"
fi

# -----------------------------------------------------------------------------
head "3. Fluxo completo no container: gerar vídeo -> cut-crop -> split"
# -----------------------------------------------------------------------------
# Gera um vídeo de teste DENTRO do container do backend (usa o ffmpeg da imagem)
# e copia para o host.
SAMPLE_LEN="${SAMPLE_LEN:-95}"  # 95s -> split deve gerar 2 clipes (60 + 35)
GEN="ffmpeg -y -hide_banner -loglevel error -f lavfi -i testsrc=duration=${SAMPLE_LEN}:size=640x480:rate=30 -f lavfi -i sine=frequency=440:duration=${SAMPLE_LEN} -c:v libx264 -preset ultrafast -pix_fmt yuv420p -c:a aac -shortest /tmp/sample.mp4"
dc exec -T "$BACKEND_SVC" sh -c "$GEN" \
  && dc cp "$BACKEND_SVC:/tmp/sample.mp4" "$TMP/sample.mp4" 2>/dev/null

if [ -s "$TMP/sample.mp4" ]; then
  ok "vídeo de teste gerado no container ($(wc -c < "$TMP/sample.mp4") bytes, ${SAMPLE_LEN}s)"
else
  bad "não consegui gerar o vídeo de teste no container"
fi

# --- cut-crop: corta 0..8s e recorta 320x240 a partir de (40,30) ---
if [ -s "$TMP/sample.mp4" ]; then
  code="$(curl -s -H "X-API-Key: ${API_KEY:-}" -o "$TMP/cropped.mp4" -w '%{http_code}' \
    -F "file=@$TMP/sample.mp4;type=video/mp4" \
    -F "start=0" -F "end=8" \
    -F "crop_x=40" -F "crop_y=30" -F "crop_w=320" -F "crop_h=240" \
    -F "output_format=mp4" \
    "$API/api/v1/edit/cut-crop")"
  if [ "$code" = "200" ] && [ -s "$TMP/cropped.mp4" ]; then
    # confere dimensões e duração com ffprobe do container
    dc cp "$TMP/cropped.mp4" "$BACKEND_SVC:/tmp/cropped.mp4" 2>/dev/null
    meta="$(dc exec -T "$BACKEND_SVC" sh -c \
      "ffprobe -v error -select_streams v:0 -show_entries stream=width,height -show_entries format=duration -of csv=s=x:p=0 /tmp/cropped.mp4")"
    echo "        ffprobe(cropped) = $meta"
    case "$meta" in
      320x240*) ok "cut-crop -> 200, saída 320x240" ;;
      *) bad "cut-crop -> 200 mas dimensões inesperadas: $meta" ;;
    esac
  else
    bad "POST /api/v1/edit/cut-crop -> $code"
  fi
fi

# --- split: fatia em clipes de 60s, confere que nenhum passa de 60.5s ---
if [ -s "$TMP/sample.mp4" ]; then
  code="$(curl -s -H "X-API-Key: ${API_KEY:-}" -D "$TMP/split.hdr" -o "$TMP/clips.zip" -w '%{http_code}' \
    -F "file=@$TMP/sample.mp4;type=video/mp4" -F "segment_seconds=60" \
    "$API/api/v1/edit/split")"
  clipcount="$(grep -i '^x-clip-count:' "$TMP/split.hdr" | tr -d '\r' | awk '{print $2}')"
  if [ "$code" = "200" ] && [ -s "$TMP/clips.zip" ]; then
    unzip -o -q "$TMP/clips.zip" -d "$TMP/clips"
    worst=0
    for f in "$TMP/clips"/*; do
      dc cp "$f" "$BACKEND_SVC:/tmp/clip.mp4" 2>/dev/null
      d="$(dc exec -T "$BACKEND_SVC" sh -c "ffprobe -v error -show_entries format=duration -of csv=p=0 /tmp/clip.mp4" | tr -d '\r')"
      echo "        $(basename "$f") = ${d}s"
      awk "BEGIN{exit !($d > $worst)}" && worst="$d"
    done
    n="$(find "$TMP/clips" -type f | wc -l | tr -d ' ')"
    if awk "BEGIN{exit !($worst <= 60.5)}"; then
      ok "split -> 200, $n clipe(s) (header X-Clip-Count=$clipcount), maior=${worst}s (<= 60.5s)"
    else
      bad "split -> algum clipe passou de 60.5s (maior=${worst}s)"
    fi
  else
    bad "POST /api/v1/edit/split -> $code"
  fi
fi

# -----------------------------------------------------------------------------
head "4. Erro estruturado (upload inválido -> {detail, error_type})"
# -----------------------------------------------------------------------------
printf 'isto nao e um video' > "$TMP/bad.mp4"
code="$(curl -s -H "X-API-Key: ${API_KEY:-}" -o "$TMP/err.json" -w '%{http_code}' \
  -F "file=@$TMP/bad.mp4;type=video/mp4" -F "start=0" -F "end=3" \
  "$API/api/v1/edit/cut-crop")"
if [ "$code" != "200" ] && grep -q '"error_type"' "$TMP/err.json"; then
  ok "cut-crop(arquivo inválido) -> $code com error_type ($(cat "$TMP/err.json"))"
else
  bad "cut-crop(arquivo inválido) -> $code sem error_type ($(cat "$TMP/err.json" 2>/dev/null))"
fi

# -----------------------------------------------------------------------------
echo
echo "============================================================"
echo "RESULTADO: $PASS passaram, $FAIL falharam"
echo "============================================================"
[ "$FAIL" -eq 0 ]
