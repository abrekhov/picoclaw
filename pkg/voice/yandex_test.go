package voice

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestYandexTranscriber_RecognizeSuccess(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "voice.ogg")
	if err := os.WriteFile(p, []byte("fake-ogg"), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotAuth string
	var gotFolderID string
	var gotFormat string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotFolderID = r.Header.Get("x-folder-id")
		gotFormat = r.URL.Query().Get("format")

		if r.Method != http.MethodPost || r.URL.Path != "/speech/v1/stt:recognize" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": "privet mir"})
	}))
	defer srv.Close()

	tr := NewYandexSTTTranscriber("sk-test", "folder-123", "ru-RU")
	tr.apiBase = srv.URL

	resp, err := tr.Transcribe(context.Background(), p)
	if err != nil {
		t.Fatalf("Transcribe() error: %v", err)
	}
	if resp.Text != "privet mir" {
		t.Fatalf("Text=%q, want %q", resp.Text, "privet mir")
	}
	if gotAuth != "Api-Key sk-test" {
		t.Fatalf("Authorization=%q", gotAuth)
	}
	if gotFolderID != "folder-123" {
		t.Fatalf("x-folder-id=%q", gotFolderID)
	}
	if gotFormat != "oggopus" {
		t.Fatalf("format=%q", gotFormat)
	}
}

func TestYandexTranscriber_RecognizeError(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "voice.ogg")
	if err := os.WriteFile(p, []byte("fake-ogg"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error_code":    "UNAUTHORIZED",
			"error_message": "bad credentials",
		})
	}))
	defer srv.Close()

	tr := NewYandexSTTTranscriber("sk-test", "", "ru-RU")
	tr.apiBase = srv.URL

	_, err := tr.Transcribe(context.Background(), p)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestYandexSTTFormat(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/tmp/a.ogg", want: "oggopus"},
		{path: "/tmp/a.oga", want: "oggopus"},
		{path: "/tmp/a.opus", want: "oggopus"},
		{path: "/tmp/a.mp3", want: "mp3"},
		{path: "/tmp/a.wav", want: "lpcm"},
	}

	for _, tt := range tests {
		got, err := yandexSTTFormat(tt.path)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.path, err)
		}
		if got != tt.want {
			t.Fatalf("%s: got %q want %q", tt.path, got, tt.want)
		}
	}

	if _, err := yandexSTTFormat("/tmp/a.m4a"); err == nil {
		t.Fatal("expected unsupported format error")
	}
}
