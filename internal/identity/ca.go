package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// CA is a per-Home certificate authority. A controller generates one for the
// Home it owns and signs a leaf certificate for each node it adopts; the leaf
// is what nodes present for mutual TLS on the mesh control plane. The CA cert
// is public (distributed as the Home's trust anchor); the private key stays on
// the controller.
type CA struct {
	priv    *ecdsa.PrivateKey
	certDER []byte
}

// GenerateCA creates a new Home CA with the given common name.
func GenerateCA(commonName string) (*CA, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ca key: %w", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Unix(0, 0),
		NotAfter:              time.Unix(0, 0).AddDate(100, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		return nil, fmt.Errorf("create ca certificate: %w", err)
	}
	return &CA{priv: priv, certDER: certDER}, nil
}

// CertificatePEM returns the CA certificate in PEM form (the Home trust anchor).
func (c *CA) CertificatePEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.certDER})
}

// PrivateKeyPEM returns the CA private key in PEM form (controller-only).
func (c *CA) PrivateKeyPEM() []byte {
	der, err := x509.MarshalECPrivateKey(c.priv)
	if err != nil {
		// Unreachable for a valid ECDSA key.
		panic(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
}

// LoadCA reconstructs a CA from a previously stored cert+key PEM pair.
func LoadCA(certPEM, keyPEM []byte) (*CA, error) {
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, errors.New("identity: invalid CA key PEM")
	}
	priv, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse ca key: %w", err)
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, errors.New("identity: invalid CA certificate PEM")
	}
	return &CA{priv: priv, certDER: certBlock.Bytes}, nil
}

const (
	caCertFileName = "ca.pem"
	caKeyFileName  = "ca-key.pem"
)

// Save writes the CA certificate and private key to dir (key with 0600).
func (c *CA) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create ca dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, caCertFileName), c.CertificatePEM(), 0o644); err != nil {
		return fmt.Errorf("write ca cert: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, caKeyFileName), c.PrivateKeyPEM(), 0o600); err != nil {
		return fmt.Errorf("write ca key: %w", err)
	}
	return nil
}

// LoadOrCreateCA loads the Home CA from dir, or generates and persists a new
// one (with the given common name) if none exists.
func LoadOrCreateCA(dir, commonName string) (*CA, error) {
	certPEM, certErr := os.ReadFile(filepath.Join(dir, caCertFileName))
	keyPEM, keyErr := os.ReadFile(filepath.Join(dir, caKeyFileName))
	if certErr == nil && keyErr == nil {
		return LoadCA(certPEM, keyPEM)
	}
	if certErr != nil && !errors.Is(certErr, os.ErrNotExist) {
		return nil, certErr
	}

	ca, err := GenerateCA(commonName)
	if err != nil {
		return nil, err
	}
	if err := ca.Save(dir); err != nil {
		return nil, err
	}
	return ca, nil
}

// CertPool returns a pool containing just this CA, for use as RootCAs (nodes
// verifying the controller) or ClientCAs (controller verifying nodes).
func (c *CA) CertPool() *x509.CertPool {
	pool := x509.NewCertPool()
	if cert, err := x509.ParseCertificate(c.certDER); err == nil {
		pool.AddCert(cert)
	}
	return pool
}

// IssueCert signs a leaf certificate for the node identified by pubDER
// (PKIX/SPKI DER). The leaf carries the node ID as its CN and is valid for both
// client and server TLS, since every meshd node is both a controller (server)
// and a peer (client).
func (c *CA) IssueCert(pubDER []byte) ([]byte, error) {
	pub, err := x509.ParsePKIXPublicKey(pubDER)
	if err != nil {
		return nil, fmt.Errorf("parse node public key: %w", err)
	}
	caCert, err := x509.ParseCertificate(c.certDER)
	if err != nil {
		return nil, fmt.Errorf("parse ca certificate: %w", err)
	}

	nodeID := NodeIDFromPublicKeyDER(pubDER)
	// Serial derived from the node ID keeps issuance deterministic and unique
	// per node without tracking a counter.
	serial := new(big.Int).SetBytes(sha256Sum(pubDER))
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: nodeID},
		NotBefore:    time.Unix(0, 0),
		NotAfter:     time.Unix(0, 0).AddDate(100, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, template, caCert, pub, c.priv)
	if err != nil {
		return nil, fmt.Errorf("sign leaf certificate: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), nil
}

func sha256Sum(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}
