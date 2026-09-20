#!/usr/bin/env bash
# Nightly off-site backup: pg_dump → local rotation → copy to R2 (rclone crypt).
#
# Root crontab on the Pi: 0 3 * * * /opt/finbox/scripts/backup-offsite.sh
# It REPLACES the pg_dump line in yonatanpi's crontab (docs/deploy-pi.md §5) —
# keep both and you get two dumps a night.
#
# Runs as root: receipt blobs are written 0600 under uid 10001 (internal/blob/fs/fs.go),
# so yonatanpi cannot read them; rclone.conf lives in /root/.config/rclone/rclone.conf (600).
#
# .env must stay KEY=value with no spaces or quotes — it is sourced, and a value with
# spaces dies before the ERR trap is armed, so the failure would be silent.
#
# On failure: Telegram message via the bot token. Setup: docs/deploy-pi.md §7.
set -euo pipefail
cd "$(dirname "$0")/.."
# shellcheck disable=SC1091
set -a; . ./.env; set +a
LOG=$PWD/backup-offsite.log
notify() { curl -sS -m 20 "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/sendMessage" -d chat_id="${TELEGRAM_ALLOWED_USER_IDS%%,*}" --data-urlencode text="$1" >/dev/null || true; }
trap 'notify "⚠️ finbox backup-offsite falló (línea $LINENO). Revisa $LOG en la Pi."' ERR

dump="backups/finbox-$(date +%F).dump"
# Two statements, NOT `a && b`: under set -e a failure left of && neither aborts nor fires the ERR trap.
docker compose exec -T postgres pg_dump -Fc -U finbox finbox > "$dump.tmp"
mv "$dump.tmp" "$dump"   # tmp+mv: a partial dump is never uploaded
find backups -name 'finbox-*.dump' -mtime +30 -delete
# copy, never sync: a local delete must not propagate to the only off-site copy.
# No --log-level INFO: it grows unrotated; NOTICE (the default) already logs errors.
rclone copy backups  r2crypt:backups  --exclude '*.tmp' --log-file "$LOG"
rclone copy receipts r2crypt:receipts --log-file "$LOG"
