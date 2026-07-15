package api

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
)

// deriveKey returns a 32-byte AES-256 key derived from the raw secret via SHA-256.
func deriveKey(secret []byte) []byte {
	h := sha256.Sum256(secret)
	return h[:]
}

func EncryptPayloadV2(payload interface{}, key []byte) (encryptedBase64, ivBase64 string, err error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", "", err
	}

	block, err := aes.NewCipher(deriveKey(key))
	if err != nil {
		return "", "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", err
	}

	iv := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", "", err
	}

	ciphertext := gcm.Seal(nil, iv, data, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), base64.StdEncoding.EncodeToString(iv), nil
}

func CalculateHMACV2(method, path, timestamp, body string, key []byte) string {
	message := fmt.Sprintf("%s%s%s%s", method, path, timestamp, body)
	mac := hmac.New(sha256.New, deriveKey(key))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
