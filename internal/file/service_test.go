package file

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bruceblink/SyncHub/internal/domain"
)

func TestCreateDirectoryRequiresDirectoryParent(t *testing.T) {
	repo := &fakeRepo{
		filesByPath: map[string]domain.FileNode{
			"/workspace/readme.txt": {ID: "file-1", Path: "/workspace/readme.txt", NodeType: domain.NodeTypeFile},
		},
	}
	service := NewService(repo, nil, 4*1024*1024, time.Hour)

	_, err := service.CreateDirectory(context.Background(), "user-1", "/workspace/readme.txt/child", nil)
	if domain.ErrorCodeOf(err) != domain.CodeInvalidArgument {
		t.Fatalf("error = %v, want invalid argument", err)
	}
	if repo.createdDirectory {
		t.Fatal("directory was created under a file parent")
	}
}

func TestInitUploadRequiresDirectoryParent(t *testing.T) {
	repo := &fakeRepo{
		filesByPath: map[string]domain.FileNode{
			"/workspace/readme.txt": {ID: "file-1", Path: "/workspace/readme.txt", NodeType: domain.NodeTypeFile},
		},
	}
	service := NewService(repo, nil, 4*1024*1024, time.Hour)

	_, err := service.InitUpload(context.Background(), "user-1", "/workspace/readme.txt/child.txt", 5, strings.Repeat("a", 64), 0, nil, "", "")
	if domain.ErrorCodeOf(err) != domain.CodeInvalidArgument {
		t.Fatalf("error = %v, want invalid argument", err)
	}
	if repo.createdUpload {
		t.Fatal("upload session was created under a file parent")
	}
}

func TestInitUploadRejectsStorageQuotaExceeded(t *testing.T) {
	repo := &fakeRepo{usage: domain.StorageUsage{BytesUsed: 90}}
	service := NewService(repo, nil, 4*1024*1024, time.Hour).WithStorageQuota(100)

	_, err := service.InitUpload(context.Background(), "user-1", "/new.txt", 11, strings.Repeat("a", 64), 0, nil, "", "")
	if domain.ErrorCodeOf(err) != domain.CodeStorageQuotaExceeded {
		t.Fatalf("error = %v, want storage quota exceeded", err)
	}
	if repo.createdUpload {
		t.Fatal("upload session was created over quota")
	}
}

func TestInitUploadQuotaUsesReplacementSizeDelta(t *testing.T) {
	repo := &fakeRepo{
		filesByPath: map[string]domain.FileNode{
			"/existing.txt": {ID: "file-1", Path: "/existing.txt", NodeType: domain.NodeTypeFile, Size: 40},
		},
		usage: domain.StorageUsage{BytesUsed: 90},
	}
	service := NewService(repo, nil, 4*1024*1024, time.Hour).WithStorageQuota(100)

	if _, err := service.InitUpload(context.Background(), "user-1", "/existing.txt", 50, strings.Repeat("a", 64), 0, nil, "", ""); err != nil {
		t.Fatalf("replace within quota: %v", err)
	}
	if !repo.createdUpload {
		t.Fatal("replacement upload session was not created")
	}
}

func TestMoveRequiresDirectoryParent(t *testing.T) {
	repo := &fakeRepo{
		filesByPath: map[string]domain.FileNode{
			"/workspace/readme.txt": {ID: "file-1", Path: "/workspace/readme.txt", NodeType: domain.NodeTypeFile},
		},
	}
	service := NewService(repo, nil, 4*1024*1024, time.Hour)

	_, err := service.Move(context.Background(), "user-1", "file-2", "/workspace/readme.txt/child.txt", nil, nil)
	if domain.ErrorCodeOf(err) != domain.CodeInvalidArgument {
		t.Fatalf("error = %v, want invalid argument", err)
	}
	if repo.movedFile {
		t.Fatal("file was moved under a file parent")
	}
}

type fakeRepo struct {
	filesByPath map[string]domain.FileNode

	createdDirectory bool
	createdUpload    bool
	movedFile        bool
	usage            domain.StorageUsage
}

func (r *fakeRepo) CreateDirectory(ctx context.Context, userID, path, name string, parentID, sourceDeviceID *string) (domain.FileNode, error) {
	_, _, _, _, _, _ = ctx, userID, path, name, parentID, sourceDeviceID
	r.createdDirectory = true
	return domain.FileNode{ID: "dir-1", Path: path, NodeType: domain.NodeTypeDirectory}, nil
}

func (r *fakeRepo) GetFileByID(ctx context.Context, userID, fileID string) (domain.FileNode, error) {
	_, _ = ctx, userID
	return domain.FileNode{ID: fileID, Path: "/workspace/file.txt", NodeType: domain.NodeTypeFile}, nil
}

func (r *fakeRepo) GetFileByPath(ctx context.Context, userID, path string) (domain.FileNode, error) {
	_, _ = ctx, userID
	if node, ok := r.filesByPath[path]; ok {
		return node, nil
	}
	return domain.FileNode{}, domain.E(domain.CodeNotFound, "file not found", nil)
}

func (r *fakeRepo) ListFiles(ctx context.Context, userID string, parentID *string, cursor string, limit int32) (domain.FileList, error) {
	_, _, _, _, _ = ctx, userID, parentID, cursor, limit
	return domain.FileList{}, nil
}

func (r *fakeRepo) SearchFiles(ctx context.Context, userID, query, cursor string, limit int32) (domain.FileList, error) {
	_, _, _, _, _ = ctx, userID, query, cursor, limit
	return domain.FileList{}, nil
}

func (r *fakeRepo) Usage(ctx context.Context, userID string) (domain.StorageUsage, error) {
	_, _ = ctx, userID
	return r.usage, nil
}

func (r *fakeRepo) ListDeletedFiles(ctx context.Context, userID, cursor string, limit int32) (domain.FileList, error) {
	_, _, _, _ = ctx, userID, cursor, limit
	return domain.FileList{}, nil
}

func (r *fakeRepo) ListFileVersions(ctx context.Context, userID, fileID string, limit int32) ([]domain.FileVersion, error) {
	_, _, _, _ = ctx, userID, fileID, limit
	return nil, nil
}

func (r *fakeRepo) PinFileVersion(ctx context.Context, userID, fileID string, version int64) (domain.FileVersion, error) {
	_, _, _, _ = ctx, userID, fileID, version
	return domain.FileVersion{}, nil
}

func (r *fakeRepo) UnpinFileVersion(ctx context.Context, userID, fileID string, version int64) (domain.FileVersion, error) {
	_, _, _, _ = ctx, userID, fileID, version
	return domain.FileVersion{}, nil
}

func (r *fakeRepo) RestoreFileVersion(ctx context.Context, userID, fileID string, version int64, sourceDeviceID *string) (domain.FileNode, int64, error) {
	_, _, _, _, _ = ctx, userID, fileID, version, sourceDeviceID
	return domain.FileNode{}, 0, nil
}

func (r *fakeRepo) MoveFile(ctx context.Context, userID, fileID, newPath, newName string, newParentID *string, baseVersion *int64, sourceDeviceID *string) (domain.FileNode, error) {
	_, _, _, _, _, _ = ctx, userID, fileID, newName, newParentID, sourceDeviceID
	_ = baseVersion
	r.movedFile = true
	return domain.FileNode{ID: fileID, Path: newPath, NodeType: domain.NodeTypeFile}, nil
}

func (r *fakeRepo) DeleteFile(ctx context.Context, userID, fileID string, baseVersion *int64, sourceDeviceID *string) error {
	_, _, _, _ = ctx, userID, fileID, sourceDeviceID
	_ = baseVersion
	return nil
}

func (r *fakeRepo) RestoreDeletedFile(ctx context.Context, userID, fileID string, sourceDeviceID *string) (domain.FileNode, error) {
	_, _, _ = ctx, userID, sourceDeviceID
	return domain.FileNode{ID: fileID}, nil
}

func (r *fakeRepo) PurgeDeletedFile(ctx context.Context, userID, fileID string) error {
	_, _, _ = ctx, userID, fileID
	return nil
}

func (r *fakeRepo) CreateUploadSession(ctx context.Context, s domain.UploadSession) (domain.UploadSession, error) {
	_ = ctx
	r.createdUpload = true
	return s, nil
}

func (r *fakeRepo) GetUploadSession(ctx context.Context, userID, uploadID string) (domain.UploadSession, error) {
	_, _, _ = ctx, userID, uploadID
	return domain.UploadSession{}, nil
}

func (r *fakeRepo) AbortUploadSession(ctx context.Context, userID, uploadID string) (domain.UploadSession, error) {
	_, _, _ = ctx, userID, uploadID
	return domain.UploadSession{ID: uploadID, Status: domain.UploadStatusAborted}, nil
}

func (r *fakeRepo) PutUploadChunk(ctx context.Context, uploadID string, chunkIndex, size int32, sha256sum, storageKey string) (domain.UploadChunk, error) {
	_, _, _, _, _, _ = ctx, uploadID, chunkIndex, size, sha256sum, storageKey
	return domain.UploadChunk{}, nil
}

func (r *fakeRepo) ListUploadChunks(ctx context.Context, uploadID string) ([]domain.UploadChunk, error) {
	_, _ = ctx, uploadID
	return nil, nil
}

func (r *fakeRepo) DeleteUploadChunk(ctx context.Context, chunkID string) error {
	_, _ = ctx, chunkID
	return nil
}

func (r *fakeRepo) CommitUpload(ctx context.Context, userID, uploadID, storageKey string) (domain.FileNode, int64, error) {
	_, _, _, _ = ctx, userID, uploadID, storageKey
	return domain.FileNode{}, 0, nil
}

func (r *fakeRepo) ReserveStorageObject(ctx context.Context, storageKey string) error {
	_, _ = ctx, storageKey
	return nil
}
