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
# Smoke: wait until THIS container start logs "polling" (--since StartedAt avoids the false negative
# where up -d did not recreate the container and the old "polling" sits outside a fixed window),
# then list --json exercises config + DB + schema.
# grep without -q: -q closes the pipe and under pipefail the SIGPIPE from compose logs fails the check.
# If up -d did not recreate the container and the old "polling" already rotated out of the json-file
# log (10m x 3), the smoke fails as a false negative: docker compose up -d --force-recreate finbox, then retry.
started=\$(docker inspect -f '{{.State.StartedAt}}' "\$(docker compose ps -aq finbox)")
n=0
until docker compose logs --since "\$started" finbox 2>&1 | grep 'finbox serve: polling' >/dev/null; do
  [ \$((n++)) -lt 45 ] || { echo 'smoke: finbox no llegó a polling en 45s; últimos logs:'; docker compose logs --tail 40 finbox; exit 1; }
  sleep 1
done
docker compose exec -T finbox finbox list --json >/dev/null </dev/null
docker compose exec -T finbox finbox version </dev/null
EOF
echo "deploy ok"
