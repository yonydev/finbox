package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var lastReq []byte // body of the most recent request the fake server saw

func fakeCompletion(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		req, _ := io.ReadAll(r.Body)
		lastReq = req
		if !strings.Contains(string(req), "Hoy es 2026-08-28.") {
			t.Errorf("request lacks the reference date: %.300s", req)
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
	ex := New("sk-test", "gpt-4o-mini")
	ex.baseURL = srv.URL + "/" // trailing slash: the client path-joins onto the base
	res, err := ex.Extract(context.Background(), []byte{0xFF, 0xD8, 0xFF, 0, 0, 0, 0, 0, 0, 0, 0, 0}, "image/jpeg", time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC))
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
	ex := New("sk-bad", "gpt-4o-mini")
	ex.baseURL = srv.URL + "/"
	_, err := ex.Extract(context.Background(), []byte{0xFF, 0xD8, 0xFF, 0, 0, 0, 0, 0, 0, 0, 0, 0}, "image/jpeg", time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC))
	if !errors.Is(err, ErrNonRetryable) {
		t.Fatalf("err = %v, want ErrNonRetryable", err)
	}
}

func TestExtractPDFSendsFilePart(t *testing.T) {
	srv := fakeCompletion(t, 200, okBody)
	defer srv.Close()
	ex := New("sk-test", "gpt-4.1-mini")
	ex.baseURL = srv.URL + "/"
	if _, err := ex.Extract(context.Background(), []byte("%PDF-1.4\n%fake"), "application/pdf", time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	body := string(lastReq)
	for _, want := range []string{`"type":"file"`, `data:application/pdf;base64,`, `"filename":"recibo.pdf"`} {
		if !strings.Contains(body, want) {
			t.Errorf("request lacks %s: %.400s", want, body)
		}
	}
	if strings.Contains(body, "image_url") {
		t.Errorf("PDF must not be sent as an image part: %.400s", body)
	}
}
