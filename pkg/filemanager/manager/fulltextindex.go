package manager

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudreve/Cloudreve/v4/application/dependency"
	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/ent/task"
	"github.com/cloudreve/Cloudreve/v4/inventory"
	"github.com/cloudreve/Cloudreve/v4/inventory/types"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/fs"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/fs/dbfs"
	"github.com/cloudreve/Cloudreve/v4/pkg/hashid"
	"github.com/cloudreve/Cloudreve/v4/pkg/logging"
	"github.com/cloudreve/Cloudreve/v4/pkg/queue"
	"github.com/cloudreve/Cloudreve/v4/pkg/searcher"
	"github.com/cloudreve/Cloudreve/v4/pkg/util"
	"github.com/samber/lo"
)

type (
	FullTextIndexTask struct {
		*queue.DBTask
	}

	FullTextIndexTaskState struct {
		Uri      *fs.URI `json:"uri"`
		EntityID int     `json:"entity_id"`
		FileID   int     `json:"file_id"`
		OwnerID  int     `json:"owner_id"`
	}

	ftsFileInfo struct {
		FileID   int
		OwnerID  int
		EntityID int
		FileName string
	}
)

func (m *manager) SearchFullText(ctx context.Context, query string, offset int) (*FullTextSearchResults, error) {
	indexer := m.dep.SearchIndexer(ctx)
	results, total, err := indexer.Search(ctx, m.user.ID, query, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to search full text: %w", err)
	}

	if len(results) == 0 {
		// No results.
		return &FullTextSearchResults{}, nil
	}

	// Traverse each file in result
	files := lo.FilterMap(results, func(result searcher.SearchResult, _ int) (FullTextSearchResult, bool) {
		file, err := m.TraverseFile(ctx, result.FileID)
		if err != nil {
			m.l.Debug("Failed to traverse file %d for full text search: %s, skipping.", result.FileID, err)
			return FullTextSearchResult{}, false
		}

		return FullTextSearchResult{
			File:    file,
			Content: result.Text,
		}, true
	})

	if len(files) == 0 {
		// No valid files, run next offset
		return m.SearchFullText(ctx, query, offset+len(results))
	}

	return &FullTextSearchResults{
		Hits:  files,
		Total: total,
	}, nil
}

func init() {
	queue.RegisterResumableTaskFactory(queue.FullTextIndexTaskType, NewFullTextIndexTaskFromModel)
	queue.RegisterResumableTaskFactory(queue.FullTextCopyTaskType, NewFullTextCopyTaskFromModel)
	queue.RegisterResumableTaskFactory(queue.FullTextChangeOwnerTaskType, NewFullTextChangeOwnerTaskFromModel)
	queue.RegisterResumableTaskFactory(queue.FullTextDeleteTaskType, NewFullTextDeleteTaskFromModel)
}

func NewFullTextIndexTask(ctx context.Context, uri *fs.URI, entityID, fileID, ownerID int, creator *ent.User) (*FullTextIndexTask, error) {
	state := &FullTextIndexTaskState{
		Uri:      uri,
		EntityID: entityID,
		FileID:   fileID,
		OwnerID:  ownerID,
	}
	stateBytes, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal state: %w", err)
	}

	return &FullTextIndexTask{
		DBTask: &queue.DBTask{
			DirectOwner: creator,
			Task: &ent.Task{
				Type:          queue.FullTextIndexTaskType,
				CorrelationID: logging.CorrelationID(ctx),
				PrivateState:  string(stateBytes),
				PublicState:   &types.TaskPublicState{},
			},
		},
	}, nil
}

func NewFullTextIndexTaskFromModel(t *ent.Task) queue.Task {
	return &FullTextIndexTask{
		DBTask: &queue.DBTask{
			Task: t,
		},
	}
}

type (
	FullTextCopyTask struct {
		*queue.DBTask
	}

	FullTextCopyTaskState struct {
		Uri            *fs.URI `json:"uri"`
		OriginalFileID int     `json:"original_file_id"`
		FileID         int     `json:"file_id"`
		OwnerID        int     `json:"owner_id"`
		EntityID       int     `json:"entity_id"`
	}
)

func NewFullTextCopyTask(ctx context.Context, uri *fs.URI, originalFileID, fileID, ownerID, entityID int, creator *ent.User) (*FullTextCopyTask, error) {
	state := &FullTextCopyTaskState{
		Uri:            uri,
		OriginalFileID: originalFileID,
		FileID:         fileID,
		OwnerID:        ownerID,
		EntityID:       entityID,
	}
	stateBytes, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal state: %w", err)
	}

	return &FullTextCopyTask{
		DBTask: &queue.DBTask{
			DirectOwner: creator,
			Task: &ent.Task{
				Type:          queue.FullTextCopyTaskType,
				CorrelationID: logging.CorrelationID(ctx),
				PrivateState:  string(stateBytes),
				PublicState:   &types.TaskPublicState{},
			},
		},
	}, nil
}

func NewFullTextCopyTaskFromModel(t *ent.Task) queue.Task {
	return &FullTextCopyTask{
		DBTask: &queue.DBTask{
			Task: t,
		},
	}
}

func (t *FullTextCopyTask) Do(ctx context.Context) (task.Status, error) {
	dep := dependency.FromContext(ctx)
	l := dep.Logger()
	fm := NewFileManager(dep, inventory.UserFromContext(ctx)).(*manager)

	if !fm.settings.FTSEnabled(ctx) {
		l.Debug("FTS disabled, skipping full text copy task.")
		return task.StatusCompleted, nil
	}

	var state FullTextCopyTaskState
	if err := json.Unmarshal([]byte(t.State()), &state); err != nil {
		return task.StatusError, fmt.Errorf("failed to unmarshal state: %s (%w)", err, queue.CriticalErr)
	}

	// Get fresh file to make sure task is not stale.
	file, err := fm.Get(ctx, state.Uri, dbfs.WithFilePublicMetadata())
	if err != nil {
		return task.StatusError, fmt.Errorf("failed to get latest file: %w", err)
	}

	if file.PrimaryEntityID() != state.EntityID {
		l.Debug("File %d entity changed, skipping copy index.", state.FileID)
		return task.StatusCompleted, nil
	}

	indexer := dep.SearchIndexer(ctx)
	if err := indexer.CopyByFileID(ctx, state.OriginalFileID, state.FileID, state.OwnerID, state.EntityID); err != nil {
		l.Warning("Failed to copy index from file %d to %d, falling back to full indexing: %s", state.OriginalFileID, state.FileID, err)
		return performIndexing(ctx, fm, state.Uri, state.EntityID, state.FileID, state.OwnerID, file.Name(), false)
	}

	// Patch metadata to mark file as indexed.
	if err := fm.fs.PatchMetadata(ctx, []*fs.URI{state.Uri}, fs.MetadataPatch{
		Key:   dbfs.FullTextIndexKey,
		Value: hashid.EncodeEntityID(fm.hasher, state.EntityID),
	}); err != nil {
		return task.StatusError, fmt.Errorf("failed to patch metadata: %w", err)
	}

	l.Debug("Successfully copied index from file %d to %d.", state.OriginalFileID, state.FileID)
	return task.StatusCompleted, nil
}

type (
	FullTextChangeOwnerTask struct {
		*queue.DBTask
	}

	FullTextChangeOwnerTaskState struct {
		Uri             *fs.URI `json:"uri"`
		EntityID        int     `json:"entity_id"`
		FileID          int     `json:"file_id"`
		OriginalOwnerID int     `json:"original_owner_id"`
		NewOwnerID      int     `json:"new_owner_id"`
	}
)

func NewFullTextChangeOwnerTask(ctx context.Context, uri *fs.URI, entityID, fileID, originalOwnerID, newOwnerID int, creator *ent.User) (*FullTextChangeOwnerTask, error) {
	state := &FullTextChangeOwnerTaskState{
		Uri:             uri,
		EntityID:        entityID,
		FileID:          fileID,
		OriginalOwnerID: originalOwnerID,
		NewOwnerID:      newOwnerID,
	}
	stateBytes, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal state: %w", err)
	}

	return &FullTextChangeOwnerTask{
		DBTask: &queue.DBTask{
			DirectOwner: creator,
			Task: &ent.Task{
				Type:          queue.FullTextChangeOwnerTaskType,
				CorrelationID: logging.CorrelationID(ctx),
				PrivateState:  string(stateBytes),
				PublicState:   &types.TaskPublicState{},
			},
		},
	}, nil
}

func NewFullTextChangeOwnerTaskFromModel(t *ent.Task) queue.Task {
	return &FullTextChangeOwnerTask{
		DBTask: &queue.DBTask{
			Task: t,
		},
	}
}

func (t *FullTextChangeOwnerTask) Do(ctx context.Context) (task.Status, error) {
	dep := dependency.FromContext(ctx)
	l := dep.Logger()
	fm := NewFileManager(dep, inventory.UserFromContext(ctx)).(*manager)

	if !fm.settings.FTSEnabled(ctx) {
		l.Debug("FTS disabled, skipping full text change owner task.")
		return task.StatusCompleted, nil
	}

	var state FullTextChangeOwnerTaskState
	if err := json.Unmarshal([]byte(t.State()), &state); err != nil {
		return task.StatusError, fmt.Errorf("failed to unmarshal state: %s (%w)", err, queue.CriticalErr)
	}

	// Get fresh file to make sure task is not stale.
	file, err := fm.Get(ctx, state.Uri, dbfs.WithFilePublicMetadata())
	if err != nil {
		return task.StatusError, fmt.Errorf("failed to get latest file: %w", err)
	}

	if file.PrimaryEntityID() != state.EntityID {
		l.Debug("File %d entity changed, skipping owner change.", state.FileID)
		return task.StatusCompleted, nil
	}

	indexer := dep.SearchIndexer(ctx)
	if err := indexer.ChangeOwner(ctx, state.FileID, state.OriginalOwnerID, state.NewOwnerID); err != nil {
		return task.StatusError, fmt.Errorf("failed to change owner for file %d: %w", state.FileID, err)
	}

	l.Debug("Successfully changed index owner for file %d from %d to %d.", state.FileID, state.OriginalOwnerID, state.NewOwnerID)
	return task.StatusCompleted, nil
}

type (
	FullTextDeleteTask struct {
		*queue.DBTask
	}

	FullTextDeleteTaskState struct {
		FileIDs []int `json:"file_ids"`
	}
)

func NewFullTextDeleteTask(ctx context.Context, fileIDs []int, creator *ent.User) (*FullTextDeleteTask, error) {
	state := &FullTextDeleteTaskState{
		FileIDs: fileIDs,
	}
	stateBytes, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal state: %w", err)
	}

	return &FullTextDeleteTask{
		DBTask: &queue.DBTask{
			DirectOwner: creator,
			Task: &ent.Task{
				Type:          queue.FullTextDeleteTaskType,
				CorrelationID: logging.CorrelationID(ctx),
				PrivateState:  string(stateBytes),
				PublicState:   &types.TaskPublicState{},
			},
		},
	}, nil
}

func NewFullTextDeleteTaskFromModel(t *ent.Task) queue.Task {
	return &FullTextDeleteTask{
		DBTask: &queue.DBTask{
			Task: t,
		},
	}
}

func (t *FullTextDeleteTask) Do(ctx context.Context) (task.Status, error) {
	dep := dependency.FromContext(ctx)
	l := dep.Logger()

	var state FullTextDeleteTaskState
	if err := json.Unmarshal([]byte(t.State()), &state); err != nil {
		return task.StatusError, fmt.Errorf("failed to unmarshal state: %s (%w)", err, queue.CriticalErr)
	}

	indexer := dep.SearchIndexer(ctx)
	if err := indexer.DeleteByFileIDs(ctx, state.FileIDs...); err != nil {
		return task.StatusError, fmt.Errorf("failed to delete index for %d file(s): %w", len(state.FileIDs), err)
	}

	l.Debug("Successfully deleted index for %d file(s).", len(state.FileIDs))
	return task.StatusCompleted, nil
}

func (t *FullTextIndexTask) Do(ctx context.Context) (task.Status, error) {
	dep := dependency.FromContext(ctx)
	l := dep.Logger()
	fm := NewFileManager(dep, inventory.UserFromContext(ctx)).(*manager)

	// Check FTS enabled
	if !fm.settings.FTSEnabled(ctx) {
		l.Debug("FTS disabled, skipping full text index task.")
		return task.StatusCompleted, nil
	}

	// Unmarshal state
	var state FullTextIndexTaskState
	if err := json.Unmarshal([]byte(t.State()), &state); err != nil {
		return task.StatusError, fmt.Errorf("failed to unmarshal state: %s (%w)", err, queue.CriticalErr)
	}

	// Get fresh file to make sure task is not stale
	file, err := fm.Get(ctx, state.Uri, dbfs.WithFilePublicMetadata())
	if err != nil {
		return task.StatusError, fmt.Errorf("failed to get latest file: %w", err)
	}

	if file.PrimaryEntityID() != state.EntityID {
		l.Debug("File %d is not the latest version, skipping indexing.", state.FileID)
		return task.StatusCompleted, nil
	}

	deleteOldChunks := false
	if _, ok := file.Metadata()[dbfs.FullTextIndexKey]; ok {
		deleteOldChunks = true
	}

	return performIndexing(ctx, fm, state.Uri, state.EntityID, state.FileID, state.OwnerID, state.Uri.Name(), deleteOldChunks)
}

// performIndexing extracts text from the entity and indexes it. This is shared between
// the regular index task and the copy task (as a fallback when copy fails).
func performIndexing(ctx context.Context, fm *manager, uri *fs.URI, entityID, fileID, ownerID int, fileName string, deleteOldChunks bool) (task.Status, error) {
	dep := dependency.FromContext(ctx)
	l := dep.Logger()

	// Get entity source
	source, err := fm.GetEntitySource(ctx, entityID)
	if err != nil {
		return task.StatusError, fmt.Errorf("failed to get entity source: %w", err)
	}
	defer source.Close()

	// Extract text
	var text string
	if source.Entity().Size() > 0 {
		extractor := dep.TextExtractor(ctx)
		text, err = extractor.Extract(ctx, source)
		if err != nil {
			l.Warning("Failed to extract text for file %d: %s", fileID, err)
			return task.StatusCompleted, nil
		}
	}

	indexer := dep.SearchIndexer(ctx)

	// Delete old chunks first so that stale chunks from a previously longer
	// version of the file are removed before upserting the new (possibly fewer)
	// chunks.
	if deleteOldChunks {
		if err := indexer.DeleteByFileIDs(ctx, fileID); err != nil {
			l.Warning("Failed to delete old index chunks for file %d: %s", fileID, err)
		}
	}

	// Index via SearchIndexer
	if err := indexer.IndexFile(ctx, ownerID, fileID, entityID, fileName, text); err != nil {
		return task.StatusError, fmt.Errorf("failed to index file %d: %w", fileID, err)
	}

	// Upsert metadata
	if err := fm.fs.PatchMetadata(ctx, []*fs.URI{uri}, fs.MetadataPatch{
		Key:   dbfs.FullTextIndexKey,
		Value: hashid.EncodeEntityID(fm.hasher, entityID),
	}); err != nil {
		return task.StatusError, fmt.Errorf("failed to patch metadata: %w", err)
	}

	l.Debug("Successfully indexed file %d for owner %d.", fileID, ownerID)
	return task.StatusCompleted, nil
}

// ShouldExtractText checks if a file is eligible for text extraction based on
// the extractor's supported extensions and max file size. This is exported for
// use by the rebuild index workflow.
func ShouldExtractText(extractor searcher.TextExtractor, fileName string, size int64) bool {
	return util.IsInExtensionList(extractor.Exts(), fileName) && extractor.MaxFileSize() > size
}

// shouldIndexFullText checks if a file should be indexed for full-text search.
func (m *manager) shouldIndexFullText(ctx context.Context, fileName string, size int64) bool {
	if !m.settings.FTSEnabled(ctx) {
		return false
	}

	extractor := m.dep.TextExtractor(ctx)
	return ShouldExtractText(extractor, fileName, size)
}

// fullTextIndexForNewEntity creates and queues a full text index task for a newly uploaded entity.
func (m *manager) fullTextIndexForNewEntity(ctx context.Context, session *fs.UploadSession, owner int) {
	if session.Props.EntityType != nil && *session.Props.EntityType != types.EntityTypeVersion {
		return
	}

	if !m.shouldIndexFullText(ctx, session.Props.Uri.Name(), session.Props.Size) {
		return
	}

	t, err := NewFullTextIndexTask(ctx, session.Props.Uri, session.EntityID, session.FileID, owner, m.user)
	if err != nil {
		m.l.Warning("Failed to create full text index task: %s", err)
		return
	}
	if err := m.dep.MediaMetaQueue(ctx).QueueTask(ctx, t); err != nil {
		m.l.Warning("Failed to queue full text index task: %s", err)
	}
}

func (m *manager) processIndexDiff(ctx context.Context, diff *fs.IndexDiff) {
	if diff == nil {
		return
	}

	for _, update := range diff.IndexToUpdate {
		t, err := NewFullTextIndexTask(ctx, &update.Uri, update.EntityID, update.FileID, update.OwnerID, m.user)
		if err != nil {
			m.l.Warning("Failed to create full text update task: %s", err)
			continue
		}
		if err := m.dep.MediaMetaQueue(ctx).QueueTask(ctx, t); err != nil {
			m.l.Warning("Failed to queue full text update task: %s", err)
		}
	}

	for _, cp := range diff.IndexToCopy {
		t, err := NewFullTextCopyTask(ctx, &cp.Uri, cp.OriginalFileID, cp.FileID, cp.OwnerID, cp.EntityID, m.user)
		if err != nil {
			m.l.Warning("Failed to create full text copy task: %s", err)
			continue
		}
		if err := m.dep.MediaMetaQueue(ctx).QueueTask(ctx, t); err != nil {
			m.l.Warning("Failed to queue full text copy task: %s", err)
		}
	}

	for _, change := range diff.IndexToChangeOwner {
		t, err := NewFullTextChangeOwnerTask(ctx, &change.Uri, change.EntityID, change.FileID, change.OriginalOwnerID, change.NewOwnerID, m.user)
		if err != nil {
			m.l.Warning("Failed to create full text change owner task: %s", err)
			continue
		}
		if err := m.dep.MediaMetaQueue(ctx).QueueTask(ctx, t); err != nil {
			m.l.Warning("Failed to queue full text change owner task: %s", err)
		}
	}

	if len(diff.IndexToDelete) > 0 && m.dep.SettingProvider().FTSEnabled(ctx) {
		t, err := NewFullTextDeleteTask(ctx, diff.IndexToDelete, m.user)
		if err != nil {
			m.l.Warning("Failed to create full text delete task: %s", err)
			return
		}
		if err := m.dep.MediaMetaQueue(ctx).QueueTask(ctx, t); err != nil {
			m.l.Warning("Failed to queue full text delete task: %s", err)
		}
	}

	ctx = context.WithoutCancel(ctx)
	indexer := m.dep.SearchIndexer(ctx)
	go func() {
		for _, rename := range diff.IndexToRename {
			if err := indexer.Rename(ctx, rename.FileID, rename.EntityID, rename.Uri.Name()); err != nil {
				m.l.Warning("Failed to rename index for file %d: %s", rename.FileID, err)
			}
		}
	}()
}
