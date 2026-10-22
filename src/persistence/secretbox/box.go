// Package secretbox encrypts upstream credentials independently of the database.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
)

var errKey = errors.New("cannot load or decrypt API state keyring")

type Box struct {
	active string
	keys   map[string]cipher.AEAD
}
type Keyring struct {
	Active string            `json:"active"`
	Keys   map[string]string `json:"keys"`
}

func Generate() (Keyring, error) {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return Keyring{}, errKey
	}
	return Keyring{Active: "key-1", Keys: map[string]string{"key-1": base64.StdEncoding.EncodeToString(key[:])}}, nil
}
func Ephemeral() (*Box, error) {
	k, err := Generate()
	if err != nil {
		return nil, err
	}
	return New(k)
}
func Load(path string) (*Box, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errKey
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(raw) > 65536 {
		return nil, errKey
	}
	var k Keyring
	if json.Unmarshal(raw, &k) != nil {
		return nil, errKey
	}
	return New(k)
}
func New(k Keyring) (*Box, error) {
	if k.Active == "" || len(k.Keys) == 0 || len(k.Keys) > 16 {
		return nil, errKey
	}
	b := &Box{active: k.Active, keys: map[string]cipher.AEAD{}}
	for id, encoded := range k.Keys {
		if id == "" || len(id) > 64 {
			return nil, errKey
		}
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(key) != 32 {
			return nil, errKey
		}
		block, err := aes.NewCipher(key)
		clear(key)
		if err != nil {
			return nil, errKey
		}
		aead, err := cipher.NewGCMWithRandomNonce(block)
		if err != nil {
			return nil, errKey
		}
		b.keys[id] = aead
	}
	if b.keys[k.Active] == nil {
		return nil, errKey
	}
	return b, nil
}
func (b *Box) ActiveID() string { return b.active }
func (b *Box) Seal(plain, aad []byte) (authstate.Ciphertext, error) {
	return authstate.Ciphertext{KeyID: b.active, Data: b.keys[b.active].Seal(nil, nil, plain, aad)}, nil
}
func (b *Box) Open(value authstate.Ciphertext, aad []byte) ([]byte, error) {
	aead := b.keys[value.KeyID]
	if aead == nil {
		return nil, errKey
	}
	plain, err := aead.Open(nil, nil, value.Data, aad)
	if err != nil {
		return nil, errKey
	}
	return plain, nil
}
