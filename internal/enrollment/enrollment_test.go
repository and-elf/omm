package enrollment_test

import (
	"context"
	"crypto/x509"
	"encoding/pem"
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

func TestEnrollmentIssuesCertificate(t *testing.T) {
	ca, err := identity.GenerateCA("home-1")
	if err != nil {
		t.Fatalf("generate ca: %v", err)
	}
	svc := enrollment.NewService(newStore(t), enrollment.Options{HomeID: "home-1", AdoptPolicy: enrollment.AdoptAlways, CA: ca})
	id, _ := identity.Generate()

	res := enroll(t, svc, id, "SN1")
	if res.Status != models.EnrollmentApproved {
		t.Fatalf("status = %q, want approved", res.Status)
	}
	// The Home CA cert is returned so the node can pin it (TOFU).
	if string(res.CACertificate) != string(ca.CertificatePEM()) {
		t.Fatal("CACertificate should be the Home CA cert")
	}
	// The issued leaf carries the node ID and verifies against the Home CA.
	block, _ := pem.Decode(res.Certificate)
	if block == nil {
		t.Fatal("expected an issued certificate")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	if leaf.Subject.CommonName != id.NodeID() {
		t.Fatalf("leaf CN = %q, want %q", leaf.Subject.CommonName, id.NodeID())
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: ca.CertPool(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatalf("issued leaf does not verify against the Home CA: %v", err)
	}
}

func TestEnrollmentWithoutCAIssuesNoCertificate(t *testing.T) {
	svc := enrollment.NewService(newStore(t), enrollment.Options{HomeID: "home-1", AdoptPolicy: enrollment.AdoptAlways})
	id, _ := identity.Generate()

	res := enroll(t, svc, id, "SN1")
	if len(res.Certificate) != 0 || len(res.CACertificate) != 0 {
		t.Fatalf("no CA configured: result should carry no certs, got cert=%d ca=%d", len(res.Certificate), len(res.CACertificate))
	}
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
	svc := enrollment.NewService(newStore(t), enrollment.Options{HomeID: "home-1", AdoptPolicy: enrollment.AdoptAlways})
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
	svc := enrollment.NewService(store, enrollment.Options{HomeID: "home-1", AdoptPolicy: enrollment.AdoptAlways})
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
	svc := enrollment.NewService(store, enrollment.Options{HomeID: "home-1", AdoptPolicy: enrollment.AdoptOff})
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

// The AdoptOnlink policy adopts a verified node only when the controller sees
// the enrollment arriving on-link (its own LAN), and leaves an off-link peer
// pending — the trust decision is the controller's, on a signal it observes.
func TestVerifyOnlinkPolicy(t *testing.T) {
	for _, onlink := range []bool{true, false} {
		store := newStore(t)
		svc := enrollment.NewService(store, enrollment.Options{HomeID: "home-1", AdoptPolicy: enrollment.AdoptOnlink})
		id, _ := identity.Generate()

		req, err := svc.Request(context.Background(), enrollment.RequestInput{
			NodeID: id.NodeID(), Serial: "SN1", PublicKey: id.PublicKeyDER(),
		})
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		sig, _ := id.Sign(req.Challenge)
		ctx := enrollment.WithPeerOnLink(context.Background(), onlink)
		res, err := svc.Verify(ctx, enrollment.VerifyInput{EnrollmentID: req.EnrollmentID, Signature: sig})
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		want := models.EnrollmentPendingApproval
		if onlink {
			want = models.EnrollmentApproved
		}
		if res.Status != want {
			t.Fatalf("onlink=%v: status=%q, want %q", onlink, res.Status, want)
		}
	}
}

func TestListPendingAndReject(t *testing.T) {
	store := newStore(t)
	svc := enrollment.NewService(store, enrollment.Options{HomeID: "home-1", AdoptPolicy: enrollment.AdoptOff})

	// Two devices verify (pending approval), one rejected.
	idA, _ := identity.Generate()
	idB, _ := identity.Generate()
	enroll(t, svc, idA, "SN-A")
	enroll(t, svc, idB, "SN-B")

	pending, err := svc.ListPending(context.Background())
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}

	if _, err := svc.Reject(context.Background(), idB.NodeID()); err != nil {
		t.Fatalf("reject: %v", err)
	}
	pending, _ = svc.ListPending(context.Background())
	if len(pending) != 1 || pending[0].NodeID != idA.NodeID() {
		t.Fatalf("expected only A pending, got %+v", pending)
	}

	// A rejected device must not be adoptable.
	if _, err := svc.Adopt(context.Background(), idB.NodeID()); err == nil {
		t.Fatal("expected rejected enrollment to be non-adoptable")
	}
}

func TestConcurrentEnrollments(t *testing.T) {
	store := newStore(t)
	svc := enrollment.NewService(store, enrollment.Options{HomeID: "home-1", AdoptPolicy: enrollment.AdoptAlways})

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
