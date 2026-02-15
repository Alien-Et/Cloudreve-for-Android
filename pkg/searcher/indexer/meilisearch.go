package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudreve/Cloudreve/v4/pkg/logging"
	"github.com/cloudreve/Cloudreve/v4/pkg/searcher"
	"github.com/cloudreve/Cloudreve/v4/pkg/setting"
	"github.com/meilisearch/meilisearch-go"
)

const (
	indexName         = "cloudreve_files"
	embedderName      = "cr-text"
	embeddingTemplate = "Chunk #{{doc.chunk_idx}} in a file named '{{doc.file_name}}': {{ doc.text }}"
)

// MeilisearchIndexer implements SearchIndexer using Meilisearch.
type MeilisearchIndexer struct {
	client    meilisearch.ServiceManager
	l         logging.Logger
	pageSize  int
	chunkSize int
	cfg       *setting.FTSIndexMeilisearchSetting
}

// NewMeilisearchIndexer creates a new MeilisearchIndexer.
func NewMeilisearchIndexer(msCfg *setting.FTSIndexMeilisearchSetting, chunkSize int, l logging.Logger) *MeilisearchIndexer {
	client := meilisearch.New(msCfg.Endpoint, meilisearch.WithAPIKey(msCfg.APIKey))
	return &MeilisearchIndexer{
		client:    client,
		l:         l,
		pageSize:  msCfg.PageSize,
		chunkSize: chunkSize,
		cfg:       msCfg,
	}
}

var (
	requiredFilterable = []string{"owner_id", "file_id", "entity_id"}
	requiredSearchable = []string{"text", "file_name"}
	requiredDistinct   = "file_id"
)

func (m *MeilisearchIndexer) IndexReady(ctx context.Context) (bool, error) {
	index := m.client.Index(indexName)

	settings, err := index.GetSettingsWithContext(ctx)
	if err != nil {
		// If the index doesn't exist, Meilisearch returns an error.
		return false, nil
	}

	// Check filterable attributes.
	for _, attr := range requiredFilterable {
		if !slices.Contains(settings.FilterableAttributes, attr) {
			return false, nil
		}
	}

	// Check searchable attributes.
	for _, attr := range requiredSearchable {
		if !slices.Contains(settings.SearchableAttributes, attr) {
			return false, nil
		}
	}

	// Check distinct attribute.
	if settings.DistinctAttribute == nil || *settings.DistinctAttribute != requiredDistinct {
		return false, nil
	}

	// Check embedder if embedding is enabled.
	if m.cfg.EmbeddingEnbaled {
		if settings.Embedders == nil {
			return false, nil
		}
		if _, ok := settings.Embedders[embedderName]; !ok {
			return false, nil
		}
	}

	return true, nil
}

func (m *MeilisearchIndexer) EnsureIndex(ctx context.Context) error {
	_, err := m.client.CreateIndexWithContext(ctx, &meilisearch.IndexConfig{
		Uid:        indexName,
		PrimaryKey: "id",
	})
	if err != nil {
		m.l.Debug("Create index returned (may already exist): %s", err)
	}

	index := m.client.Index(indexName)

	filterableAttrs := []any{"owner_id", "file_id", "entity_id"}
	if _, err := index.UpdateFilterableAttributesWithContext(ctx, &filterableAttrs); err != nil {
		return fmt.Errorf("failed to set filterable attributes: %w", err)
	}

	searchableAttrs := []string{"text", "file_name"}
	if _, err := index.UpdateSearchableAttributesWithContext(ctx, &searchableAttrs); err != nil {
		return fmt.Errorf("failed to set searchable attributes: %w", err)
	}

	_, err = index.UpdateDistinctAttributeWithContext(ctx, "file_id")
	if err != nil {
		return fmt.Errorf("failed to set distinct attribute: %w", err)
	}

	if m.cfg.EmbeddingEnbaled {
		var embedder meilisearch.Embedder
		if err := json.Unmarshal([]byte(m.cfg.EmbeddingSetting), &embedder); err != nil {
			m.cfg.EmbeddingEnbaled = false
			m.l.Warning("Failed to unmarshal embedding setting: %s, fallback to disable embedding", err)
			return nil
		}

		embedder.DocumentTemplate = embeddingTemplate
		_, err := index.UpdateEmbeddersWithContext(ctx, map[string]meilisearch.Embedder{
			embedderName: embedder,
		})
		if err != nil {
			return fmt.Errorf("failed to set embedders: %w", err)
		}
	} else {
		_, err := index.ResetEmbeddersWithContext(ctx)
		if err != nil {
			m.l.Warning("Failed to reset embedder: %w", err)
		}
	}

	return nil
}

func (m *MeilisearchIndexer) IndexFile(ctx context.Context, ownerID, fileID, entityID int, fileName, text string) error {
	chunks := ChunkText(text, m.chunkSize)
	if len(chunks) == 0 {
		return nil
	}

	docs := make([]searcher.SearchDocument, 0, len(chunks))
	for i, chunk := range chunks {
		docs = append(docs, searcher.SearchDocument{
			ID:       fmt.Sprintf("%d_%d", fileID, i),
			FileID:   fileID,
			OwnerID:  ownerID,
			EntityID: entityID,
			ChunkIdx: i,
			FileName: fileName,
			Text:     chunk,
		})
	}

	index := m.client.Index(indexName)
	pk := "id"
	if _, err := index.AddDocumentsWithContext(ctx, docs, &meilisearch.DocumentOptions{PrimaryKey: &pk}); err != nil {
		return fmt.Errorf("failed to add documents: %w", err)
	}

	return nil
}

func (m *MeilisearchIndexer) DeleteByFileIDs(ctx context.Context, fileID ...int) error {
	if len(fileID) == 0 {
		return nil
	}

	index := m.client.Index(indexName)
	strs := make([]string, len(fileID))
	for i, id := range fileID {
		strs[i] = fmt.Sprintf("%d", id)
	}
	filter := fmt.Sprintf("file_id IN [%s]", strings.Join(strs, ", "))
	if _, err := index.DeleteDocumentsByFilterWithContext(ctx, filter, nil); err != nil {
		return fmt.Errorf("failed to delete documents by file_ids: %w", err)
	}
	return nil
}

func (m *MeilisearchIndexer) ChangeOwner(ctx context.Context, fileID, oldOwnerID, newOwnerID int) error {
	index := m.client.Index(indexName)
	filter := fmt.Sprintf("file_id = %d AND owner_id = %d", fileID, oldOwnerID)

	// Fetch all existing document chunks in batches.
	const batchSize int64 = 100
	var allDocs []searcher.SearchDocument
	for offset := int64(0); ; offset += batchSize {
		var result meilisearch.DocumentsResult
		if err := index.GetDocumentsWithContext(ctx, &meilisearch.DocumentsQuery{
			Filter: filter,
			Limit:  batchSize,
			Offset: offset,
		}, &result); err != nil {
			return fmt.Errorf("failed to get documents: %w", err)
		}

		for _, hit := range result.Results {
			var doc searcher.SearchDocument
			if err := hit.DecodeInto(&doc); err != nil {
				m.l.Warning("Failed to decode document during owner change: %s", err)
				continue
			}
			allDocs = append(allDocs, doc)
		}

		if int64(len(result.Results)) < batchSize {
			break
		}
	}

	if len(allDocs) == 0 {
		return nil
	}

	// Update owner_id in place — primary key is {fileID}_{chunkIdx} so it stays the same.
	for i := range allDocs {
		allDocs[i].OwnerID = newOwnerID
	}

	if _, err := index.UpdateDocumentsInBatchesWithContext(ctx, allDocs, 100, nil); err != nil {
		return fmt.Errorf("failed to update documents with new owner: %w", err)
	}

	return nil
}

func (m *MeilisearchIndexer) CopyByFileID(ctx context.Context, srcFileID, dstFileID, dstOwnerID, dstEntityID int) error {
	index := m.client.Index(indexName)
	filter := fmt.Sprintf("file_id = %d", srcFileID)

	const batchSize int64 = 100
	var allDocs []searcher.SearchDocument
	for offset := int64(0); ; offset += batchSize {
		var result meilisearch.DocumentsResult
		if err := index.GetDocumentsWithContext(ctx, &meilisearch.DocumentsQuery{
			Filter: filter,
			Limit:  batchSize,
			Offset: offset,
		}, &result); err != nil {
			return fmt.Errorf("failed to get source documents: %w", err)
		}

		for _, hit := range result.Results {
			var doc searcher.SearchDocument
			if err := hit.DecodeInto(&doc); err != nil {
				m.l.Warning("Failed to decode document during copy: %s", err)
				continue
			}
			allDocs = append(allDocs, doc)
		}

		if int64(len(result.Results)) < batchSize {
			break
		}
	}

	if len(allDocs) == 0 {
		return fmt.Errorf("no source documents found for file %d", srcFileID)
	}

	for i := range allDocs {
		if allDocs[i].EntityID != dstEntityID {
			m.l.Warning("Entity id mismatch for file %d, original: %d, destination: %d", srcFileID, allDocs[i].EntityID, dstEntityID)
			continue
		}

		allDocs[i].ID = fmt.Sprintf("%d_%d", dstFileID, allDocs[i].ChunkIdx)
		allDocs[i].FileID = dstFileID
		allDocs[i].OwnerID = dstOwnerID
		allDocs[i].EntityID = dstEntityID
	}

	if len(allDocs) == 0 {
		return fmt.Errorf("no source documents found for file %d", srcFileID)
	}

	pk := "id"
	if _, err := index.AddDocumentsWithContext(ctx, allDocs, &meilisearch.DocumentOptions{PrimaryKey: &pk}); err != nil {
		return fmt.Errorf("failed to add copied documents: %w", err)
	}

	return nil
}

func (m *MeilisearchIndexer) Rename(ctx context.Context, fileID, entityID int, newFileName string) error {
	index := m.client.Index(indexName)
	filter := fmt.Sprintf("file_id = %d AND entity_id = %d", fileID, entityID)

	const batchSize int64 = 100
	var allDocs []searcher.SearchDocument
	for offset := int64(0); ; offset += batchSize {
		var result meilisearch.DocumentsResult
		if err := index.GetDocumentsWithContext(ctx, &meilisearch.DocumentsQuery{
			Filter: filter,
			Limit:  batchSize,
			Offset: offset,
		}, &result); err != nil {
			return fmt.Errorf("failed to get documents for rename: %w", err)
		}

		for _, hit := range result.Results {
			var doc searcher.SearchDocument
			if err := hit.DecodeInto(&doc); err != nil {
				m.l.Warning("Failed to decode document during rename: %s", err)
				continue
			}
			doc.FileName = newFileName
			allDocs = append(allDocs, doc)
		}

		if int64(len(result.Results)) < batchSize {
			break
		}
	}

	if len(allDocs) == 0 {
		return nil
	}

	if _, err := index.UpdateDocumentsInBatchesWithContext(ctx, allDocs, 100, nil); err != nil {
		return fmt.Errorf("failed to update documents with new file name: %w", err)
	}

	return nil
}

func (m *MeilisearchIndexer) Search(ctx context.Context, ownerID int, query string, offset int) ([]searcher.SearchResult, int64, error) {
	index := m.client.Index(indexName)

	searchReq := &meilisearch.SearchRequest{
		Filter:                fmt.Sprintf("owner_id = %d", ownerID),
		Limit:                 int64(m.pageSize),
		Offset:                int64(offset),
		AttributesToHighlight: []string{"text"},
	}

	if m.cfg.EmbeddingEnbaled {
		searchReq.Hybrid = &meilisearch.SearchRequestHybrid{
			Embedder: embedderName,
		}
	}

	resp, err := index.SearchWithContext(ctx, query, searchReq)
	if err != nil {
		return nil, 0, fmt.Errorf("search failed: %w", err)
	}

	results := make([]searcher.SearchResult, 0, len(resp.Hits))
	seen := make(map[int]struct{})
	for _, hit := range resp.Hits {
		var doc searcher.SearchDocument
		if err := hit.DecodeInto(&doc); err != nil {
			continue
		}

		if _, exists := seen[doc.FileID]; exists {
			continue
		}
		seen[doc.FileID] = struct{}{}

		// Extract text from raw JSON for display
		textStr := doc.Text
		if doc.Formated != nil {
			textStr = doc.Formated.Text
		}

		results = append(results, searcher.SearchResult{
			FileID:   doc.FileID,
			OwnerID:  doc.OwnerID,
			FileName: doc.FileName,
			Text:     textStr,
		})
	}

	return results, resp.EstimatedTotalHits, nil
}

func (m *MeilisearchIndexer) DeleteAll(ctx context.Context) error {
	index := m.client.Index(indexName)
	if _, err := index.DeleteAllDocumentsWithContext(ctx, nil); err != nil {
		return fmt.Errorf("failed to delete all documents: %w", err)
	}
	return nil
}

func (m *MeilisearchIndexer) Close() error {
	return nil
}
