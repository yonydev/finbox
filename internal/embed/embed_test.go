package embed

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbedRequestAndParse(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"index":0,"embedding":[0.5,0.25]},{"index":1,"embedding":[1,0]}]}`))
	}))
	defer srv.Close()
	c := &Client{APIKey: "sk-test", BaseURL: srv.URL + "/"}
	vecs, err := c.Embed(context.Background(), []string{"pañales", "gasolina"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"dimensions":512`, `"` + Model + `"`, `"pañales"`, `"gasolina"`} {
		if !strings.Contains(body, want) {
			t.Errorf("request lacks %s: %s", want, body)
		}
	}
	if len(vecs) != 2 || vecs[0][0] != 0.5 || vecs[1][0] != 1 {
		t.Fatalf("vecs = %v", vecs)
	}
}

func TestEmbedCountMismatchAndHTTPError(t *testing.T) {
	short := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"index":0,"embedding":[0.5]}]}`))
	}))
	defer short.Close()
	c := &Client{APIKey: "sk-test", BaseURL: short.URL + "/"}
	if _, err := c.Embed(context.Background(), []string{"a", "b"}); err == nil {
		t.Error("a short response must be an error")
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(401)
		w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer bad.Close()
	c.BaseURL = bad.URL + "/"
	if _, err := c.Embed(context.Background(), []string{"a"}); err == nil {
		t.Error("401 must be an error")
	}
}

func TestSearchLineRequestAndParse(t *testing.T) {
	var body string
	calls := 0
	content := `{\"search_line\":\"pañales de huggies\"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"` + content + `"}}]}`))
	}))
	defer srv.Close()
	c := &Client{APIKey: "sk-test", BaseURL: srv.URL + "/", ChatModel: "gpt-test"}
	line, err := c.SearchLine(context.Background(), "chedraui · super · godonites panal med 11 un")
	if err != nil {
		t.Fatal(err)
	}
	if line != "pañales de huggies" {
		t.Fatalf("line = %q", line)
	}
	for _, want := range []string{`"model":"gpt-test"`, `"temperature":0`, `"response_format"`, `json_object`,
		"godonites panal med 11 un", lineShots[0].doc} {
		if !strings.Contains(body, want) {
			t.Errorf("request lacks %s: %s", want, body)
		}
	}

	off := &Client{APIKey: "sk-test", BaseURL: srv.URL + "/"}
	if line, err := off.SearchLine(context.Background(), "x"); line != "" || err != nil || calls != 1 {
		t.Fatalf("ChatModel \"\": %q, err %v, %d calls", line, err, calls)
	}
	content = `{\"otra_cosa\":\"x\"}`
	if _, err := c.SearchLine(context.Background(), "x"); err == nil {
		t.Error("a response without search_line must be an error")
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer bad.Close()
	c.BaseURL = bad.URL + "/"
	if _, err := c.SearchLine(context.Background(), "x"); err == nil {
		t.Error("500 must be an error")
	}
}
