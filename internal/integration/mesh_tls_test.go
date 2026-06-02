package integration

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/and-elf/omm/internal/api"
	"github.com/and-elf/omm/internal/enrollment"
	"github.com/and-elf/omm/internal/identity"
	"github.com/and-elf/omm/internal/models"
	"github.com/and-elf/omm/internal/storage"
)

// The mesh control plane runs mutual TLS: post-enrollment routes require a
// verified client cert, while the bootstrap /enroll/* routes stay reachable for
// a node that has not been issued one yet.
func TestMeshTLSEnforcement(t *testing.T) {
	db, err := storage.OpenDB(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	store := storage.NewStore(db)
	if err := store.CreateHome(context.Background(), models.Home{ID: "h1", Name: "Home"}); err != nil {
		t.Fatalf("create home: %v", err)
	}

	ca, _ := identity.GenerateCA("home-h1")
	controller, _ := identity.Generate()
	svc := enrollment.NewService(store, enrollment.Options{HomeID: "h1"})

	// Mesh server with mutual-TLS enforcement.
	serverLeaf, _ := ca.IssueCert(controller.PublicKeyDER())
	serverCfg, err := identity.ServerTLSConfig(serverLeaf, controller.PrivateKeyPEM(), ca.CertificatePEM())
	if err != nil {
		t.Fatalf("server tls config: %v", err)
	}
	srv := httptest.NewUnstartedServer(api.NewMeshRouter(store, noopProfileManager{}, api.WithEnrollment(svc), api.WithMeshClientAuth()))
	srv.TLS = serverCfg
	srv.StartTLS()
	t.Cleanup(srv.Close)

	get := func(cfg *tls.Config, path string) (int, error) {
		c := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}
		resp, err := c.Get(srv.URL + path)
		if err != nil {
			return 0, err
		}
		resp.Body.Close()
		return resp.StatusCode, nil
	}
	post := func(cfg *tls.Config, path string) (int, error) {
		c := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}
		resp, err := c.Post(srv.URL+path, "application/json", nil)
		if err != nil {
			return 0, err
		}
		resp.Body.Close()
		return resp.StatusCode, nil
	}

	// A node with its issued leaf reaches the protected route.
	node, _ := identity.Generate()
	nodeLeaf, _ := ca.IssueCert(node.PublicKeyDER())
	authed, _ := identity.ClientTLSConfig(nodeLeaf, node.PrivateKeyPEM(), ca.CertificatePEM())
	if code, err := get(authed, "/homes/h1"); err != nil || code != http.StatusOK {
		t.Fatalf("authenticated GET /homes/h1 = %d err=%v, want 200", code, err)
	}

	// An anonymous (bootstrap) client is refused on the protected route...
	anon := identity.InsecureClientTLSConfig()
	if code, err := get(anon, "/homes/h1"); err != nil || code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET /homes/h1 = %d err=%v, want 401", code, err)
	}
	// ...but can still reach the bootstrap enrollment endpoint (route exists;
	// empty body is a 400, not a 401/404).
	if code, err := post(anon, "/enroll/request"); err != nil || code == http.StatusUnauthorized || code == http.StatusNotFound {
		t.Fatalf("anonymous POST /enroll/request = %d err=%v, want it reachable (not 401/404)", code, err)
	}
}
