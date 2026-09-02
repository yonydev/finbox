# finbox

finbox turns a photo of a receipt into a structured expense: send it to a Telegram bot, tap Confirm, and the expense shows up in Postgres. It's one Go binary that runs as either a Telegram bot daemon or a CLI, built to run on a Raspberry Pi for near-zero infrastructure cost.

```mermaid
flowchart LR
    YOU(["📱 You<br/>photo of receipt"])
    TG["Telegram API"]
    OAI["OpenAI API<br/>vision · extraction"]

    subgraph PI["Raspberry Pi 5 · Docker Compose — dev on laptop, deploy at end of Phase 1"]
        subgraph FD["finbox serve · daemon mode"]
            BOT["internal/telegram<br/>confirm ✅ ❌ · /list /month /pending /help"]
            subgraph PL["internal/pipeline"]
                direction LR
                F["fetch"] --> X["extract<br/>⟨Extractor⟩"] --> V["validate<br/>code · PAN scrub"] --> S["persist"]
            end
            BOT --> PL
        end
        CLI["finbox · CLI mode<br/>same binary · --json · exit codes"]
        CMD["internal/command<br/>shared command registry"]
        BLOB["⟨BlobStore⟩ → blob/fs<br/>/data/receipts · SSD"]
        STORE["internal/store<br/>pgx · migrations"]
        PG[("PostgreSQL<br/>pgvector ready · SSD")]
    end

    WEB["Phase 2 · dashboard<br/>served from the Pi over Tailscale"]
    API["Phase 2 · finbox api · HTTP"]

    YOU -- "photo" --> TG
    TG <-- "long polling · outbound only" --> BOT
    F -- "original image" --> BLOB
    X <-- "image ⇄ structured JSON" --> OAI
    S -- "validated txn" --> STORE
    BOT -- "queries" --> CMD
    CLI -- "queries" --> CMD
    CMD --> STORE
    STORE -- "SQL" --> PG
    WEB -.-> API
    API -.-> CMD

    classDef go fill:#e7f3ec,stroke:#177e52,color:#10603e
    classDef ext fill:#fbf1e3,stroke:#a8600f,color:#7a4507
    classDef infra fill:#f1f4f1,stroke:#3d4a43,color:#232a26
    classDef p2 fill:none,stroke:#66736c,color:#66736c,stroke-dasharray:5 4
    classDef plain fill:none,stroke:#3d4a43,color:#232a26

    class BOT,F,X,V,S,CLI,STORE,CMD go
    class TG,OAI ext
    class BLOB,PG infra
    class WEB,API p2
    class YOU plain
```

## Prerequisites

- Docker (with the `compose` plugin)
- Go 1.27+
- An OpenAI API key

## BotFather setup

Talk to [@BotFather](https://t.me/BotFather) on Telegram and create two bots — one for local development, one for production:

1. `/newbot` → name it something like `yourname_finbox_dev_bot`, copy the token it gives you.
2. `/newbot` again → `yourname_finbox_bot` (or similar) for the real, production bot. Copy its token too.

Each bot has its own token; the token you put in `.env` determines which bot the running `finbox` process talks to. Use the dev bot's token while working locally.

## Local run

```bash
cp .env.example .env
# edit .env: paste in your dev bot's TELEGRAM_BOT_TOKEN and your OPENAI_API_KEY

docker compose up -d postgres
go run ./cmd/finbox migrate
go run ./cmd/finbox serve
```

`serve` runs the bot and pipeline as a daemon. Every other subcommand (`list`, `edit`, `void`, `reprocess`, ...) is a one-shot CLI call against the same database.

## First-run bootstrap

`TELEGRAM_ALLOWED_USER_IDS` starts empty, so the bot refuses to process anything at first — this is deliberate. To find your own Telegram user id:

1. Send the bot (the dev bot, from BotFather) any message.
2. Look at the `finbox serve` logs for a `WARN`-level line like `allowlist empty: refusing to process — add this id to TELEGRAM_ALLOWED_USER_IDS`, with a `user_id` field alongside it.
3. Put that id in `TELEGRAM_ALLOWED_USER_IDS` in `.env`.
4. Restart `finbox serve`.

## Daily use

Send a photo of a receipt to the bot. It replies with a summary card (merchant, date, total, items, any warnings) and `✅ Confirmar` / `❌ Descartar` buttons. Confirming inserts the expense; discarding leaves the receipt on record but out of your totals. A discarded card keeps a `🔄 Reintentar` button, and re-sending the same photo also revives it with a fresh confirm card — nothing is lost by discarding.

Bot commands:

- `/list [N]` — last N confirmed expenses (default 10)
- `/month [token]` — total and count for a month (e.g. `/month aug`), reported per currency
- `/pending` — receipts still awaiting confirmation or that failed extraction
- `/help` — command list and accepted photo formats

CLI commands, same underlying data:

```bash
finbox list --json
finbox edit <id> --total 285.00
finbox void <id>
finbox reprocess <id>
```

Every read/write CLI command accepts `--json` for scripting, with stable exit codes (`0` ok, `1` runtime error, `2` usage error, `3` not found/ambiguous id). `<id>` can be a full UUID or its 8-character short prefix, same one shown in `/list` and `finbox list`.

## Deploying to the Pi

`deploy.sh` runs from your laptop and deploys over SSH to whatever host `FINBOX_DEPLOY_HOST` points at (default alias: `finbox-pi`, configured in your own `~/.ssh/config` — never checked into this repo). It builds the image, brings up Postgres, runs migrations, and restarts the app:

```bash
./deploy.sh
```

It refuses to run if the Pi's data SSD isn't mounted. A fuller operations runbook (backups, restore drills, mount-ordering guards) lands in a later task.

## Testing

Most tests are plain unit tests and need nothing special. Tests that touch Postgres need a running instance:

```bash
docker compose up -d postgres
TEST_DB_URL=postgres://finbox:finbox@localhost:5432/finbox go test ./...
```
