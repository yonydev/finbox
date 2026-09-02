package telegram

import (
	"fmt"
	"html"
	"strings"
	"time"

	"finbox/internal/messages"
	"finbox/internal/money"
	"finbox/internal/store"
	"finbox/internal/validate"
)

const Budget = 3500
const maxItemsShown = 10

func Card(shortID string, v validate.Validated) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🧾 <code>%s</code> · <b>%s</b>\n", html.EscapeString(shortID), html.EscapeString(v.Merchant))
	fmt.Fprintf(&b, "📅 %s · 💰 %s %s\n", v.OccurredOn.Format("2006-01-02"),
		money.Format(v.AmountMinor, v.Currency), html.EscapeString(v.Currency))
	if len(v.Items) > 0 {
		b.WriteString("─────\n")
		for i, it := range v.Items {
			if i == maxItemsShown {
				fmt.Fprintf(&b, "… y %d más\n", len(v.Items)-maxItemsShown)
				break
			}
			line := html.EscapeString(it.Name)
			if it.AmountMinor != nil {
				line += " · " + money.Format(*it.AmountMinor, v.Currency)
			}
			b.WriteString(line + "\n")
		}
	}
	for _, w := range v.Warnings {
		b.WriteString(html.EscapeString(w) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func SavedCard(shortID string, v validate.Validated) string {
	return Card(shortID, v) + "\n\n" + messages.Saved
}

func DiscardedCard(shortID string) string {
	return fmt.Sprintf("%s · <code>%s</code>", messages.Discarded, html.EscapeString(shortID))
}

func FailedCard(shortID, failReason string) string {
	return fmt.Sprintf("⚠️ <code>%s</code> · %s", html.EscapeString(shortID), html.EscapeString(failReason))
}

func ListLines(rows []store.TxnRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, fmt.Sprintf("<code>%s</code> · %s · %s · %s",
			html.EscapeString(r.ShortID), r.OccurredOn.Format("02/01"),
			html.EscapeString(r.Merchant), money.Format(r.AmountMinor, r.Currency)))
	}
	return out
}

func MonthSummary(year int, month time.Month, totals []store.CurrencyTotal, count int) string {
	if count == 0 {
		return fmt.Sprintf("%04d-%02d: sin gastos", year, int(month))
	}
	parts := make([]string, 0, len(totals))
	for _, t := range totals {
		parts = append(parts, fmt.Sprintf("%s %s", money.Format(t.AmountMinor, t.Currency), html.EscapeString(t.Currency)))
	}
	return fmt.Sprintf("<b>%04d-%02d</b>: %s · %d gastos", year, int(month), strings.Join(parts, " + "), count)
}

// Chunk packs lines into messages of at most budget chars.
func Chunk(lines []string, budget int) []string {
	var out []string
	var cur strings.Builder
	for _, l := range lines {
		if cur.Len() > 0 && cur.Len()+1+len(l) > budget {
			out = append(out, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteByte('\n')
		}
		cur.WriteString(l)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
