#!/usr/bin/env bash
# Cheap gate for a prompt/model change: the receipts that have flipped before, each extracted N times,
# grouped so an unstable field shows up as two lines. Run BEFORE the full corpus, after every prompt edit.
# Run:  scripts/probe.sh            # 5 repeats, the sensitive set below
#       scripts/probe.sh 10 2d57cc06 55e4d64b   # N repeats, only these sha prefixes
# Same env as extract-corpus.sh (OPENAI_API_KEY from .env, FINBOX_OPENAI_MODEL default gpt-4.1-mini).
set -euo pipefail
cd "$(dirname "$0")/.."
[ -n "${OPENAI_API_KEY:-}" ] || OPENAI_API_KEY=$(sed -n 's/^OPENAI_API_KEY=//p' .env)
[ -n "$OPENAI_API_KEY" ] || { echo "falta OPENAI_API_KEY (entorno o .env)" >&2; exit 1; }
export OPENAI_API_KEY FINBOX_OPENAI_MODEL=${FINBOX_OPENAI_MODEL:-gpt-4.1-mini}
n=${1:-5}; shift || true
# why each one is here: tip voucher (Monto+Propina) · CFE bill (572.83 vs TOTAL A PAGAR 573) · Soriana split payment
# · 7-Eleven US-format date · KINDER (no items, category) · GTU refacciones (category) · big la Comer (loyalty lines)
# · Cocoteros (no items) · GoodNites at la Comer (ropa vs super) · Liverpool "Consumo"
shas=(${@:-2d57cc06 55e4d64b 0a205b1d d15ee7bf 8fa201d2 52fa40ca 4e4de111 d425c561 0a6462fd 092c4be6})
bin=$(mktemp -d)/finbox; go build -o "$bin" ./cmd/finbox
for sha in "${shas[@]}"; do
  f=$(find testdata/real -type f -name "$sha*" | head -1)
  [ -n "$f" ] || { echo "sin blob para $sha" >&2; continue; }
  for _ in $(seq "$n"); do
    "$bin" extract "$f" --json --today "$(date -r "$f" +%F)" |
      python3 -c 'import json,sys;j=json.load(sys.stdin);print(sys.argv[1],j["date"],j["total"],j.get("category",""),"|",j["merchant"][:24])' "$sha" ||
      echo "$sha FAIL"
    sleep 1
  done | sort | uniq -c
done
