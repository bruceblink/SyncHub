package storage

import (
	"context"
	"io"
)

type ByteRange struct {
	Start int64
	End   *int64
}

type ObjectInfo struct {
	Size int64
}

type Storage interface {
	PutChunk(ctx context.Context, key string, r io.Reader, checksum string) error
	Compose(ctx context.Context, targetKey string, chunkKeys []string) error
	Read(ctx context.Context, key string, br *ByteRange) (io.ReadCloser, ObjectInfo, error)
	Delete(ctx context.Context, key string) error
}
