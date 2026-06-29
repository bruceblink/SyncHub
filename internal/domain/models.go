package domain

import "time"

type User struct {
	ID           string
	Email        string
	PasswordHash string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type RefreshToken struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

type FileNode struct {
	ID               string
	UserID           string
	ParentID         *string
	Name             string
	Path             string
	NodeType         string
	CurrentVersionID *string
	Size             int64
	SHA256           *string
	StorageKey       *string
	Version          int64
	DeletedAt        *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type FileVersion struct {
	ID                string
	FileID            string
	UserID            string
	Version           int64
	Size              int64
	SHA256            string
	StorageKey        string
	CreatedByDeviceID *string
	CreatedAt         time.Time
}

type UploadSession struct {
	ID             string
	UserID         string
	TargetPath     string
	TargetFileID   *string
	BaseVersion    *int64
	TotalSize      int64
	ChunkSize      int32
	SHA256         string
	Status         string
	StagingKey     string
	ExpiresAt      time.Time
	IdempotencyKey *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type UploadChunk struct {
	ID         string
	UploadID   string
	ChunkIndex int32
	Size       int32
	SHA256     string
	StorageKey string
	CreatedAt  time.Time
}

type ChangeEvent struct {
	ID             int64
	UserID         string
	FileID         string
	EventType      string
	Version        *int64
	Path           string
	OldPath        *string
	SourceDeviceID *string
	CreatedAt      time.Time
}

const (
	NodeTypeFile      = "file"
	NodeTypeDirectory = "directory"

	UploadStatusPending   = "pending"
	UploadStatusCommitted = "committed"
	UploadStatusExpired   = "expired"
	UploadStatusAborted   = "aborted"

	EventCreate  = "create"
	EventUpdate  = "update"
	EventMove    = "move"
	EventDelete  = "delete"
	EventRestore = "restore"
)
