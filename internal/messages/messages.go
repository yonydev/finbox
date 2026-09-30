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
	NotACommand         = "mándame la foto de un ticket 🧾 o regístralo a mano con /add (más en /help)"
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
	// names the /month bucket.
	CategoryLine = "categoría: %s"
	NoCategory   = "sin categoría"
	// CategorySuggested marks a category the extractor proposed, not the human.
	CategorySuggested = "sugerida"
	UnknownCategory   = "no conozco la categoría %q · elige una: %s"
	// CorrectionHelp carries its own HTML — send it raw, never escaped.
	CorrectionHelp = `no te entendí 🤔
<code>15/09</code> o <code>ayer</code> → fecha
<code>285.00</code> → total
<code>comercio Farmacia 24</code> → comercio
<code>categoria super</code> → categoría
varios de un jalón: <code>total 285 fecha 15/09</code>
¿es un gasto nuevo y no una corrección? → /add`
	// AddHelp carries its own HTML — send it raw, never escaped.
	AddHelp = `➕ <b>/add</b> registra un gasto sin ticket
<code>/add monto comercio</code> y, si quieres, <code>fecha …</code> <code>categoria …</code> <code>moneda …</code>

<code>/add 500 Limpieza Paty</code>
<code>/add 120 Uber fecha ayer</code>
<code>/add 350 Soriana categoria super</code>
<code>/add -120 Uber</code> → reembolso

· el monto va primero y es el único número
· fecha, categoría y moneda siempre con su palabra: <code>fecha ayer</code>, <code>categoria super</code>, <code>moneda USD</code>, en cualquier orden
· ¿el comercio lleva números o más de 4 palabras? escríbelo con <code>comercio</code>: <code>/add 24 comercio Farmacia 24</code>
· sin fecha es hoy, sin moneda es MXN`
	// The /add error headers; each is sent with AddHelp below it.
	AddNoAmount   = "falta el monto 🤔"
	AddNoMerchant = "falta el comercio 🤔"
	AddTwoNumbers = "veo dos números y solo uno puede ser el monto 🤔"
	AddUnparsed   = "no te entendí 🤔"
	// AddSavedHint goes under ✅ Guardado on manual rows only.
	AddSavedHint = "💡 ¿algo mal? respóndeme con el dato (ej. <code>categoria servicios</code>, <code>fecha ayer</code>, <code>450</code>) o toca ↩️ Deshacer"
	Undone       = "↩️ Deshecho"
	// UndoneCard is Undone · short id · comercio · monto.
	UndoneCard    = "%s · <code>%s</code> · %s · %s"
	AlreadyUndone = "ya estaba deshecho"
	// DuplicateWarning names the row it collided with: comercio · 02/01 · monto.
	DuplicateWarning = "⚠️ posible duplicado: %s · %s · %s"
	// SearchHelp carries its own HTML — send it raw, never escaped.
	SearchHelp = `🔎 <b>/search</b> busca gastos por comercio, categoría o lo que compraste
<code>/search pañales</code> · <code>/search tamales</code> · <code>/search estacionamiento</code>

· aguanta errores y pedazos de palabra: <code>farmasia</code> encuentra la farmacia, <code>uber</code> los viajes
· ¿gastos parecidos a uno que ya tienes? <code>/search parecido a a69931a9</code> (el id sale en /list)
· ¿quieres ver el ticket? toca el botón con su id debajo de la tabla y te lo reenvío
· por monto o fecha no busca: para eso /list y /month`
	// SearchHeader, SearchNothing: the query, escaped. SearchFooter: how many
	// rows came back — a count, never a sum.
	SearchHeader        = "🔎 «%s»"
	SearchFooter        = "parecidos: %d"
	SearchNothing       = "🔎 nada parecido a «%s» · prueba con el comercio o lo que compraste"
	SearchNoSuchExpense = "no encuentro el gasto <code>%s</code> · el id sale en /list"
	NoExpenses          = "sin gastos todavía"
	NothingPending      = "nada pendiente ✨"
	HelpText            = `🧾 <b>finbox</b>
<i>convierte tickets en gastos</i>

Mándame la <b>foto de un ticket</b> y te devuelvo el resumen para confirmar con un tap.

<b>Comandos</b>
─────────────
➕ /add <code>monto comercio</code>
      gasto sin ticket · <code>/add 500 Limpieza Paty</code> · más en /add
🔎 /search <code>palabras</code>
      gastos parecidos a lo que escribas · <code>/search pañales</code> · más en /search
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
	BtnUndo    = "↩️ Deshacer"
	BtnConfirm = "✅ Confirmar"
	BtnDiscard = "❌ Descartar"
	BtnRetry   = "🔄 Reintentar"
	BtnClose   = "❌ Cerrar"
	// ReceiptButton labels one /search row's ticket button: the short id.
	ReceiptButton = "🧾 %s"
	ReceiptGone   = "no encuentro el ticket original en el chat · si borraste la foto o el PDF, no lo puedo reenviar"
	ListClosed    = "🧾 lista cerrada"
	ListCapNote   = "máx. 50 — usa <code>finbox list</code> para más"
)

// CategoryLabels holds the es-MX names that differ from the slug; the rest
// render as the slug itself (category.Label).
var CategoryLabels = map[string]string{"super": "súper", "educacion": "educación"}
