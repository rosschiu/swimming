#!/usr/bin/env bash
set -euo pipefail

root=/opt/swimming
repo=rosschiu/swimming
api="https://api.github.com/repos/$repo"
token=$(cat "${CREDENTIALS_DIRECTORY:?Missing systemd credentials}/github-token")
[[ "$token" =~ ^github_pat_[a-zA-Z0-9_]+$ ]] || {
  echo 'A dedicated fine-grained GitHub token is required.' >&2
  exit 1
}

umask 077
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
printf 'header = "Authorization: Bearer %s"\n' "$token" > "$work/auth.conf"
unset token

github() {
  curl --disable --config "$work/auth.conf" --fail --silent --show-error \
    --proto '=https' --proto-redir '=https' --location --max-time 60 "$@"
}

main_revision() {
  github "$api/git/ref/heads/main" | jq -er '.object.sha | select(type == "string" and test("^[0-9a-f]{40}$"))'
}

revision=$(main_revision)
if [[ "$(readlink "$root/current" || true)" == "$root/releases/$revision" ]] &&
  curl --disable -fsS --max-time 2 http://127.0.0.1:18083/api/v1/ready > /dev/null; then
  exit 0
fi

run=$(github "$api/actions/workflows/ci.yml/runs?branch=main&event=push&head_sha=$revision&per_page=1" |
  jq -cer '.workflow_runs | if type == "array" then .[0] else error("invalid workflow runs") end | . // {}')
if [[ "$run" == '{}' ]]; then
  exit 0
fi
if ! jq -e --arg revision "$revision" --arg repo "$repo" '
  .head_sha == $revision and .head_branch == "main" and .event == "push" and
  .head_repository.full_name == $repo and .status == "completed" and .conclusion == "success"
' <<< "$run" > /dev/null; then
  exit 0
fi
run_id=$(jq -er '.id | tostring | select(test("^[1-9][0-9]*$"))' <<< "$run")

artifact=$(github "$api/actions/runs/$run_id/artifacts?name=swimming-vm-release&per_page=100" |
  jq -cer '[.artifacts[] | select(.name == "swimming-vm-release" and .expired == false)] |
    if length == 1 then .[0] elif length == 0 then {} else error("ambiguous release artifact") end')
if [[ "$artifact" == '{}' ]]; then
  exit 0
fi
artifact_id=$(jq -er '.id | tostring | select(test("^[1-9][0-9]*$"))' <<< "$artifact")
digest=$(jq -er '.digest | select(type == "string" and test("^sha256:[0-9a-f]{64}$")) | ltrimstr("sha256:")' <<< "$artifact")
github "$api/actions/artifacts/$artifact_id/zip" > "$work/artifact.zip"
printf '%s  %s\n' "$digest" "$work/artifact.zip" | sha256sum --check --status
unzip -p "$work/artifact.zip" swimming-release.tar.gz > "$work/swimming-release.tar.gz"

latest=$(main_revision)
if [[ "$latest" != "$revision" ]]; then
  echo 'Main changed during download. A later check will evaluate the new revision.'
  exit 0
fi
/usr/local/sbin/swimming-install-release "$revision" < "$work/swimming-release.tar.gz"
