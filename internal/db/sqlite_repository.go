package db

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bruceblink/SyncHub/internal/domain"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
	modernsqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

//go:embed sqlite_schema.sql
var sqliteSchema string

type SQLiteRepository struct {
	db *sql.DB
}

func OpenSQLite(ctx context.Context, databaseURL string) (*SQLiteRepository, error) {
	if databaseURL == "" {
		databaseURL = "./.data/synchub.db"
	}
	if err := ensureSQLiteDir(databaseURL); err != nil {
		return nil, err
	}
	conn, err := sql.Open("sqlite", databaseURL)
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	repo := &SQLiteRepository{db: conn}
	if _, err := conn.ExecContext(ctx, "pragma foreign_keys = on; pragma busy_timeout = 5000;"); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, sqliteSchema); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := conn.PingContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return repo, nil
}

func (r *SQLiteRepository) Close() error {
	return r.db.Close()
}

func (r *SQLiteRepository) Ping(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

func (r *SQLiteRepository) CreateUser(ctx context.Context, email, passwordHash string) (domain.User, error) {
	now := time.Now().UTC()
	user := domain.User{
		ID:           uuid.NewString(),
		Email:        email,
		PasswordHash: passwordHash,
		Status:       "active",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	_, err := r.db.ExecContext(ctx, `
		insert into users (id, email, password_hash, status, created_at, updated_at)
		values (?, ?, ?, ?, ?, ?)
	`, user.ID, user.Email, user.PasswordHash, user.Status, user.CreatedAt, user.UpdatedAt)
	if isSQLiteUniqueViolation(err) {
		return domain.User{}, domain.E(domain.CodeAlreadyExists, "email already exists", err)
	}
	return user, wrapSQLiteDBErr(err)
}

func (r *SQLiteRepository) GetUserByEmail(ctx context.Context, email string) (domain.User, error) {
	var user domain.User
	err := r.db.QueryRowContext(ctx, `
		select id, email, password_hash, status, created_at, updated_at
		from users
		where email = ? and status = 'active'
	`, email).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	return user, wrapSQLiteNotFound(err, "user not found")
}

func (r *SQLiteRepository) GetUserByID(ctx context.Context, id string) (domain.User, error) {
	var user domain.User
	err := r.db.QueryRowContext(ctx, `
		select id, email, password_hash, status, created_at, updated_at
		from users
		where id = ? and status = 'active'
	`, id).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	return user, wrapSQLiteNotFound(err, "user not found")
}

func (r *SQLiteRepository) CreateRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (domain.RefreshToken, error) {
	token := domain.RefreshToken{ID: uuid.NewString(), UserID: userID, TokenHash: tokenHash, ExpiresAt: expiresAt, CreatedAt: time.Now().UTC()}
	_, err := r.db.ExecContext(ctx, `
		insert into refresh_tokens (id, user_id, token_hash, expires_at, created_at)
		values (?, ?, ?, ?, ?)
	`, token.ID, token.UserID, token.TokenHash, token.ExpiresAt, token.CreatedAt)
	return token, wrapSQLiteDBErr(err)
}

func (r *SQLiteRepository) GetRefreshToken(ctx context.Context, tokenHash string) (domain.RefreshToken, error) {
	var token domain.RefreshToken
	err := r.db.QueryRowContext(ctx, `
		select id, user_id, token_hash, expires_at, revoked_at, created_at
		from refresh_tokens
		where token_hash = ?
	`, tokenHash).Scan(&token.ID, &token.UserID, &token.TokenHash, &token.ExpiresAt, &token.RevokedAt, &token.CreatedAt)
	return token, wrapSQLiteNotFound(err, "refresh token not found")
}

func (r *SQLiteRepository) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	_, err := r.db.ExecContext(ctx, `update refresh_tokens set revoked_at = ? where token_hash = ?`, time.Now().UTC(), tokenHash)
	return wrapSQLiteDBErr(err)
}

func (r *SQLiteRepository) CreateDirectory(ctx context.Context, userID, path, name string, parentID *string) (domain.FileNode, error) {
	now := time.Now().UTC()
	node := domain.FileNode{
		ID:        uuid.NewString(),
		UserID:    userID,
		ParentID:  parentID,
		Name:      name,
		Path:      path,
		NodeType:  domain.NodeTypeDirectory,
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := r.db.ExecContext(ctx, `
		insert into file_nodes (id, user_id, parent_id, name, path, node_type, version, created_at, updated_at)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, node.ID, node.UserID, node.ParentID, node.Name, node.Path, node.NodeType, node.Version, node.CreatedAt, node.UpdatedAt)
	if isSQLiteUniqueViolation(err) {
		return domain.FileNode{}, domain.E(domain.CodeAlreadyExists, "file path already exists", err)
	}
	if err != nil {
		return domain.FileNode{}, wrapSQLiteDBErr(err)
	}
	_, err = r.createChangeEvent(ctx, nil, userID, node.ID, domain.EventCreate, nil, path, nil)
	return node, wrapSQLiteDBErr(err)
}

func (r *SQLiteRepository) GetFileByID(ctx context.Context, userID, fileID string) (domain.FileNode, error) {
	var node domain.FileNode
	err := r.db.QueryRowContext(ctx, `
		select id, user_id, parent_id, name, path, node_type, current_version_id, size, sha256, storage_key, version, deleted_at, created_at, updated_at
		from file_nodes
		where user_id = ? and id = ? and deleted_at is null
	`, userID, fileID).Scan(fileNodeScan(&node)...)
	return node, wrapSQLiteNotFound(err, "file not found")
}

func (r *SQLiteRepository) GetFileByPath(ctx context.Context, userID, path string) (domain.FileNode, error) {
	var node domain.FileNode
	err := r.db.QueryRowContext(ctx, `
		select id, user_id, parent_id, name, path, node_type, current_version_id, size, sha256, storage_key, version, deleted_at, created_at, updated_at
		from file_nodes
		where user_id = ? and path = ? and deleted_at is null
	`, userID, path).Scan(fileNodeScan(&node)...)
	return node, wrapSQLiteNotFound(err, "file not found")
}

func (r *SQLiteRepository) ListFiles(ctx context.Context, userID string, parentID *string, limit int32) ([]domain.FileNode, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var (
		rows *sql.Rows
		err  error
	)
	if parentID == nil {
		rows, err = r.db.QueryContext(ctx, `
			select id, user_id, parent_id, name, path, node_type, current_version_id, size, sha256, storage_key, version, deleted_at, created_at, updated_at
			from file_nodes
			where user_id = ? and parent_id is null and deleted_at is null
			order by node_type, name
			limit ?
		`, userID, limit)
	} else {
		rows, err = r.db.QueryContext(ctx, `
			select id, user_id, parent_id, name, path, node_type, current_version_id, size, sha256, storage_key, version, deleted_at, created_at, updated_at
			from file_nodes
			where user_id = ? and parent_id = ? and deleted_at is null
			order by node_type, name
			limit ?
		`, userID, *parentID, limit)
	}
	if err != nil {
		return nil, wrapSQLiteDBErr(err)
	}
	defer rows.Close()

	var nodes []domain.FileNode
	for rows.Next() {
		var node domain.FileNode
		if err := rows.Scan(fileNodeScan(&node)...); err != nil {
			return nil, wrapSQLiteDBErr(err)
		}
		nodes = append(nodes, node)
	}
	return nodes, wrapSQLiteDBErr(rows.Err())
}

func (r *SQLiteRepository) ListFileVersions(ctx context.Context, userID, fileID string, limit int32) ([]domain.FileVersion, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		select v.id, v.file_id, v.user_id, v.version, v.size, v.sha256, v.storage_key, v.created_by_device_id, v.created_at
		from file_versions v
		join file_nodes n on n.id = v.file_id and n.user_id = v.user_id
		where v.user_id = ? and v.file_id = ? and n.deleted_at is null
		order by v.version desc
		limit ?
	`, userID, fileID, limit)
	if err != nil {
		return nil, wrapSQLiteDBErr(err)
	}
	defer rows.Close()

	versions := make([]domain.FileVersion, 0)
	for rows.Next() {
		var version domain.FileVersion
		if err := rows.Scan(fileVersionScan(&version)...); err != nil {
			return nil, wrapSQLiteDBErr(err)
		}
		versions = append(versions, version)
	}
	return versions, wrapSQLiteDBErr(rows.Err())
}

func (r *SQLiteRepository) RestoreFileVersion(ctx context.Context, userID, fileID string, version int64) (domain.FileNode, int64, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.FileNode{}, 0, wrapSQLiteDBErr(err)
	}
	defer func() { _ = tx.Rollback() }()

	node, err := r.getFileByIDTx(ctx, tx, userID, fileID)
	if err != nil {
		return domain.FileNode{}, 0, err
	}
	if node.NodeType != domain.NodeTypeFile {
		return domain.FileNode{}, 0, domain.E(domain.CodeInvalidArgument, "only files can be restored", nil)
	}

	var source domain.FileVersion
	err = tx.QueryRowContext(ctx, `
		select id, file_id, user_id, version, size, sha256, storage_key, created_by_device_id, created_at
		from file_versions
		where user_id = ? and file_id = ? and version = ?
	`, userID, fileID, version).Scan(fileVersionScan(&source)...)
	if err != nil {
		return domain.FileNode{}, 0, wrapSQLiteNotFound(err, "file version not found")
	}

	now := time.Now().UTC()
	newVersion := node.Version + 1
	versionID := uuid.NewString()
	_, err = tx.ExecContext(ctx, `
		insert into file_versions (id, file_id, user_id, version, size, sha256, storage_key, created_by_device_id, created_at)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, versionID, fileID, userID, newVersion, source.Size, source.SHA256, source.StorageKey, source.CreatedByDeviceID, now)
	if err != nil {
		return domain.FileNode{}, 0, wrapSQLiteDBErr(err)
	}
	_, err = tx.ExecContext(ctx, `
		update file_nodes
		set current_version_id = ?, size = ?, sha256 = ?, storage_key = ?, version = ?, updated_at = ?
		where user_id = ? and id = ? and deleted_at is null
	`, versionID, source.Size, source.SHA256, source.StorageKey, newVersion, now, userID, fileID)
	if err != nil {
		return domain.FileNode{}, 0, wrapSQLiteDBErr(err)
	}
	restored, err := r.getFileByIDTx(ctx, tx, userID, fileID)
	if err != nil {
		return domain.FileNode{}, 0, err
	}
	changeID, err := r.createChangeEvent(ctx, tx, userID, fileID, domain.EventRestore, &restored.Version, restored.Path, nil)
	if err != nil {
		return domain.FileNode{}, 0, err
	}
	return restored, changeID, wrapSQLiteDBErr(tx.Commit())
}

func (r *SQLiteRepository) MoveFile(ctx context.Context, userID, fileID, newPath, newName string, newParentID *string) (domain.FileNode, error) {
	old, err := r.GetFileByID(ctx, userID, fileID)
	if err != nil {
		return domain.FileNode{}, err
	}
	result, err := r.db.ExecContext(ctx, `
		update file_nodes
		set parent_id = ?, name = ?, path = ?, version = version + 1, updated_at = ?
		where user_id = ? and id = ? and deleted_at is null
	`, newParentID, newName, newPath, time.Now().UTC(), userID, fileID)
	if isSQLiteUniqueViolation(err) {
		return domain.FileNode{}, domain.E(domain.CodeAlreadyExists, "file path already exists", err)
	}
	if err != nil {
		return domain.FileNode{}, wrapSQLiteDBErr(err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return domain.FileNode{}, domain.E(domain.CodeFileNotFound, "file not found", nil)
	}
	node, err := r.GetFileByID(ctx, userID, fileID)
	if err != nil {
		return domain.FileNode{}, err
	}
	_, err = r.createChangeEvent(ctx, nil, userID, node.ID, domain.EventMove, &node.Version, node.Path, &old.Path)
	return node, wrapSQLiteDBErr(err)
}

func (r *SQLiteRepository) DeleteFile(ctx context.Context, userID, fileID string) error {
	node, err := r.GetFileByID(ctx, userID, fileID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	result, err := r.db.ExecContext(ctx, `
		update file_nodes
		set deleted_at = ?, version = version + 1, updated_at = ?
		where user_id = ? and id = ? and deleted_at is null
	`, now, now, userID, fileID)
	if err != nil {
		return wrapSQLiteDBErr(err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return domain.E(domain.CodeFileNotFound, "file not found", nil)
	}
	_, err = r.createChangeEvent(ctx, nil, userID, fileID, domain.EventDelete, &node.Version, node.Path, &node.Path)
	return wrapSQLiteDBErr(err)
}

func (r *SQLiteRepository) CreateUploadSession(ctx context.Context, s domain.UploadSession) (domain.UploadSession, error) {
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	if s.StagingKey == "" {
		s.StagingKey = "staging/" + s.UserID + "/" + s.ID
	}
	if s.Status == "" {
		s.Status = domain.UploadStatusPending
	}
	now := time.Now().UTC()
	s.CreatedAt = now
	s.UpdatedAt = now
	_, err := r.db.ExecContext(ctx, `
		insert into upload_sessions (id, user_id, target_path, target_file_id, base_version, total_size, chunk_size, sha256, status, staging_key, expires_at, idempotency_key, created_at, updated_at)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, s.ID, s.UserID, s.TargetPath, s.TargetFileID, s.BaseVersion, s.TotalSize, s.ChunkSize, s.SHA256, s.Status, s.StagingKey, s.ExpiresAt, s.IdempotencyKey, s.CreatedAt, s.UpdatedAt)
	if isSQLiteUniqueViolation(err) && s.IdempotencyKey != nil {
		return r.getUploadSessionByIdempotencyKey(ctx, s.UserID, *s.IdempotencyKey)
	}
	return s, wrapSQLiteDBErr(err)
}

func (r *SQLiteRepository) GetUploadSession(ctx context.Context, userID, uploadID string) (domain.UploadSession, error) {
	var s domain.UploadSession
	err := r.db.QueryRowContext(ctx, `
		select id, user_id, target_path, target_file_id, base_version, total_size, chunk_size, sha256, status, staging_key, expires_at, idempotency_key, created_at, updated_at
		from upload_sessions
		where user_id = ? and id = ?
	`, userID, uploadID).Scan(uploadSessionScan(&s)...)
	return s, wrapSQLiteNotFound(err, "upload session not found")
}

func (r *SQLiteRepository) ExpireUploadSessions(ctx context.Context, now time.Time, limit int32) (int64, error) {
	if limit <= 0 {
		limit = 1000
	}
	result, err := r.db.ExecContext(ctx, `
		update upload_sessions
		set status = ?, updated_at = ?
		where id in (
			select id
			from upload_sessions
			where status = ? and expires_at <= ?
			order by expires_at
			limit ?
		)
	`, domain.UploadStatusExpired, now, domain.UploadStatusPending, now, limit)
	if err != nil {
		return 0, wrapSQLiteDBErr(err)
	}
	rows, err := result.RowsAffected()
	return rows, wrapSQLiteDBErr(err)
}

func (r *SQLiteRepository) getUploadSessionByIdempotencyKey(ctx context.Context, userID, idempotencyKey string) (domain.UploadSession, error) {
	var s domain.UploadSession
	err := r.db.QueryRowContext(ctx, `
		select id, user_id, target_path, target_file_id, base_version, total_size, chunk_size, sha256, status, staging_key, expires_at, idempotency_key, created_at, updated_at
		from upload_sessions
		where user_id = ? and idempotency_key = ?
	`, userID, idempotencyKey).Scan(uploadSessionScan(&s)...)
	return s, wrapSQLiteNotFound(err, "upload session not found")
}

func (r *SQLiteRepository) PutUploadChunk(ctx context.Context, uploadID string, chunkIndex, size int32, sha256sum, storageKey string) (domain.UploadChunk, error) {
	chunk := domain.UploadChunk{ID: uuid.NewString(), UploadID: uploadID, ChunkIndex: chunkIndex, Size: size, SHA256: sha256sum, StorageKey: storageKey}
	_, err := r.db.ExecContext(ctx, `
		insert into upload_chunks (id, upload_id, chunk_index, size, sha256, storage_key, created_at)
		values (?, ?, ?, ?, ?, ?, ?)
		on conflict (upload_id, chunk_index) do update
		set size = excluded.size, sha256 = excluded.sha256, storage_key = excluded.storage_key
	`, chunk.ID, chunk.UploadID, chunk.ChunkIndex, chunk.Size, chunk.SHA256, chunk.StorageKey, time.Now().UTC())
	if err != nil {
		return domain.UploadChunk{}, wrapSQLiteDBErr(err)
	}
	err = r.db.QueryRowContext(ctx, `
		select id, upload_id, chunk_index, size, sha256, storage_key, created_at
		from upload_chunks
		where upload_id = ? and chunk_index = ?
	`, uploadID, chunkIndex).Scan(&chunk.ID, &chunk.UploadID, &chunk.ChunkIndex, &chunk.Size, &chunk.SHA256, &chunk.StorageKey, &chunk.CreatedAt)
	return chunk, wrapSQLiteDBErr(err)
}

func (r *SQLiteRepository) ListUploadChunks(ctx context.Context, uploadID string) ([]domain.UploadChunk, error) {
	rows, err := r.db.QueryContext(ctx, `
		select id, upload_id, chunk_index, size, sha256, storage_key, created_at
		from upload_chunks
		where upload_id = ?
		order by chunk_index
	`, uploadID)
	if err != nil {
		return nil, wrapSQLiteDBErr(err)
	}
	defer rows.Close()

	var chunks []domain.UploadChunk
	for rows.Next() {
		var chunk domain.UploadChunk
		if err := rows.Scan(&chunk.ID, &chunk.UploadID, &chunk.ChunkIndex, &chunk.Size, &chunk.SHA256, &chunk.StorageKey, &chunk.CreatedAt); err != nil {
			return nil, wrapSQLiteDBErr(err)
		}
		chunks = append(chunks, chunk)
	}
	return chunks, wrapSQLiteDBErr(rows.Err())
}

func (r *SQLiteRepository) CommitUpload(ctx context.Context, userID, uploadID, storageKey string) (domain.FileNode, int64, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return domain.FileNode{}, 0, wrapSQLiteDBErr(err)
	}
	defer func() { _ = tx.Rollback() }()

	var s domain.UploadSession
	err = tx.QueryRowContext(ctx, `
		select id, user_id, target_path, target_file_id, base_version, total_size, chunk_size, sha256, status, staging_key, expires_at, idempotency_key, created_at, updated_at
		from upload_sessions
		where user_id = ? and id = ?
	`, userID, uploadID).Scan(uploadSessionScan(&s)...)
	if err != nil {
		return domain.FileNode{}, 0, wrapSQLiteNotFound(err, "upload session not found")
	}
	if s.Status == domain.UploadStatusCommitted {
		node, err := r.getFileByPathTx(ctx, tx, userID, s.TargetPath)
		if err != nil {
			return domain.FileNode{}, 0, err
		}
		return node, 0, wrapSQLiteDBErr(tx.Commit())
	}
	if s.Status != domain.UploadStatusPending || time.Now().After(s.ExpiresAt) {
		return domain.FileNode{}, 0, domain.E(domain.CodeUploadSessionExpired, "upload session is not active", nil)
	}

	existing, existingErr := r.getFileByPathTx(ctx, tx, userID, s.TargetPath)
	if existingErr != nil && domain.ErrorCodeOf(existingErr) != domain.CodeNotFound {
		return domain.FileNode{}, 0, existingErr
	}
	if existingErr == nil && s.BaseVersion != nil && existing.Version != *s.BaseVersion {
		remoteVersion := existing.Version
		if err := createSQLiteSyncConflictTx(ctx, tx, domain.SyncConflict{
			UserID:        userID,
			FileID:        &existing.ID,
			Path:          s.TargetPath,
			LocalVersion:  s.BaseVersion,
			RemoteVersion: &remoteVersion,
		}); err != nil {
			return domain.FileNode{}, 0, err
		}
		if err := tx.Commit(); err != nil {
			return domain.FileNode{}, 0, wrapSQLiteDBErr(err)
		}
		return domain.FileNode{}, 0, domain.E(domain.CodeFileConflict, "base version conflict", nil)
	}

	parentPath, name, err := domain.SplitPath(s.TargetPath)
	if err != nil {
		return domain.FileNode{}, 0, err
	}
	var parentID *string
	if parentPath != "/" {
		parent, err := r.getFileByPathTx(ctx, tx, userID, parentPath)
		if err != nil {
			return domain.FileNode{}, 0, err
		}
		parentID = &parent.ID
	}

	var node domain.FileNode
	eventType := domain.EventUpdate
	now := time.Now().UTC()
	if existingErr == nil {
		newVersion := existing.Version + 1
		versionID := uuid.NewString()
		_, err = tx.ExecContext(ctx, `
			insert into file_versions (id, file_id, user_id, version, size, sha256, storage_key, created_at)
			values (?, ?, ?, ?, ?, ?, ?, ?)
		`, versionID, existing.ID, userID, newVersion, s.TotalSize, s.SHA256, storageKey, now)
		if err != nil {
			return domain.FileNode{}, 0, wrapSQLiteDBErr(err)
		}
		_, err = tx.ExecContext(ctx, `
			update file_nodes
			set current_version_id = ?, size = ?, sha256 = ?, storage_key = ?, version = ?, updated_at = ?
			where user_id = ? and id = ?
		`, versionID, s.TotalSize, s.SHA256, storageKey, newVersion, now, userID, existing.ID)
		if err != nil {
			return domain.FileNode{}, 0, wrapSQLiteDBErr(err)
		}
		node, err = r.getFileByPathTx(ctx, tx, userID, s.TargetPath)
		if err != nil {
			return domain.FileNode{}, 0, err
		}
	} else {
		eventType = domain.EventCreate
		fileID := uuid.NewString()
		versionID := uuid.NewString()
		_, err = tx.ExecContext(ctx, `
			insert into file_nodes (id, user_id, parent_id, name, path, node_type, current_version_id, size, sha256, storage_key, version, created_at, updated_at)
			values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
		`, fileID, userID, parentID, name, s.TargetPath, domain.NodeTypeFile, versionID, s.TotalSize, s.SHA256, storageKey, now, now)
		if err != nil {
			return domain.FileNode{}, 0, wrapSQLiteDBErr(err)
		}
		_, err = tx.ExecContext(ctx, `
			insert into file_versions (id, file_id, user_id, version, size, sha256, storage_key, created_at)
			values (?, ?, ?, 1, ?, ?, ?, ?)
		`, versionID, fileID, userID, s.TotalSize, s.SHA256, storageKey, now)
		if err != nil {
			return domain.FileNode{}, 0, wrapSQLiteDBErr(err)
		}
		node, err = r.getFileByPathTx(ctx, tx, userID, s.TargetPath)
		if err != nil {
			return domain.FileNode{}, 0, err
		}
	}
	changeID, err := r.createChangeEvent(ctx, tx, userID, node.ID, eventType, &node.Version, node.Path, nil)
	if err != nil {
		return domain.FileNode{}, 0, err
	}
	_, err = tx.ExecContext(ctx, `
		update upload_sessions
		set status = ?, updated_at = ?
		where user_id = ? and id = ?
	`, domain.UploadStatusCommitted, time.Now().UTC(), userID, uploadID)
	if err != nil {
		return domain.FileNode{}, 0, wrapSQLiteDBErr(err)
	}
	return node, changeID, wrapSQLiteDBErr(tx.Commit())
}

func (r *SQLiteRepository) CreateDevice(ctx context.Context, userID, name, platform string) (domain.Device, error) {
	now := time.Now().UTC()
	device := domain.Device{
		ID:        uuid.NewString(),
		UserID:    userID,
		Name:      name,
		Platform:  platform,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := r.db.ExecContext(ctx, `
		insert into devices (id, user_id, name, platform, last_applied_change_id, created_at, updated_at)
		values (?, ?, ?, ?, ?, ?, ?)
	`, device.ID, device.UserID, device.Name, device.Platform, device.LastAppliedChangeID, device.CreatedAt, device.UpdatedAt)
	return device, wrapSQLiteDBErr(err)
}

func (r *SQLiteRepository) HeartbeatDevice(ctx context.Context, userID, deviceID string) (domain.Device, error) {
	now := time.Now().UTC()
	result, err := r.db.ExecContext(ctx, `
		update devices
		set last_seen_at = ?, updated_at = ?
		where user_id = ? and id = ?
	`, now, now, userID, deviceID)
	if err != nil {
		return domain.Device{}, wrapSQLiteDBErr(err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return domain.Device{}, domain.E(domain.CodeNotFound, "device not found", nil)
	}
	return r.getDevice(ctx, userID, deviceID)
}

func (r *SQLiteRepository) ListChanges(ctx context.Context, userID, deviceID string, afterChangeID int64, limit int32) ([]domain.ChangeEvent, error) {
	if _, err := r.getDevice(ctx, userID, deviceID); err != nil {
		return nil, err
	}
	if err := r.validateChangeCursor(ctx, userID, afterChangeID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := r.db.QueryContext(ctx, `
		select id, user_id, file_id, event_type, version, path, old_path, source_device_id, created_at
		from change_events
		where user_id = ? and id > ?
		order by id
		limit ?
	`, userID, afterChangeID, limit)
	if err != nil {
		return nil, wrapSQLiteDBErr(err)
	}
	defer rows.Close()

	var events []domain.ChangeEvent
	for rows.Next() {
		var event domain.ChangeEvent
		if err := rows.Scan(changeEventScan(&event)...); err != nil {
			return nil, wrapSQLiteDBErr(err)
		}
		events = append(events, event)
	}
	return events, wrapSQLiteDBErr(rows.Err())
}

func (r *SQLiteRepository) validateChangeCursor(ctx context.Context, userID string, afterChangeID int64) error {
	if afterChangeID == 0 {
		return nil
	}
	var minID, maxID, count int64
	err := r.db.QueryRowContext(ctx, `
		select coalesce(min(id), 0), coalesce(max(id), 0), count(*)
		from change_events
		where user_id = ?
	`, userID).Scan(&minID, &maxID, &count)
	if err != nil {
		return wrapSQLiteDBErr(err)
	}
	if count == 0 || afterChangeID > maxID || afterChangeID < minID-1 {
		return domain.E(domain.CodeSyncCursorExpired, "sync cursor is outside the available change feed; run a full scan", nil)
	}
	return nil
}

func (r *SQLiteRepository) AckDevice(ctx context.Context, userID, deviceID string, lastAppliedChangeID int64) (domain.Device, error) {
	now := time.Now().UTC()
	result, err := r.db.ExecContext(ctx, `
		update devices
		set last_applied_change_id = ?, updated_at = ?
		where user_id = ? and id = ?
	`, lastAppliedChangeID, now, userID, deviceID)
	if err != nil {
		return domain.Device{}, wrapSQLiteDBErr(err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return domain.Device{}, domain.E(domain.CodeNotFound, "device not found", nil)
	}
	return r.getDevice(ctx, userID, deviceID)
}

func (r *SQLiteRepository) CreateSyncConflict(ctx context.Context, conflict domain.SyncConflict) (domain.SyncConflict, error) {
	if conflict.ID == "" {
		conflict.ID = uuid.NewString()
	}
	if conflict.Resolution == "" {
		conflict.Resolution = domain.ConflictResolutionPending
	}
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx, `
		insert into sync_conflicts (id, user_id, file_id, path, local_version, remote_version, resolution, created_at)
		values (?, ?, ?, ?, ?, ?, ?, ?)
	`, conflict.ID, conflict.UserID, conflict.FileID, conflict.Path, conflict.LocalVersion, conflict.RemoteVersion, conflict.Resolution, now)
	if err != nil {
		return domain.SyncConflict{}, wrapSQLiteDBErr(err)
	}
	return r.getSyncConflict(ctx, conflict.ID)
}

func createSQLiteSyncConflictTx(ctx context.Context, tx *sql.Tx, conflict domain.SyncConflict) error {
	if conflict.ID == "" {
		conflict.ID = uuid.NewString()
	}
	if conflict.Resolution == "" {
		conflict.Resolution = domain.ConflictResolutionPending
	}
	_, err := tx.ExecContext(ctx, `
		insert into sync_conflicts (id, user_id, file_id, path, local_version, remote_version, resolution, created_at)
		values (?, ?, ?, ?, ?, ?, ?, ?)
	`, conflict.ID, conflict.UserID, conflict.FileID, conflict.Path, conflict.LocalVersion, conflict.RemoteVersion, conflict.Resolution, time.Now().UTC())
	return wrapSQLiteDBErr(err)
}

func (r *SQLiteRepository) ListSyncConflicts(ctx context.Context, userID, resolution string, limit int32) ([]domain.SyncConflict, error) {
	if resolution == "" {
		resolution = domain.ConflictResolutionPending
	}
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := r.db.QueryContext(ctx, `
		select id, user_id, file_id, path, local_version, remote_version, resolution, created_at, resolved_at
		from sync_conflicts
		where user_id = ? and resolution = ?
		order by created_at desc, id desc
		limit ?
	`, userID, resolution, limit)
	if err != nil {
		return nil, wrapSQLiteDBErr(err)
	}
	defer rows.Close()

	var conflicts []domain.SyncConflict
	for rows.Next() {
		var conflict domain.SyncConflict
		if err := rows.Scan(syncConflictScan(&conflict)...); err != nil {
			return nil, wrapSQLiteDBErr(err)
		}
		conflicts = append(conflicts, conflict)
	}
	return conflicts, wrapSQLiteDBErr(rows.Err())
}

func (r *SQLiteRepository) getSyncConflict(ctx context.Context, conflictID string) (domain.SyncConflict, error) {
	var conflict domain.SyncConflict
	err := r.db.QueryRowContext(ctx, `
		select id, user_id, file_id, path, local_version, remote_version, resolution, created_at, resolved_at
		from sync_conflicts
		where id = ?
	`, conflictID).Scan(syncConflictScan(&conflict)...)
	return conflict, wrapSQLiteNotFound(err, "sync conflict not found")
}

func (r *SQLiteRepository) getDevice(ctx context.Context, userID, deviceID string) (domain.Device, error) {
	var device domain.Device
	err := r.db.QueryRowContext(ctx, `
		select id, user_id, name, platform, last_seen_at, last_applied_change_id, created_at, updated_at
		from devices
		where user_id = ? and id = ?
	`, userID, deviceID).Scan(deviceScan(&device)...)
	return device, wrapSQLiteNotFound(err, "device not found")
}

func (r *SQLiteRepository) getFileByIDTx(ctx context.Context, tx *sql.Tx, userID, fileID string) (domain.FileNode, error) {
	var node domain.FileNode
	err := tx.QueryRowContext(ctx, `
		select id, user_id, parent_id, name, path, node_type, current_version_id, size, sha256, storage_key, version, deleted_at, created_at, updated_at
		from file_nodes
		where user_id = ? and id = ? and deleted_at is null
	`, userID, fileID).Scan(fileNodeScan(&node)...)
	return node, wrapSQLiteNotFound(err, "file not found")
}

func (r *SQLiteRepository) getFileByPathTx(ctx context.Context, tx *sql.Tx, userID, path string) (domain.FileNode, error) {
	var node domain.FileNode
	err := tx.QueryRowContext(ctx, `
		select id, user_id, parent_id, name, path, node_type, current_version_id, size, sha256, storage_key, version, deleted_at, created_at, updated_at
		from file_nodes
		where user_id = ? and path = ? and deleted_at is null
	`, userID, path).Scan(fileNodeScan(&node)...)
	return node, wrapSQLiteNotFound(err, "file not found")
}

func (r *SQLiteRepository) createChangeEvent(ctx context.Context, tx *sql.Tx, userID, fileID, eventType string, version *int64, path string, oldPath *string) (int64, error) {
	query := `
		insert into change_events (user_id, file_id, event_type, version, path, old_path, created_at)
		values (?, ?, ?, ?, ?, ?, ?)
	`
	var (
		result sql.Result
		err    error
	)
	if tx != nil {
		result, err = tx.ExecContext(ctx, query, userID, fileID, eventType, version, path, oldPath, time.Now().UTC())
	} else {
		result, err = r.db.ExecContext(ctx, query, userID, fileID, eventType, version, path, oldPath, time.Now().UTC())
	}
	if err != nil {
		return 0, wrapSQLiteDBErr(err)
	}
	id, err := result.LastInsertId()
	return id, wrapSQLiteDBErr(err)
}

func ensureSQLiteDir(databaseURL string) error {
	path := sqliteFilePath(databaseURL)
	if path == "" || path == ":memory:" {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func sqliteFilePath(databaseURL string) string {
	if !strings.HasPrefix(databaseURL, "file:") {
		return databaseURL
	}
	path := strings.TrimPrefix(databaseURL, "file:")
	path, _, _ = strings.Cut(path, "?")
	return path
}

func wrapSQLiteNotFound(err error, message string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return domain.E(domain.CodeNotFound, message, err)
	}
	return wrapSQLiteDBErr(err)
}

func wrapSQLiteDBErr(err error) error {
	if err == nil {
		return nil
	}
	return domain.E(domain.CodeInternal, "database error", err)
}

func isSQLiteUniqueViolation(err error) bool {
	var sqliteErr *modernsqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	return sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE || sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
}
