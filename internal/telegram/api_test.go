package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func fakeTelegramServer(t *testing.T, handler func(method string, body map[string]any) any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		// path shape: /bot<token>/<method>
		method := r.URL.Path[len("/bottest-token/"):]
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": handler(method, body)})
	}))
}

func TestGetUpdatesAndSend(t *testing.T) {
	srv := fakeTelegramServer(t, func(method string, body map[string]any) any {
		switch method {
		case "getUpdates":
			if body["offset"].(float64) != 5 {
				t.Errorf("offset = %v", body["offset"])
			}
			return []map[string]any{{"update_id": 6, "message": map[string]any{
				"message_id": 42, "text": "/list",
				"from": map[string]any{"id": 111}, "chat": map[string]any{"id": 111},
			}}}
		case "sendMessage":
			if body["parse_mode"] != "HTML" {
				t.Errorf("parse_mode = %v", body["parse_mode"])
			}
			return map[string]any{"message_id": 43, "chat": map[string]any{"id": 111}}
		}
		t.Errorf("unexpected method %s", method)
		return nil
	})
	defer srv.Close()
	c := NewClient("test-token", WithBaseURL(srv.URL))
	ups, err := c.GetUpdates(context.Background(), 5, 0)
	if err != nil || len(ups) != 1 || ups[0].UpdateID != 6 || ups[0].Message.Text != "/list" {
		t.Fatalf("updates: %+v %v", ups, err)
	}
	msg, err := c.SendMessage(context.Background(), 111, "<b>hola</b>", nil)
	if err != nil || msg.MessageID != 43 {
		t.Fatalf("send: %+v %v", msg, err)
	}
}

func TestAPIErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"ok":false,"description":"Bad Request: chat not found"}`))
	}))
	defer srv.Close()
	c := NewClient("test-token", WithBaseURL(srv.URL))
	if _, err := c.SendMessage(context.Background(), 1, "x", nil); err == nil {
		t.Fatal("want error")
	}
}
