// Package category holds the fixed expense taxonomy. Slugs are ASCII and
// stored as-is; the accented es-MX labels live in messages and are only used
// when rendering.
package category

import (
	"slices"
	"strings"

	"finbox/internal/messages"
)

var Slugs = []string{"super", "restaurantes", "hogar", "servicios", "transporte",
	"salud", "educacion", "entretenimiento", "ropa", "otros"}

var fold = strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u")

// Parse maps user input ("Educación", "SÚPER") to a slug; ok is false for
// anything off the list, and slug is then empty.
func Parse(s string) (string, bool) {
	slug := fold.Replace(strings.ToLower(strings.TrimSpace(s)))
	if !slices.Contains(Slugs, slug) {
		return "", false
	}
	return slug, true
}

// Label returns the es-MX name for a slug (the slug itself when they match).
func Label(slug string) string {
	if l := messages.CategoryLabels[slug]; l != "" {
		return l
	}
	return slug
}
