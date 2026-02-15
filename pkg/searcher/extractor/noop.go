package extractor

import (
	"context"
	"io"
)

// NoopExtractor is a no-op implementation of TextExtractor, used when text extraction is disabled.
type NoopExtractor struct{}

func (n *NoopExtractor) Exts() []string     { return nil }
func (n *NoopExtractor) MaxFileSize() int64 { return 0 }
func (n *NoopExtractor) Extract(ctx context.Context, reader io.Reader) (string, error) {
	return "", nil
}
