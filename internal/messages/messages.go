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
	ReceiptNotFound     = "ticket no encontrado"
	ReceiptStillReading = "sigo leyendo el ticket, espera la tarjeta"
	ReceiptInactive     = "este ticket no está activo · usa 🔄 Reintentar"
	TotalMustBePositive = "el total del ticket debe ser positivo"
	ReplyHint           = "💡 ¿algo mal o falta la categoría? respóndeme con el dato (ej. <code>15/09</code>, <code>comercio Oxxo</code>, <code>categoria super</code>)"
	// OnTicket labels the raw receipt name under a card title that was normalized.
	OnTicket = "en el ticket: %s"
	// CategoryLine is the 🏷 card line; NoCategory fills it when unset and
	// names the /month bucket. No provenance suffix today: the human is the
	// only writer. When the extractor proposes one, that case gets "· sugerida".
	CategoryLine    = "categoría: %s"
	NoCategory      = "sin categoría"
	UnknownCategory = "no conozco la categoría %q · elige una: %s"
	// CorrectionHelp carries its own HTML — send it raw, never escaped.
	CorrectionHelp = `no te entendí 🤔
<code>15/09</code> o <code>ayer</code> → fecha
<code>285.00</code> → total
<code>comercio Farmacia 24</code> → comercio
<code>categoria super</code> → categoría
varios de un jalón: <code>total 285 fecha 15/09</code>`
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
      total del mes por categoría · <code>aug</code>, <code>ago</code> o <code>2026-01</code>
⏳ /pending
      tickets pendientes o fallidos
❓ /help
      esta ayuda

<b>Tips</b>
─────────────
📎 los tickets largos se leen mejor como <b>archivo</b>: como foto, Telegram los comprime y algunos datos pueden salir mal.
✏️ ¿algo mal o falta la categoría? responde a la tarjeta con el dato (ej. <code>15/09</code>, <code>comercio Oxxo</code>, <code>categoria super</code>)
🏷 categorías: super, restaurantes, hogar, servicios, transporte, salud, educacion, entretenimiento, ropa, otros
📸 JPEG, PNG, WebP o PDF · máx. 20 MB
🍏 si tu iPhone los guarda como HEIC y quieres mandarlos como archivo, un camino es Ajustes → Cámara → Formatos → <i>Más compatible</i>`
	BtnConfirm  = "✅ Confirmar"
	BtnDiscard  = "❌ Descartar"
	BtnRetry    = "🔄 Reintentar"
	BtnClose    = "❌ Cerrar"
	ListClosed  = "🧾 lista cerrada"
	ListCapNote = "máx. 50 — usa <code>finbox list</code> para más"
)

// CategoryLabels holds the es-MX names that differ from the slug; the rest
// render as the slug itself (category.Label).
var CategoryLabels = map[string]string{"super": "súper", "educacion": "educación"}
