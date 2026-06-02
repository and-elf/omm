package identity

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// startMeshServer brings up an HTTPS test server using the mesh server TLS
// config (server leaf issued by ca), echoing whether the request carried a
// verified client certificate.
func startMeshServer(t *testing.T, ca *CA, server *Identity) *httptest.Server {
	t.Helper()
	leaf, err := ca.IssueCert(server.PublicKeyDER())
	if err != nil {
		t.Fatalf("issue server leaf: %v", err)
	}
	cfg, err := ServerTLSConfig(leaf, server.PrivateKeyPEM(), ca.CertificatePEM())
	if err != nil {
		t.Fatalf("server tls config: %v", err)
	}
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.TLS.VerifiedChains) > 0 {
			_, _ = io.WriteString(w, "mutual")
		} else {
			_, _ = io.WriteString(w, "anon")
		}
	}))
	s.TLS = cfg
	s.StartTLS()
	t.Cleanup(s.Close)
	return s
}

func getWith(server *httptest.Server, cfg *tls.Config) (int, string, error) {
	c := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}
	resp, err := c.Get(server.URL + "/")
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), nil
}

func TestMeshMutualTLS(t *testing.T) {
	ca, _ := GenerateCA("home-1")
	server, _ := Generate()
	node, _ := Generate()
	srv := startMeshServer(t, ca, server)

	// Node with the pinned CA + its issued leaf: mutual auth succeeds and the
	// server sees a verified peer.
	nodeLeaf, _ := ca.IssueCert(node.PublicKeyDER())
	cfg, err := ClientTLSConfig(nodeLeaf, node.PrivateKeyPEM(), ca.CertificatePEM())
	if err != nil {
		t.Fatalf("client tls config: %v", err)
	}
	code, body, err := getWith(srv, cfg)
	if err != nil || code != http.StatusOK || body != "mutual" {
		t.Fatalf("authenticated client: got %d %q err=%v, want 200 mutual", code, body, err)
	}
}

func TestMeshClientRejectsWrongCA(t *testing.T) {
	ca, _ := GenerateCA("home-1")
	other, _ := GenerateCA("home-2")
	server, _ := Generate()
	node, _ := Generate()
	srv := startMeshServer(t, ca, server)

	// Node pinning the WRONG CA must refuse the server's certificate.
	nodeLeaf, _ := other.IssueCert(node.PublicKeyDER())
	cfg, _ := ClientTLSConfig(nodeLeaf, node.PrivateKeyPEM(), other.CertificatePEM())
	if _, _, err := getWith(srv, cfg); err == nil {
		t.Fatal("client pinning the wrong CA should fail the handshake")
	}
}

func TestMeshBootstrapIsAnonymous(t *testing.T) {
	ca, _ := GenerateCA("home-1")
	server, _ := Generate()
	srv := startMeshServer(t, ca, server)

	// Bootstrap transport: no pinned CA, no client cert (first enrollment).
	// Connects (TOFU — encrypted but unverified) without a verified identity.
	code, body, err := getWith(srv, InsecureClientTLSConfig())
	if err != nil || code != http.StatusOK || body != "anon" {
		t.Fatalf("bootstrap client: got %d %q err=%v, want 200 anon", code, body, err)
	}
}
