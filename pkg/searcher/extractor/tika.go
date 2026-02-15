package extractor

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/cloudreve/Cloudreve/v4/pkg/logging"
	"github.com/cloudreve/Cloudreve/v4/pkg/request"
	"github.com/cloudreve/Cloudreve/v4/pkg/setting"
)

// TikaExtractor extracts text from documents using Apache Tika.
type TikaExtractor struct {
	client      request.Client
	settings    setting.Provider
	l           logging.Logger
	exts        []string
	maxFileSize int64
	endpoint    string
}

// NewTikaExtractor creates a new TikaExtractor.
func NewTikaExtractor(client request.Client, settings setting.Provider, l logging.Logger, cfg *setting.FTSTikaExtractorSetting) *TikaExtractor {
	exts := cfg.Exts
	return &TikaExtractor{
		client:      client,
		settings:    settings,
		l:           l,
		exts:        exts,
		maxFileSize: cfg.MaxFileSize,
		endpoint:    cfg.Endpoint,
	}
}

// Exts returns the list of supported file extensions.
func (t *TikaExtractor) Exts() []string {
	return t.exts
}

// MaxFileSize returns the maximum file size for text extraction.
func (t *TikaExtractor) MaxFileSize() int64 {
	return t.maxFileSize
}

// Extract sends the document to Tika and returns the extracted plain text.
func (t *TikaExtractor) Extract(ctx context.Context, reader io.Reader) (string, error) {
	if t.endpoint == "" {
		return "", fmt.Errorf("tika endpoint not configured")
	}

	endpoint := strings.TrimRight(t.endpoint, "/") + "/tika"
	resp := t.client.Request(
		"PUT",
		endpoint,
		reader,
		request.WithHeader(map[string][]string{
			"Accept": {"text/plain"},
		}),
	)
	if resp.Err != nil {
		return "", fmt.Errorf("tika request failed: %w", resp.Err)
	}
	defer resp.Response.Body.Close()

	if resp.Response.StatusCode != 200 {
		return "", fmt.Errorf("tika returned status %d", resp.Response.StatusCode)
	}

	body, err := io.ReadAll(resp.Response.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read tika response: %w", err)
	}

	return strings.TrimSpace(string(body)), nil
}
