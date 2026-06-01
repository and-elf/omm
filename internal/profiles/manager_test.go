package profiles

import (
	"context"
	"errors"
	"testing"

	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/uci"
)

// fakeUCI records the ordered sequence of UCI operations so tests can assert
// that a profile is committed and then applied (reloaded).
type fakeUCI struct {
	ops       []string
	reloadErr error
}

var _ uci.Client = (*fakeUCI)(nil)

func (f *fakeUCI) Get(ctx context.Context, pkg, section, option string) (string, error) {
	return "", nil
}

func (f *fakeUCI) Set(ctx context.Context, pkg, section, option, value string) error {
	f.ops = append(f.ops, "set:"+pkg)
	return nil
}

func (f *fakeUCI) Commit(ctx context.Context, pkg string) error {
	f.ops = append(f.ops, "commit:"+pkg)
	return nil
}

func (f *fakeUCI) Reload(ctx context.Context) error {
	f.ops = append(f.ops, "reload")
	return f.reloadErr
}

func (f *fakeUCI) Close() error { return nil }

func TestApplyProfileReloadsAfterCommit(t *testing.T) {
	fake := &fakeUCI{}
	m := NewManager(nil, fake)

	profile := models.Profile{HomeID: "h1", NodeName: "garage", MeshSSID: "omm", MeshKey: "secret123"}
	if err := m.ApplyProfile(context.Background(), profile); err != nil {
		t.Fatalf("apply profile: %v", err)
	}

	// Committing UCI only writes the staged config; the change is not live
	// until netifd is reloaded. The reload must therefore run, and run after
	// every commit.
	last := fake.ops[len(fake.ops)-1]
	if last != "reload" {
		t.Fatalf("expected reload to run last, got ops %v", fake.ops)
	}
	for i, op := range fake.ops {
		if op == "reload" {
			for _, later := range fake.ops[i+1:] {
				if later == "commit:wireless" || later == "commit:system" {
					t.Fatalf("commit ran after reload, ops %v", fake.ops)
				}
			}
		}
	}
}

func TestApplyProfileReloadFailurePropagates(t *testing.T) {
	fake := &fakeUCI{reloadErr: errors.New("netifd down")}
	m := NewManager(nil, fake)

	err := m.ApplyProfile(context.Background(), models.Profile{HomeID: "h1", MeshSSID: "omm"})
	if err == nil {
		t.Fatal("expected reload failure to propagate, got nil")
	}
}
