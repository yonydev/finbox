#!/usr/bin/env bash
set -euo pipefail

HOST="${FINBOX_DEPLOY_HOST:-finbox-pi}"
DIR="${FINBOX_DEPLOY_DIR:-~/finbox}"

ssh "$HOST" bash -s <<EOF
set -euo pipefail
cd $DIR
# SSD sentinel: refuse to run against an unmounted data disk (spec §10)
test -f /mnt/ssd/.finbox-ssd || { echo "SSD sentinel missing — is /mnt/ssd mounted?"; exit 1; }
git pull --ff-only
docker compose build finbox
docker compose up -d postgres
until docker compose exec -T postgres pg_isready -U finbox >/dev/null 2>&1; do sleep 1; done
docker compose run --rm finbox migrate
docker compose up -d
sleep 2
docker compose exec -T finbox finbox version
EOF
echo "deploy ok"
