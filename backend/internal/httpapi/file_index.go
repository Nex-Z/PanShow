package httpapi

import (
	"context"
	"errors"
	"log"
	pathpkg "path"
	"sort"
	"strings"
	"sync"
	"time"

	"panshow/backend/internal/model"
	"panshow/backend/internal/storage"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type fileIndexStore struct {
	db *gorm.DB
}

func newFileIndexStore(db *gorm.DB) *fileIndexStore {
	return &fileIndexStore{db: db}
}

func (store *fileIndexStore) ListDirectory(ctx context.Context, dir string) ([]storage.FileEntry, string, bool, error) {
	if store == nil || store.db == nil {
		return nil, "", false, nil
	}

	var indexedDir model.FileIndexDir
	if err := store.db.WithContext(ctx).First(&indexedDir, "path = ?", dir).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return store.ListSyntheticDirectory(ctx, dir)
		}
		return nil, "", false, err
	}

	var rows []model.FileIndexEntry
	if err := store.db.WithContext(ctx).
		Where("parent_path = ?", dir).
		Order("is_dir DESC, name ASC, path ASC").
		Find(&rows).Error; err != nil {
		return nil, "", false, err
	}

	entries := make([]storage.FileEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, fileIndexEntryToStorage(row))
	}
	return entries, "postgres", true, nil
}

func (store *fileIndexStore) ListSyntheticDirectory(ctx context.Context, dir string) ([]storage.FileEntry, string, bool, error) {
	if store == nil || store.db == nil {
		return nil, "", false, nil
	}

	prefix := indexedDirChildPrefix(dir)
	var rows []model.FileIndexDir
	if err := store.db.WithContext(ctx).
		Select("path").
		Where("path LIKE ?", prefix+"%").
		Order("path ASC").
		Find(&rows).Error; err != nil {
		return nil, "", false, err
	}

	paths := make([]string, 0, len(rows))
	for _, row := range rows {
		paths = append(paths, row.Path)
	}
	entries := syntheticDirectoryEntries(dir, paths)
	if len(entries) == 0 {
		return nil, "", false, nil
	}
	return entries, "synthetic", true, nil
}

func (store *fileIndexStore) StatFile(ctx context.Context, filePath string) (storage.FileEntry, bool, error) {
	if store == nil || store.db == nil {
		return storage.FileEntry{}, false, nil
	}

	var row model.FileIndexEntry
	if err := store.db.WithContext(ctx).First(&row, "path = ? AND is_dir = ?", filePath, false).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return storage.FileEntry{}, false, nil
		}
		return storage.FileEntry{}, false, err
	}
	return fileIndexEntryToStorage(row), true, nil
}

func (store *fileIndexStore) DirectoryIndexed(ctx context.Context, dir string) (bool, error) {
	if store == nil || store.db == nil {
		return false, nil
	}

	var count int64
	if err := store.db.WithContext(ctx).
		Model(&model.FileIndexDir{}).
		Where("path = ?", dir).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (store *fileIndexStore) ReplaceDirectory(ctx context.Context, dir string, entries []storage.FileEntry, now time.Time) (bool, int, error) {
	if store == nil || store.db == nil {
		return false, 0, nil
	}
	if len(entries) == 0 {
		result := store.db.WithContext(ctx).
			Model(&model.FileIndexDir{}).
			Where("path = ?", dir).
			Updates(map[string]any{
				"synced_at":  now,
				"last_error": "refresh returned empty result; kept existing index",
			})
		return false, 0, result.Error
	}

	rows := fileIndexRowsFromEntries(dir, entries, now)
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("parent_path = ?", dir).Delete(&model.FileIndexEntry{}).Error; err != nil {
			return err
		}
		if err := tx.CreateInBatches(rows, 200).Error; err != nil {
			return err
		}
		indexedDir := model.FileIndexDir{
			Path:       dir,
			SyncedAt:   now,
			EntryCount: len(rows),
			LastError:  "",
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "path"}},
			DoUpdates: clause.Assignments(map[string]any{
				"synced_at":   now,
				"entry_count": len(rows),
				"last_error":  "",
			}),
		}).Create(&indexedDir).Error
	})
	if err != nil {
		return false, 0, err
	}
	return true, len(rows), nil
}

func (store *fileIndexStore) RecordRefreshError(ctx context.Context, dir string, refreshErr error) {
	if store == nil || store.db == nil || refreshErr == nil {
		return
	}
	if err := store.db.WithContext(ctx).
		Model(&model.FileIndexDir{}).
		Where("path = ?", dir).
		Update("last_error", refreshErr.Error()).Error; err != nil {
		log.Printf("file index error update failed dir=%q: %v", dir, err)
	}
}

func fileIndexRowsFromEntries(dir string, entries []storage.FileEntry, now time.Time) []model.FileIndexEntry {
	rows := make([]model.FileIndexEntry, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name
		if name == "" {
			name = pathpkg.Base(entry.Path)
		}
		rows = append(rows, model.FileIndexEntry{
			Path:                entry.Path,
			ParentPath:          dir,
			Name:                name,
			IsDir:               entry.IsDir,
			Size:                entry.Size,
			LastModified:        entry.LastModified,
			ContentType:         entry.ContentType,
			MetadataUnavailable: entry.MetadataUnavailable,
			IndexedAt:           now,
		})
	}
	return rows
}

func fileIndexEntryToStorage(row model.FileIndexEntry) storage.FileEntry {
	return storage.FileEntry{
		Name:                row.Name,
		Path:                row.Path,
		IsDir:               row.IsDir,
		Size:                row.Size,
		LastModified:        row.LastModified,
		ContentType:         row.ContentType,
		MetadataUnavailable: row.MetadataUnavailable,
	}
}

func indexedDirChildPrefix(dir string) string {
	if dir == "/" {
		return "/"
	}
	return strings.TrimRight(dir, "/") + "/"
}

func syntheticDirectoryEntries(dir string, indexedDirs []string) []storage.FileEntry {
	prefix := indexedDirChildPrefix(dir)
	children := make(map[string]storage.FileEntry)
	for _, indexedDir := range indexedDirs {
		if !strings.HasPrefix(indexedDir, prefix) || indexedDir == dir {
			continue
		}
		remainder := strings.TrimPrefix(indexedDir, prefix)
		name, _, _ := strings.Cut(remainder, "/")
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		childPath := pathpkg.Join(dir, name)
		if !strings.HasPrefix(childPath, "/") {
			childPath = "/" + childPath
		}
		children[childPath] = storage.FileEntry{
			Name:  name,
			Path:  childPath,
			IsDir: true,
		}
	}

	paths := make([]string, 0, len(children))
	for childPath := range children {
		paths = append(paths, childPath)
	}
	sort.Strings(paths)

	entries := make([]storage.FileEntry, 0, len(paths))
	for _, childPath := range paths {
		entries = append(entries, children[childPath])
	}
	return entries
}

func todayIndexPath(now time.Time, location *time.Location) string {
	if location == nil {
		location = time.Local
	}
	return now.In(location).Format("/2006/01/02")
}

func (api *API) indexEnabled() bool {
	return api.cfg.IndexEnabled && api.fileIndex != nil && api.fileIndex.db != nil
}

func (api *API) indexedFilesResponse(ctx context.Context, dir string) (filesResponse, string, bool, error) {
	entries, source, ok, err := api.fileIndex.ListDirectory(ctx, dir)
	if err != nil || !ok {
		return filesResponse{}, source, ok, err
	}
	return filesResponse{Path: dir, Entries: entries}, source, true, nil
}

func (api *API) indexedFileDetail(ctx context.Context, filePath string) (fileDetailResponse, bool, error) {
	entry, ok, err := api.fileIndex.StatFile(ctx, filePath)
	if err != nil || !ok {
		return fileDetailResponse{}, ok, err
	}
	return fileDetailResponse{File: entry}, true, nil
}

func (api *API) refreshDirectoryIndex(ctx context.Context, dir string) (bool, int, error) {
	if !api.indexEnabled() || api.storage == nil {
		return false, 0, nil
	}
	unlock := api.lockDirectoryIndexRefresh(dir)
	defer unlock()

	log.Printf("file index refresh start dir=%q", dir)
	entries, err := api.storage.List(ctx, dir)
	if err != nil {
		api.fileIndex.RecordRefreshError(ctx, dir, err)
		log.Printf("file index refresh failed dir=%q: %v", dir, err)
		return false, 0, err
	}

	indexed, count, err := api.fileIndex.ReplaceDirectory(ctx, dir, entries, time.Now())
	if err != nil {
		log.Printf("file index store failed dir=%q: %v", dir, err)
		return false, 0, err
	}
	if !indexed {
		log.Printf("file index refresh empty dir=%q", dir)
		return false, 0, nil
	}

	patterns := cacheDeletePatterns(dir)
	api.r2Cache.DeletePatterns(patterns...)
	if err := api.session.DeleteCachePatterns(ctx, patterns...); err != nil {
		log.Printf("file index cache clear failed dir=%q: %v", dir, err)
	}
	api.storeCachedJSONContext(ctx, listCacheKey(dir), filesResponse{Path: dir, Entries: entries})

	log.Printf("file index refresh success dir=%q entries=%d", dir, count)
	return true, count, nil
}

func (api *API) lockDirectoryIndexRefresh(dir string) func() {
	api.indexLocksM.Lock()
	if api.indexLocks == nil {
		api.indexLocks = make(map[string]*sync.Mutex)
	}
	lock := api.indexLocks[dir]
	if lock == nil {
		lock = &sync.Mutex{}
		api.indexLocks[dir] = lock
	}
	api.indexLocksM.Unlock()

	lock.Lock()
	return lock.Unlock
}

func (api *API) startFileIndexRefresher() {
	if !api.indexEnabled() || api.storage == nil || api.cfg.IndexTodayRefreshInterval <= 0 {
		return
	}
	location := api.indexLocation()
	run := func() {
		ctx, cancel := context.WithTimeout(context.Background(), api.indexRefreshTimeout())
		defer cancel()

		dir := todayIndexPath(time.Now(), location)
		if _, _, err := api.refreshDirectoryIndex(ctx, dir); err != nil {
			log.Printf("today file index refresh failed dir=%q: %v", dir, err)
		}
	}

	go func() {
		if api.cfg.IndexRefreshOnStart {
			run()
		}
		ticker := time.NewTicker(api.cfg.IndexTodayRefreshInterval)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	}()
}

func (api *API) indexLocation() *time.Location {
	name := api.cfg.IndexTimezone
	if name == "" {
		name = "Asia/Shanghai"
	}
	location, err := time.LoadLocation(name)
	if err == nil {
		return location
	}
	if name == "Asia/Shanghai" {
		log.Printf("load index timezone failed name=%q: %v; using fixed UTC+8", name, err)
		return time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	log.Printf("load index timezone failed name=%q: %v; using local timezone", name, err)
	return time.Local
}

func (api *API) indexRefreshTimeout() time.Duration {
	if api.cfg.R2RequestTimeout > 0 {
		return api.cfg.R2RequestTimeout + 30*time.Second
	}
	return 2 * time.Minute
}
