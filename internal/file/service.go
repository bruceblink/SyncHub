package file

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"path"
	"strings"
	"time"

	"github.com/bruceblink/SyncHub/internal/domain"
	"github.com/bruceblink/SyncHub/internal/storage"
)

type Repository interface {
	CreateDirectory(ctx context.Context, userID, path, name string, parentID *string) (domain.FileNode, error)
	GetFileByID(ctx context.Context, userID, fileID string) (domain.FileNode, error)
	GetFileByPath(ctx context.Context, userID, path string) (domain.FileNode, error)
	ListFiles(ctx context.Context, userID string, parentID *string, limit int32) ([]domain.FileNode, error)
	ListFileVersions(ctx context.Context, userID, fileID string, limit int32) ([]domain.FileVersion, error)
	RestoreFileVersion(ctx context.Context, userID, fileID string, version int64) (domain.FileNode, int64, error)
	MoveFile(ctx context.Context, userID, fileID, newPath, newName string, newParentID *string) (domain.FileNode, error)
	DeleteFile(ctx context.Context, userID, fileID string) error
	CreateUploadSession(ctx context.Context, s domain.UploadSession) (domain.UploadSession, error)
	GetUploadSession(ctx context.Context, userID, uploadID string) (domain.UploadSession, error)
	PutUploadChunk(ctx context.Context, uploadID string, chunkIndex, size int32, sha256sum, storageKey string) (domain.UploadChunk, error)
	ListUploadChunks(ctx context.Context, uploadID string) ([]domain.UploadChunk, error)
	CommitUpload(ctx context.Context, userID, uploadID, storageKey string) (domain.FileNode, int64, error)
}

type Service struct {
	repo             Repository
	store            storage.Storage
	chunkSize        int64
	uploadSessionTTL time.Duration
}

func NewService(repo Repository, store storage.Storage, chunkSize int64, ttl time.Duration) *Service {
	return &Service{repo: repo, store: store, chunkSize: chunkSize, uploadSessionTTL: ttl}
}

func (s *Service) CreateDirectory(ctx context.Context, userID, targetPath string) (domain.FileNode, error) {
	normalized, err := domain.NormalizePath(targetPath)
	if err != nil {
		return domain.FileNode{}, err
	}
	parentPath, name, err := domain.SplitPath(normalized)
	if err != nil {
		return domain.FileNode{}, err
	}
	var parentID *string
	if parentPath != "/" {
		parent, err := s.repo.GetFileByPath(ctx, userID, parentPath)
		if err != nil {
			return domain.FileNode{}, err
		}
		if parent.NodeType != domain.NodeTypeDirectory {
			return domain.FileNode{}, domain.E(domain.CodeInvalidArgument, "parent is not a directory", nil)
		}
		parentID = &parent.ID
	}
	return s.repo.CreateDirectory(ctx, userID, normalized, name, parentID)
}

func (s *Service) GetByID(ctx context.Context, userID, fileID string) (domain.FileNode, error) {
	return s.repo.GetFileByID(ctx, userID, fileID)
}

func (s *Service) GetByPath(ctx context.Context, userID, p string) (domain.FileNode, error) {
	normalized, err := domain.NormalizePath(p)
	if err != nil {
		return domain.FileNode{}, err
	}
	return s.repo.GetFileByPath(ctx, userID, normalized)
}

func (s *Service) List(ctx context.Context, userID string, parentID *string, limit int32) ([]domain.FileNode, error) {
	return s.repo.ListFiles(ctx, userID, parentID, limit)
}

func (s *Service) Versions(ctx context.Context, userID, fileID string, limit int32) ([]domain.FileVersion, error) {
	if strings.TrimSpace(fileID) == "" {
		return nil, domain.E(domain.CodeInvalidArgument, "file id is required", nil)
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	node, err := s.repo.GetFileByID(ctx, userID, fileID)
	if err != nil {
		return nil, err
	}
	if node.NodeType != domain.NodeTypeFile {
		return nil, domain.E(domain.CodeInvalidArgument, "file versions are only available for files", nil)
	}
	return s.repo.ListFileVersions(ctx, userID, fileID, limit)
}

func (s *Service) RestoreVersion(ctx context.Context, userID, fileID string, version int64) (domain.FileNode, int64, error) {
	if strings.TrimSpace(fileID) == "" {
		return domain.FileNode{}, 0, domain.E(domain.CodeInvalidArgument, "file id is required", nil)
	}
	if version <= 0 {
		return domain.FileNode{}, 0, domain.E(domain.CodeInvalidArgument, "version must be positive", nil)
	}
	node, err := s.repo.GetFileByID(ctx, userID, fileID)
	if err != nil {
		return domain.FileNode{}, 0, err
	}
	if node.NodeType != domain.NodeTypeFile {
		return domain.FileNode{}, 0, domain.E(domain.CodeInvalidArgument, "only files can be restored", nil)
	}
	return s.repo.RestoreFileVersion(ctx, userID, fileID, version)
}

func (s *Service) Move(ctx context.Context, userID, fileID, newPath string) (domain.FileNode, error) {
	normalized, err := domain.NormalizePath(newPath)
	if err != nil {
		return domain.FileNode{}, err
	}
	parentPath, name, err := domain.SplitPath(normalized)
	if err != nil {
		return domain.FileNode{}, err
	}
	var parentID *string
	if parentPath != "/" {
		parent, err := s.repo.GetFileByPath(ctx, userID, parentPath)
		if err != nil {
			return domain.FileNode{}, err
		}
		parentID = &parent.ID
	}
	return s.repo.MoveFile(ctx, userID, fileID, normalized, name, parentID)
}

func (s *Service) Delete(ctx context.Context, userID, fileID string) error {
	return s.repo.DeleteFile(ctx, userID, fileID)
}

func (s *Service) InitUpload(ctx context.Context, userID, targetPath string, size int64, sha256sum string, requestedChunkSize int64, baseVersion *int64, idempotencyKey string) (domain.UploadSession, error) {
	normalized, err := domain.NormalizePath(targetPath)
	if err != nil {
		return domain.UploadSession{}, err
	}
	if size < 0 || sha256sum == "" {
		return domain.UploadSession{}, domain.E(domain.CodeInvalidArgument, "size and sha256 are required", nil)
	}
	chunkSize := requestedChunkSize
	if chunkSize <= 0 {
		chunkSize = s.chunkSize
	}
	if chunkSize <= 0 || chunkSize > math.MaxInt32 {
		return domain.UploadSession{}, domain.E(domain.CodeInvalidArgument, "invalid chunk size", nil)
	}
	existing, err := s.repo.GetFileByPath(ctx, userID, normalized)
	var targetFileID *string
	if err == nil {
		targetFileID = &existing.ID
	}
	var key *string
	if trimmed := strings.TrimSpace(idempotencyKey); trimmed != "" {
		key = &trimmed
	}
	session := domain.UploadSession{
		UserID:         userID,
		TargetPath:     normalized,
		TargetFileID:   targetFileID,
		BaseVersion:    baseVersion,
		TotalSize:      size,
		ChunkSize:      int32(chunkSize),
		SHA256:         sha256sum,
		ExpiresAt:      time.Now().Add(s.uploadSessionTTL),
		IdempotencyKey: key,
	}
	return s.repo.CreateUploadSession(ctx, session)
}

func (s *Service) PutChunk(ctx context.Context, userID, uploadID string, chunkIndex int32, r io.Reader, checksum string) (domain.UploadChunk, error) {
	session, err := s.repo.GetUploadSession(ctx, userID, uploadID)
	if err != nil {
		return domain.UploadChunk{}, err
	}
	if session.Status != domain.UploadStatusPending || time.Now().After(session.ExpiresAt) {
		return domain.UploadChunk{}, domain.E(domain.CodeUploadSessionExpired, "upload session is not active", nil)
	}
	key := fmt.Sprintf("%s/%d", session.StagingKey, chunkIndex)
	counter := &countingReader{r: r}
	if err := s.store.PutChunk(ctx, key, counter, checksum); err != nil {
		return domain.UploadChunk{}, domain.E(domain.CodeUploadChecksumMismatch, "chunk checksum mismatch", err)
	}
	if counter.n > math.MaxInt32 {
		return domain.UploadChunk{}, domain.E(domain.CodeInvalidArgument, "chunk too large", nil)
	}
	return s.repo.PutUploadChunk(ctx, uploadID, chunkIndex, int32(counter.n), checksum, key)
}

func (s *Service) UploadStatus(ctx context.Context, userID, uploadID string) (domain.UploadSession, []domain.UploadChunk, error) {
	session, err := s.repo.GetUploadSession(ctx, userID, uploadID)
	if err != nil {
		return domain.UploadSession{}, nil, err
	}
	chunks, err := s.repo.ListUploadChunks(ctx, uploadID)
	return session, chunks, err
}

func (s *Service) CommitUpload(ctx context.Context, userID, uploadID string) (domain.FileNode, int64, error) {
	session, chunks, err := s.UploadStatus(ctx, userID, uploadID)
	if err != nil {
		return domain.FileNode{}, 0, err
	}
	if session.Status == domain.UploadStatusCommitted {
		node, err := s.repo.GetFileByPath(ctx, userID, session.TargetPath)
		return node, 0, err
	}
	expectedChunks := int(math.Ceil(float64(session.TotalSize) / float64(session.ChunkSize)))
	if session.TotalSize == 0 {
		expectedChunks = 1
	}
	if len(chunks) != expectedChunks {
		return domain.FileNode{}, 0, domain.E(domain.CodeInvalidArgument, "missing upload chunks", nil)
	}
	chunkKeys := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		if int(chunk.ChunkIndex) != i {
			return domain.FileNode{}, 0, domain.E(domain.CodeInvalidArgument, "upload chunks are not contiguous", nil)
		}
		chunkKeys = append(chunkKeys, chunk.StorageKey)
	}
	targetKey := objectKey(userID, session.SHA256)
	if err := s.store.Compose(ctx, targetKey, chunkKeys); err != nil {
		return domain.FileNode{}, 0, err
	}
	rc, _, err := s.store.Read(ctx, targetKey, nil)
	if err != nil {
		return domain.FileNode{}, 0, err
	}
	h := sha256.New()
	_, copyErr := io.Copy(h, rc)
	closeErr := rc.Close()
	if copyErr != nil {
		return domain.FileNode{}, 0, copyErr
	}
	if closeErr != nil {
		return domain.FileNode{}, 0, closeErr
	}
	if hex.EncodeToString(h.Sum(nil)) != session.SHA256 {
		return domain.FileNode{}, 0, domain.E(domain.CodeUploadChecksumMismatch, "file checksum mismatch", nil)
	}
	return s.repo.CommitUpload(ctx, userID, uploadID, targetKey)
}

func (s *Service) Download(ctx context.Context, userID, fileID string, br *storage.ByteRange) (io.ReadCloser, storage.ObjectInfo, domain.FileNode, error) {
	node, err := s.repo.GetFileByID(ctx, userID, fileID)
	if err != nil {
		return nil, storage.ObjectInfo{}, domain.FileNode{}, err
	}
	if node.StorageKey == nil {
		return nil, storage.ObjectInfo{}, domain.FileNode{}, domain.E(domain.CodeFileNotFound, "file has no content", nil)
	}
	rc, info, err := s.store.Read(ctx, *node.StorageKey, br)
	return rc, info, node, err
}

func objectKey(userID, sha256sum string) string {
	prefix := sha256sum
	if len(prefix) > 2 {
		prefix = prefix[:2]
	}
	return path.Join("objects", userID, prefix, sha256sum)
}

type countingReader struct {
	r io.Reader
	n int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.n += int64(n)
	return n, err
}
