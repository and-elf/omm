package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/and-elf/omm/internal/models"
)

func TestUpsertHomeInsertsThenUpdates(t *testing.T) {
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	store := NewStore(db)
	ctx := context.Background()

	if err := store.UpsertHome(ctx, models.Home{ID: "h1", Name: "Cottage", Controller: "aa:bb", LastSeen: 1}); err != nil {
		t.Fatalf("upsert insert: %v", err)
	}
	if err := store.UpsertHome(ctx, models.Home{ID: "h1", Name: "Summer Cottage", Controller: "cc:dd", LastSeen: 2}); err != nil {
		t.Fatalf("upsert update: %v", err)
	}

	home, err := store.GetHome(ctx, "h1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if home.Name != "Summer Cottage" || home.Controller != "cc:dd" || home.LastSeen != 2 {
		t.Fatalf("upsert did not update: %+v", home)
	}
	homes, _ := store.ListHomes(ctx)
	if len(homes) != 1 {
		t.Fatalf("expected single home after upsert, got %d", len(homes))
	}
}

// TestConcurrentNodeCreation exercises many simultaneous writers against a
// file-backed database, mirroring many nodes enrolling at once. Without proper
// SQLite concurrency configuration this fails with "database is locked".
func TestConcurrentNodeCreation(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "concurrent.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)

	const n = 64
	var wg sync.WaitGroup
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			node := models.Node{ID: fmt.Sprintf("node-%d", i), Serial: fmt.Sprintf("SN-%d", i)}
			if err := store.CreateNode(context.Background(), node); err != nil {
				errs <- fmt.Errorf("node-%d: %w", i, err)
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent CreateNode failed: %v", err)
	}

	nodes, err := store.ListNodes(context.Background())
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if len(nodes) != n {
		t.Fatalf("expected %d nodes, got %d", n, len(nodes))
	}
}
