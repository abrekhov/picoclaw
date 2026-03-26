package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/logger"
)

// YandexSTTTranscriber implements Transcriber using Yandex Cloud SpeechKit STT.
// Telegram voice notes are typically OGG/Opus, which works well with the
// synchronous API used here.

type YandexSTTTranscriber struct {
	apiKey     string
	folderID   string
	lang       string
	apiBase    string
	httpClient *http.Client
}

type yandexRecognizeResp struct {
	Result       string `json:"result"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

func NewYandexSTTTranscriber(apiKey, folderID, lang string) *YandexSTTTranscriber {
	if lang == "" {
		lang = "ru-RU"
	}
	return &YandexSTTTranscriber{
		apiKey:       apiKey,
		folderID:     folderID,
		lang:         lang,
		apiBase:      "https://stt.api.cloud.yandex.net",
		httpClient:   &http.Client{Timeout: 60 * time.Second},
	}
}

func (t *YandexSTTTranscriber) Name() string { return "yandex" }

func (t *YandexSTTTranscriber) Transcribe(ctx context.Context, audioFilePath string) (*TranscriptionResponse, error) {
	if t.apiKey == "" {
		return nil, fmt.Errorf("yandex stt: missing api key")
	}

	b, err := os.ReadFile(audioFilePath)
	if err != nil {
		return nil, fmt.Errorf("read audio file: %w", err)
	}

	format, err := yandexSTTFormat(audioFilePath)
	if err != nil {
		return nil, err
	}
	reqURL := fmt.Sprintf("%s/speech/v1/stt:recognize?lang=%s&format=%s", t.apiBase, t.lang, format)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Api-Key "+t.apiKey)
	req.Header.Set("Content-Type", "application/octet-stream")
	if t.folderID != "" {
		req.Header.Set("x-folder-id", t.folderID)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("recognize audio: %w", err)
	}
	defer resp.Body.Close()

	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.ErrorCF("voice", "Yandex STT request error", map[string]any{"status": resp.StatusCode, "resp": string(rb)})
		return nil, fmt.Errorf("yandex stt recognize: http %d: %s", resp.StatusCode, string(rb))
	}

	var out yandexRecognizeResp
	if err := json.Unmarshal(rb, &out); err != nil {
		return nil, fmt.Errorf("parse recognition response: %w", err)
	}
	if out.ErrorCode != "" || out.ErrorMessage != "" {
		return nil, fmt.Errorf("yandex stt error: %s %s", out.ErrorCode, out.ErrorMessage)
	}
	text := strings.TrimSpace(out.Result)
	if text == "" {
		return &TranscriptionResponse{Text: ""}, nil
	}
	return &TranscriptionResponse{Text: text}, nil
}

func yandexSTTFormat(audioFilePath string) (string, error) {
	switch strings.ToLower(filepath.Ext(audioFilePath)) {
	case ".ogg", ".oga", ".opus":
		return "oggopus", nil
	case ".mp3":
		return "mp3", nil
	case ".wav":
		return "lpcm", nil
	default:
		return "", fmt.Errorf("yandex stt: unsupported audio format for %q", filepath.Base(audioFilePath))
	}
}
