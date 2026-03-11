package voice

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/logger"
)

// YandexSTTTranscriber implements Transcriber using Yandex Cloud SpeechKit STT.
// It uses the long-running recognition API (v2):
//   POST /speech/stt/v2/longRunningRecognize
// then polls:
//   GET /operations/{id}
//
// For Telegram voice notes, audio is typically OGG/OPUS, so we send audioEncoding=OGG_OPUS.
// If you feed other formats, you may need to adjust encoding and/or add normalization.

type YandexSTTTranscriber struct {
	apiKey     string
	folderID   string
	lang       string
	apiBase    string
	httpClient *http.Client

	// polling
	pollInterval time.Duration
	pollTimeout  time.Duration
}

type yandexLROStartResp struct {
	ID string `json:"id"`
}

type yandexOperationResp struct {
	Done  bool `json:"done"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
	Result *struct {
		Chunks []struct {
			Alternatives []struct {
				Text string `json:"text"`
			} `json:"alternatives"`
		} `json:"chunks"`
	} `json:"result,omitempty"`
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
		pollInterval: 500 * time.Millisecond,
		pollTimeout:  60 * time.Second,
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

	cfg := map[string]any{
		"specification": map[string]any{
			"languageCode":  t.lang,
			"audioEncoding": "OGG_OPUS",
		},
	}
	if t.folderID != "" {
		cfg["folderId"] = t.folderID
	}

	payload := map[string]any{
		"config": cfg,
		"audio": map[string]any{
			"content": base64.StdEncoding.EncodeToString(b),
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	startURL := t.apiBase + "/speech/stt/v2/longRunningRecognize"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, startURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Api-Key "+t.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("start recognition: %w", err)
	}
	defer resp.Body.Close()

	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.ErrorCF("voice", "Yandex STT start error", map[string]any{"status": resp.StatusCode, "resp": string(rb)})
		return nil, fmt.Errorf("yandex stt start: http %d: %s", resp.StatusCode, string(rb))
	}

	var start yandexLROStartResp
	if err := json.Unmarshal(rb, &start); err != nil {
		return nil, fmt.Errorf("parse start response: %w", err)
	}
	if start.ID == "" {
		return nil, fmt.Errorf("yandex stt: empty operation id")
	}

	pollURL := t.apiBase + "/operations/" + start.ID
	deadline := time.Now().Add(t.pollTimeout)
	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("yandex stt: timeout waiting operation")
		}

		preq, _ := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
		preq.Header.Set("Authorization", "Api-Key "+t.apiKey)

		presp, err := t.httpClient.Do(preq)
		if err != nil {
			return nil, fmt.Errorf("poll operation: %w", err)
		}
		prb, _ := io.ReadAll(presp.Body)
		presp.Body.Close()
		if presp.StatusCode < 200 || presp.StatusCode >= 300 {
			return nil, fmt.Errorf("yandex stt poll: http %d: %s", presp.StatusCode, string(prb))
		}

		var op yandexOperationResp
		if err := json.Unmarshal(prb, &op); err != nil {
			return nil, fmt.Errorf("parse operation response: %w", err)
		}

		if !op.Done {
			time.Sleep(t.pollInterval)
			continue
		}

		if op.Error != nil {
			return nil, fmt.Errorf("yandex stt operation error: %s", op.Error.Message)
		}

		var texts []string
		if op.Result != nil {
			for _, ch := range op.Result.Chunks {
				if len(ch.Alternatives) > 0 {
					if t := strings.TrimSpace(ch.Alternatives[0].Text); t != "" {
						texts = append(texts, t)
					}
				}
			}
		}

		return &TranscriptionResponse{Text: strings.Join(texts, " ")}, nil
	}
}
