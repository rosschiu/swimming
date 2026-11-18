#!/usr/bin/env bash
set -euo pipefail

revision=${1:?Pass the full Git revision}
[[ $# == 1 && "$revision" =~ ^[0-9a-f]{40}$ ]] || exit 2
root=/opt/swimming
release="$root/releases/$revision"
current="$root/current"
exec 9>"$root/deploy.lock"
flock 9

ready() {
  curl -fsS --max-time 2 http://127.0.0.1:18083/api/v1/ready > /dev/null
}

wait_until_ready() {
  for ((attempt=0; attempt<30; attempt++)); do
    if ready; then
      return 0
    fi
    sleep 1
  done
  return 1
}

previous=$(readlink "$current" || true)
if [[ "$previous" == "$release" ]] && ready; then
  exit 0
fi

staging=$(mktemp -d "$root/releases/.incoming.XXXXXXXX")
trap 'rm -rf "$staging"; rm -f "$root/.current-next"' EXIT
chown swimming:swimming "$staging"
runuser -u swimming -- tar --no-same-owner --no-same-permissions -xzf - -C "$staging"
chown -hR root:root "$staging"
[[ -z $(find "$staging" ! -type d ! -type f -print -quit) ]]
[[ $(cat "$staging/REVISION") == "$revision" ]]
[[ -x "$staging/swimming-api" && -x "$staging/swimming-migrate" && -f "$staging/web/index.html" ]]
chmod 755 "$staging"

if [[ ! -d "$release" ]]; then
  mv -- "$staging" "$release"
fi

switch_to() {
  ln -s -- "$1" "$root/.current-next"
  mv -Tf -- "$root/.current-next" "$current"
}

systemctl start "swimming-migrate@$revision.service"
switch_to "$release"

if systemctl restart swimming.service && wait_until_ready; then
  exit 0
fi

echo "Release $revision failed readiness" >&2
if [[ -z "$previous" ]]; then
  systemctl stop swimming.service
  rm -f "$current"
elif [[ "$previous" != "$release" ]]; then
  switch_to "$previous"
  if systemctl restart swimming.service && wait_until_ready; then
    echo "Previous release restored and ready" >&2
  else
    echo "Previous release restored but failed readiness" >&2
  fi
fi
exit 1
