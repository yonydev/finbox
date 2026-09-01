package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func fakeCompletion(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
}

const okBody = `{
  "id":"cmpl-1","model":"gpt-4o-mini",
  "choices":[{"message":{"role":"assistant","content":"{\"merchant\":\"Walmart\",\"date\":\"2026-08-28\",\"currency\":\"MXN\",\"total\":\"364.00\",\"items\":[{\"name\":\"Café\",\"quantity\":\"1\",\"amount\":\"189.00\"}]}"}}],
  "usage":{"prompt_tokens":900,"completion_tokens":80}
}`

func TestExtractOK(t *testing.T) {
	srv := fakeCompletion(t, 200, okBody)
	defer srv.Close()
	ex := New("sk-test", "gpt-4o-mini", WithBaseURL(srv.URL+"/")) // trailing slash: the client path-joins onto the base
	res, err := ex.Extract(context.Background(), []byte{0xFF, 0xD8, 0xFF, 0, 0, 0, 0, 0, 0, 0, 0, 0}, "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if res.Extraction.Merchant != "Walmart" || res.Extraction.Total != "364.00" {
		t.Errorf("extraction = %+v", res.Extraction)
	}
	if res.PromptTokens != 900 {
		t.Errorf("usage not captured: %+v", res)
	}
	var chk map[string]any
	if err := json.Unmarshal(res.RawJSON, &chk); err != nil {
		t.Errorf("RawJSON not valid JSON: %v", err)
	}
}

func TestExtractNonRetryable(t *testing.T) {
	srv := fakeCompletion(t, 401, `{"error":{"message":"bad key","type":"invalid_request_error"}}`)
	defer srv.Close()
	ex := New("sk-bad", "gpt-4o-mini", WithBaseURL(srv.URL+"/"))
	_, err := ex.Extract(context.Background(), []byte{0xFF, 0xD8, 0xFF, 0, 0, 0, 0, 0, 0, 0, 0, 0}, "image/jpeg")
	if !errors.Is(err, ErrNonRetryable) {
		t.Fatalf("err = %v, want ErrNonRetryable", err)
	}
}
