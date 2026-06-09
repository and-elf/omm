package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/and-elf/omm/internal/enrollment"
	"github.com/and-elf/omm/internal/identity"
	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/storage"
)

func setupEnrollRouter(t *testing.T, autoAdopt bool) (http.Handler, storage.Store) {
	t.Helper()
	db, err := storage.OpenDB(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	store := storage.NewStore(db)
	policy := enrollment.AdoptOff
	if autoAdopt {
		policy = enrollment.AdoptAlways
	}
	svc := enrollment.NewService(store, enrollment.Options{HomeID: "home-1", AdoptPolicy: policy})
	return NewRouter(store, noopProfileManager{}, WithEnrollment(svc)), store
}

func postJSON(t *testing.T, router http.Handler, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body := &bytes.Buffer{}
	if err := json.NewEncoder(body).Encode(payload); err != nil {
		t.Fatalf("encode: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, body)
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)
	return rw
}

// The controller's AdoptOnlink policy is gated by the handler's on-link check
// (the verifiable request source), not the node's word: an on-link peer is
// auto-adopted, an off-link one is left pending.
func TestEnrollOnlinkPolicyGatedByHandler(t *testing.T) {
	for _, onlink := range []bool{true, false} {
		db, err := storage.OpenDB(":memory:")
		if err != nil {
			t.Fatalf("open db: %v", err)
		}
		t.Cleanup(func() { db.Close() })
		store := storage.NewStore(db)
		svc := enrollment.NewService(store, enrollment.Options{HomeID: "home-1", AdoptPolicy: enrollment.AdoptOnlink})
		router := NewRouter(store, noopProfileManager{}, WithEnrollment(svc),
			WithOnLink(func(net.IP) bool { return onlink }))
		id, _ := identity.Generate()

		rw := postJSON(t, router, "/enroll/request", enrollment.RequestInput{
			NodeID: id.NodeID(), Serial: "SN1", PublicKey: id.PublicKeyDER(),
		})
		var reqRes enrollment.RequestResult
		_ = json.Unmarshal(rw.Body.Bytes(), &reqRes)

		sig, _ := id.Sign(reqRes.Challenge)
		rw = postJSON(t, router, "/enroll/verify", enrollment.VerifyInput{
			EnrollmentID: reqRes.EnrollmentID, Signature: sig,
		})
		var verRes enrollment.Result
		_ = json.Unmarshal(rw.Body.Bytes(), &verRes)

		want := models.EnrollmentPendingApproval
		if onlink {
			want = models.EnrollmentApproved
		}
		if verRes.Status != want {
			t.Fatalf("onlink=%v: status=%q, want %q", onlink, verRes.Status, want)
		}
	}
}

func TestEnrollEndpointsAutoAdopt(t *testing.T) {
	router, store := setupEnrollRouter(t, true)
	id, _ := identity.Generate()

	// request
	rw := postJSON(t, router, "/enroll/request", enrollment.RequestInput{
		NodeID: id.NodeID(), Serial: "SN1", PublicKey: id.PublicKeyDER(),
	})
	if rw.Code != http.StatusOK {
		t.Fatalf("request: expected 200, got %d (%s)", rw.Code, rw.Body)
	}
	var reqRes enrollment.RequestResult
	if err := json.Unmarshal(rw.Body.Bytes(), &reqRes); err != nil {
		t.Fatalf("decode request result: %v", err)
	}
	if len(reqRes.Challenge) == 0 || reqRes.EnrollmentID == "" {
		t.Fatalf("missing challenge/id: %+v", reqRes)
	}

	// verify
	sig, _ := id.Sign(reqRes.Challenge)
	rw = postJSON(t, router, "/enroll/verify", enrollment.VerifyInput{
		EnrollmentID: reqRes.EnrollmentID, Signature: sig,
	})
	if rw.Code != http.StatusOK {
		t.Fatalf("verify: expected 200, got %d (%s)", rw.Code, rw.Body)
	}
	var verRes enrollment.Result
	_ = json.Unmarshal(rw.Body.Bytes(), &verRes)
	if verRes.Status != models.EnrollmentApproved {
		t.Fatalf("expected approved, got %q", verRes.Status)
	}

	// node now in inventory
	if _, err := store.GetNode(context.Background(), id.NodeID()); err != nil {
		t.Fatalf("expected node in inventory: %v", err)
	}

	// ack -> active
	rw = postJSON(t, router, "/enroll/"+reqRes.EnrollmentID+"/ack", nil)
	if rw.Code != http.StatusOK {
		t.Fatalf("ack: expected 200, got %d", rw.Code)
	}
	_ = json.Unmarshal(rw.Body.Bytes(), &verRes)
	if verRes.Status != models.EnrollmentActive {
		t.Fatalf("expected active after ack, got %q", verRes.Status)
	}
}

func TestEnrollVerifyBadSignatureUnauthorized(t *testing.T) {
	router, _ := setupEnrollRouter(t, true)
	id, _ := identity.Generate()

	rw := postJSON(t, router, "/enroll/request", enrollment.RequestInput{
		NodeID: id.NodeID(), Serial: "SN1", PublicKey: id.PublicKeyDER(),
	})
	var reqRes enrollment.RequestResult
	_ = json.Unmarshal(rw.Body.Bytes(), &reqRes)

	rw = postJSON(t, router, "/enroll/verify", enrollment.VerifyInput{
		EnrollmentID: reqRes.EnrollmentID, Signature: []byte("nope"),
	})
	if rw.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for bad signature, got %d", rw.Code)
	}
}

func TestAdoptEndpointManualFlow(t *testing.T) {
	router, store := setupEnrollRouter(t, false)
	id, _ := identity.Generate()

	rw := postJSON(t, router, "/enroll/request", enrollment.RequestInput{
		NodeID: id.NodeID(), Serial: "SN1", PublicKey: id.PublicKeyDER(),
	})
	var reqRes enrollment.RequestResult
	_ = json.Unmarshal(rw.Body.Bytes(), &reqRes)
	sig, _ := id.Sign(reqRes.Challenge)
	rw = postJSON(t, router, "/enroll/verify", enrollment.VerifyInput{EnrollmentID: reqRes.EnrollmentID, Signature: sig})

	var verRes enrollment.Result
	_ = json.Unmarshal(rw.Body.Bytes(), &verRes)
	if verRes.Status != models.EnrollmentPendingApproval {
		t.Fatalf("expected pending_approval, got %q", verRes.Status)
	}

	rw = postJSON(t, router, "/nodes/"+id.NodeID()+"/adopt", nil)
	if rw.Code != http.StatusOK {
		t.Fatalf("adopt: expected 200, got %d (%s)", rw.Code, rw.Body)
	}
	if _, err := store.GetNode(context.Background(), id.NodeID()); err != nil {
		t.Fatalf("expected node after adopt: %v", err)
	}
}

func TestListPendingAndRejectEndpoints(t *testing.T) {
	router, store := setupEnrollRouter(t, false) // manual approval -> pending
	id, _ := identity.Generate()

	rw := postJSON(t, router, "/enroll/request", enrollment.RequestInput{
		NodeID: id.NodeID(), Serial: "SN1", PublicKey: id.PublicKeyDER(),
	})
	var reqRes enrollment.RequestResult
	_ = json.Unmarshal(rw.Body.Bytes(), &reqRes)
	sig, _ := id.Sign(reqRes.Challenge)
	postJSON(t, router, "/enroll/verify", enrollment.VerifyInput{EnrollmentID: reqRes.EnrollmentID, Signature: sig})

	// GET /enroll lists the pending device.
	req := httptest.NewRequest(http.MethodGet, "/enroll", nil)
	rw = httptest.NewRecorder()
	router.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("list enroll: expected 200, got %d", rw.Code)
	}
	var list struct {
		Enrollments []struct {
			NodeID string `json:"node_id"`
			Status string `json:"status"`
		} `json:"enrollments"`
	}
	_ = json.Unmarshal(rw.Body.Bytes(), &list)
	if len(list.Enrollments) != 1 || list.Enrollments[0].NodeID != id.NodeID() {
		t.Fatalf("expected one pending enrollment, got %+v", list.Enrollments)
	}

	// Reject it.
	rw = postJSON(t, router, "/nodes/"+id.NodeID()+"/reject", nil)
	if rw.Code != http.StatusOK {
		t.Fatalf("reject: expected 200, got %d (%s)", rw.Code, rw.Body)
	}
	if _, err := store.GetNode(context.Background(), id.NodeID()); err == nil {
		t.Fatal("rejected device must not be in the node inventory")
	}
}

func TestEnrollRoutesAbsentWithoutService(t *testing.T) {
	db, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { db.Close() })
	router := NewRouter(storage.NewStore(db), noopProfileManager{})

	// Without an enrollment service the route is not registered, so the request
	// falls through to the SPA catch-all rather than the JSON enroll handler.
	rw := postJSON(t, router, "/enroll/request", enrollment.RequestInput{})
	if ct := rw.Header().Get("Content-Type"); strings.HasPrefix(ct, "application/json") {
		t.Fatalf("did not expect the JSON enroll handler to run without a service (content-type %q)", ct)
	}
}
