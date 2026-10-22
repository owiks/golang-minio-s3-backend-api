package secretbox

import (
	"bytes"
	"testing"
)

func TestAuthenticatedEncryptionAndKeyRotation(t *testing.T) {
	ring, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	box, err := New(ring)
	if err != nil {
		t.Fatal(err)
	}
	first, err := box.Seal([]byte("secret"), []byte("connection/1"))
	if err != nil {
		t.Fatal(err)
	}
	second, _ := box.Seal([]byte("secret"), []byte("connection/1"))
	if bytes.Equal(first.Data, second.Data) {
		t.Fatal("nonce reused")
	}
	if _, err = box.Open(first, []byte("connection/2")); err == nil {
		t.Fatal("ciphertext can move between connections")
	}
	changed := first
	changed.Data = bytes.Clone(first.Data)
	changed.Data[0] ^= 1
	if _, err = box.Open(changed, []byte("connection/1")); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	extra, _ := Generate()
	ring.Keys["key-2"] = extra.Keys[extra.Active]
	ring.Active = "key-2"
	next, _ := New(ring)
	if _, err = next.Open(first, []byte("connection/1")); err != nil {
		t.Fatal("old key unavailable during rotation", err)
	}
	c, _ := next.Seal([]byte("new-secret"), []byte("connection/1"))
	if c.KeyID != "key-2" {
		t.Fatal("inactive key used")
	}
	delete(ring.Keys, "key-1")
	final, _ := New(ring)
	if _, err = final.Open(first, []byte("connection/1")); err == nil {
		t.Fatal("removed key still works")
	}
	if _, err = New(Keyring{Active: "missing", Keys: ring.Keys}); err == nil {
		t.Fatal("invalid active key accepted")
	}
}
