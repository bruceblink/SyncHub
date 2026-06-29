package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Local struct {
	root string
}

func NewLocal(root string) *Local {
	return &Local{root: root}
}

func (s *Local) PutChunk(ctx context.Context, key string, r io.Reader, checksum string) error {
	target, err := s.resolve(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := target + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, copyErr := io.Copy(f, io.TeeReader(r, h))
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if checksum != "" && hex.EncodeToString(h.Sum(nil)) != checksum {
		_ = os.Remove(tmp)
		return errors.New("checksum mismatch")
	}
	select {
	case <-ctx.Done():
		_ = os.Remove(tmp)
		return ctx.Err()
	default:
	}
	return os.Rename(tmp, target)
}

func (s *Local) Compose(ctx context.Context, targetKey string, chunkKeys []string) error {
	target, err := s.resolve(targetKey)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := target + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	for _, key := range chunkKeys {
		select {
		case <-ctx.Done():
			_ = out.Close()
			_ = os.Remove(tmp)
			return ctx.Err()
		default:
		}
		src, err := s.resolve(key)
		if err != nil {
			_ = out.Close()
			_ = os.Remove(tmp)
			return err
		}
		in, err := os.Open(src)
		if err != nil {
			_ = out.Close()
			_ = os.Remove(tmp)
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := in.Close()
		if copyErr != nil {
			_ = out.Close()
			_ = os.Remove(tmp)
			return copyErr
		}
		if closeErr != nil {
			_ = out.Close()
			_ = os.Remove(tmp)
			return closeErr
		}
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, target)
}

func (s *Local) Read(ctx context.Context, key string, br *ByteRange) (io.ReadCloser, ObjectInfo, error) {
	_ = ctx
	target, err := s.resolve(key)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	f, err := os.Open(target)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, ObjectInfo{}, err
	}
	if br != nil {
		if _, err := f.Seek(br.Start, io.SeekStart); err != nil {
			_ = f.Close()
			return nil, ObjectInfo{}, err
		}
		if br.End != nil {
			return &limitedReadCloser{Reader: io.LimitReader(f, *br.End-br.Start+1), closer: f}, ObjectInfo{Size: info.Size()}, nil
		}
	}
	return f, ObjectInfo{Size: info.Size()}, nil
}

func (s *Local) Delete(ctx context.Context, key string) error {
	_ = ctx
	target, err := s.resolve(key)
	if err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Local) resolve(key string) (string, error) {
	key = strings.ReplaceAll(key, "\\", "/")
	if strings.HasPrefix(key, "/") || strings.Contains(key, "../") || key == ".." || strings.ContainsRune(key, 0) {
		return "", errors.New("invalid storage key")
	}
	return filepath.Join(s.root, filepath.FromSlash(key)), nil
}

type limitedReadCloser struct {
	io.Reader
	closer io.Closer
}

func (r *limitedReadCloser) Close() error {
	return r.closer.Close()
}
