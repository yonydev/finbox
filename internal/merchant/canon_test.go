package merchant

import "testing"

func TestCanon(t *testing.T) {
	cases := map[string]string{
		"OXXO S.A. DE C.V.":             "OXXO",
		"FARMACIAS DEL AHORRO SA DE CV": "FARMACIAS DEL AHORRO",
		"Bodega Aurrera, S de RL de CV": "Bodega Aurrera",
		"Grupo Comercial SAPI DE C.V.":  "Grupo Comercial",
		"CLIP*Tacos El Güero":           "Tacos El Güero",
		"MERCADO PAGO - Cafetería":      "Cafetería",
		"SANTANDER: Gasolinera 7":       "Gasolinera 7",
		"CLIP":                          "CLIP",         // processor alone keeps its name
		"S.A. DE C.V.":                  "S.A. DE C.V.", // nothing but a corporate form: too few letters left
		"  Tacos   El   Güero ":         "Tacos El Güero",
		"Farmacia 24":                   "Farmacia 24",
		"OXXO":                          "OXXO",
	}
	for raw, want := range cases {
		if got := Canon(raw); got != want {
			t.Errorf("Canon(%q) = %q, want %q", raw, got, want)
		}
	}
}

// A canon is stable: running it again changes nothing, so `finbox rerule` is
// idempotent and a rename never has to be repeated.
func TestCanonIsIdempotent(t *testing.T) {
	for _, raw := range []string{"OXXO S.A. DE C.V.", "CLIP*Tacos El Güero", "CLIP", "Farmacia 24"} {
		once := Canon(raw)
		if twice := Canon(once); twice != once {
			t.Errorf("Canon(Canon(%q)) = %q, want %q", raw, twice, once)
		}
	}
}
