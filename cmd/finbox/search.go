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

type hitJSON struct {
	txnJSON
	Distance float64 `json:"distance"`
	TrgmRank int     `json:"trgm_rank"`
	VecRank  int     `json:"vec_rank"`
}

// cmdSearch prints the 5 closest expenses, nearest first, with their raw
// distance and their rank in each list. It never filters beyond what Search
// does, so --mode hybrid is exactly what the bot answers; trgm and vec print
// the untouched top-5 the eval reads the distance spreads from.
func cmdSearch(argv []string, stdout, stderr io.Writer) int {
	fsx := flag.NewFlagSet("search", flag.ContinueOnError)
	mode := fsx.String("mode", "hybrid", "hybrid (lo que usa el bot) | trgm | vec")
	asJSON := fsx.Bool("json", false, "salida JSON")
	const usage = "uso: finbox search [--mode hybrid|trgm|vec] [--json] <texto…>"
	setUsage(fsx, usage)
	if ok, code := parseFlags(fsx, argv, stdout, stderr); !ok {
		return code
	}
	query := strings.Join(fsx.Args(), " ")
	if query == "" || (*mode != "vec" && *mode != "trgm" && *mode != "hybrid") {
		cliErr(stderr, *asJSON, usage)
		return exitUsage
	}
	return withStore(stderr, *asJSON, func(e cliEnv) int {
		var emb *embed.Client
		if *mode != "trgm" {
			if e.cfg.OpenAIKey == "" {
				cliErr(stderr, *asJSON, "falta OPENAI_API_KEY")
				return exitUsage
			}
			emb = &embed.Client{APIKey: e.cfg.OpenAIKey}
		}
		hits, err := command.Search(e.ctx, e.st, emb, *mode, query)
		if err != nil {
			return mapErr(stderr, *asJSON, err)
		}
		if *asJSON {
			out := make([]hitJSON, 0, len(hits))
			for _, h := range hits {
				out = append(out, hitJSON{toJSON(h.TxnRow), h.Distance, h.TrgmRank, h.VecRank})
			}
			json.NewEncoder(stdout).Encode(out)
			return exitOK
		}
		for _, h := range hits {
			fmt.Fprintf(stdout, "%.3f · t%d v%d · %s · %s · %s · %s\n", h.Distance, h.TrgmRank, h.VecRank,
				h.ShortID, h.OccurredOn.Format("2006-01-02"), h.MerchantCanon, money.Format(h.AmountMinor, h.Currency))
		}
		return exitOK
	})
}

// cmdReembed indexes every active expense whose doc or embedding model changed.
// Idempotent and cheap on a no-op, so deploy.sh runs it every time.
func cmdReembed(argv []string, stdout, stderr io.Writer) int {
	fsx := flag.NewFlagSet("reembed", flag.ContinueOnError)
	dryRun := fsx.Bool("dry-run", false, "solo muestra los docs por indexar, no llama a OpenAI")
	setUsage(fsx, "uso: finbox reembed [--dry-run]")
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
			if d.Stale(embed.Model) {
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
		if len(stale) == 0 { // the usual deploy: nothing to do, no key needed, no call
			fmt.Fprintln(stdout, "0 gastos indexados")
			return exitOK
		}
		if e.cfg.OpenAIKey == "" {
			cliErr(stderr, false, "falta OPENAI_API_KEY")
			return exitUsage
		}
		texts := make([]string, len(stale))
		for i, d := range stale {
			texts[i] = d.Doc
		}
		// ponytail: one call; the API takes 2048 inputs per request, chunk when the corpus passes that.
		vecs, err := (&embed.Client{APIKey: e.cfg.OpenAIKey}).Embed(e.ctx, texts)
		if err != nil {
			return mapErr(stderr, false, err)
		}
		for i, d := range stale {
			if err := e.st.UpsertEmbedding(e.ctx, d.ID, embed.Model, d.Hash, d.Doc, vecs[i]); err != nil {
				return mapErr(stderr, false, err)
			}
		}
		fmt.Fprintf(stdout, "%d gastos indexados\n", len(stale))
		return exitOK
	})
}
