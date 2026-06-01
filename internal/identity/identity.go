// Package identity manages a node's cryptographic device identity: an ECDSA
// P-256 key pair and a self-signed certificate. The node ID is derived
// deterministically from the public key, and the identity signs enrollment
// challenges so a controller can prove the node controls its key.
package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

const (
	keyFileName  = "key.pem"
	certFileName = "cert.pem"
)

// Identity is a node's persistent device identity.
type Identity struct {
	priv    *ecdsa.PrivateKey
	certDER []byte
}

// Generate creates a fresh identity with a new key pair and self-signed
// certificate.
func Generate() (*Identity, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: NodeIDFromPublicKeyDER(pubDER)},
		NotBefore:    time.Unix(0, 0),
		NotAfter:     time.Unix(0, 0).AddDate(100, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		return nil, fmt.Errorf("create certificate: %w", err)
	}

	return &Identity{priv: priv, certDER: certDER}, nil
}

// NodeID returns the node identifier derived from the public key.
func (i *Identity) NodeID() string {
	return NodeIDFromPublicKeyDER(i.PublicKeyDER())
}

// PublicKeyDER returns the PKIX/SPKI DER encoding of the public key.
func (i *Identity) PublicKeyDER() []byte {
	der, err := x509.MarshalPKIXPublicKey(&i.priv.PublicKey)
	if err != nil {
		// Unreachable for a valid ECDSA key.
		panic(err)
	}
	return der
}

// CertificatePEM returns the self-signed certificate in PEM form.
func (i *Identity) CertificatePEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: i.certDER})
}

// Sign produces an ASN.1 ECDSA signature over the SHA-256 digest of data.
func (i *Identity) Sign(data []byte) ([]byte, error) {
	digest := sha256.Sum256(data)
	return ecdsa.SignASN1(rand.Reader, i.priv, digest[:])
}

// Save writes the key and certificate to dir, creating it if necessary. The
// private key is written with 0600 permissions.
func (i *Identity) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create identity dir: %w", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(i.priv)
	if err != nil {
		return fmt.Errorf("marshal private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(filepath.Join(dir, keyFileName), keyPEM, 0o600); err != nil {
		return fmt.Errorf("write key: %w", err)
	}

	if err := os.WriteFile(filepath.Join(dir, certFileName), i.CertificatePEM(), 0o644); err != nil {
		return fmt.Errorf("write certificate: %w", err)
	}
	return nil
}

// Load reads an identity previously written by Save.
func Load(dir string) (*Identity, error) {
	keyPEM, err := os.ReadFile(filepath.Join(dir, keyFileName))
	if err != nil {
		return nil, err
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, errors.New("identity: invalid key PEM")
	}
	priv, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	certPEM, err := os.ReadFile(filepath.Join(dir, certFileName))
	if err != nil {
		return nil, err
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, errors.New("identity: invalid certificate PEM")
	}

	return &Identity{priv: priv, certDER: certBlock.Bytes}, nil
}

// LoadOrCreate loads the identity from dir, or generates and persists a new one
// if none exists.
func LoadOrCreate(dir string) (*Identity, error) {
	id, err := Load(dir)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	id, err = Generate()
	if err != nil {
		return nil, err
	}
	if err := id.Save(dir); err != nil {
		return nil, err
	}
	return id, nil
}

// NodeIDFromPublicKeyDER returns the node identifier for a PKIX/SPKI DER public
// key: the lowercase hex SHA-256 of the DER bytes.
func NodeIDFromPublicKeyDER(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

// VerifySignature reports whether sig is a valid ASN.1 ECDSA signature over the
// SHA-256 digest of challenge for the public key encoded in pubDER.
func VerifySignature(pubDER, challenge, sig []byte) (bool, error) {
	pub, err := x509.ParsePKIXPublicKey(pubDER)
	if err != nil {
		return false, fmt.Errorf("parse public key: %w", err)
	}
	ecPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return false, errors.New("identity: public key is not ECDSA")
	}
	digest := sha256.Sum256(challenge)
	return ecdsa.VerifyASN1(ecPub, digest[:], sig), nil
}
