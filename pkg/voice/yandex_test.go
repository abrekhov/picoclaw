package voice

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestYandexTranscriber_PollSuccess(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "voice.ogg")
	if err := os.WriteFile(p, []byte("fake-ogg"), 0o644); err != nil {
		t.Fatal(err)
	}

	var polls atomic.Int32
	opID := "op-123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/speech/stt/v2/longRunningRecognize":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": opID})
			return
		case r.Method == http.MethodGet && r.URL.Path == "/operations/"+opID:
			n := polls.Add(1)
			if n < 2 {
				_ = json.NewEncoder(w).Encode(map[string]any{"done": false})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"done": true,
				"result": map[string]any{
					"chunks": []any{
						map[string]any{"alternatives": []any{map[string]any{"text": "privet"}}},
						map[string]any{"alternatives": []any{map[string]any{"text": "mir"}}},
					},
				},
			})
			return
		default:
			http.NotFound(w, r)
			return
		}
	}))
	defer srv.Close()

	tr := NewYandexSTTTranscriber("sk-test", "", "ru-RU")
	tr.apiBase = srv.URL
	tr.pollInterval = 1 * time.Millisecond
	tr.pollTimeout = 200 * time.Millisecond

	resp, err := tr.Transcribe(context.Background(), p)
	if err != nil {
		t.Fatalf("Transcribe() error: %v", err)
	}
	if resp.Text != "privet mir" {
		t.Fatalf("Text=%q, want %q", resp.Text, "privet mir")
	}
}

func TestYandexTranscriber_OperationError(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "voice.ogg")
	_ = os.WriteFile(p, []byte("fake-ogg"), 0o644)

	opID := "op-err"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": opID})
			return
		}
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"done":  true,
				"error": map[string]any{"message": "bad audio"},
			})
			return
		}
	}))
	defer srv.Close()

	tr := NewYandexSTTTranscriber("sk-test", "", "ru-RU")
	tr.apiBase = srv.URL
	tr.pollInterval = 1 * time.Millisecond
	tr.pollTimeout = 200 * time.Millisecond

	_, err := tr.Transcribe(context.Background(), p)
	if err == nil {
		t.Fatal("expected error")
	}
}
