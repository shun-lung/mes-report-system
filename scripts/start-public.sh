#!/usr/bin/env bash
# 啟動資料庫、API（含網頁）與 Cloudflare 快速通道，並印出公開網址。
#   ./scripts/start-public.sh           一般啟動
#   ./scripts/start-public.sh --build   先重新建置前端（改過前端程式後使用）
# 按 Ctrl+C 會一併停止 API 與通道（資料庫保持執行）。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CLOUDFLARED="${CLOUDFLARED:-$HOME/.local/bin/cloudflared}"

if lsof -nP -iTCP:8080 -sTCP:LISTEN >/dev/null 2>&1; then
  echo "8080 已被占用（可能 API 已在執行），請先停止後再啟動。" >&2
  exit 1
fi

echo "▸ 啟動資料庫"
docker compose -f "$ROOT/deploy/docker-compose.yml" up -d

if [[ ! -d "$ROOT/frontend/dist" || "${1:-}" == "--build" ]]; then
  echo "▸ 建置前端"
  (cd "$ROOT/frontend" && npm run build)
fi

echo "▸ 啟動 API"
cd "$ROOT/backend"
go build -o bin/mes-api ./cmd/api
GIN_MODE=release ./bin/mes-api &
API_PID=$!
trap 'kill $API_PID 2>/dev/null' EXIT
for _ in $(seq 1 30); do
  curl -s -o /dev/null http://127.0.0.1:8080/ && break
  sleep 1
done

echo "▸ 建立 Cloudflare 通道（約 10 秒）"
"$CLOUDFLARED" tunnel --no-autoupdate --url http://127.0.0.1:8080 2>&1 | while IFS= read -r line; do
  if url=$(grep -oE 'https://[a-z0-9-]+\.trycloudflare\.com' <<<"$line"); then
    echo
    echo "  公開網址：$url"
    echo "  （按 Ctrl+C 停止）"
    echo
  elif grep -q ' ERR ' <<<"$line"; then
    echo "$line" >&2
  fi
done
