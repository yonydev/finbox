// Package messages holds every user-facing string (es-MX). One place,
// so future i18n is "add a second map".
package messages

const (
	TooBig              = "imagen demasiado grande (máx. 20 MB)"
	UnsupportedFormat   = "formato no soportado 📸 — solo JPEG/PNG/WebP/PDF (HEIC no: en iPhone usa Ajustes → Cámara → Formatos → Más compatible)"
	Reading             = "🧾 Leyendo tu ticket…"
	Saved               = "✅ Guardado"
	Discarded           = "❌ Descartado"
	AlreadySaved        = "ya estaba guardado"
	AlreadyProcessed    = "ya procesé este ticket ✅"
	AwaitingYourConfirm = "este ticket está esperando tu confirmación"
	NotACommand         = "mándame una foto de un ticket 🧾 (o usa /help)"
	SomethingWrong      = "algo salió mal, revisa los logs"
	DownloadFailed      = "no pude descargar el archivo, reintenta"
	ReceiptNotFound     = "recibo no encontrado"
	ReceiptStillReading = "sigo leyendo el ticket, espera la tarjeta"
	ReceiptInactive     = "este recibo no está activo · usa 🔄 Reintentar"
	TotalMustBePositive = "el total del ticket debe ser positivo"
	ReplyHint           = "💡 ¿algo mal? respóndeme con el dato correcto (ej. <code>15/09</code> o <code>comercio Oxxo</code>)"
	// OnTicket labels the raw receipt name under a card title that was normalized.
	OnTicket = "en el ticket: %s"
	// CorrectionHelp carries its own HTML — send it raw, never escaped.
	CorrectionHelp = `no te entendí 🤔
<code>15/09</code> (o <code>ayer</code>) → fecha · <code>285.00</code> → total · <code>comercio Farmacia 24</code> → comercio
y varios de un jalón: <code>total 285 fecha 15/09</code>`
	NoExpenses     = "sin gastos todavía"
	NothingPending = "nada pendiente ✨"
	HelpText       = `🧾 <b>finbox</b>
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
✏️ ¿algo salió mal? responde a la tarjeta con el dato correcto (fecha, total o comercio)
📸 JPEG, PNG, WebP o PDF · máx. 20 MB
🍏 Si tu iPhone los guarda como HEIC y quieres mandarlos como archivo, un camino es Ajustes → Cámara → Formatos → <i>Más compatible</i>`
	BtnConfirm  = "✅ Confirmar"
	BtnDiscard  = "❌ Descartar"
	BtnRetry    = "🔄 Reintentar"
	BtnClose    = "❌ Cerrar"
	ListClosed  = "🧾 lista cerrada"
	ListCapNote = "máx. 50 — usa <code>finbox list</code> para más"
)
