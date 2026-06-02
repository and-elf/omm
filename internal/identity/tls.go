package identity

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
)

// Mesh TLS uses certificates whose identity is the node ID (the CN), not a DNS
// name, all signed by a per-Home CA. Verification therefore checks "issued by
// the pinned Home CA" rather than a hostname. First contact (before the node
// has the CA) is unverified — TOFU — and trust is established by pinning the CA
// the controller returns during enrollment.

func certPoolFromPEM(caPEM []byte) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("identity: no CA certificate in PEM")
	}
	return pool, nil
}

// verifyAgainstCA verifies the leaf in rawCerts[0] chains to caPool for the
// given key usage, ignoring DNS names (this PKI has none).
func verifyAgainstCA(rawCerts [][]byte, caPool *x509.CertPool, usage x509.ExtKeyUsage) error {
	if len(rawCerts) == 0 {
		return errors.New("identity: no peer certificate")
	}
	cert, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return fmt.Errorf("parse peer certificate: %w", err)
	}
	_, err = cert.Verify(x509.VerifyOptions{Roots: caPool, KeyUsages: []x509.ExtKeyUsage{usage}})
	return err
}

// ServerTLSConfig builds the mesh control-plane server config. It presents the
// Home-issued leaf (certPEM/keyPEM) and verifies any client certificate against
// the Home CA — but clients without one still connect, since first-enrollment
// nodes have no cert yet. Per-route enforcement (require a verified peer for
// post-enrollment endpoints) is the caller's responsibility.
func ServerTLSConfig(certPEM, keyPEM, caPEM []byte) (*tls.Config, error) {
	leaf, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("server keypair: %w", err)
	}
	pool, err := certPoolFromPEM(caPEM)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{leaf},
		ClientCAs:    pool,
		ClientAuth:   tls.VerifyClientCertIfGiven,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// ClientTLSConfig builds an authenticated mesh client config: it verifies the
// server against the pinned Home CA (by issuer, not hostname) and presents the
// node's Home-issued leaf for mutual auth.
func ClientTLSConfig(certPEM, keyPEM, caPEM []byte) (*tls.Config, error) {
	leaf, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("client keypair: %w", err)
	}
	pool, err := certPoolFromPEM(caPEM)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{leaf},
		// We verify the chain ourselves against the pinned CA without a DNS
		// name check, so disable the default (hostname-based) verification.
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			return verifyAgainstCA(rawCerts, pool, x509.ExtKeyUsageServerAuth)
		},
		MinVersion: tls.VersionTLS12,
	}, nil
}

// InsecureClientTLSConfig is the bootstrap transport for the very first
// enrollment, before the node has pinned the Home CA: encrypted but not
// verified (TOFU). It presents no client certificate.
func InsecureClientTLSConfig() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}
}
