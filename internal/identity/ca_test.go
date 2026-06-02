package identity

import (
	"crypto/x509"
	"encoding/pem"
	"testing"
)

func parseLeaf(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("leaf: invalid PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	return cert
}

// A Home CA issues a leaf certificate for a node's public key; the leaf must
// chain to the CA, carry the node ID as its CN, and be usable for both client
// and server TLS (the node is both a controller and a peer).
func TestCAIssuesVerifiableLeaf(t *testing.T) {
	ca, err := GenerateCA("home-h1")
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	node, err := Generate()
	if err != nil {
		t.Fatalf("Generate node: %v", err)
	}

	leafPEM, err := ca.IssueCert(node.PublicKeyDER())
	if err != nil {
		t.Fatalf("IssueCert: %v", err)
	}
	leaf := parseLeaf(t, leafPEM)

	if leaf.Subject.CommonName != node.NodeID() {
		t.Fatalf("leaf CN = %q, want node ID %q", leaf.Subject.CommonName, node.NodeID())
	}

	for _, usage := range []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth} {
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: ca.CertPool(), KeyUsages: []x509.ExtKeyUsage{usage}}); err != nil {
			t.Fatalf("leaf does not verify for usage %v: %v", usage, err)
		}
	}
}

// A CA round-tripped through PEM (the controller persists cert+key) issues
// certs that still verify.
func TestCALoadRoundTrip(t *testing.T) {
	ca, err := GenerateCA("home-h1")
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	loaded, err := LoadCA(ca.CertificatePEM(), ca.PrivateKeyPEM())
	if err != nil {
		t.Fatalf("LoadCA: %v", err)
	}
	node, _ := Generate()
	leafPEM, err := loaded.IssueCert(node.PublicKeyDER())
	if err != nil {
		t.Fatalf("IssueCert after load: %v", err)
	}
	if _, err := parseLeaf(t, leafPEM).Verify(x509.VerifyOptions{Roots: loaded.CertPool(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatalf("leaf from reloaded CA does not verify: %v", err)
	}
}

// A leaf must NOT verify against an unrelated CA — the trust anchor matters.
func TestCARejectsForeignLeaf(t *testing.T) {
	ca1, _ := GenerateCA("home-1")
	ca2, _ := GenerateCA("home-2")
	node, _ := Generate()

	leafPEM, _ := ca1.IssueCert(node.PublicKeyDER())
	if _, err := parseLeaf(t, leafPEM).Verify(x509.VerifyOptions{Roots: ca2.CertPool(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err == nil {
		t.Fatal("leaf from CA1 must not verify against CA2")
	}
}
