package integration

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/and-elf/omm/internal/api"
	"github.com/and-elf/omm/internal/client"
	"github.com/and-elf/omm/internal/enrollment"
	"github.com/and-elf/omm/internal/identity"
	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/storage"
)

// A node joins a controller whose mesh plane runs mutual TLS: it bootstrap-
// enrolls (no cert yet), is issued a leaf + the Home CA, then uses them to make
// the protected RemoteHome call succeed through the mTLS gate, recording the
// joined Home with the CA pinned.
func TestJoinOverMeshTLS(t *testing.T) {
	ctx := context.Background()

	// Controller store + Home CA + auto-adopt enrollment, Home advertises CA.
	cdb, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { cdb.Close() })
	cstore := storage.NewStore(cdb)
	ca, _ := identity.GenerateCA("home-h1")
	if err := cstore.CreateHome(ctx, models.Home{ID: "h1", Name: "Home", Certificate: ca.CertificatePEM()}); err != nil {
		t.Fatalf("create home: %v", err)
	}
	svc := enrollment.NewService(cstore, enrollment.Options{HomeID: "h1", AutoAdopt: true, CA: ca})

	controller, _ := identity.Generate()
	serverLeaf, _ := ca.IssueCert(controller.PublicKeyDER())
	serverCfg, err := identity.ServerTLSConfig(serverLeaf, controller.PrivateKeyPEM(), ca.CertificatePEM())
	if err != nil {
		t.Fatalf("server tls: %v", err)
	}
	srv := httptest.NewUnstartedServer(api.NewMeshRouter(cstore, noopProfileManager{}, api.WithEnrollment(svc), api.WithMeshClientAuth()))
	srv.TLS = serverCfg
	srv.StartTLS()
	t.Cleanup(srv.Close)

	// Node joins over the https mesh URL, recording into its own store.
	ndb, _ := storage.OpenDB(":memory:")
	t.Cleanup(func() { ndb.Close() })
	nstore := storage.NewStore(ndb)
	node, _ := identity.Generate()

	result, err := client.JoinAndRecord(ctx, node, srv.URL, "node-sn", nstore, client.Options{})
	if err != nil {
		t.Fatalf("join over TLS failed: %v", err)
	}
	if result.Status != models.EnrollmentApproved && result.Status != models.EnrollmentActive {
		t.Fatalf("status = %q, want approved/active", result.Status)
	}
	if len(result.Certificate) == 0 {
		t.Fatal("expected an issued leaf certificate")
	}

	// RemoteHome (a protected route) succeeded through the mTLS gate, so the
	// Home was recorded with the CA pinned.
	home, err := nstore.GetHome(ctx, "h1")
	if err != nil {
		t.Fatalf("joined Home not recorded (RemoteHome blocked by mTLS?): %v", err)
	}
	if string(home.Certificate) != string(ca.CertificatePEM()) {
		t.Fatal("joined Home did not pin the controller's Home CA")
	}
}
