package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"finbox/internal/config"
	"finbox/internal/extract/openai"
	"finbox/internal/imgtype"
)

func cmdExtract(argv []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("extract", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "salida JSON")
	today := fs.String("today", time.Now().Format("2006-01-02"), "fecha de referencia YYYY-MM-DD (el día en que se tomó la foto)")
	const usage = "uso: finbox extract <imagen> [--json] [--today YYYY-MM-DD]"
	setUsage(fs, usage)
	// stdlib flag stops parsing at the first positional arg, so pop the
	// path FIRST — otherwise `finbox extract foto.jpg --json` never sees --json.
	path, argv := popID(argv)
	if ok, code := parseFlags(fs, argv, stdout, stderr); !ok {
		return code
	}
	if path == "" {
		fmt.Fprintln(stderr, usage)
		return exitUsage
	}
	ref, err := time.Parse("2006-01-02", *today)
	if err != nil {
		fmt.Fprintln(stderr, "--today debe ser YYYY-MM-DD")
		return exitUsage
	}
	img, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "no pude leer el archivo: %v\n", err)
		return exitNotFound
	}
	ty, ok := imgtype.Sniff(img)
	if !ok {
		fmt.Fprintln(stderr, "formato no soportado (se acepta JPEG, PNG, WebP, PDF)")
		return exitUsage
	}
	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return exitUsage
	}
	if cfg.OpenAIKey == "" {
		fmt.Fprintln(stderr, "falta OPENAI_API_KEY en el entorno")
		return exitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := openai.New(cfg.OpenAIKey, cfg.OpenAIModel).Extract(ctx, img, ty.MIME(), ref)
	if err != nil {
		fmt.Fprintf(stderr, "extracción falló: %v\n", err)
		return exitRuntime
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.Encode(res.Extraction)
	} else {
		fmt.Fprintf(stdout, "%s · %s · %s %s · %d items (tokens: %d+%d)\n",
			res.Extraction.Merchant, res.Extraction.Date, res.Extraction.Total,
			res.Extraction.Currency, len(res.Extraction.Items),
			res.PromptTokens, res.CompletionTokens)
	}
	return exitOK
}
