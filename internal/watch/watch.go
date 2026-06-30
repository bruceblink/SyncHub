package watch

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/bruceblink/SyncHub/internal/manifest"
)

const (
	ChangeCreated = "created"
	ChangeUpdated = "updated"
	ChangeDeleted = "deleted"
)

type Snapshot map[string]manifest.Entry

type Change struct {
	Type         string
	RelativePath string
	Path         string
	Before       *manifest.Entry
	After        *manifest.Entry
}

type Poller struct {
	root       string
	remotePath string
	snapshot   Snapshot
}

func NewPoller(ctx context.Context, root, remotePath string) (*Poller, error) {
	snapshot, err := Scan(ctx, root, remotePath)
	if err != nil {
		return nil, err
	}
	return &Poller{root: root, remotePath: remotePath, snapshot: snapshot}, nil
}

func (p *Poller) Poll(ctx context.Context) ([]Change, error) {
	current, err := Scan(ctx, p.root, p.remotePath)
	if err != nil {
		return nil, err
	}
	changes := Diff(p.snapshot, current)
	p.snapshot = current
	return changes, nil
}

func (p *Poller) Run(ctx context.Context, interval time.Duration, handle func([]Change) error) error {
	if interval <= 0 {
		return errors.New("watch interval must be positive")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			changes, err := p.Poll(ctx)
			if err != nil {
				return err
			}
			if len(changes) > 0 && handle != nil {
				if err := handle(changes); err != nil {
					return err
				}
			}
		}
	}
}

func Scan(ctx context.Context, root, remotePath string) (Snapshot, error) {
	m, err := manifest.Scan(ctx, root, remotePath)
	if err != nil {
		return nil, err
	}
	return SnapshotFromManifest(m), nil
}

func SnapshotFromManifest(m manifest.Manifest) Snapshot {
	snapshot := make(Snapshot, len(m.Items))
	for _, item := range m.Items {
		snapshot[item.RelativePath] = item
	}
	return snapshot
}

func Diff(previous, current Snapshot) []Change {
	changes := make([]Change, 0)
	for relativePath, after := range current {
		before, ok := previous[relativePath]
		switch {
		case !ok:
			changes = append(changes, change(ChangeCreated, relativePath, nil, &after))
		case contentChanged(before, after):
			changes = append(changes, change(ChangeUpdated, relativePath, &before, &after))
		}
	}
	for relativePath, before := range previous {
		if _, ok := current[relativePath]; !ok {
			changes = append(changes, change(ChangeDeleted, relativePath, &before, nil))
		}
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].RelativePath == changes[j].RelativePath {
			return changes[i].Type < changes[j].Type
		}
		return changes[i].RelativePath < changes[j].RelativePath
	})
	return changes
}

func contentChanged(before, after manifest.Entry) bool {
	return before.Size != after.Size || before.SHA256 != after.SHA256
}

func change(changeType, relativePath string, before, after *manifest.Entry) Change {
	result := Change{Type: changeType, RelativePath: relativePath}
	if after != nil {
		afterCopy := *after
		result.After = &afterCopy
		result.Path = after.Path
	} else if before != nil {
		beforeCopy := *before
		result.Before = &beforeCopy
		result.Path = before.Path
	}
	if before != nil && result.Before == nil {
		beforeCopy := *before
		result.Before = &beforeCopy
	}
	return result
}
