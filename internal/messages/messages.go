// Package messages holds every user-facing string (es-MX). One place,
// so future i18n is "add a second map" (spec §7).
package messages

const (
	TooBig            = "imagen demasiado grande (máx. 20 MB)"
	UnsupportedFormat = "formato no soportado 📸 — envíalo como foto (JPEG/PNG/WebP)"
	Reading           = "🧾 Leyendo tu ticket…"
	Saved             = "✅ Guardado"
	Discarded         = "❌ Descartado"
	AlreadySaved      = "ya estaba guardado"
	AlreadyProcessed  = "ya procesé este ticket ✅"
	NotACommand       = "mándame una foto de un ticket 🧾 (o usa /help)"
	EditComingSoon    = "la edición desde el chat llega pronto; usa <code>finbox edit %s</code>"
	SomethingWrong    = "algo salió mal, revisa los logs"
	DownloadFailed    = "no pude descargar la foto, reintenta"
	ReceiptNotFound   = "recibo no encontrado"
	NoExpenses        = "sin gastos todavía"
	NothingPending    = "nada pendiente ✨"
	HelpText          = `<b>finbox</b> — convierte tickets en gastos

Mándame una <b>foto de un ticket</b> y te muestro el resumen para confirmar.

/list [N] — últimos N gastos (máx. 50)
/month [mes] — total del mes (ej. /month aug, /month ago, /month 2026-01)
/pending — recibos pendientes o fallidos
/help — esta ayuda

Formatos: JPEG, PNG, WebP (como foto, no como archivo). Máx. 20 MB.`
	BtnConfirm  = "✅ Confirmar"
	BtnDiscard  = "❌ Descartar"
	BtnRetry    = "🔄 Reintentar"
	ListCapNote = "máx. 50 — usa <code>finbox list</code> para más"
)
