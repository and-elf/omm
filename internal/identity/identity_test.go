package identity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateProducesUsableIdentity(t *testing.T) {
	id, err := Generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if id.NodeID() == "" {
		t.Fatal("expected non-empty node id")
	}
	if len(id.PublicKeyDER()) == 0 {
		t.Fatal("expected non-empty public key DER")
	}
	if len(id.CertificatePEM()) == 0 {
		t.Fatal("expected a self-signed certificate")
	}
}

func TestNodeIDDerivesFromPublicKey(t *testing.T) {
	id, err := Generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if got := NodeIDFromPublicKeyDER(id.PublicKeyDER()); got != id.NodeID() {
		t.Fatalf("node id mismatch: %q vs %q", got, id.NodeID())
	}
}

func TestSignVerifyRoundTrip(t *testing.T) {
	id, err := Generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	challenge := []byte("a-random-challenge")

	sig, err := id.Sign(challenge)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	ok, err := VerifySignature(id.PublicKeyDER(), challenge, sig)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Fatal("expected valid signature to verify")
	}

	if ok, _ := VerifySignature(id.PublicKeyDER(), []byte("tampered"), sig); ok {
		t.Fatal("expected verification to fail for a different challenge")
	}

	other, _ := Generate()
	if ok, _ := VerifySignature(other.PublicKeyDER(), challenge, sig); ok {
		t.Fatal("expected verification to fail with a different key")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	id, err := Generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if err := id.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Private key must not be world-readable.
	info, err := os.Stat(filepath.Join(dir, "key.pem"))
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected key perm 0600, got %o", perm)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.NodeID() != id.NodeID() {
		t.Fatalf("node id changed after reload: %q vs %q", loaded.NodeID(), id.NodeID())
	}

	challenge := []byte("verify-after-reload")
	sig, err := loaded.Sign(challenge)
	if err != nil {
		t.Fatalf("sign after reload: %v", err)
	}
	if ok, _ := VerifySignature(loaded.PublicKeyDER(), challenge, sig); !ok {
		t.Fatal("signature from reloaded identity did not verify")
	}
}

func TestLoadOrCreatePersistsIdentity(t *testing.T) {
	dir := t.TempDir()

	first, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("first load-or-create: %v", err)
	}
	second, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("second load-or-create: %v", err)
	}
	if first.NodeID() != second.NodeID() {
		t.Fatalf("identity not persisted: %q vs %q", first.NodeID(), second.NodeID())
	}
}
