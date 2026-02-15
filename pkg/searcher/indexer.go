package searcher

import (
	"context"
	"io"
)

type SearchDocument struct {
	ID       string       `json:"id"`
	FileID   int          `json:"file_id"`
	OwnerID  int          `json:"owner_id"`
	EntityID int          `json:"entity_id"`
	ChunkIdx int          `json:"chunk_idx"`
	FileName string       `json:"file_name"`
	Text     string       `json:"text"`
	Formated *FormatedHit `json:"_formatted,omitempty"`
}

type FormatedHit struct {
	Text string `json:"text"`
}

type SearchResult struct {
	FileID   int    `json:"file_id"`
	OwnerID  int    `json:"owner_id"`
	EntityID int    `json:"entity_id"`
	FileName string `json:"file_name"`
	Text     string `json:"text"`
}

type SearchIndexer interface {
	IndexFile(ctx context.Context, ownerID, fileID, entityID int, fileName, text string) error
	DeleteByFileIDs(ctx context.Context, fileID ...int) error
	ChangeOwner(ctx context.Context, fileID, oldOwnerID, newOwnerID int) error
	CopyByFileID(ctx context.Context, srcFileID, dstFileID, dstOwnerID, dstEntityID int) error
	Rename(ctx context.Context, fileID, entityID int, newFileName string) error
	Search(ctx context.Context, ownerID int, query string, offset int) ([]SearchResult, int64, error)
	// IndexReady reports whether the search index exists and has the required
	// configuration (filterable/searchable attributes, etc.).
	IndexReady(ctx context.Context) (bool, error)
	EnsureIndex(ctx context.Context) error
	// DeleteAll removes all documents from the index.
	DeleteAll(ctx context.Context) error
	Close() error
}

type TextExtractor interface {
	Exts() []string
	MaxFileSize() int64
	Extract(ctx context.Context, reader io.Reader) (string, error)
}
