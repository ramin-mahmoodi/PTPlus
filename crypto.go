package httpmux

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
)

func deriveKey(psk string) []byte {
	sum := sha256.Sum256([]byte(psk))
	return sum[:]
}

// Wire: [12 bytes nonce][ciphertext...]
func EncryptPSK(plain []byte, psk string) ([]byte, error) {
	if psk == "" {
		return plain, nil
	}
	key := deriveKey(psk)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize()) // 12
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, plain, nil)
	out := append(nonce, ct...)
	return out, nil
}

func DecryptPSK(data []byte, psk string) ([]byte, error) {
	if psk == "" {
		return data, nil
	}
	key := deriveKey(psk)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	ns := gcm.NonceSize()
	if len(data) < ns {
		return nil, errors.New("ciphertext too short")
	}
	nonce := data[:ns]
	ct := data[ns:]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, err
	}
	return plain, nil
}
