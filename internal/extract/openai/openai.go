package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

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
- Si la imagen es un screenshot de un cargo bancario sin items, devuelve items: [].
- NUNCA transcribas números de tarjeta, cuenta o CLABE.
- No inventes valores: campo ilegible = "" u omitido.`

type Option func(*Extractor)

func WithBaseURL(u string) Option { return func(e *Extractor) { e.baseURL = u } }

type Extractor struct {
	apiKey, model, baseURL string
}

func New(apiKey, model string, opts ...Option) *Extractor {
	e := &Extractor{apiKey: apiKey, model: model}
	for _, o := range opts {
		o(e)
	}
	return e
}

func (e *Extractor) client() oa.Client {
	opts := []option.RequestOption{option.WithAPIKey(e.apiKey)}
	if e.baseURL != "" {
		opts = append(opts, option.WithBaseURL(e.baseURL))
	}
	return oa.NewClient(opts...)
}

func (e *Extractor) Extract(ctx context.Context, image []byte, mime string) (extract.Result, error) {
	dataURL := fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(image))
	client := e.client()
	resp, err := client.Chat.Completions.New(ctx, oa.ChatCompletionNewParams{
		Model: oa.ChatModel(e.model), // ChatModel is a defined string type; plain string needs the conversion
		Messages: []oa.ChatCompletionMessageParamUnion{
			oa.SystemMessage(systemPrompt),
			oa.UserMessage([]oa.ChatCompletionContentPartUnionParam{
				oa.ImageContentPart(oa.ChatCompletionContentPartImageImageURLParam{URL: dataURL}),
				oa.TextContentPart("Extrae este ticket."),
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
