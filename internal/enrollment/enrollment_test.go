package enrollment_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/and-elf/omm/internal/enrollment"
	"github.com/and-elf/omm/internal/identity"
	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/storage"
)

func newStore(t *testing.T) storage.Store {
	t.Helper()
	db, err := storage.OpenDB(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return storage.NewStore(db)
}

// enroll runs the full request+verify exchange for id and returns the result.
func enroll(t *testing.T, svc *enrollment.Service, id *identity.Identity, serial string) enrollment.Result {
	t.Helper()
	req, err := svc.Request(context.Background(), enrollment.RequestInput{
		NodeID:    id.NodeID(),
		Serial:    serial,
		PublicKey: id.PublicKeyDER(),
	})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	sig, err := id.Sign(req.Challenge)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	res, err := svc.Verify(context.Background(), enrollment.VerifyInput{
		EnrollmentID: req.EnrollmentID,
		Signature:    sig,
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	return res
}

func TestRequestRejectsIdentityMismatch(t *testing.T) {
	svc := enrollment.NewService(newStore(t), enrollment.Options{HomeID: "home-1"})
	id, _ := identity.Generate()

	_, err := svc.Request(context.Background(), enrollment.RequestInput{
		NodeID:    "not-the-real-node-id",
		Serial:    "SN1",
		PublicKey: id.PublicKeyDER(),
	})
	if err == nil {
		t.Fatal("expected identity mismatch to be rejected")
	}
}

func TestVerifyRejectsBadSignature(t *testing.T) {
	svc := enrollment.NewService(newStore(t), enrollment.Options{HomeID: "home-1", AutoAdopt: true})
	id, _ := identity.Generate()

	req, err := svc.Request(context.Background(), enrollment.RequestInput{
		NodeID: id.NodeID(), Serial: "SN1", PublicKey: id.PublicKeyDER(),
	})
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	_, err = svc.Verify(context.Background(), enrollment.VerifyInput{
		EnrollmentID: req.EnrollmentID,
		Signature:    []byte("garbage"),
	})
	if err == nil {
		t.Fatal("expected invalid signature to be rejected")
	}
}

func TestAutoAdoptEnrollsNode(t *testing.T) {
	store := newStore(t)
	svc := enrollment.NewService(store, enrollment.Options{HomeID: "home-1", AutoAdopt: true})
	id, _ := identity.Generate()

	res := enroll(t, svc, id, "SN1")
	if res.Status != models.EnrollmentApproved {
		t.Fatalf("expected approved, got %q", res.Status)
	}

	// Node must now exist in the controller inventory.
	node, err := store.GetNode(context.Background(), id.NodeID())
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if node.CurrentHome != "home-1" || node.Serial != "SN1" {
		t.Fatalf("unexpected node: %+v", node)
	}

	// Ack moves the enrollment to active.
	ack, err := svc.Ack(context.Background(), enrollmentIDFor(t, store, id.NodeID()))
	if err != nil {
		t.Fatalf("ack: %v", err)
	}
	if ack.Status != models.EnrollmentActive {
		t.Fatalf("expected active after ack, got %q", ack.Status)
	}
}

func TestManualAdopt(t *testing.T) {
	store := newStore(t)
	svc := enrollment.NewService(store, enrollment.Options{HomeID: "home-1", AutoAdopt: false})
	id, _ := identity.Generate()

	res := enroll(t, svc, id, "SN1")
	if res.Status != models.EnrollmentPendingApproval {
		t.Fatalf("expected pending_approval, got %q", res.Status)
	}

	if _, err := store.GetNode(context.Background(), id.NodeID()); err == nil {
		t.Fatal("node should not exist before approval")
	}

	adopted, err := svc.Adopt(context.Background(), id.NodeID())
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if adopted.Status != models.EnrollmentApproved {
		t.Fatalf("expected approved after adopt, got %q", adopted.Status)
	}
	if _, err := store.GetNode(context.Background(), id.NodeID()); err != nil {
		t.Fatalf("node should exist after adopt: %v", err)
	}
}

func TestConcurrentEnrollments(t *testing.T) {
	store := newStore(t)
	svc := enrollment.NewService(store, enrollment.Options{HomeID: "home-1", AutoAdopt: true})

	const n = 32
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id, err := identity.Generate()
			if err != nil {
				errs <- err
				return
			}
			req, err := svc.Request(context.Background(), enrollment.RequestInput{
				NodeID: id.NodeID(), Serial: fmt.Sprintf("SN-%d", i), PublicKey: id.PublicKeyDER(),
			})
			if err != nil {
				errs <- fmt.Errorf("request %d: %w", i, err)
				return
			}
			sig, _ := id.Sign(req.Challenge)
			if _, err := svc.Verify(context.Background(), enrollment.VerifyInput{EnrollmentID: req.EnrollmentID, Signature: sig}); err != nil {
				errs <- fmt.Errorf("verify %d: %w", i, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent enrollment failed: %v", err)
	}

	nodes, err := store.ListNodes(context.Background())
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if len(nodes) != n {
		t.Fatalf("expected %d enrolled nodes, got %d", n, len(nodes))
	}
}

func enrollmentIDFor(t *testing.T, store storage.Store, nodeID string) string {
	t.Helper()
	e, err := store.GetEnrollmentByNodeID(context.Background(), nodeID)
	if err != nil {
		t.Fatalf("get enrollment: %v", err)
	}
	return e.ID
}
