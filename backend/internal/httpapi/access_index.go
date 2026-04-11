package httpapi

import (
	"context"
	"sync"
	"time"

	"panshow/backend/internal/model"
	"panshow/backend/internal/service"

	"gorm.io/gorm"
)

const directoryAccessIndexTTL = 30 * time.Second

type directoryAccessRule struct {
	Path    string
	Version uint
}

type directoryAccessIndex struct {
	db  *gorm.DB
	ttl time.Duration

	mu        sync.RWMutex
	rules     map[string]directoryAccessRule
	version   int64
	expiresAt time.Time
}

func newDirectoryAccessIndex(db *gorm.DB, ttl time.Duration) *directoryAccessIndex {
	return &directoryAccessIndex{
		db:    db,
		ttl:   ttl,
		rules: make(map[string]directoryAccessRule),
	}
}

func (idx *directoryAccessIndex) RulesFor(ctx context.Context, dir string, version int64) ([]directoryAccessRule, error) {
	rulesByPath, err := idx.snapshot(ctx, version)
	if err != nil {
		return nil, err
	}

	ancestors := service.DirectoryAncestors(dir)
	rules := make([]directoryAccessRule, 0, len(ancestors))
	for _, ancestor := range ancestors {
		if rule, ok := rulesByPath[ancestor]; ok {
			rules = append(rules, rule)
		}
	}
	return rules, nil
}

func (idx *directoryAccessIndex) Invalidate() {
	idx.mu.Lock()
	idx.expiresAt = time.Time{}
	idx.mu.Unlock()
}

func (idx *directoryAccessIndex) snapshot(ctx context.Context, version int64) (map[string]directoryAccessRule, error) {
	now := time.Now()
	idx.mu.RLock()
	if idx.version == version && now.Before(idx.expiresAt) {
		rules := idx.rules
		idx.mu.RUnlock()
		return rules, nil
	}
	idx.mu.RUnlock()

	idx.mu.Lock()
	defer idx.mu.Unlock()

	now = time.Now()
	if idx.version == version && now.Before(idx.expiresAt) {
		return idx.rules, nil
	}

	var rows []directoryAccessRule
	if err := idx.db.Model(&model.DirectoryPassword{}).
		Select("path", "version").
		Where("enabled = ?", true).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	rules := make(map[string]directoryAccessRule, len(rows))
	for _, row := range rows {
		rules[row.Path] = row
	}
	idx.rules = rules
	idx.version = version
	idx.expiresAt = time.Now().Add(idx.ttl)
	return idx.rules, nil
}
