// Package merchant derives the canonical merchant name — what the user sees on
// every card and list — from the raw text a receipt carries. Pure function of
// the raw string: it only removes noise (spacing, the payment processor's tag,
// the corporate form), never re-cases or invents a token, so the same raw
// always collapses to the same canon and a rename stays a one-time job.
package merchant

import (
	"regexp"
	"strings"
	"unicode"
)

// processorTag is the acquirer/bank label some terminals print before the real
// name ("CLIP*Tacos El Güero"). A separator is required, so a receipt whose
// whole merchant IS the processor keeps its name.
var processorTag = regexp.MustCompile(`(?i)^(?:clip|multiva|banorte|bbva|santander|mercado pago)\b[\s*·:\-–]+`)

var spaces = regexp.MustCompile(`\s+`)

// legalTail holds the tokens (dots dropped) a Mexican corporate form is built
// from: "S.A. DE C.V.", "S DE RL DE CV", "SAPI DE CV".
var legalTail = map[string]bool{
	"s": true, "a": true, "sa": true, "sab": true, "sapi": true,
	"de": true, "c": true, "v": true, "cv": true,
	"r": true, "l": true, "rl": true, "srl": true,
}

// Canon returns the canonical name for raw. It falls back to raw whenever the
// rules would leave fewer than three letters: a canon that lost the name is
// worse than no canon at all ("CLIP" stays "CLIP").
func Canon(raw string) string {
	collapsed := spaces.ReplaceAllString(strings.TrimSpace(raw), " ")
	fields := strings.Fields(processorTag.ReplaceAllString(collapsed, ""))
	for len(fields) > 0 && legalTail[strings.ToLower(strings.ReplaceAll(fields[len(fields)-1], ".", ""))] {
		fields = fields[:len(fields)-1]
	}
	out := strings.TrimRight(strings.Join(fields, " "), " ,.;-")
	if letters(out) < 3 {
		return collapsed
	}
	return out
}

func letters(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			n++
		}
	}
	return n
}
