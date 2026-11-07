package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const documentDownloadTimeout = time.Minute

type documentSource interface {
	Open(context.Context, string) (io.ReadCloser, error)
}

// telegramDocumentSource streams Bot API downloads without logging token-bearing
// URLs. Unlike telebot.File, both requests honor the caller's context.
type telegramDocumentSource struct {
	apiURL string
	token  string
	client *http.Client
}

func newTelegramDocumentSource(apiURL, token string) *telegramDocumentSource {
	return &telegramDocumentSource{
		apiURL: strings.TrimRight(apiURL, "/"),
		token:  token,
		client: &http.Client{Timeout: documentDownloadTimeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}
}

func (s *telegramDocumentSource) Open(ctx context.Context, fileID string) (io.ReadCloser, error) {
	if fileID == "" {
		return nil, errors.New("Telegram document is missing its file ID")
	}
	payload, err := json.Marshal(struct {
		FileID string `json:"file_id"`
	}{fileID})
	if err != nil {
		return nil, err
	}
	metadata, err := s.request(ctx, http.MethodPost, s.apiURL+"/bot"+s.token+"/getFile", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer func() { _ = metadata.Close() }()
	var response struct {
		OK        bool `json:"ok"`
		ErrorCode int  `json:"error_code"`
		Result    struct {
			FilePath string `json:"file_path"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(metadata, 64*1024)).Decode(&response); err != nil {
		return nil, errors.New("Telegram returned invalid document metadata")
	}
	if !response.OK {
		return nil, fmt.Errorf("Telegram getFile failed (API code %d)", response.ErrorCode)
	}
	filePath, err := url.Parse(response.Result.FilePath)
	if err != nil || filePath.IsAbs() || filePath.Host != "" || filePath.RawQuery != "" || filePath.Fragment != "" {
		return nil, errors.New("Telegram returned an invalid document download path")
	}
	if filePath.Path == "" || strings.HasPrefix(filePath.Path, "/") || strings.Contains(filePath.Path, "\\") {
		return nil, errors.New("Telegram returned an invalid document download path")
	}
	for _, part := range strings.Split(filePath.Path, "/") {
		if part == "." || part == ".." {
			return nil, errors.New("Telegram returned an invalid document download path")
		}
	}
	return s.request(ctx, http.MethodGet, s.apiURL+"/file/bot"+s.token+"/"+filePath.EscapedPath(), nil)
}

func (s *telegramDocumentSource) request(ctx context.Context, method, endpoint string, body io.Reader) (io.ReadCloser, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, errors.New("cannot create Telegram document request")
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := s.client.Do(request)
	if err != nil {
		var urlError *url.Error
		if errors.As(err, &urlError) {
			err = urlError.Err // net/http errors otherwise include the bot token in the URL.
		}
		if errors.Is(err, context.Canceled) {
			return nil, fmt.Errorf("Telegram document request failed: %w", context.Canceled)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("Telegram document request failed: %w", context.DeadlineExceeded)
		}
		message := err.Error()
		if s.token != "" {
			message = strings.ReplaceAll(message, s.token, "[redacted]")
		}
		return nil, fmt.Errorf("Telegram document request failed: %s", message)
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return nil, fmt.Errorf("Telegram document request failed (HTTP %d)", response.StatusCode)
	}
	return response.Body, nil
}
