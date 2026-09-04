#!/usr/bin/env bash
set -euo pipefail

HOST="${FINBOX_DEPLOY_HOST:-finbox-pi}"
DIR="${FINBOX_DEPLOY_DIR:-~/finbox}"
# Sentinel file proving the target's data disk is mounted; set to "skip" if not applicable.
SENTINEL="${FINBOX_DATA_SENTINEL:-/mnt/ssd/.finbox-ssd}"

ssh "$HOST" bash -s <<EOF
set -euo pipefail
cd $DIR
# Data-disk sentinel: refuse to run against an unmounted data disk
[ "$SENTINEL" = skip ] || test -f "$SENTINEL" || { echo "data-disk sentinel $SENTINEL missing — is the disk mounted?"; exit 1; }
git pull --ff-only
docker compose build finbox
docker compose up -d postgres
until docker compose exec -T postgres pg_isready -U finbox >/dev/null 2>&1; do sleep 1; done
docker compose run --rm -T finbox migrate   # -T: don't let the container eat this script's stdin
docker compose up -d
sleep 2
docker compose exec -T finbox finbox version
EOF
echo "deploy ok"
