package main

import (
	"fmt"
	"io"
	"os"
	_ "time/tzdata"
)

const version = "0.1.0-dev"

// exit codes per spec §6
const (
	exitOK       = 0
	exitRuntime  = 1
	exitUsage    = 2
	exitNotFound = 3
)

type subcommand struct {
	name, summary string
	run           func(argv []string, stdout, stderr io.Writer) int
}

func subcommands() []subcommand {
	return []subcommand{
		{"version", "print version", cmdVersion},
		{"help", "show help", cmdHelp},
		{"extract", "extrae un ticket local (Milestone 0)", cmdExtract},
		{"migrate", "aplica migraciones", cmdMigrate},
	}
}

func run(argv []string, stdout, stderr io.Writer) int {
	if len(argv) < 2 {
		printHelp(stderr)
		return exitUsage
	}
	name := argv[1]
	for _, sc := range subcommands() {
		if sc.name == name {
			return sc.run(argv[2:], stdout, stderr)
		}
	}
	fmt.Fprintf(stderr, "subcomando desconocido: %s\n", name)
	printHelp(stderr)
	return exitUsage
}

func cmdVersion(_ []string, stdout, _ io.Writer) int {
	fmt.Fprintf(stdout, "finbox %s\n", version)
	return exitOK
}

func cmdHelp(_ []string, stdout, _ io.Writer) int {
	printHelp(stdout)
	return exitOK
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "uso: finbox <subcomando> [flags]")
	fmt.Fprintln(w, "finbox — convierte tickets en gastos")
	for _, sc := range subcommands() {
		fmt.Fprintf(w, "  %-10s %s\n", sc.name, sc.summary)
	}
}

func main() {
	os.Exit(run(os.Args, os.Stdout, os.Stderr))
}
