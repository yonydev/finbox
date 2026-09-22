package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	oa "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"finbox/internal/extract"
)

// ErrNonRetryable re-exports the extract-package sentinel so this package's
// tests read naturally; the canonical definition lives in internal/extract.
var ErrNonRetryable = extract.ErrNonRetryable

const systemPrompt = `Eres un extractor de tickets de compra mexicanos.
Devuelve SOLO un JSON con: merchant (string), date (YYYY-MM-DD), currency (ISO 4217, "" si no es legible),
total (string decimal, ej. "364.00"), items (array de {name, quantity, amount}).
- amount de cada item es el TOTAL DE LA LÍNEA como string decimal; omítelo si el precio no es legible.
- Cargos que el ticket cobra aparte de los productos (envío, propina, descuento) van como items para que los items sumen el total; descuentos con signo negativo ("-50.00"). No agregues IVA/impuestos como item cuando ya están incluidos en los precios de línea.
- Si la imagen es un screenshot de un cargo bancario sin items, devuelve items: [].
- NUNCA transcribas números de tarjeta, cuenta o CLABE.
- No inventes valores: campo ilegible = "" u omitido.
Fecha (el mensaje del usuario dice la fecha de hoy):
- Es la fecha de la compra o del pago; no vencimiento, entrega ni vigencia.
- Las fechas van en DD/MM/AA o DD/MM/AAAA salvo que el ticket indique otro formato. Léela como MM/DD si DD/MM es imposible o posterior a hoy.
- Año de dos dígitos AA = 20AA (26 → 2026). Sin año impreso: el de hoy, o el anterior si quedaría en el futuro. La fecha nunca es posterior a hoy.
- Sin ninguna fecha legible: date = "".
Total:
- total es la línea TOTAL: lo que pagó el cliente por toda la compra. No es una forma de pago (TARJETA, DÉBITO, EFECTIVO, CAMBIO) ni un IMPORTE parcial: TOTAL 2,601.00 pagado con dos tarjetas → total 2601.00.
- Si hay PROPINA: total es el Total impreso que ya la incluye (Monto 806.00 + Propina 80.60 → Total 886.60). Si solo hay Total y Propina por separado, total es ese Total; nunca sumes.
- Voucher de terminal bancaria con una sola cantidad (Total M.N., Importe): esa es el total.`

type Extractor struct {
	apiKey, model, baseURL string
}

func New(apiKey, model string) *Extractor {
	return &Extractor{apiKey: apiKey, model: model}
}

func (e *Extractor) client() oa.Client {
	// The pipeline already retries 3×; the SDK's default MaxRetries(2) would
	// multiply that to 9 HTTP attempts per receipt.
	opts := []option.RequestOption{option.WithAPIKey(e.apiKey), option.WithMaxRetries(0)}
	if e.baseURL != "" {
		opts = append(opts, option.WithBaseURL(e.baseURL))
	}
	return oa.NewClient(opts...)
}

// today is the reference date the prompt's date rules lean on (upload day in
// prod, blob mtime in the corpus): it disambiguates DD/MM vs MM/DD and 2-digit years.
func (e *Extractor) Extract(ctx context.Context, image []byte, mime string, today time.Time) (extract.Result, error) {
	// The bot's poll loop is sequential with no deadline of its own; without
	// this bound one slow request blocks confirmations and commands.
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	dataURL := fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(image))
	part := oa.ImageContentPart(oa.ChatCompletionContentPartImageImageURLParam{URL: dataURL})
	if mime == "application/pdf" {
		// Chat Completions takes PDFs as a `file` part: the API extracts the text and
		// renders each page as an image, so the same prompt works unchanged.
		part = oa.FileContentPart(oa.ChatCompletionContentPartFileFileParam{
			FileData: oa.String(dataURL), Filename: oa.String("recibo.pdf"),
		})
	}
	client := e.client()
	resp, err := client.Chat.Completions.New(ctx, oa.ChatCompletionNewParams{
		Model: oa.ChatModel(e.model), // ChatModel is a defined string type; plain string needs the conversion
		Messages: []oa.ChatCompletionMessageParamUnion{
			oa.SystemMessage(systemPrompt),
			oa.UserMessage([]oa.ChatCompletionContentPartUnionParam{
				part,
				oa.TextContentPart("Hoy es " + today.Format("2006-01-02") + ". Extrae este ticket."),
			}),
		},
		ResponseFormat: oa.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &oa.ResponseFormatJSONObjectParam{},
		},
	})
	if err != nil {
		var apiErr *oa.Error
		if errors.As(err, &apiErr) {
			// 4xx (other than 408 timeout / 429 rate-limit-but-maybe-retryable) is
			// permanent: bad key, bad request, exhausted quota. 429 from OpenAI covers
			// both true rate limits and hard quota exhaustion; either way retries
			// within a single interactive `extract` run won't help, so treat it as
			// non-retryable too.
			if apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 && apiErr.StatusCode != 408 {
				return extract.Result{}, fmt.Errorf("%w: %v", ErrNonRetryable, err)
			}
		}
		return extract.Result{}, err
	}
	if len(resp.Choices) == 0 {
		return extract.Result{}, fmt.Errorf("openai: empty choices")
	}
	raw := []byte(resp.Choices[0].Message.Content)
	var ex extract.Extraction
	if err := json.Unmarshal(raw, &ex); err != nil {
		return extract.Result{}, fmt.Errorf("openai: malformed extraction JSON: %w", err)
	}
	return extract.Result{
		Extraction:       ex,
		Model:            resp.Model,
		RawJSON:          raw,
		PromptTokens:     int(resp.Usage.PromptTokens),
		CompletionTokens: int(resp.Usage.CompletionTokens),
	}, nil
}
