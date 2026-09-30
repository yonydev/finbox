// Package embed wraps the OpenAI embeddings endpoint and the one chat call
// that writes an expense's search line. The name shadows the stdlib embed
// package: never import both in one file.
package embed

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	oa "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

const (
	Model = "text-embedding-3-small"
	Dims  = 512
)

// Client is nil-able: a nil *Client means the feature is off, so /search falls
// back to trigrams and nothing is indexed.
type Client struct {
	APIKey, BaseURL string // BaseURL "" = OpenAI; tests set srv.URL+"/" (the SDK path-joins)
	ChatModel       string // "" = no search lines; serve and reembed set it from cfg.OpenAIModel
}

func (c *Client) client() oa.Client {
	opts := []option.RequestOption{option.WithAPIKey(c.APIKey), option.WithMaxRetries(0)}
	if c.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(c.BaseURL))
	}
	return oa.NewClient(opts...)
}

// Embed returns one vector per text, in order. 10 s deadline and no SDK
// retries: the callers are best-effort hooks and a batch job, neither wants 3×
// the latency.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client := c.client()
	resp, err := client.Embeddings.New(ctx, oa.EmbeddingNewParams{
		Model:          oa.EmbeddingModel(Model),
		Input:          oa.EmbeddingNewParamsInputUnion{OfArrayOfStrings: texts},
		Dimensions:     oa.Int(Dims),
		EncodingFormat: oa.EmbeddingNewParamsEncodingFormatFloat,
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Data) != len(texts) {
		return nil, fmt.Errorf("openai: %d embeddings for %d textos", len(resp.Data), len(texts))
	}
	out := make([][]float64, len(texts))
	for i, d := range resp.Data {
		out[i] = d.Embedding
	}
	return out, nil
}

// LinePrompt is the system message of the search-line call. Exported because
// it is part of the cache key: editing it regenerates every line.
const LinePrompt = `Eres el índice de búsqueda de un registro de gastos personales en México. Recibes una línea con el comercio, la categoría y los productos de un gasto tal como quedaron en el ticket, separados por " · ".
Devuelve SOLO un JSON {"search_line": "..."}: una frase de hasta 25 palabras en español y en minúsculas con las palabras con las que el dueño buscaría este gasto y que NO aparecen ya en la línea:
- qué se compró, en palabras comunes, y la marca detrás del producto si la conoces (huggies pull-ups → pañales, calzones entrenadores; magna, premium → gasolina; bolillo, baguette, cuernito → pan, panadería; lo mein, kung pao → comida china).
- sinónimos del comercio o del servicio (comisión federal de electricidad → cfe, luz, electricidad, recibo de luz; smart fit → gimnasio, membresía; lavandería → lavado de ropa, tintorería).
Prohibido: montos, cantidades, fechas, nombres de personas, direcciones, apps o servicios de entrega, inventar productos que no estén en la línea, repetir la categoría o el nombre del comercio. Si no hay nada útil que agregar, "search_line": "".`

// lineShots are invented docs in BuildDoc format on purpose: a real expense
// here would teach the model an eval answer.
var lineShots = [3]struct{ doc, json string }{
	{"farmacia guadalajara · salud · pampers swaddlers 40 pz · nan optipro 1 800 g",
		`{"search_line": "pañales desechables para bebé, fórmula infantil de leche en polvo, farmacia"}`},
	{"izzi · servicios · paquete internet + tv",
		`{"search_line": "internet de la casa, televisión por cable, wifi, telecomunicaciones, recibo mensual"}`},
	{"soriana hiper · super · tortillinas tia rosa · coca cola 2.5 l · zote rosa 400 g",
		`{"search_line": "tortillas de harina, refresco de cola, jabón de lavandería en barra, despensa"}`},
}

// SearchLine asks the model for the synonyms and product words the base doc
// lacks. An empty ChatModel is the off switch and costs no call. 15 s deadline
// and no SDK retries, like Embed.
func (c *Client) SearchLine(ctx context.Context, base string) (string, error) {
	if c.ChatModel == "" {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	msgs := []oa.ChatCompletionMessageParamUnion{oa.SystemMessage(LinePrompt)}
	for _, s := range lineShots {
		msgs = append(msgs, oa.UserMessage(s.doc), oa.AssistantMessage(s.json))
	}
	client := c.client()
	resp, err := client.Chat.Completions.New(ctx, oa.ChatCompletionNewParams{
		Model:       oa.ChatModel(c.ChatModel),
		Temperature: oa.Float(0), // a prompt iteration must compare prompts, not samples
		Messages:    append(msgs, oa.UserMessage(base)),
		ResponseFormat: oa.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &oa.ResponseFormatJSONObjectParam{},
		},
	})
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("openai: empty choices")
	}
	var out struct {
		Line *string `json:"search_line"`
	}
	if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &out); err != nil {
		return "", fmt.Errorf("openai: malformed search_line JSON: %w", err)
	}
	if out.Line == nil {
		return "", fmt.Errorf("openai: no search_line in %q", resp.Choices[0].Message.Content)
	}
	return *out.Line, nil
}
