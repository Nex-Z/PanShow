package model

import "time"

const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

type User struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"size:80;uniqueIndex;not null" json:"username"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	Role         string    `gorm:"size:20;not null;index" json:"role"`
	Active       bool      `gorm:"not null;default:true" json:"active"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type DirectoryPassword struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Path         string    `gorm:"size:1024;uniqueIndex;index:idx_enabled_path,sort:asc;not null" json:"path"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	Enabled      bool      `gorm:"not null;default:true;index:idx_enabled_path,sort:asc" json:"enabled"`
	Version      uint      `gorm:"not null;default:1" json:"version"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type Announcement struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Title     string    `gorm:"size:160;not null" json:"title"`
	Pattern   string    `gorm:"size:1024;not null;index:idx_announcements_enabled_pattern,sort:asc" json:"pattern"`
	Content   string    `gorm:"type:text;not null" json:"content"`
	Enabled   bool      `gorm:"not null;default:true;index:idx_announcements_enabled_pattern,sort:asc" json:"enabled"`
	SortOrder int       `gorm:"not null;default:100;index" json:"sortOrder"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type SiteConfig struct {
	Key       string    `gorm:"primaryKey;size:120" json:"key"`
	Value     string    `gorm:"type:text" json:"value"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type FileIndexEntry struct {
	Path                string     `gorm:"primaryKey;size:2048" json:"path"`
	ParentPath          string     `gorm:"size:2048;not null;index:idx_file_index_parent,priority:1" json:"parentPath"`
	Name                string     `gorm:"size:512;not null;index:idx_file_index_parent,priority:2" json:"name"`
	IsDir               bool       `gorm:"not null;index:idx_file_index_parent,priority:3" json:"isDir"`
	Size                int64      `gorm:"not null;default:0" json:"size"`
	LastModified        *time.Time `json:"lastModified,omitempty"`
	ContentType         string     `gorm:"size:255" json:"contentType,omitempty"`
	MetadataUnavailable bool       `gorm:"not null;default:false" json:"metadataUnavailable,omitempty"`
	IndexedAt           time.Time  `gorm:"not null;index" json:"indexedAt"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}

type FileIndexDir struct {
	Path       string    `gorm:"primaryKey;size:2048" json:"path"`
	SyncedAt   time.Time `gorm:"not null;index" json:"syncedAt"`
	EntryCount int       `gorm:"not null;default:0" json:"entryCount"`
	LastError  string    `gorm:"type:text" json:"lastError,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}
