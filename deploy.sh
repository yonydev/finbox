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
# </dev/null on every exec/run: they attach stdin, which here is the script bash -s is reading
until docker compose exec -T postgres pg_isready -U finbox >/dev/null 2>&1 </dev/null; do sleep 1; done
# Pre-migration dump: instant rollback point if a migration goes wrong.
# \$ so date runs on the target, not expanded by the local heredoc.
mkdir -p backups
docker compose exec -T postgres pg_dump -Fc -U finbox finbox > "backups/pre-migrate-\$(date +%F-%H%M%S).dump" </dev/null
find backups -name 'pre-migrate-*.dump' -mtime +14 -delete
docker compose run --rm -T finbox migrate </dev/null
docker compose up -d
sleep 2
docker compose exec -T finbox finbox version </dev/null
EOF
echo "deploy ok"
