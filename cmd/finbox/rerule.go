package main

import (
	"flag"
	"fmt"
	"io"

	"finbox/internal/merchant"
)

// cmdRerule recomputes the canonical merchant name of every transaction the
// user has never renamed. Idempotent, logs no edits: run it right after
// migrate, and again after tuning the normalizer (--dry-run first to read the
// diff).
func cmdRerule(argv []string, stdout, stderr io.Writer) int {
	fsx := flag.NewFlagSet("rerule", flag.ContinueOnError)
	dryRun := fsx.Bool("dry-run", false, "solo muestra los cambios, no escribe")
	setUsage(fsx, "uso: finbox rerule [--dry-run]")
	if ok, code := parseFlags(fsx, argv, stdout, stderr); !ok {
		return code
	}
	return withStore(stderr, false, func(e cliEnv) int {
		changes, err := e.st.RecanonTransactions(e.ctx, merchant.Canon, *dryRun)
		if err != nil {
			return mapErr(stderr, false, err)
		}
		for _, c := range changes {
			fmt.Fprintf(stdout, "%s · %s → %s\n", c[0][:8], c[1], c[2])
		}
		suffix := ""
		if *dryRun {
			suffix = " (dry-run, sin escribir)"
		}
		fmt.Fprintf(stdout, "%d comercios recalculados%s\n", len(changes), suffix)
		return exitOK
	})
}
