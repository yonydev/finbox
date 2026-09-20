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

func Card(shortID string, v validate.Validated, edited bool) string {
	var b strings.Builder
	mark := "" // trailing, like ListTable: the emoji's odd width stays out of the layout
	if edited {
		mark = " ✏️"
	}
	fmt.Fprintf(&b, "🧾 <code>%s</code> · <b>%s</b>%s\n", html.EscapeString(shortID), html.EscapeString(v.Merchant), mark)
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

// PendingCard is the confirm-or-correct card; the hint lives here and not in
// Card, which SavedCard also renders.
func PendingCard(shortID string, v validate.Validated, edited bool) string {
	return Card(shortID, v, edited) + "\n\n" + messages.ReplyHint
}

func SavedCard(shortID string, v validate.Validated, edited bool) string {
	return Card(shortID, v, edited) + "\n\n" + messages.Saved
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

// ListTable renders rows as a monospace table — Telegram HTML has no <table>,
// so <pre> with padded columns is the closest thing. Returns one ready-to-send
// message, "" when there are no rows.
// Single message: the 50-row cap keeps the table ≈1.8KB, half the 3.5KB
// Budget; resurrect Chunk()-based splitting if the cap ever grows past ~90.
func ListTable(rows []store.TxnRow) string {
	if len(rows) == 0 {
		return ""
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
	edited := 0
	for i, r := range rows {
		if _, seen := totals[r.Currency]; !seen {
			currencies = append(currencies, r.Currency)
		}
		totals[r.Currency] += r.AmountMinor
		line := fmt.Sprintf("%-8s  %-5s  %*s  %s",
			r.ShortID, r.OccurredOn.Format("02/01"), amtW, amounts[i],
			validate.CapRunes(r.Merchant, merchW))
		if r.Edited { // trailing so the emoji's odd width can't break column alignment
			line += " ✏️"
			edited++
		}
		lines = append(lines, line)
	}
	parts := make([]string, 0, len(currencies))
	for _, c := range currencies {
		parts = append(parts, fmt.Sprintf("%s %s", money.Format(totals[c], c), c))
	}
	footer := strings.Repeat("─", listTableWidth) + "\n" +
		fmt.Sprintf("TOTAL %s · %d", strings.Join(parts, " + "), len(rows))
	if edited > 0 { // the window's own metric, visible day to day
		footer += fmt.Sprintf(" · ✏️ %d (%d%%)", edited, edited*100/len(rows))
	}

	body := header + "\n" + strings.Join(lines, "\n") + "\n" + footer
	return "<pre>" + html.EscapeString(body) + "</pre>"
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
