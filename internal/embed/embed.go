// Package embed wraps the OpenAI embeddings endpoint. The name shadows the
// stdlib embed package: never import both in one file.
package embed

import (
	"context"
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
	APIKey, Model, BaseURL string // BaseURL "" = OpenAI; tests set srv.URL+"/" (the SDK path-joins)
}

// Embed returns one vector per text, in order. 10 s deadline and no SDK
// retries: the callers are best-effort hooks and a batch job, neither wants 3×
// the latency.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	opts := []option.RequestOption{option.WithAPIKey(c.APIKey), option.WithMaxRetries(0)}
	if c.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(c.BaseURL))
	}
	client := oa.NewClient(opts...)
	resp, err := client.Embeddings.New(ctx, oa.EmbeddingNewParams{
		Model:          oa.EmbeddingModel(c.Model),
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
