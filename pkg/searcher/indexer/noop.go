package indexer

import (
	"context"

	"github.com/cloudreve/Cloudreve/v4/pkg/searcher"
)

// NoopIndexer is a no-op implementation of SearchIndexer, used when FTS is disabled.
type NoopIndexer struct{}

func (n *NoopIndexer) IndexFile(ctx context.Context, ownerID, fileID, entityID int, fileName, text string) error {
	return nil
}

func (n *NoopIndexer) DeleteByFileIDs(ctx context.Context, fileID ...int) error {
	return nil
}

func (n *NoopIndexer) ChangeOwner(ctx context.Context, fileID, oldOwnerID, newOwnerID int) error {
	return nil
}

func (n *NoopIndexer) CopyByFileID(ctx context.Context, srcFileID, dstFileID, dstOwnerID, dstEntityID int) error {
	return nil
}

func (n *NoopIndexer) Rename(ctx context.Context, fileID, entityID int, newFileName string) error {
	return nil
}

func (n *NoopIndexer) Search(ctx context.Context, ownerID int, query string, offset int) ([]searcher.SearchResult, int64, error) {
	return nil, 0, nil
}

func (n *NoopIndexer) IndexReady(ctx context.Context) (bool, error) {
	return true, nil
}

func (n *NoopIndexer) EnsureIndex(ctx context.Context) error {
	return nil
}

func (n *NoopIndexer) DeleteAll(ctx context.Context) error {
	return nil
}

func (n *NoopIndexer) Close() error {
	return nil
}
