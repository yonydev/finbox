#!/usr/bin/env bash
# Extraction corpus: `finbox extract --json` over every real receipt blob, one JSON per distinct sha
# per model, so a prompt/model change is measured with `diff -r out/<before> out/<after>`.
#
# Blobs (never committed, testdata/real/ is gitignored):  rclone copy r2crypt:receipts testdata/real
# Run:  scripts/extract-corpus.sh                     # model from FINBOX_OPENAI_MODEL, default gpt-4.1-mini
#       FINBOX_OPENAI_MODEL=gpt-4o scripts/extract-corpus.sh
# Resumable: an existing out/<model>/<sha>.json is skipped; failures are listed and do not stop the loop,
# rerun to pick them up. Same model but a new prompt: `mv out/<model> out/<model>-before` first.
# OpenAI's 200k tokens/min tier is ~20 photos/min and the extractor treats 429 as non-retryable: paced.
set -euo pipefail
cd "$(dirname "$0")/.."
[ -d testdata/real ] || { echo "falta testdata/real/ — rclone copy r2crypt:receipts testdata/real" >&2; exit 1; }
[ -n "${OPENAI_API_KEY:-}" ] || OPENAI_API_KEY=$(sed -n 's/^OPENAI_API_KEY=//p' .env)
[ -n "$OPENAI_API_KEY" ] || { echo "falta OPENAI_API_KEY (entorno o .env)" >&2; exit 1; }
export OPENAI_API_KEY
model=${FINBOX_OPENAI_MODEL:-gpt-4.1-mini}
out=out/$model
mkdir -p "$out"
go build -o "$out/.finbox" ./cmd/finbox
fail=0
while read -r f; do
  sha=$(basename "$f"); sha=${sha%.*}
  [ -s "$out/$sha.json" ] && continue
  if FINBOX_OPENAI_MODEL=$model "$out/.finbox" extract "$f" --json --today "$(date -r "$f" +%F)" > "$out/$sha.json.tmp"; then
    mv "$out/$sha.json.tmp" "$out/$sha.json"   # tmp+mv: a failed call never leaves a half JSON that resume would skip
  else
    echo "FAIL $f" >&2; rm -f "$out/$sha.json.tmp"; fail=$((fail+1))
  fi
  sleep 5
done < <(find testdata/real -type f | sort)
echo "$(find "$out" -name '*.json' | wc -l) JSON en $out, $fail fallos"
[ "$fail" -eq 0 ]
