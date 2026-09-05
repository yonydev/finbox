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

// listTableWidth is the widest line a phone-sized Telegram window shows
// without wrapping <pre> content; the merchant column absorbs the slack.
const listTableWidth = 34

// ListTable renders rows as monospace tables — Telegram HTML has no <table>,
// so <pre> with padded columns is the closest thing. Each returned string is
// one ready-to-send message; the totals footer goes on the last one.
func ListTable(rows []store.TxnRow) []string {
	if len(rows) == 0 {
		return nil
	}
	amtW := len("MONTO")
	amounts := make([]string, len(rows))
	for i, r := range rows {
		amounts[i] = money.Format(r.AmountMinor, r.Currency)
		if len(amounts[i]) > amtW {
			amtW = len(amounts[i])
		}
	}
	merchW := listTableWidth - 8 - 5 - amtW - 6 // id, fecha, monto + three 2-space gaps
	if merchW < 4 {
		merchW = 4
	}
	header := fmt.Sprintf("%-8s  %-5s  %*s  %s", "ID", "FECHA", amtW, "MONTO", "COMERCIO")
	lines := make([]string, 0, len(rows))
	totals := map[string]int64{}
	var currencies []string // order of first appearance
	for i, r := range rows {
		if _, seen := totals[r.Currency]; !seen {
			currencies = append(currencies, r.Currency)
		}
		totals[r.Currency] += r.AmountMinor
		lines = append(lines, fmt.Sprintf("%-8s  %-5s  %*s  %s",
			r.ShortID, r.OccurredOn.Format("02/01"), amtW, amounts[i],
			truncateRunes(r.Merchant, merchW)))
	}
	parts := make([]string, 0, len(currencies))
	for _, c := range currencies {
		parts = append(parts, fmt.Sprintf("%s %s", money.Format(totals[c], c), c))
	}
	footer := strings.Repeat("─", listTableWidth) + "\n" +
		fmt.Sprintf("TOTAL %s · %d", strings.Join(parts, " + "), len(rows))

	overhead := len("<pre></pre>") + len(header) + len(footer) + 2
	out := make([]string, 0, 1)
	chunks := Chunk(lines, Budget-overhead)
	for i, ch := range chunks {
		body := header + "\n" + ch
		if i == len(chunks)-1 {
			body += "\n" + footer
		}
		out = append(out, "<pre>"+html.EscapeString(body)+"</pre>")
	}
	return out
}

// truncateRunes caps s at max runes, marking the cut with an ellipsis.
// Rune-based: merchants carry accents, and a byte cut could split one.
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
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
