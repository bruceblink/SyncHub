package db

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bruceblink/SyncHub/internal/domain"
	"github.com/bruceblink/SyncHub/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEmbeddedMigrationsIncludeLatestNewsPreferencesConstraint(t *testing.T) {
	loaded, err := loadPostgresMigrations(migrations.FS)
	if err != nil {
		t.Fatalf("load embedded migrations: %v", err)
	}
	if len(loaded) == 0 {
		t.Fatal("no embedded migrations loaded")
	}
	var preferences *postgresMigration
	for i := range loaded {
		if loaded[i].Version == "000011_add_latestnews_preferences_collection" {
			preferences = &loaded[i]
			break
		}
	}
	if preferences == nil {
		t.Fatal("LatestNews preferences migration is missing")
	}
	if !strings.Contains(preferences.SQL, "'preferences'") || !strings.Contains(preferences.SQL, "app_metadata_documents_collection_check") {
		t.Fatalf("preferences migration does not expand the collection constraint: %s", preferences.SQL)
	}
}

func TestEmbeddedMigrationsIncludeOAuthIdentitySchema(t *testing.T) {
	loaded, err := loadPostgresMigrations(migrations.FS)
	if err != nil {
		t.Fatalf("load embedded migrations: %v", err)
	}
	latest := loaded[len(loaded)-1]
	if latest.Version != "000012_add_oauth_identities" {
		t.Fatalf("latest migration = %q", latest.Version)
	}
	for _, fragment := range []string{"oauth_identities", "oauth_login_codes", "password_hash drop not null"} {
		if !strings.Contains(latest.SQL, fragment) {
			t.Fatalf("OAuth migration is missing %q: %s", fragment, latest.SQL)
		}
	}
}

func TestApplyPostgresMigrationsCreatesConflictSchema(t *testing.T) {
	repo, pool := newPostgresMigrationTestRepository(t)
	ctx := context.Background()

	var tableExists bool
	if err := pool.QueryRow(ctx, `
		select exists(
			select 1
			from information_schema.tables
			where table_schema = current_schema()
				and table_name = 'sync_conflicts'
		)
	`).Scan(&tableExists); err != nil {
		t.Fatalf("check sync_conflicts table: %v", err)
	}
	if !tableExists {
		t.Fatal("sync_conflicts table was not created")
	}

	email := "postgres-conflict-" + uuid.NewString() + "@example.com"
	user, err := repo.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	localVersion := int64(1)
	remoteVersion := int64(2)
	created, err := repo.CreateSyncConflict(ctx, domain.SyncConflict{
		UserID:        user.ID,
		Path:          "/workspace/a.txt",
		LocalVersion:  &localVersion,
		RemoteVersion: &remoteVersion,
	})
	if err != nil {
		t.Fatalf("create sync conflict: %v", err)
	}
	conflicts, err := repo.ListSyncConflicts(ctx, user.ID, "", 100)
	if err != nil {
		t.Fatalf("list sync conflicts: %v", err)
	}
	if len(conflicts) != 1 || conflicts[0].ID != created.ID {
		t.Fatalf("conflicts = %#v, want created conflict", conflicts)
	}
	resolved, err := repo.UpdateSyncConflictResolution(ctx, user.ID, created.ID, domain.ConflictResolutionKeepBoth)
	if err != nil {
		t.Fatalf("resolve sync conflict: %v", err)
	}
	if resolved.Resolution != domain.ConflictResolutionKeepBoth || resolved.ResolvedAt == nil {
		t.Fatalf("resolved conflict = %#v, want keep_both with resolved_at", resolved)
	}
}

func TestPostgresListActivityAllowsEmptyFileFilter(t *testing.T) {
	repo, _ := newPostgresMigrationTestRepository(t)
	ctx := context.Background()
	user, err := repo.CreateUser(ctx, "postgres-activity-"+uuid.NewString()+"@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := repo.CreateDirectory(ctx, user.ID, "/docs", "docs", nil, nil); err != nil {
		t.Fatalf("create activity event: %v", err)
	}

	events, err := repo.ListActivity(ctx, user.ID, "", 0, 50)
	if err != nil {
		t.Fatalf("list activity without file filter: %v", err)
	}
	if len(events) != 1 || events[0].Path != "/docs" {
		t.Fatalf("activity events = %#v, want /docs create event", events)
	}
}

func TestPostgresStoresLatestNewsPreferencesMetadata(t *testing.T) {
	repo, _ := newPostgresMigrationTestRepository(t)
	ctx := context.Background()
	user, err := repo.CreateUser(ctx, "postgres-preferences-"+uuid.NewString()+"@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	payload := []byte(`{"colorScheme":"auto"}`)

	document, err := repo.PutMetadataDocument(ctx, user.ID, "latestnews", "preferences", payload)
	if err != nil {
		t.Fatalf("store LatestNews preferences: %v", err)
	}
	if document.Collection != "preferences" || string(document.Payload) != string(payload) {
		t.Fatalf("stored preferences = %#v", document)
	}
}

func TestPostgresOAuthIdentityLinksExistingUserAndReusesProviderID(t *testing.T) {
	repo, _ := newPostgresMigrationTestRepository(t)
	ctx := context.Background()
	email := "postgres-oauth-" + uuid.NewString() + "@example.com"
	existing, err := repo.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatalf("create existing user: %v", err)
	}

	linked, err := repo.ResolveOAuthUser(ctx, "github", "github-123", email, "octocat", "avatar-1")
	if err != nil || linked.ID != existing.ID {
		t.Fatalf("link existing user = %#v err=%v", linked, err)
	}
	reused, err := repo.ResolveOAuthUser(ctx, "github", "github-123", "changed@example.com", "octocat-renamed", "avatar-2")
	if err != nil || reused.ID != existing.ID || reused.Email != email {
		t.Fatalf("reuse provider identity = %#v err=%v", reused, err)
	}

	codeHash := "oauth-code-hash"
	if err := repo.CreateOAuthLoginCode(ctx, existing.ID, codeHash, time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("create OAuth code: %v", err)
	}
	if exchanged, err := repo.ConsumeOAuthLoginCode(ctx, codeHash, time.Now()); err != nil || exchanged.ID != existing.ID {
		t.Fatalf("consume OAuth code = %#v err=%v", exchanged, err)
	}
	if _, err := repo.ConsumeOAuthLoginCode(ctx, codeHash, time.Now()); domain.ErrorCodeOf(err) != domain.CodeNotFound {
		t.Fatalf("replayed OAuth code error = %v", err)
	}
}

func TestPostgresOAuthIdentityCreatesPasswordlessUser(t *testing.T) {
	repo, _ := newPostgresMigrationTestRepository(t)
	created, err := repo.ResolveOAuthUser(context.Background(), "github", "github-new-"+uuid.NewString(), "new-oauth-"+uuid.NewString()+"@example.com", "new-user", "")
	if err != nil {
		t.Fatalf("create OAuth user: %v", err)
	}
	if created.ID == "" || created.PasswordHash != "" {
		t.Fatalf("OAuth user = %#v", created)
	}
}

func TestPostgresObjectGCQueueProtectsReferencedAndReusedObjects(t *testing.T) {
	repo, pool := newPostgresMigrationTestRepository(t)
	ctx := context.Background()
	user, err := repo.CreateUser(ctx, "postgres-gc-"+uuid.NewString()+"@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	storageKey := "objects/" + user.ID + "/aa/hash"
	node := commitPostgresTestFile(t, repo, user.ID, "/gc.txt", storageKey)
	if err := repo.DeleteFile(ctx, user.ID, node.ID, nil, nil); err != nil {
		t.Fatalf("soft delete file: %v", err)
	}
	if err := repo.PurgeDeletedFile(ctx, user.ID, node.ID); err != nil {
		t.Fatalf("purge file: %v", err)
	}
	var status string
	if err := pool.QueryRow(ctx, `select status from object_gc_queue where storage_key = $1`, storageKey).Scan(&status); err != nil {
		t.Fatalf("read gc queue: %v", err)
	}
	if status != "pending" {
		t.Fatalf("gc status = %q, want pending", status)
	}
	if _, err := pool.Exec(ctx, `update object_gc_queue set available_at = now() - interval '1 minute' where storage_key = $1`, storageKey); err != nil {
		t.Fatalf("make gc item available: %v", err)
	}
	items, err := repo.ClaimObjectGCItems(ctx, time.Now().UTC(), 10)
	if err != nil || len(items) != 1 || items[0].StorageKey != storageKey {
		t.Fatalf("claimed items = %#v err = %v", items, err)
	}
	if err := repo.ReserveStorageObject(ctx, storageKey); domain.ErrorCodeOf(err) != domain.CodeFileConflict {
		t.Fatalf("reserve processing object error = %v", err)
	}
	if err := repo.RetryObjectGC(ctx, storageKey, "temporary", time.Now().UTC()); err != nil {
		t.Fatalf("retry gc: %v", err)
	}
	if err := repo.ReserveStorageObject(ctx, storageKey); err != nil {
		t.Fatalf("reserve pending object: %v", err)
	}
	var queued bool
	if err := pool.QueryRow(ctx, `select exists(select 1 from object_gc_queue where storage_key = $1)`, storageKey).Scan(&queued); err != nil || queued {
		t.Fatalf("queue remains = %v err = %v", queued, err)
	}
}

func TestPostgresObjectGCDoesNotClaimReferencedObject(t *testing.T) {
	repo, pool := newPostgresMigrationTestRepository(t)
	ctx := context.Background()
	user, err := repo.CreateUser(ctx, "postgres-gc-ref-"+uuid.NewString()+"@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	storageKey := "objects/" + user.ID + "/bb/hash"
	commitPostgresTestFile(t, repo, user.ID, "/referenced.txt", storageKey)
	if _, err := pool.Exec(ctx, `insert into object_gc_queue (storage_key, available_at) values ($1, now() - interval '1 minute')`, storageKey); err != nil {
		t.Fatalf("enqueue referenced object: %v", err)
	}
	items, err := repo.ClaimObjectGCItems(ctx, time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("claim gc items: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("claimed referenced items = %#v", items)
	}
}

func TestPostgresUploadIdempotencyOnlyReusesActiveSession(t *testing.T) {
	repo, _ := newPostgresMigrationTestRepository(t)
	ctx := context.Background()
	user, err := repo.CreateUser(ctx, "postgres-upload-idempotency-"+uuid.NewString()+"@example.com", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	key := "same-content"
	create := func(expiresAt time.Time) domain.UploadSession {
		session, err := repo.CreateUploadSession(ctx, domain.UploadSession{
			UserID: user.ID, TargetPath: "/file.txt", TotalSize: 1, ChunkSize: 1,
			SHA256: "hash", ExpiresAt: expiresAt, IdempotencyKey: &key,
		})
		if err != nil {
			t.Fatalf("create upload session: %v", err)
		}
		return session
	}

	first := create(time.Now().Add(time.Hour))
	reused := create(time.Now().Add(time.Hour))
	if reused.ID != first.ID {
		t.Fatalf("active idempotent session id = %q, want %q", reused.ID, first.ID)
	}
	if _, _, err := repo.CommitUpload(ctx, user.ID, first.ID, "objects/"+user.ID+"/hash"); err != nil {
		t.Fatalf("commit first upload: %v", err)
	}
	afterCommit := create(time.Now().Add(time.Hour))
	if afterCommit.ID == first.ID {
		t.Fatal("committed upload session was reused")
	}

	expiredKey := "expired-content"
	expired, err := repo.CreateUploadSession(ctx, domain.UploadSession{
		UserID: user.ID, TargetPath: "/expired.txt", TotalSize: 1, ChunkSize: 1,
		SHA256: "hash", ExpiresAt: time.Now().Add(-time.Minute), IdempotencyKey: &expiredKey,
	})
	if err != nil {
		t.Fatalf("create expired upload: %v", err)
	}
	replacement, err := repo.CreateUploadSession(ctx, domain.UploadSession{
		UserID: user.ID, TargetPath: "/expired.txt", TotalSize: 1, ChunkSize: 1,
		SHA256: "hash", ExpiresAt: time.Now().Add(time.Hour), IdempotencyKey: &expiredKey,
	})
	if err != nil {
		t.Fatalf("replace expired upload: %v", err)
	}
	if replacement.ID == expired.ID {
		t.Fatal("expired pending upload session was reused")
	}
}

func commitPostgresTestFile(t *testing.T, repo *Repository, userID, targetPath, storageKey string) domain.FileNode {
	t.Helper()
	now := time.Now().UTC()
	session, err := repo.CreateUploadSession(context.Background(), domain.UploadSession{
		ID: uuid.NewString(), UserID: userID, TargetPath: targetPath, TotalSize: 1,
		ChunkSize: 1, SHA256: "hash", Status: domain.UploadStatusPending,
		StagingKey: "staging/" + uuid.NewString(), ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create upload session: %v", err)
	}
	node, _, err := repo.CommitUpload(context.Background(), userID, session.ID, storageKey)
	if err != nil {
		t.Fatalf("commit test file: %v", err)
	}
	return node
}

func newPostgresMigrationTestRepository(t *testing.T) (*Repository, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(adminPool.Close)

	schema := "synchub_test_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	if _, err := adminPool.Exec(ctx, "create schema "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "drop schema if exists "+pgx.Identifier{schema}.Sanitize()+" cascade")
	})

	pool, err := Connect(ctx, dsn, schema)
	if err != nil {
		t.Fatalf("connect test schema: %v", err)
	}
	t.Cleanup(pool.Close)
	var activeSchema string
	if err := pool.QueryRow(ctx, "select current_schema()").Scan(&activeSchema); err != nil {
		t.Fatalf("read active test schema: %v", err)
	}
	if activeSchema != schema {
		t.Fatalf("active test schema = %q, want %q", activeSchema, schema)
	}

	if err := ApplyPostgresMigrations(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return NewRepository(pool), pool
}
