#!/usr/bin/env bash
# Teste de download REAL (usa rede) contra a stack já rodando.
# Baixa de 2-3 plataformas via os endpoints de job (start -> poll -> file).
#   API_KEY=<a do backend/.env> bash infra/smoke/real-download.sh   (rodar da raiz do repo)
set -u
API="${API:-http://localhost:8000}"
TMP=".smoke-tmp/real-download"; rm -rf "$TMP"; mkdir -p "$TMP"
trap 'rm -rf "$TMP"' EXIT
PASS=0; FAIL=0
ok(){ echo "  PASS  $1"; PASS=$((PASS+1)); }
bad(){ echo "  FAIL  $1"; FAIL=$((FAIL+1)); }

# plat|url|mode  (mode: single | clips)
CASES=(
  "youtube|https://www.youtube.com/watch?v=aqz-KE-bpKQ|single"
  "tiktok|https://www.tiktok.com/@scout2015/video/6718335390845095173|single"
  "tiktok|https://www.tiktok.com/@scout2015/video/6718335390845095173|clips"
)

for c in "${CASES[@]}"; do
  plat="${c%%|*}"; rest="${c#*|}"; url="${rest%%|*}"; mode="${rest##*|}"
  echo "=== $plat ($mode) ==="
  ep="download/start"; [ "$mode" = "clips" ] && ep="download/clips/start"
  jid="$(curl -s -H "X-API-Key: ${API_KEY:-}" -m 60 -X POST "$API/api/v1/$plat/$ep" \
        -H 'Content-Type: application/json' \
        -d "{\"url\":\"$url\",\"format\":\"mp4\",\"quality\":\"best\"}" \
        | sed -n 's/.*"job_id":"\([^"]*\)".*/\1/p')"
  if [ -z "$jid" ]; then bad "$plat/$mode: sem job_id"; continue; fi
  echo "    job_id=$jid"

  status=""; detail=""
  for i in $(seq 1 120); do
    sleep 2
    resp="$(curl -s -H "X-API-Key: ${API_KEY:-}" "$API/api/v1/downloads/$jid")"
    status="$(echo "$resp" | sed -n 's/.*"status":"\([^"]*\)".*/\1/p')"
    pct="$(echo "$resp" | sed -n 's/.*"percent":\([0-9.]*\).*/\1/p')"
    phase="$(echo "$resp" | sed -n 's/.*"phase":"\([^"]*\)".*/\1/p')"
    printf '\r    [%3ss] status=%-11s percent=%-5s %s            ' "$((i*2))" "$status" "$pct" "$phase"
    [ "$status" = "finished" ] && break
    [ "$status" = "error" ] && { detail="$(echo "$resp" | sed -n 's/.*"detail":"\([^"]*\)".*/\1/p')"; break; }
  done
  echo

  if [ "$status" != "finished" ]; then
    bad "$plat/$mode: terminou como '$status' ($detail)"
    continue
  fi

  out="$TMP/${plat}_${mode}.bin"
  code="$(curl -s -H "X-API-Key: ${API_KEY:-}" -o "$out" -w '%{http_code}' "$API/api/v1/downloads/$jid/file")"
  sz="$(wc -c < "$out" 2>/dev/null || echo 0)"
  if [ "$code" = "200" ] && [ "$sz" -gt 10000 ]; then
    ok "$plat/$mode: baixado $sz bytes (HTTP $code)"
  else
    bad "$plat/$mode: /file -> HTTP $code, $sz bytes"
  fi

  if [ "$mode" = "clips" ]; then
    clips="$(curl -s -H "X-API-Key: ${API_KEY:-}" "$API/api/v1/downloads/$jid" | grep -o '"duration_seconds":[0-9.]*' | cut -d: -f2)"
    echo "    durações dos clipes: $clips"
    worst=0; for d in $clips; do awk "BEGIN{exit !($d> $worst)}" && worst="$d"; done
    if awk "BEGIN{exit !($worst <= 60.5)}"; then
      ok "$plat/clips: nenhum clipe > 60.5s (maior=${worst}s)"
    else
      bad "$plat/clips: clipe > 60.5s (maior=${worst}s)"
    fi
  fi
done

echo
echo "RESULTADO REAL-DOWNLOAD: $PASS passaram, $FAIL falharam"
[ "$FAIL" -eq 0 ]
