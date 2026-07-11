package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/bruceblink/SyncHub/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Ping(ctx context.Context) error {
	if r.pool == nil {
		return nil
	}
	return r.pool.Ping(ctx)
}

func (r *Repository) CreateUser(ctx context.Context, email, passwordHash string) (domain.User, error) {
	now := time.Now().UTC()
	user := domain.User{
		ID:           uuid.NewString(),
		Email:        email,
		PasswordHash: passwordHash,
		Status:       "active",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	err := r.pool.QueryRow(ctx, `
		insert into users (id, email, password_hash, status, created_at, updated_at)
		values ($1, $2, $3, $4, $5, $6)
		returning id, email, password_hash, status, created_at, updated_at
	`, user.ID, user.Email, user.PasswordHash, user.Status, user.CreatedAt, user.UpdatedAt).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Status, &user.CreatedAt, &user.UpdatedAt,
	)
	if isUniqueViolation(err) {
		return domain.User{}, domain.E(domain.CodeAlreadyExists, "email already exists", err)
	}
	return user, wrapDBErr(err)
}

func (r *Repository) GetUserByEmail(ctx context.Context, email string) (domain.User, error) {
	var user domain.User
	err := r.pool.QueryRow(ctx, `
		select id, email, password_hash, status, created_at, updated_at
		from users
		where email = $1 and status = 'active'
	`, email).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	return user, wrapNotFound(err, "user not found")
}

func (r *Repository) GetUserByID(ctx context.Context, id string) (domain.User, error) {
	var user domain.User
	err := r.pool.QueryRow(ctx, `
		select id, email, password_hash, status, created_at, updated_at
		from users
		where id = $1 and status = 'active'
	`, id).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	return user, wrapNotFound(err, "user not found")
}

func (r *Repository) CreateRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (domain.RefreshToken, error) {
	token := domain.RefreshToken{ID: uuid.NewString(), UserID: userID, TokenHash: tokenHash, ExpiresAt: expiresAt, CreatedAt: time.Now().UTC()}
	err := r.pool.QueryRow(ctx, `
		insert into refresh_tokens (id, user_id, token_hash, expires_at, created_at)
		values ($1, $2, $3, $4, $5)
		returning id, user_id, token_hash, expires_at, revoked_at, created_at
	`, token.ID, token.UserID, token.TokenHash, token.ExpiresAt, token.CreatedAt).Scan(
		&token.ID, &token.UserID, &token.TokenHash, &token.ExpiresAt, &token.RevokedAt, &token.CreatedAt,
	)
	return token, wrapDBErr(err)
}

func (r *Repository) GetRefreshToken(ctx context.Context, tokenHash string) (domain.RefreshToken, error) {
	var token domain.RefreshToken
	err := r.pool.QueryRow(ctx, `
		select id, user_id, token_hash, expires_at, revoked_at, created_at
		from refresh_tokens
		where token_hash = $1
	`, tokenHash).Scan(&token.ID, &token.UserID, &token.TokenHash, &token.ExpiresAt, &token.RevokedAt, &token.CreatedAt)
	return token, wrapNotFound(err, "refresh token not found")
}

func (r *Repository) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	_, err := r.pool.Exec(ctx, `update refresh_tokens set revoked_at = now() where token_hash = $1`, tokenHash)
	return wrapDBErr(err)
}

func (r *Repository) CreateDirectory(ctx context.Context, userID, path, name string, parentID, sourceDeviceID *string) (domain.FileNode, error) {
	node := domain.FileNode{ID: uuid.NewString(), UserID: userID, ParentID: parentID, Name: name, Path: path, NodeType: domain.NodeTypeDirectory, Version: 1}
	err := r.pool.QueryRow(ctx, `
		insert into file_nodes (id, user_id, parent_id, name, path, node_type, version)
		values ($1, $2, $3, $4, $5, $6, $7)
		returning id, user_id, parent_id, name, path, node_type, current_version_id, size, sha256, storage_key, version, deleted_at, created_at, updated_at
	`, node.ID, node.UserID, node.ParentID, node.Name, node.Path, node.NodeType, node.Version).Scan(fileNodeScan(&node)...)
	if isUniqueViolation(err) {
		return domain.FileNode{}, domain.E(domain.CodeAlreadyExists, "file path already exists", err)
	}
	if err != nil {
		return domain.FileNode{}, wrapDBErr(err)
	}
	_, err = r.createChangeEvent(ctx, nil, userID, node.ID, domain.EventCreate, nil, path, nil, sourceDeviceID)
	return node, wrapDBErr(err)
}

func (r *Repository) GetFileByID(ctx context.Context, userID, fileID string) (domain.FileNode, error) {
	var node domain.FileNode
	err := r.pool.QueryRow(ctx, `
		select id, user_id, parent_id, name, path, node_type, current_version_id, size, sha256, storage_key, version, deleted_at, created_at, updated_at
		from file_nodes
		where user_id = $1 and id = $2 and deleted_at is null
	`, userID, fileID).Scan(fileNodeScan(&node)...)
	return node, wrapNotFound(err, "file not found")
}

func (r *Repository) GetFileByPath(ctx context.Context, userID, path string) (domain.FileNode, error) {
	var node domain.FileNode
	err := r.pool.QueryRow(ctx, `
		select id, user_id, parent_id, name, path, node_type, current_version_id, size, sha256, storage_key, version, deleted_at, created_at, updated_at
		from file_nodes
		where user_id = $1 and path = $2 and deleted_at is null
	`, userID, path).Scan(fileNodeScan(&node)...)
	return node, wrapNotFound(err, "file not found")
}

func (r *Repository) ListFiles(ctx context.Context, userID string, parentID *string, cursor string, limit int32) (domain.FileList, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	queryLimit := limit + 1
	rows, err := r.pool.Query(ctx, `
		select id, user_id, parent_id, name, path, node_type, current_version_id, size, sha256, storage_key, version, deleted_at, created_at, updated_at
		from file_nodes
		where user_id = $1 and (($2::uuid is null and parent_id is null) or parent_id = $2::uuid) and deleted_at is null
			and (
				$3 = ''
				or (node_type, name, id) > (
					select node_type, name, id
					from file_nodes
					where user_id = $1 and id = $3::uuid
						and (($2::uuid is null and parent_id is null) or parent_id = $2::uuid)
						and deleted_at is null
				)
			)
		order by node_type, name, id
		limit $4
	`, userID, parentID, cursor, queryLimit)
	if err != nil {
		return domain.FileList{}, wrapDBErr(err)
	}
	defer rows.Close()
	var nodes []domain.FileNode
	for rows.Next() {
		var node domain.FileNode
		if err := rows.Scan(fileNodeScan(&node)...); err != nil {
			return domain.FileList{}, wrapDBErr(err)
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return domain.FileList{}, wrapDBErr(err)
	}
	result := domain.FileList{Items: nodes}
	if len(nodes) > int(limit) {
		result.Items = nodes[:limit]
		result.NextCursor = result.Items[len(result.Items)-1].ID
	}
	return result, nil
}

func (r *Repository) SearchFiles(ctx context.Context, userID, query, cursor string, limit int32) (domain.FileList, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		select id, user_id, parent_id, name, path, node_type, current_version_id, size, sha256, storage_key, version, deleted_at, created_at, updated_at
		from file_nodes where user_id = $1 and deleted_at is null and (name ilike '%' || $2 || '%' or path ilike '%' || $2 || '%') and ($3 = '' or id > $3::uuid)
		order by id limit $4`, userID, query, cursor, limit+1)
	if err != nil {
		return domain.FileList{}, wrapDBErr(err)
	}
	defer rows.Close()
	items := []domain.FileNode{}
	for rows.Next() {
		var node domain.FileNode
		if err := rows.Scan(fileNodeScan(&node)...); err != nil {
			return domain.FileList{}, wrapDBErr(err)
		}
		items = append(items, node)
	}
	if err := rows.Err(); err != nil {
		return domain.FileList{}, wrapDBErr(err)
	}
	result := domain.FileList{Items: items}
	if len(items) > int(limit) {
		result.Items = items[:limit]
		result.NextCursor = result.Items[len(result.Items)-1].ID
	}
	return result, nil
}

func (r *Repository) Usage(ctx context.Context, userID string) (domain.StorageUsage, error) {
	var usage domain.StorageUsage
	err := r.pool.QueryRow(ctx, `select count(*), coalesce(sum(size), 0) from file_nodes where user_id = $1 and node_type = $2 and deleted_at is null`, userID, domain.NodeTypeFile).Scan(&usage.FileCount, &usage.BytesUsed)
	return usage, wrapDBErr(err)
}

func (r *Repository) ListDeletedFiles(ctx context.Context, userID, cursor string, limit int32) (domain.FileList, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		select id, user_id, parent_id, name, path, node_type, current_version_id, size, sha256, storage_key, version, deleted_at, created_at, updated_at
		from file_nodes n
		where user_id = $1 and deleted_at is not null
		  and (parent_id is null or not exists (select 1 from file_nodes p where p.id = n.parent_id and p.user_id = n.user_id and p.deleted_at is not null))
		  and ($2 = '' or (deleted_at, id) < (select deleted_at, id from file_nodes where user_id = $1 and id::text = $2 and deleted_at is not null))
		order by deleted_at desc, id desc limit $3
	`, userID, cursor, limit+1)
	if err != nil {
		return domain.FileList{}, wrapDBErr(err)
	}
	defer rows.Close()
	items := []domain.FileNode{}
	for rows.Next() {
		var node domain.FileNode
		if err := rows.Scan(fileNodeScan(&node)...); err != nil {
			return domain.FileList{}, wrapDBErr(err)
		}
		items = append(items, node)
	}
	if err := rows.Err(); err != nil {
		return domain.FileList{}, wrapDBErr(err)
	}
	result := domain.FileList{Items: items}
	if len(items) > int(limit) {
		result.Items = items[:limit]
		result.NextCursor = result.Items[len(result.Items)-1].ID
	}
	return result, nil
}

func (r *Repository) RestoreDeletedFile(ctx context.Context, userID, fileID string, sourceDeviceID *string) (domain.FileNode, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.FileNode{}, wrapDBErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var node domain.FileNode
	err = tx.QueryRow(ctx, `select id, user_id, parent_id, name, path, node_type, current_version_id, size, sha256, storage_key, version, deleted_at, created_at, updated_at from file_nodes where user_id = $1 and id = $2 and deleted_at is not null for update`, userID, fileID).Scan(fileNodeScan(&node)...)
	if err != nil {
		return domain.FileNode{}, wrapNotFound(err, "deleted file not found")
	}
	var existing string
	err = tx.QueryRow(ctx, `select id from file_nodes where user_id = $1 and path = $2 and deleted_at is null`, userID, node.Path).Scan(&existing)
	if err == nil {
		return domain.FileNode{}, domain.E(domain.CodeAlreadyExists, "an active file already uses this path", nil)
	}
	if err != pgx.ErrNoRows {
		return domain.FileNode{}, wrapDBErr(err)
	}
	err = tx.QueryRow(ctx, `update file_nodes set deleted_at = null, version = version + 1, updated_at = now() where user_id = $1 and id = $2 returning id, user_id, parent_id, name, path, node_type, current_version_id, size, sha256, storage_key, version, deleted_at, created_at, updated_at`, userID, fileID).Scan(fileNodeScan(&node)...)
	if err != nil {
		return domain.FileNode{}, wrapDBErr(err)
	}
	if node.NodeType == domain.NodeTypeDirectory {
		if _, err = tx.Exec(ctx, `update file_nodes set deleted_at = null, version = version + 1, updated_at = now() where user_id = $1 and deleted_at is not null and path like replace(replace(replace($2, '\', '\\'), '%', '\%'), '_', '\_') || '/%' escape '\'`, userID, node.Path); err != nil {
			return domain.FileNode{}, wrapDBErr(err)
		}
	}
	rows, err := tx.Query(ctx, `select id, user_id, parent_id, name, path, node_type, current_version_id, size, sha256, storage_key, version, deleted_at, created_at, updated_at from file_nodes where user_id = $1 and deleted_at is null and (id = $2 or path like replace(replace(replace($3, '\', '\\'), '%', '\%'), '_', '\_') || '/%' escape '\')`, userID, fileID, node.Path)
	if err != nil {
		return domain.FileNode{}, wrapDBErr(err)
	}
	restoredNodes := []domain.FileNode{}
	for rows.Next() {
		var restored domain.FileNode
		if err := rows.Scan(fileNodeScan(&restored)...); err != nil {
			rows.Close()
			return domain.FileNode{}, wrapDBErr(err)
		}
		restoredNodes = append(restoredNodes, restored)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return domain.FileNode{}, wrapDBErr(err)
	}
	rows.Close()
	for _, restored := range restoredNodes {
		if _, err = r.createChangeEvent(ctx, tx, userID, restored.ID, domain.EventRestore, &restored.Version, restored.Path, nil, sourceDeviceID); err != nil {
			return domain.FileNode{}, err
		}
		if restored.ID == node.ID {
			node = restored
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.FileNode{}, wrapDBErr(err)
	}
	return node, nil
}

func (r *Repository) PurgeDeletedFile(ctx context.Context, userID, fileID string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return wrapDBErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var rootPath string
	err = tx.QueryRow(ctx, `select path from file_nodes where user_id = $1 and id = $2 and deleted_at is not null for update`, userID, fileID).Scan(&rootPath)
	if err != nil {
		return wrapNotFound(err, "deleted file not found")
	}
	pathPattern := escapePostgresLikePrefix(rootPath) + "/%"
	if _, err = tx.Exec(ctx, `
		update file_nodes set current_version_id = null, parent_id = null
		where user_id = $1 and deleted_at is not null and (id = $2 or path like $3 escape '\')
	`, userID, fileID, pathPattern); err != nil {
		return wrapDBErr(err)
	}
	result, err := tx.Exec(ctx, `
		delete from file_nodes
		where user_id = $1 and deleted_at is not null and (id = $2 or path like $3 escape '\')
	`, userID, fileID, pathPattern)
	if err != nil {
		return wrapDBErr(err)
	}
	if result.RowsAffected() == 0 {
		return domain.E(domain.CodeNotFound, "deleted file not found", nil)
	}
	return wrapDBErr(tx.Commit(ctx))
}

func (r *Repository) PurgeExpiredDeletedFiles(ctx context.Context, cutoff time.Time, limit int32) (int64, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := r.pool.Query(ctx, `
		select n.user_id, n.id
		from file_nodes n
		where n.deleted_at < $1
		  and (n.parent_id is null or not exists (
			select 1 from file_nodes p where p.id = n.parent_id and p.user_id = n.user_id and p.deleted_at is not null
		  ))
		order by n.deleted_at, n.id
		limit $2
	`, cutoff, limit)
	if err != nil {
		return 0, wrapDBErr(err)
	}
	type target struct{ userID, fileID string }
	targets := make([]target, 0)
	for rows.Next() {
		var item target
		if err := rows.Scan(&item.userID, &item.fileID); err != nil {
			rows.Close()
			return 0, wrapDBErr(err)
		}
		targets = append(targets, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, wrapDBErr(err)
	}
	rows.Close()
	var purged int64
	for _, item := range targets {
		if err := r.PurgeDeletedFile(ctx, item.userID, item.fileID); err != nil {
			if domain.ErrorCodeOf(err) == domain.CodeNotFound {
				continue
			}
			return purged, err
		}
		purged++
	}
	return purged, nil
}

func (r *Repository) ListFileVersions(ctx context.Context, userID, fileID string, limit int32) ([]domain.FileVersion, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		select v.id, v.file_id, v.user_id, v.version, v.size, v.sha256, v.storage_key, v.created_by_device_id, v.pinned_at, v.created_at
		from file_versions v
		join file_nodes n on n.id = v.file_id and n.user_id = v.user_id
		where v.user_id = $1 and v.file_id = $2 and n.deleted_at is null
		order by v.version desc
		limit $3
	`, userID, fileID, limit)
	if err != nil {
		return nil, wrapDBErr(err)
	}
	defer rows.Close()

	versions := make([]domain.FileVersion, 0)
	for rows.Next() {
		var version domain.FileVersion
		if err := rows.Scan(fileVersionScan(&version)...); err != nil {
			return nil, wrapDBErr(err)
		}
		versions = append(versions, version)
	}
	return versions, wrapDBErr(rows.Err())
}

func (r *Repository) PinFileVersion(ctx context.Context, userID, fileID string, version int64) (domain.FileVersion, error) {
	var pinned domain.FileVersion
	err := r.pool.QueryRow(ctx, `
		update file_versions v
		set pinned_at = coalesce(v.pinned_at, now())
		from file_nodes n
		where v.user_id = $1 and v.file_id = $2 and v.version = $3
			and n.id = v.file_id and n.user_id = v.user_id
			and n.node_type = $4 and n.deleted_at is null
		returning v.id, v.file_id, v.user_id, v.version, v.size, v.sha256, v.storage_key, v.created_by_device_id, v.pinned_at, v.created_at
	`, userID, fileID, version, domain.NodeTypeFile).Scan(fileVersionScan(&pinned)...)
	return pinned, wrapNotFound(err, "file version not found")
}

func (r *Repository) UnpinFileVersion(ctx context.Context, userID, fileID string, version int64) (domain.FileVersion, error) {
	var unpinned domain.FileVersion
	err := r.pool.QueryRow(ctx, `
		update file_versions v
		set pinned_at = null
		from file_nodes n
		where v.user_id = $1 and v.file_id = $2 and v.version = $3
			and n.id = v.file_id and n.user_id = v.user_id
			and n.node_type = $4 and n.deleted_at is null
		returning v.id, v.file_id, v.user_id, v.version, v.size, v.sha256, v.storage_key, v.created_by_device_id, v.pinned_at, v.created_at
	`, userID, fileID, version, domain.NodeTypeFile).Scan(fileVersionScan(&unpinned)...)
	return unpinned, wrapNotFound(err, "file version not found")
}

func (r *Repository) RestoreFileVersion(ctx context.Context, userID, fileID string, version int64, sourceDeviceID *string) (domain.FileNode, int64, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.FileNode{}, 0, wrapDBErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	node, err := r.getFileByIDTx(ctx, tx, userID, fileID)
	if err != nil {
		return domain.FileNode{}, 0, err
	}
	if node.NodeType != domain.NodeTypeFile {
		return domain.FileNode{}, 0, domain.E(domain.CodeInvalidArgument, "only files can be restored", nil)
	}

	var source domain.FileVersion
	err = tx.QueryRow(ctx, `
		select id, file_id, user_id, version, size, sha256, storage_key, created_by_device_id, pinned_at, created_at
		from file_versions
		where user_id = $1 and file_id = $2 and version = $3
	`, userID, fileID, version).Scan(fileVersionScan(&source)...)
	if err != nil {
		return domain.FileNode{}, 0, wrapNotFound(err, "file version not found")
	}

	newVersion := node.Version + 1
	versionID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		insert into file_versions (id, file_id, user_id, version, size, sha256, storage_key, created_by_device_id)
		values ($1,$2,$3,$4,$5,$6,$7,$8)
	`, versionID, fileID, userID, newVersion, source.Size, source.SHA256, source.StorageKey, source.CreatedByDeviceID)
	if err != nil {
		return domain.FileNode{}, 0, wrapDBErr(err)
	}
	var restored domain.FileNode
	err = tx.QueryRow(ctx, `
		update file_nodes
		set current_version_id = $3, size = $4, sha256 = $5, storage_key = $6, version = $7, updated_at = now()
		where user_id = $1 and id = $2 and deleted_at is null
		returning id, user_id, parent_id, name, path, node_type, current_version_id, size, sha256, storage_key, version, deleted_at, created_at, updated_at
	`, userID, fileID, versionID, source.Size, source.SHA256, source.StorageKey, newVersion).Scan(fileNodeScan(&restored)...)
	if err != nil {
		return domain.FileNode{}, 0, wrapDBErr(err)
	}
	changeID, err := r.createChangeEvent(ctx, tx, userID, fileID, domain.EventRestore, &restored.Version, restored.Path, nil, sourceDeviceID)
	if err != nil {
		return domain.FileNode{}, 0, err
	}
	return restored, changeID, wrapDBErr(tx.Commit(ctx))
}

func (r *Repository) MoveFile(ctx context.Context, userID, fileID, newPath, newName string, newParentID *string, baseVersion *int64, sourceDeviceID *string) (domain.FileNode, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.FileNode{}, wrapDBErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	old, err := r.getFileByIDTx(ctx, tx, userID, fileID)
	if err != nil {
		return domain.FileNode{}, err
	}
	if old.NodeType == domain.NodeTypeDirectory && isDescendantPath(newPath, old.Path) {
		return domain.FileNode{}, domain.E(domain.CodeInvalidArgument, "directory cannot be moved into itself", nil)
	}
	if baseVersion != nil && old.Version != *baseVersion {
		if err := createVersionConflictTx(ctx, tx, userID, old, baseVersion); err != nil {
			return domain.FileNode{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.FileNode{}, wrapDBErr(err)
		}
		return domain.FileNode{}, domain.E(domain.CodeFileConflict, "base version conflict", nil)
	}

	var node domain.FileNode
	err = tx.QueryRow(ctx, `
		update file_nodes
		set parent_id = $3, name = $4, path = $5, version = version + 1, updated_at = now()
		where user_id = $1 and id = $2 and deleted_at is null
		returning id, user_id, parent_id, name, path, node_type, current_version_id, size, sha256, storage_key, version, deleted_at, created_at, updated_at
	`, userID, fileID, newParentID, newName, newPath).Scan(fileNodeScan(&node)...)
	if isUniqueViolation(err) {
		return domain.FileNode{}, domain.E(domain.CodeAlreadyExists, "file path already exists", err)
	}
	if err != nil {
		return domain.FileNode{}, wrapNotFound(err, "file not found")
	}
	if old.NodeType == domain.NodeTypeDirectory {
		_, err = tx.Exec(ctx, `
			update file_nodes
			set path = $2 || substring(path from length($3) + 1), updated_at = now()
			where user_id = $1 and deleted_at is null
				and path like replace(replace(replace($3, '\', '\\'), '%', '\%'), '_', '\_') || '/%' escape '\'
		`, userID, newPath, old.Path)
		if isUniqueViolation(err) {
			return domain.FileNode{}, domain.E(domain.CodeAlreadyExists, "file path already exists", err)
		}
		if err != nil {
			return domain.FileNode{}, wrapDBErr(err)
		}
	}
	if _, err = r.createChangeEvent(ctx, tx, userID, node.ID, domain.EventMove, &node.Version, node.Path, &old.Path, sourceDeviceID); err != nil {
		return domain.FileNode{}, err
	}
	return node, wrapDBErr(tx.Commit(ctx))
}

func (r *Repository) DeleteFile(ctx context.Context, userID, fileID string, baseVersion *int64, sourceDeviceID *string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return wrapDBErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	node, err := r.getFileByIDTx(ctx, tx, userID, fileID)
	if err != nil {
		return err
	}
	if baseVersion != nil && node.Version != *baseVersion {
		if err := createVersionConflictTx(ctx, tx, userID, node, baseVersion); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return wrapDBErr(err)
		}
		return domain.E(domain.CodeFileConflict, "base version conflict", nil)
	}
	nextVersion := node.Version + 1
	tag, err := tx.Exec(ctx, `
		update file_nodes
		set deleted_at = now(), version = $3, updated_at = now()
		where user_id = $1 and id = $2 and deleted_at is null
	`, userID, fileID, nextVersion)
	if err != nil {
		return wrapDBErr(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.E(domain.CodeFileNotFound, "file not found", nil)
	}
	if node.NodeType == domain.NodeTypeDirectory {
		if _, err := tx.Exec(ctx, `
			update file_nodes
			set deleted_at = now(), version = version + 1, updated_at = now()
			where user_id = $1 and deleted_at is null
				and path like replace(replace(replace($2, '\', '\\'), '%', '\%'), '_', '\_') || '/%' escape '\'
		`, userID, node.Path); err != nil {
			return wrapDBErr(err)
		}
	}
	if _, err = r.createChangeEvent(ctx, tx, userID, fileID, domain.EventDelete, &nextVersion, node.Path, &node.Path, sourceDeviceID); err != nil {
		return err
	}
	return wrapDBErr(tx.Commit(ctx))
}

func (r *Repository) CreateUploadSession(ctx context.Context, s domain.UploadSession) (domain.UploadSession, error) {
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	if s.StagingKey == "" {
		s.StagingKey = "staging/" + s.UserID + "/" + s.ID
	}
	if s.Status == "" {
		s.Status = domain.UploadStatusPending
	}
	err := r.pool.QueryRow(ctx, `
		insert into upload_sessions (id, user_id, target_path, target_file_id, base_version, total_size, chunk_size, sha256, status, staging_key, expires_at, idempotency_key, source_device_id)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		returning id, user_id, target_path, target_file_id, base_version, total_size, chunk_size, sha256, status, staging_key, expires_at, idempotency_key, source_device_id, created_at, updated_at
	`, s.ID, s.UserID, s.TargetPath, s.TargetFileID, s.BaseVersion, s.TotalSize, s.ChunkSize, s.SHA256, s.Status, s.StagingKey, s.ExpiresAt, s.IdempotencyKey, s.SourceDeviceID).Scan(uploadSessionScan(&s)...)
	if isUniqueViolation(err) && s.IdempotencyKey != nil {
		return r.getUploadSessionByIdempotencyKey(ctx, s.UserID, *s.IdempotencyKey)
	}
	return s, wrapDBErr(err)
}

func (r *Repository) GetUploadSession(ctx context.Context, userID, uploadID string) (domain.UploadSession, error) {
	var s domain.UploadSession
	err := r.pool.QueryRow(ctx, `
		select id, user_id, target_path, target_file_id, base_version, total_size, chunk_size, sha256, status, staging_key, expires_at, idempotency_key, source_device_id, created_at, updated_at
		from upload_sessions
		where user_id = $1 and id = $2
	`, userID, uploadID).Scan(uploadSessionScan(&s)...)
	return s, wrapNotFound(err, "upload session not found")
}

func (r *Repository) AbortUploadSession(ctx context.Context, userID, uploadID string) (domain.UploadSession, error) {
	var session domain.UploadSession
	err := r.pool.QueryRow(ctx, `
		update upload_sessions
		set status = $3, updated_at = now()
		where user_id = $1 and id = $2 and status = $4
		returning id, user_id, target_path, target_file_id, base_version, total_size, chunk_size, sha256, status, staging_key, expires_at, idempotency_key, source_device_id, created_at, updated_at
	`, userID, uploadID, domain.UploadStatusAborted, domain.UploadStatusPending).Scan(uploadSessionScan(&session)...)
	if errors.Is(err, pgx.ErrNoRows) {
		session, err = r.GetUploadSession(ctx, userID, uploadID)
		if err != nil {
			return domain.UploadSession{}, err
		}
		if session.Status != domain.UploadStatusAborted {
			return domain.UploadSession{}, domain.E(domain.CodeUploadSessionExpired, "upload session cannot be aborted", nil)
		}
	}
	return session, wrapDBErr(err)
}

func (r *Repository) ExpireUploadSessions(ctx context.Context, now time.Time, limit int32) (int64, error) {
	if limit <= 0 {
		limit = 1000
	}
	tag, err := r.pool.Exec(ctx, `
		with expired as (
			select id
			from upload_sessions
			where status = $1 and expires_at <= $2
			order by expires_at
			limit $3
		)
		update upload_sessions
		set status = $4, updated_at = now()
		where id in (select id from expired)
	`, domain.UploadStatusPending, now, limit, domain.UploadStatusExpired)
	return tag.RowsAffected(), wrapDBErr(err)
}

func (r *Repository) DeleteExpiredFileVersions(ctx context.Context, cutoff time.Time, minVersions int64, limit int32) (int64, error) {
	if minVersions <= 0 {
		minVersions = 20
	}
	if limit <= 0 {
		limit = 1000
	}
	tag, err := r.pool.Exec(ctx, `
		with ranked as (
			select v.id, v.pinned_at, v.created_at, n.current_version_id,
				row_number() over (partition by v.file_id order by v.version desc) as version_rank
			from file_versions v
			join file_nodes n on n.id = v.file_id and n.user_id = v.user_id
		), candidates as (
			select id
			from ranked
			where pinned_at is null
				and id <> current_version_id
				and created_at < $1
				and version_rank > $2
			order by created_at, id
			limit $3
		)
		delete from file_versions
		where id in (select id from candidates)
	`, cutoff, minVersions, limit)
	return tag.RowsAffected(), wrapDBErr(err)
}

func (r *Repository) getUploadSessionByIdempotencyKey(ctx context.Context, userID, idempotencyKey string) (domain.UploadSession, error) {
	var s domain.UploadSession
	err := r.pool.QueryRow(ctx, `
		select id, user_id, target_path, target_file_id, base_version, total_size, chunk_size, sha256, status, staging_key, expires_at, idempotency_key, source_device_id, created_at, updated_at
		from upload_sessions
		where user_id = $1 and idempotency_key = $2
	`, userID, idempotencyKey).Scan(uploadSessionScan(&s)...)
	return s, wrapNotFound(err, "upload session not found")
}

func (r *Repository) PutUploadChunk(ctx context.Context, uploadID string, chunkIndex, size int32, sha256sum, storageKey string) (domain.UploadChunk, error) {
	chunk := domain.UploadChunk{ID: uuid.NewString(), UploadID: uploadID, ChunkIndex: chunkIndex, Size: size, SHA256: sha256sum, StorageKey: storageKey}
	err := r.pool.QueryRow(ctx, `
		insert into upload_chunks (id, upload_id, chunk_index, size, sha256, storage_key)
		select $1,$2,$3,$4,$5,$6
		where exists (select 1 from upload_sessions where id = $2 and status = $7)
		on conflict (upload_id, chunk_index) do update
		set size = excluded.size, sha256 = excluded.sha256, storage_key = excluded.storage_key
		returning id, upload_id, chunk_index, size, sha256, storage_key, created_at
	`, chunk.ID, chunk.UploadID, chunk.ChunkIndex, chunk.Size, chunk.SHA256, chunk.StorageKey, domain.UploadStatusPending).Scan(&chunk.ID, &chunk.UploadID, &chunk.ChunkIndex, &chunk.Size, &chunk.SHA256, &chunk.StorageKey, &chunk.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.UploadChunk{}, domain.E(domain.CodeUploadSessionExpired, "upload session is not active", nil)
	}
	return chunk, wrapDBErr(err)
}

func (r *Repository) ListUploadChunks(ctx context.Context, uploadID string) ([]domain.UploadChunk, error) {
	rows, err := r.pool.Query(ctx, `
		select id, upload_id, chunk_index, size, sha256, storage_key, created_at
		from upload_chunks
		where upload_id = $1
		order by chunk_index
	`, uploadID)
	if err != nil {
		return nil, wrapDBErr(err)
	}
	defer rows.Close()
	var chunks []domain.UploadChunk
	for rows.Next() {
		var chunk domain.UploadChunk
		if err := rows.Scan(&chunk.ID, &chunk.UploadID, &chunk.ChunkIndex, &chunk.Size, &chunk.SHA256, &chunk.StorageKey, &chunk.CreatedAt); err != nil {
			return nil, wrapDBErr(err)
		}
		chunks = append(chunks, chunk)
	}
	return chunks, wrapDBErr(rows.Err())
}

func (r *Repository) ListExpiredUploadChunks(ctx context.Context, limit int32) ([]domain.ExpiredUploadChunk, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := r.pool.Query(ctx, `
		select c.id, c.storage_key
		from upload_chunks c
		join upload_sessions s on s.id = c.upload_id
		where s.status in ($1, $2)
		order by s.expires_at, c.created_at, c.id
		limit $3
	`, domain.UploadStatusExpired, domain.UploadStatusAborted, limit)
	if err != nil {
		return nil, wrapDBErr(err)
	}
	defer rows.Close()

	var chunks []domain.ExpiredUploadChunk
	for rows.Next() {
		var chunk domain.ExpiredUploadChunk
		if err := rows.Scan(&chunk.ID, &chunk.StorageKey); err != nil {
			return nil, wrapDBErr(err)
		}
		chunks = append(chunks, chunk)
	}
	return chunks, wrapDBErr(rows.Err())
}

func (r *Repository) DeleteUploadChunk(ctx context.Context, chunkID string) error {
	_, err := r.pool.Exec(ctx, `delete from upload_chunks where id = $1`, chunkID)
	return wrapDBErr(err)
}

func (r *Repository) CommitUpload(ctx context.Context, userID, uploadID, storageKey string) (domain.FileNode, int64, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.FileNode{}, 0, wrapDBErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var s domain.UploadSession
	err = tx.QueryRow(ctx, `
		select id, user_id, target_path, target_file_id, base_version, total_size, chunk_size, sha256, status, staging_key, expires_at, idempotency_key, source_device_id, created_at, updated_at
		from upload_sessions
		where user_id = $1 and id = $2
		for update
	`, userID, uploadID).Scan(uploadSessionScan(&s)...)
	if err != nil {
		return domain.FileNode{}, 0, wrapNotFound(err, "upload session not found")
	}
	if s.Status == domain.UploadStatusCommitted {
		node, err := r.getFileByPathTx(ctx, tx, userID, s.TargetPath)
		if err != nil {
			return domain.FileNode{}, 0, err
		}
		return node, 0, tx.Commit(ctx)
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
		if err := createSyncConflictTx(ctx, tx, domain.SyncConflict{
			UserID:        userID,
			FileID:        &existing.ID,
			Path:          s.TargetPath,
			LocalVersion:  s.BaseVersion,
			RemoteVersion: &remoteVersion,
		}); err != nil {
			return domain.FileNode{}, 0, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.FileNode{}, 0, wrapDBErr(err)
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
	if existingErr == nil {
		newVersion := existing.Version + 1
		versionID := uuid.NewString()
		_, err = tx.Exec(ctx, `
			insert into file_versions (id, file_id, user_id, version, size, sha256, storage_key, created_by_device_id)
			values ($1,$2,$3,$4,$5,$6,$7,$8)
		`, versionID, existing.ID, userID, newVersion, s.TotalSize, s.SHA256, storageKey, s.SourceDeviceID)
		if err != nil {
			return domain.FileNode{}, 0, wrapDBErr(err)
		}
		err = tx.QueryRow(ctx, `
			update file_nodes
			set current_version_id = $3, size = $4, sha256 = $5, storage_key = $6, version = $7, updated_at = now()
			where user_id = $1 and id = $2
			returning id, user_id, parent_id, name, path, node_type, current_version_id, size, sha256, storage_key, version, deleted_at, created_at, updated_at
		`, userID, existing.ID, versionID, s.TotalSize, s.SHA256, storageKey, newVersion).Scan(fileNodeScan(&node)...)
		if err != nil {
			return domain.FileNode{}, 0, wrapDBErr(err)
		}
	} else {
		eventType = domain.EventCreate
		fileID := uuid.NewString()
		versionID := uuid.NewString()
		_, err = tx.Exec(ctx, `
			insert into file_nodes (id, user_id, parent_id, name, path, node_type, current_version_id, size, sha256, storage_key, version)
			values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,1)
		`, fileID, userID, parentID, name, s.TargetPath, domain.NodeTypeFile, versionID, s.TotalSize, s.SHA256, storageKey)
		if err != nil {
			return domain.FileNode{}, 0, wrapDBErr(err)
		}
		_, err = tx.Exec(ctx, `
			insert into file_versions (id, file_id, user_id, version, size, sha256, storage_key, created_by_device_id)
			values ($1,$2,$3,1,$4,$5,$6,$7)
		`, versionID, fileID, userID, s.TotalSize, s.SHA256, storageKey, s.SourceDeviceID)
		if err != nil {
			return domain.FileNode{}, 0, wrapDBErr(err)
		}
		node, err = r.getFileByPathTx(ctx, tx, userID, s.TargetPath)
		if err != nil {
			return domain.FileNode{}, 0, err
		}
	}
	changeID, err := r.createChangeEvent(ctx, tx, userID, node.ID, eventType, &node.Version, node.Path, nil, s.SourceDeviceID)
	if err != nil {
		return domain.FileNode{}, 0, err
	}
	_, err = tx.Exec(ctx, `update upload_sessions set status = $3, updated_at = now() where user_id = $1 and id = $2`, userID, uploadID, domain.UploadStatusCommitted)
	if err != nil {
		return domain.FileNode{}, 0, wrapDBErr(err)
	}
	return node, changeID, wrapDBErr(tx.Commit(ctx))
}

func (r *Repository) CreateDevice(ctx context.Context, userID, name, platform string) (domain.Device, error) {
	device := domain.Device{ID: uuid.NewString(), UserID: userID, Name: name, Platform: platform}
	err := r.pool.QueryRow(ctx, `
		insert into devices (id, user_id, name, platform, last_applied_change_id)
		values ($1, $2, $3, $4, $5)
		returning id, user_id, name, platform, last_seen_at, last_sync_at, last_sync_status, last_sync_error, last_applied_change_id, created_at, updated_at
	`, device.ID, device.UserID, device.Name, device.Platform, device.LastAppliedChangeID).Scan(deviceScan(&device)...)
	return device, wrapDBErr(err)
}

func (r *Repository) ListDevices(ctx context.Context, userID string, limit int32) ([]domain.Device, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		select id, user_id, name, platform, last_seen_at, last_sync_at, last_sync_status, last_sync_error, last_applied_change_id, created_at, updated_at
		from devices
		where user_id = $1
		order by updated_at desc, id desc
		limit $2
	`, userID, limit)
	if err != nil {
		return nil, wrapDBErr(err)
	}
	defer rows.Close()

	devices := make([]domain.Device, 0)
	for rows.Next() {
		var device domain.Device
		if err := rows.Scan(deviceScan(&device)...); err != nil {
			return nil, wrapDBErr(err)
		}
		devices = append(devices, device)
	}
	return devices, wrapDBErr(rows.Err())
}

func (r *Repository) DeleteDevice(ctx context.Context, userID, deviceID string) error {
	result, err := r.pool.Exec(ctx, `delete from devices where user_id = $1 and id = $2`, userID, deviceID)
	if err != nil {
		return wrapDBErr(err)
	}
	if result.RowsAffected() == 0 {
		return domain.E(domain.CodeNotFound, "device not found", nil)
	}
	return nil
}

func (r *Repository) HeartbeatDevice(ctx context.Context, userID, deviceID, status, syncError string) (domain.Device, error) {
	var device domain.Device
	err := r.pool.QueryRow(ctx, `
		update devices
		set last_seen_at = now(),
			last_sync_at = case when $3 = '' then last_sync_at else now() end,
			last_sync_status = coalesce(nullif($3, ''), last_sync_status),
			last_sync_error = case when $3 = '' then last_sync_error when $3 = 'success' then null else nullif($4, '') end,
			updated_at = now()
		where user_id = $1 and id = $2
		returning id, user_id, name, platform, last_seen_at, last_sync_at, last_sync_status, last_sync_error, last_applied_change_id, created_at, updated_at
	`, userID, deviceID, status, syncError).Scan(deviceScan(&device)...)
	return device, wrapNotFound(err, "device not found")
}

func (r *Repository) ListChanges(ctx context.Context, userID, deviceID string, afterChangeID int64, limit int32) ([]domain.ChangeEvent, error) {
	if _, err := r.getDevice(ctx, userID, deviceID); err != nil {
		return nil, err
	}
	if err := r.validateChangeCursor(ctx, userID, afterChangeID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := r.pool.Query(ctx, `
		select id, user_id, file_id, event_type, version, path, old_path, source_device_id, created_at
		from change_events
		where user_id = $1 and id > $2
		order by id
		limit $3
	`, userID, afterChangeID, limit)
	if err != nil {
		return nil, wrapDBErr(err)
	}
	defer rows.Close()

	var events []domain.ChangeEvent
	for rows.Next() {
		var event domain.ChangeEvent
		if err := rows.Scan(changeEventScan(&event)...); err != nil {
			return nil, wrapDBErr(err)
		}
		events = append(events, event)
	}
	return events, wrapDBErr(rows.Err())
}

func (r *Repository) ListActivity(ctx context.Context, userID, fileID string, beforeEventID int64, limit int32) ([]domain.ChangeEvent, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		select id, user_id, file_id, event_type, version, path, old_path, source_device_id, created_at
		from change_events
		where user_id = $1
		  and (nullif($2::text, '') is null or file_id = nullif($2::text, '')::uuid)
		  and ($3 = 0 or id < $3)
		order by id desc
		limit $4
	`, userID, fileID, beforeEventID, limit)
	if err != nil {
		return nil, wrapDBErr(err)
	}
	defer rows.Close()

	events := make([]domain.ChangeEvent, 0)
	for rows.Next() {
		var event domain.ChangeEvent
		if err := rows.Scan(changeEventScan(&event)...); err != nil {
			return nil, wrapDBErr(err)
		}
		events = append(events, event)
	}
	return events, wrapDBErr(rows.Err())
}

func (r *Repository) validateChangeCursor(ctx context.Context, userID string, afterChangeID int64) error {
	if afterChangeID == 0 {
		return nil
	}
	var minID, maxID, count int64
	err := r.pool.QueryRow(ctx, `
		select coalesce(min(id), 0), coalesce(max(id), 0), count(*)
		from change_events
		where user_id = $1
	`, userID).Scan(&minID, &maxID, &count)
	if err != nil {
		return wrapDBErr(err)
	}
	if count == 0 || afterChangeID > maxID || afterChangeID < minID-1 {
		return domain.E(domain.CodeSyncCursorExpired, "sync cursor is outside the available change feed; run a full scan", nil)
	}
	return nil
}

func (r *Repository) AckDevice(ctx context.Context, userID, deviceID string, lastAppliedChangeID int64) (domain.Device, error) {
	var device domain.Device
	err := r.pool.QueryRow(ctx, `
		update devices
		set last_applied_change_id = $3, updated_at = now()
		where user_id = $1 and id = $2
		returning id, user_id, name, platform, last_seen_at, last_sync_at, last_sync_status, last_sync_error, last_applied_change_id, created_at, updated_at
	`, userID, deviceID, lastAppliedChangeID).Scan(deviceScan(&device)...)
	return device, wrapNotFound(err, "device not found")
}

func (r *Repository) CreateSyncConflict(ctx context.Context, conflict domain.SyncConflict) (domain.SyncConflict, error) {
	if conflict.ID == "" {
		conflict.ID = uuid.NewString()
	}
	if conflict.Resolution == "" {
		conflict.Resolution = domain.ConflictResolutionPending
	}
	err := r.pool.QueryRow(ctx, `
		insert into sync_conflicts (id, user_id, file_id, path, local_version, remote_version, resolution)
		values ($1,$2,$3,$4,$5,$6,$7)
		returning id, user_id, file_id, path, local_version, remote_version, resolution, created_at, resolved_at
	`, conflict.ID, conflict.UserID, conflict.FileID, conflict.Path, conflict.LocalVersion, conflict.RemoteVersion, conflict.Resolution).Scan(syncConflictScan(&conflict)...)
	return conflict, wrapDBErr(err)
}

func createSyncConflictTx(ctx context.Context, tx pgx.Tx, conflict domain.SyncConflict) error {
	if conflict.ID == "" {
		conflict.ID = uuid.NewString()
	}
	if conflict.Resolution == "" {
		conflict.Resolution = domain.ConflictResolutionPending
	}
	_, err := tx.Exec(ctx, `
		insert into sync_conflicts (id, user_id, file_id, path, local_version, remote_version, resolution)
		values ($1,$2,$3,$4,$5,$6,$7)
	`, conflict.ID, conflict.UserID, conflict.FileID, conflict.Path, conflict.LocalVersion, conflict.RemoteVersion, conflict.Resolution)
	return wrapDBErr(err)
}

func createVersionConflictTx(ctx context.Context, tx pgx.Tx, userID string, node domain.FileNode, localVersion *int64) error {
	remoteVersion := node.Version
	return createSyncConflictTx(ctx, tx, domain.SyncConflict{
		UserID:        userID,
		FileID:        &node.ID,
		Path:          node.Path,
		LocalVersion:  localVersion,
		RemoteVersion: &remoteVersion,
	})
}

func (r *Repository) ListSyncConflicts(ctx context.Context, userID, resolution string, limit int32) ([]domain.SyncConflict, error) {
	if resolution == "" {
		resolution = domain.ConflictResolutionPending
	}
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := r.pool.Query(ctx, `
		select id, user_id, file_id, path, local_version, remote_version, resolution, created_at, resolved_at
		from sync_conflicts
		where user_id = $1 and resolution = $2
		order by created_at desc, id desc
		limit $3
	`, userID, resolution, limit)
	if err != nil {
		return nil, wrapDBErr(err)
	}
	defer rows.Close()

	var conflicts []domain.SyncConflict
	for rows.Next() {
		var conflict domain.SyncConflict
		if err := rows.Scan(syncConflictScan(&conflict)...); err != nil {
			return nil, wrapDBErr(err)
		}
		conflicts = append(conflicts, conflict)
	}
	return conflicts, wrapDBErr(rows.Err())
}

func (r *Repository) UpdateSyncConflictResolution(ctx context.Context, userID, conflictID, resolution string) (domain.SyncConflict, error) {
	var conflict domain.SyncConflict
	err := r.pool.QueryRow(ctx, `
		update sync_conflicts
		set resolution = $3,
		    resolved_at = case when $3 = $4 then null else now() end
		where user_id = $1 and id = $2
		returning id, user_id, file_id, path, local_version, remote_version, resolution, created_at, resolved_at
	`, userID, conflictID, resolution, domain.ConflictResolutionPending).Scan(syncConflictScan(&conflict)...)
	return conflict, wrapNotFound(err, "sync conflict not found")
}

func (r *Repository) getDevice(ctx context.Context, userID, deviceID string) (domain.Device, error) {
	var device domain.Device
	err := r.pool.QueryRow(ctx, `
		select id, user_id, name, platform, last_seen_at, last_sync_at, last_sync_status, last_sync_error, last_applied_change_id, created_at, updated_at
		from devices
		where user_id = $1 and id = $2
	`, userID, deviceID).Scan(deviceScan(&device)...)
	return device, wrapNotFound(err, "device not found")
}

func (r *Repository) getFileByIDTx(ctx context.Context, tx pgx.Tx, userID, fileID string) (domain.FileNode, error) {
	var node domain.FileNode
	err := tx.QueryRow(ctx, `
		select id, user_id, parent_id, name, path, node_type, current_version_id, size, sha256, storage_key, version, deleted_at, created_at, updated_at
		from file_nodes
		where user_id = $1 and id = $2 and deleted_at is null
		for update
	`, userID, fileID).Scan(fileNodeScan(&node)...)
	return node, wrapNotFound(err, "file not found")
}

func (r *Repository) getFileByPathTx(ctx context.Context, tx pgx.Tx, userID, path string) (domain.FileNode, error) {
	var node domain.FileNode
	err := tx.QueryRow(ctx, `
		select id, user_id, parent_id, name, path, node_type, current_version_id, size, sha256, storage_key, version, deleted_at, created_at, updated_at
		from file_nodes
		where user_id = $1 and path = $2 and deleted_at is null
	`, userID, path).Scan(fileNodeScan(&node)...)
	return node, wrapNotFound(err, "file not found")
}

func (r *Repository) createChangeEvent(ctx context.Context, tx pgx.Tx, userID, fileID, eventType string, version *int64, path string, oldPath, sourceDeviceID *string) (int64, error) {
	query := `
		insert into change_events (user_id, file_id, event_type, version, path, old_path, source_device_id)
		values ($1,$2,$3,$4,$5,$6,$7)
		returning id
	`
	var id int64
	var err error
	if tx != nil {
		err = tx.QueryRow(ctx, query, userID, fileID, eventType, version, path, oldPath, sourceDeviceID).Scan(&id)
	} else {
		err = r.pool.QueryRow(ctx, query, userID, fileID, eventType, version, path, oldPath, sourceDeviceID).Scan(&id)
	}
	return id, wrapDBErr(err)
}

func fileNodeScan(n *domain.FileNode) []any {
	return []any{&n.ID, &n.UserID, &n.ParentID, &n.Name, &n.Path, &n.NodeType, &n.CurrentVersionID, &n.Size, &n.SHA256, &n.StorageKey, &n.Version, &n.DeletedAt, &n.CreatedAt, &n.UpdatedAt}
}

func fileVersionScan(v *domain.FileVersion) []any {
	return []any{&v.ID, &v.FileID, &v.UserID, &v.Version, &v.Size, &v.SHA256, &v.StorageKey, &v.CreatedByDeviceID, &v.PinnedAt, &v.CreatedAt}
}

func uploadSessionScan(s *domain.UploadSession) []any {
	return []any{&s.ID, &s.UserID, &s.TargetPath, &s.TargetFileID, &s.BaseVersion, &s.TotalSize, &s.ChunkSize, &s.SHA256, &s.Status, &s.StagingKey, &s.ExpiresAt, &s.IdempotencyKey, &s.SourceDeviceID, &s.CreatedAt, &s.UpdatedAt}
}

func deviceScan(d *domain.Device) []any {
	return []any{&d.ID, &d.UserID, &d.Name, &d.Platform, &d.LastSeenAt, &d.LastSyncAt, &d.LastSyncStatus, &d.LastSyncError, &d.LastAppliedChangeID, &d.CreatedAt, &d.UpdatedAt}
}

func changeEventScan(e *domain.ChangeEvent) []any {
	return []any{&e.ID, &e.UserID, &e.FileID, &e.EventType, &e.Version, &e.Path, &e.OldPath, &e.SourceDeviceID, &e.CreatedAt}
}

func syncConflictScan(c *domain.SyncConflict) []any {
	return []any{&c.ID, &c.UserID, &c.FileID, &c.Path, &c.LocalVersion, &c.RemoteVersion, &c.Resolution, &c.CreatedAt, &c.ResolvedAt}
}

func wrapNotFound(err error, message string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.E(domain.CodeNotFound, message, err)
	}
	return wrapDBErr(err)
}

func wrapDBErr(err error) error {
	if err == nil {
		return nil
	}
	return domain.E(domain.CodeInternal, "database error", err)
}

func escapePostgresLikePrefix(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
