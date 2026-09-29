package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"finbox/internal/command"
	"finbox/internal/embed"
	"finbox/internal/money"
	"finbox/internal/store"
)

// embedChunk is how many docs go in one embeddings call; the API takes 2048,
// the whole corpus fits in one call today.
const embedChunk = 100

type hitJSON struct {
	txnJSON
	Distance float64 `json:"distance"`
}

// cmdSearch prints the 5 closest expenses, nearest first, with their raw
// distance. It never applies the bot's cutoff: the eval must see the whole
// top-5. --mode trgm is the no-API baseline.
func cmdSearch(argv []string, stdout, stderr io.Writer) int {
	fsx := flag.NewFlagSet("search", flag.ContinueOnError)
	mode := fsx.String("mode", "vec", "vec | trgm")
	asJSON := fsx.Bool("json", false, "salida JSON")
	const usage = "uso: finbox search [--mode vec|trgm] [--json] <texto…>"
	setUsage(fsx, usage)
	if ok, code := parseFlags(fsx, argv, stdout, stderr); !ok {
		return code
	}
	query := strings.Join(fsx.Args(), " ")
	if query == "" || (*mode != "vec" && *mode != "trgm") {
		cliErr(stderr, *asJSON, usage)
		return exitUsage
	}
	return withStore(stderr, *asJSON, func(e cliEnv) int {
		var emb *embed.Client
		if *mode == "vec" {
			if e.cfg.OpenAIKey == "" {
				cliErr(stderr, *asJSON, "falta OPENAI_API_KEY")
				return exitUsage
			}
			emb = &embed.Client{APIKey: e.cfg.OpenAIKey, Model: embed.Model}
		}
		hits, err := command.Search(e.ctx, e.st, emb, query)
		if err != nil {
			return mapErr(stderr, *asJSON, err)
		}
		if *asJSON {
			out := make([]hitJSON, 0, len(hits))
			for _, h := range hits {
				out = append(out, hitJSON{toJSON(h.TxnRow), h.Distance})
			}
			json.NewEncoder(stdout).Encode(out)
			return exitOK
		}
		for _, h := range hits {
			fmt.Fprintf(stdout, "%.3f · %s · %s · %s · %s\n", h.Distance, h.ShortID,
				h.OccurredOn.Format("2006-01-02"), h.MerchantCanon, money.Format(h.AmountMinor, h.Currency))
		}
		return exitOK
	})
}

// cmdReembed indexes every active expense whose doc or model changed.
// Idempotent and cheap on a no-op, so deploy.sh runs it every time.
func cmdReembed(argv []string, stdout, stderr io.Writer) int {
	fsx := flag.NewFlagSet("reembed", flag.ContinueOnError)
	dryRun := fsx.Bool("dry-run", false, "solo muestra los docs por indexar, no llama a OpenAI")
	model := fsx.String("model", embed.Model, "modelo de embeddings")
	setUsage(fsx, "uso: finbox reembed [--dry-run] [--model M]")
	if ok, code := parseFlags(fsx, argv, stdout, stderr); !ok {
		return code
	}
	return withStore(stderr, false, func(e cliEnv) int {
		docs, err := e.st.TxnDocs(e.ctx, "")
		if err != nil {
			return mapErr(stderr, false, err)
		}
		var stale []store.TxnDoc
		for _, d := range docs {
			if d.Stale(*model) {
				stale = append(stale, d)
			}
		}
		if *dryRun {
			for _, d := range stale {
				fmt.Fprintf(stdout, "%s · %s\n", d.ID[:8], d.Doc)
			}
			fmt.Fprintf(stdout, "%d gastos por indexar (dry-run, sin escribir)\n", len(stale))
			return exitOK
		}
		if e.cfg.OpenAIKey == "" {
			cliErr(stderr, false, "falta OPENAI_API_KEY")
			return exitUsage
		}
		emb := &embed.Client{APIKey: e.cfg.OpenAIKey, Model: *model}
		for i := 0; i < len(stale); i += embedChunk {
			chunk := stale[i:min(i+embedChunk, len(stale))]
			texts := make([]string, len(chunk))
			for j, d := range chunk {
				texts[j] = d.Doc
			}
			vecs, err := emb.Embed(e.ctx, texts)
			if err != nil {
				return mapErr(stderr, false, err)
			}
			for j, d := range chunk {
				if err := e.st.UpsertEmbedding(e.ctx, d.ID, *model, d.Hash, d.Doc, vecs[j]); err != nil {
					return mapErr(stderr, false, err)
				}
			}
		}
		fmt.Fprintf(stdout, "%d gastos indexados\n", len(stale))
		return exitOK
	})
}
