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
	PinnedAt          *time.Time
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

type ExpiredUploadChunk struct {
	ID         string
	StorageKey string
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

type Device struct {
	ID                  string
	UserID              string
	Name                string
	Platform            string
	LastSeenAt          *time.Time
	LastAppliedChangeID int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type SyncConflict struct {
	ID            string
	UserID        string
	FileID        *string
	Path          string
	LocalVersion  *int64
	RemoteVersion *int64
	Resolution    string
	CreatedAt     time.Time
	ResolvedAt    *time.Time
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

	ConflictResolutionPending    = "pending"
	ConflictResolutionKeepLocal  = "keep_local"
	ConflictResolutionKeepRemote = "keep_remote"
	ConflictResolutionKeepBoth   = "keep_both"
)
