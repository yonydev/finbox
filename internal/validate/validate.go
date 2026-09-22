package validate

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"finbox/internal/extract"
	"finbox/internal/merchant"
	"finbox/internal/money"
)

type Item struct {
	Position      int
	Name          string
	QuantityMilli *int64
	AmountMinor   *int64
}

type Validated struct {
	Merchant string // raw receipt text, scrubbed and capped; never shown by default
	// MerchantCanon is the name the user sees; equal to Merchant unless the
	// normalizer or a correction changed it.
	MerchantCanon string
	OccurredOn    time.Time
	Currency      string
	AmountMinor   int64
	Items         []Item
	Warnings      []string
}

// ItemsWarnPrefix starts the items-vs-total warning; callers that trust the
// total (a human typed it) drop warnings with this prefix.
const ItemsWarnPrefix = "⚠️ los items suman"

// digit runs possibly separated by single spaces/dashes
var digitRun = regexp.MustCompile(`\d(?:[ -]?\d)+`)

func luhn(digits string) bool {
	sum, alt := 0, false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if alt {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		alt = !alt
	}
	return sum%10 == 0
}

// Scrub redacts 13–19 digit Luhn-passing runs (cards) and any 18-digit run
// (CLABE) from free text. Spec §4.
func Scrub(s string) string {
	return digitRun.ReplaceAllStringFunc(s, func(run string) string {
		digits := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, run)
		n := len(digits)
		if n == 18 {
			return "[redactado]"
		}
		if n >= 13 && n <= 19 && luhn(digits) {
			return "[redactado]"
		}
		return run
	})
}

// CapRunes caps s at max runes (rune-based: accents must not be split).
// Card text fields must stay bounded: unbounded model output can push the
// Telegram message past 4096 chars, the edit fails (only logged), and the
// receipt strands in awaiting_confirm with no buttons.
func CapRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// ScrubMerchant scrubs, trims and caps a merchant string. One definition for both
// sides of a merchant diff (Run and pipeline.EditsFromRaw), so a canon edit is
// never logged as a phantom change.
func ScrubMerchant(s string) string { return CapRunes(strings.TrimSpace(Scrub(s)), 120) }

func Run(ex extract.Extraction, now time.Time, loc *time.Location) (Validated, error) {
	v := Validated{Merchant: ScrubMerchant(ex.Merchant)}
	if v.Merchant == "" {
		return v, fmt.Errorf("comercio ilegible")
	}
	v.MerchantCanon = merchant.Canon(v.Merchant)
	if c := ScrubMerchant(ex.MerchantCanon); c != "" { // a correction outranks the normalizer
		v.MerchantCanon = c
	}
	v.Currency = strings.ToUpper(strings.TrimSpace(ex.Currency))
	if v.Currency == "" {
		v.Currency = "MXN"
		v.Warnings = append(v.Warnings, "⚠️ moneda ilegible — asumí MXN")
	} else if !money.Known(v.Currency) {
		// The DB check (^[A-Z]{3}$) would reject e.g. "MX$" at confirm time,
		// where the bot swallows the error — the receipt would be stuck
		// unconfirmable forever. Fall back like the empty case instead.
		v.Warnings = append(v.Warnings, fmt.Sprintf("⚠️ moneda no reconocida (%s) — asumí MXN", CapRunes(v.Currency, 12)))
		v.Currency = "MXN"
	}
	total, err := money.ParseMinor(ex.Total, v.Currency)
	if err != nil {
		return v, fmt.Errorf("total ilegible: %w", err)
	}
	if total <= 0 {
		return v, fmt.Errorf("el total debe ser positivo (Phase 1)")
	}
	v.AmountMinor = total
	day, err := time.ParseInLocation("2006-01-02", ex.Date, loc)
	if err != nil {
		return v, fmt.Errorf("fecha ilegible: %q", ex.Date)
	}
	v.OccurredOn = day
	if day.After(now.In(loc)) {
		v.Warnings = append(v.Warnings, "⚠️ fecha futura — revisa la fecha")
	}
	allPriced := true
	var sum int64
	for i, it := range ex.Items {
		item := Item{Position: i + 1, Name: CapRunes(strings.TrimSpace(Scrub(it.Name)), 80)}
		if q := strings.TrimSpace(it.Quantity); q != "" {
			if qm, err := money.ParseMilli(q); err == nil {
				item.QuantityMilli = &qm
			}
		}
		if a := strings.TrimSpace(it.Amount); a != "" {
			am, err := money.ParseMinor(a, v.Currency)
			if err == nil && am != 0 { // negative = discount/credit line, counts toward the sum
				item.AmountMinor = &am
				sum += am
			} else {
				allPriced = false
			}
		} else {
			allPriced = false
		}
		v.Items = append(v.Items, item)
	}
	if len(v.Items) > 0 && allPriced && sum != total {
		v.Warnings = append(v.Warnings, fmt.Sprintf(ItemsWarnPrefix+" %s, el total dice %s",
			money.Format(sum, v.Currency), money.Format(total, v.Currency)))
	}
	return v, nil
}
