package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"finbox/internal/blob/fs"
	"finbox/internal/command"
	"finbox/internal/config"
	"finbox/internal/extract/openai"
	"finbox/internal/pipeline"
	"finbox/internal/store"
)

type cliEnv struct {
	cfg config.Config
	st  *store.Store
	ctx context.Context
}

func withStore(stderr io.Writer, asJSON bool, fn func(e cliEnv) int) int {
	cfg, err := config.FromEnv(os.Getenv)
	if err != nil || cfg.DBURL == "" {
		cliErr(stderr, asJSON, "falta FINBOX_DB_URL")
		return exitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	st, err := store.New(ctx, cfg.DBURL)
	if err != nil {
		cliErr(stderr, asJSON, fmt.Sprintf("db: %v", err))
		return exitRuntime
	}
	defer st.Close()
	return fn(cliEnv{cfg: cfg, st: st, ctx: ctx})
}

func cliErr(stderr io.Writer, asJSON bool, msg string) {
	if asJSON {
		json.NewEncoder(stderr).Encode(map[string]string{"error": msg})
	} else {
		fmt.Fprintln(stderr, msg)
	}
}

func mapErr(stderr io.Writer, asJSON bool, err error) int {
	cliErr(stderr, asJSON, err.Error())
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrAmbiguous) {
		return exitNotFound
	}
	return exitRuntime
}

// popID extracts a leading positional id so flags may FOLLOW it — stdlib
// flag stops parsing at the first non-flag arg, so `edit <id> --total N`
// would otherwise silently ignore every flag.
func popID(argv []string) (string, []string) {
	if len(argv) > 0 && !strings.HasPrefix(argv[0], "-") {
		return argv[0], argv[1:]
	}
	return "", argv
}

// hasJSONFlag scans the raw (unparsed) argv for --json/-json. Used only when
// flag.Parse itself failed, so the *bool a normal fsx.Bool("json", ...) would
// give us can't be trusted — parsing never got that far.
func hasJSONFlag(argv []string) bool {
	for _, a := range argv {
		if a == "--json" || a == "-json" {
			return true
		}
	}
	return false
}

// parseFlags parses argv with fsx, discarding flag's own plain-text error
// output, and instead reports a parse failure through cliErr so the --json
// contract (errors as {"error":...} on stderr) holds even when parsing
// itself fails, e.g. an unknown flag. Returns false on error; callers should
// return exitUsage in that case.
func parseFlags(fsx *flag.FlagSet, argv []string, stderr io.Writer) bool {
	fsx.SetOutput(io.Discard)
	if err := fsx.Parse(argv); err != nil {
		cliErr(stderr, hasJSONFlag(argv), err.Error())
		return false
	}
	return true
}

type txnJSON struct {
	ID          string `json:"id"`
	ShortID     string `json:"short_id"`
	Date        string `json:"date"`
	Merchant    string `json:"merchant"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	Source      string `json:"source"`
}

func toJSON(r store.TxnRow) txnJSON {
	return txnJSON{ID: r.ID, ShortID: r.ShortID, Date: r.OccurredOn.Format("2006-01-02"),
		Merchant: r.Merchant, AmountMinor: r.AmountMinor, Currency: r.Currency, Source: r.Source}
}

func cmdList(argv []string, stdout, stderr io.Writer) int {
	fsx := flag.NewFlagSet("list", flag.ContinueOnError)
	limit := fsx.Int("limit", 10, "máx. de filas")
	month := fsx.String("month", "", "mes: aug | ago | 2026-08")
	asJSON := fsx.Bool("json", false, "salida JSON")
	if !parseFlags(fsx, argv, stderr) {
		return exitUsage
	}
	return withStore(stderr, *asJSON, func(e cliEnv) int {
		rows, err := command.List(e.ctx, e.st, *limit, *month, time.Now(), e.cfg.Loc)
		if err != nil {
			return mapErr(stderr, *asJSON, err)
		}
		if *asJSON {
			out := make([]txnJSON, 0, len(rows))
			for _, r := range rows {
				out = append(out, toJSON(r))
			}
			json.NewEncoder(stdout).Encode(out)
			return exitOK
		}
		for _, r := range rows {
			fmt.Fprintf(stdout, "%s · %s · %s · %d.%02d %s\n", r.ShortID,
				r.OccurredOn.Format("2006-01-02"), r.Merchant, r.AmountMinor/100, r.AmountMinor%100, r.Currency)
		}
		return exitOK
	})
}

func cmdEdit(argv []string, stdout, stderr io.Writer) int {
	fsx := flag.NewFlagSet("edit", flag.ContinueOnError)
	total := fsx.String("total", "", "nuevo total, ej. 285.00")
	merchant := fsx.String("merchant", "", "nuevo comercio")
	date := fsx.String("date", "", "nueva fecha YYYY-MM-DD")
	currency := fsx.String("currency", "", "nueva moneda ISO 4217")
	asJSON := fsx.Bool("json", false, "salida JSON")
	id, rest := popID(argv)
	if !parseFlags(fsx, rest, stderr) {
		return exitUsage
	}
	if id == "" {
		cliErr(stderr, *asJSON, "uso: finbox edit <id> [--total N] [--merchant S] [--date D] [--currency C]")
		return exitUsage
	}
	return withStore(stderr, *asJSON, func(e cliEnv) int {
		row, err := command.Edit(e.ctx, e.st, id,
			command.EditOpts{Total: *total, Merchant: *merchant, Date: *date, Currency: *currency}, e.cfg.Loc)
		if err != nil {
			return mapErr(stderr, *asJSON, err)
		}
		if *asJSON {
			json.NewEncoder(stdout).Encode(toJSON(row))
		} else {
			fmt.Fprintf(stdout, "editado %s · %s · %d.%02d %s\n", row.ShortID, row.Merchant,
				row.AmountMinor/100, row.AmountMinor%100, row.Currency)
		}
		return exitOK
	})
}

func cmdVoid(argv []string, stdout, stderr io.Writer) int {
	fsx := flag.NewFlagSet("void", flag.ContinueOnError)
	asJSON := fsx.Bool("json", false, "salida JSON")
	id, rest := popID(argv)
	if !parseFlags(fsx, rest, stderr) {
		return exitUsage
	}
	if id == "" {
		cliErr(stderr, *asJSON, "uso: finbox void <id>")
		return exitUsage
	}
	return withStore(stderr, *asJSON, func(e cliEnv) int {
		// shadow the outer `id` (the raw prefix arg) with the resolved
		// transaction uuid returned by command.Void — legal Go, deliberate:
		// everything below this line should only ever see the resolved id.
		id, err := command.Void(e.ctx, e.st, id)
		if err != nil {
			return mapErr(stderr, *asJSON, err)
		}
		if *asJSON {
			json.NewEncoder(stdout).Encode(map[string]string{"voided": id})
		} else {
			fmt.Fprintf(stdout, "anulado %s\n", id[:8])
		}
		return exitOK
	})
}

func cmdReprocess(argv []string, stdout, stderr io.Writer) int {
	fsx := flag.NewFlagSet("reprocess", flag.ContinueOnError)
	asJSON := fsx.Bool("json", false, "salida JSON")
	id, rest := popID(argv)
	if !parseFlags(fsx, rest, stderr) {
		return exitUsage
	}
	if id == "" {
		cliErr(stderr, *asJSON, "uso: finbox reprocess <receipt-id>")
		return exitUsage
	}
	return withStore(stderr, *asJSON, func(e cliEnv) int {
		if e.cfg.OpenAIKey == "" || e.cfg.BlobDir == "" {
			cliErr(stderr, *asJSON, "faltan OPENAI_API_KEY / FINBOX_BLOB_DIR")
			return exitUsage
		}
		d := pipeline.Deps{Store: e.st, Blob: fs.New(e.cfg.BlobDir),
			Extractor: openai.New(e.cfg.OpenAIKey, e.cfg.OpenAIModel), Loc: e.cfg.Loc, Log: logger()}
		res, err := command.Reprocess(e.ctx, d, id, time.Now())
		if err != nil {
			return mapErr(stderr, *asJSON, err)
		}
		outcome := map[pipeline.Outcome]string{
			pipeline.OutcomeAwaitingConfirm: "awaiting_confirm", pipeline.OutcomeFailed: "failed",
			pipeline.OutcomeRejected: "rejected", pipeline.OutcomeDuplicate: "duplicate",
		}[res.Outcome]
		if *asJSON {
			json.NewEncoder(stdout).Encode(map[string]string{
				"receipt_id": res.ReceiptID, "outcome": outcome, "fail_reason": res.FailReason})
		} else {
			fmt.Fprintf(stdout, "%s → %s %s\n", res.ReceiptID[:8], outcome, res.FailReason)
		}
		return exitOK
	})
}
