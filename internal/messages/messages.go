// Package messages holds every user-facing string (es-MX). One place,
// so future i18n is "add a second map".
package messages

const (
	TooBig              = "imagen demasiado grande (máx. 20 MB)"
	UnsupportedFormat   = "formato no soportado 📸 — solo JPEG/PNG/WebP (HEIC no: en iPhone usa Ajustes → Cámara → Formatos → Más compatible)"
	Reading             = "🧾 Leyendo tu ticket…"
	Saved               = "✅ Guardado"
	Discarded           = "❌ Descartado"
	AlreadySaved        = "ya estaba guardado"
	AlreadyProcessed    = "ya procesé este ticket ✅"
	AwaitingYourConfirm = "este ticket está esperando tu confirmación"
	NotACommand         = "mándame una foto de un ticket 🧾 (o usa /help)"
	EditComingSoon      = "la edición desde el chat llega pronto; usa <code>finbox edit %s</code>"
	SomethingWrong      = "algo salió mal, revisa los logs"
	DownloadFailed      = "no pude descargar la foto, reintenta"
	ReceiptNotFound     = "recibo no encontrado"
	ReceiptStillReading = "sigo leyendo el ticket, espera la tarjeta"
	ReceiptInactive     = "este recibo no está activo · usa 🔄 Reintentar"
	TotalMustBePositive = "el total del ticket debe ser positivo"
	NoExpenses          = "sin gastos todavía"
	NothingPending      = "nada pendiente ✨"
	HelpText            = `🧾 <b>finbox</b>
<i>convierte tickets en gastos</i>

Mándame la <b>foto de un ticket</b> y te devuelvo el resumen para confirmar con un tap.

<b>Comandos</b>
─────────────
📋 /list <code>N</code>
      últimos N gastos · default 10, máx. 50
📆 /month <code>mes</code>
      total del mes · <code>aug</code>, <code>ago</code> o <code>2026-01</code>
⏳ /pending
      recibos pendientes o fallidos
❓ /help
      esta ayuda

<b>Tips</b>
─────────────
📎 Los tickets largos se leen mejor como <b>archivo</b>: como foto, Telegram los comprime y algunos datos pueden salir mal.
📸 JPEG, PNG o WebP · máx. 20 MB
🍏 Si tu iPhone los guarda como HEIC y quieres mandarlos como archivo, un camino es Ajustes → Cámara → Formatos → <i>Más compatible</i>`
	BtnConfirm  = "✅ Confirmar"
	BtnDiscard  = "❌ Descartar"
	BtnRetry    = "🔄 Reintentar"
	BtnClose    = "❌ Cerrar"
	ListClosed  = "🧾 lista cerrada"
	ListCapNote = "máx. 50 — usa <code>finbox list</code> para más"
)
