package category

import "testing"

func TestParseFoldsAndLabels(t *testing.T) {
	for in, want := range map[string]string{
		"super": "super", "SÚPER": "super", " Educación ": "educacion", "Otros": "otros",
	} {
		got, ok := Parse(in)
		if !ok || got != want {
			t.Errorf("Parse(%q) = %q %v, want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "comida", "hijos", "super market"} {
		if got, ok := Parse(in); ok {
			t.Errorf("Parse(%q) = %q, want rejected", in, got)
		}
	}
	// every slug has a label, and a label parses back to its slug
	for _, s := range Slugs {
		back, ok := Parse(Label(s))
		if !ok || back != s {
			t.Errorf("Label(%q) = %q → %q %v", s, Label(s), back, ok)
		}
	}
}
