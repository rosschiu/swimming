#!/usr/bin/env bash
set -euo pipefail

revision=${1:?Pass the full Git revision}
archive=${2:?Pass the output archive path}
[[ "$revision" =~ ^[0-9a-f]{40}$ ]]
repo=$(cd "$(dirname "$0")/../.." && pwd)
[[ $(git -C "$repo" rev-parse HEAD) == "$revision" ]]
staging=$(mktemp -d)
trap 'rm -rf "$staging"' EXIT

npm run build --prefix "$repo/apps/web"
cp -R "$repo/apps/web/dist" "$staging/web"
(
  cd "$repo/services/api"
  export GOOS=linux GOARCH=amd64 CGO_ENABLED=0
  go build -trimpath -o "$staging/swimming-api" ./cmd/server
  go build -trimpath -o "$staging/swimming-migrate" ./cmd/migrate
)
printf '%s\n' "$revision" > "$staging/REVISION"

tar -czf "$archive" -C "$staging" REVISION swimming-api swimming-migrate web
