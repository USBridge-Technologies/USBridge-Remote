package config

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	pinArgonTime    uint32 = 3
	pinArgonMemory  uint32 = 64 * 1024 // 64 MiB
	pinArgonThreads uint8  = 1
	pinArgonKeyLen  uint32 = 32
	pinSaltLen             = 16
)

var ErrWeakPIN = errors.New("admin PIN must be at least 4 characters")

// HashAdminPIN hashes an administrator PIN/password using Argon2id and encodes
// it in standard PHC string format:
// argon2id$v=19$m=65536,t=3,p=1$<salt_b64>$<hash_b64>
func HashAdminPIN(pin string) (string, error) {
	if len(pin) < 4 {
		return "", ErrWeakPIN
	}
	salt := make([]byte, pinSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pin), salt, pinArgonTime, pinArgonMemory, pinArgonThreads, pinArgonKeyLen)
	return fmt.Sprintf("argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		pinArgonMemory, pinArgonTime, pinArgonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyAdminPIN verifies pin against an Argon2id PHC hash produced by HashAdminPIN.
func VerifyAdminPIN(encoded, pin string) bool {
	if encoded == "" || pin == "" {
		return false
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" {
		return false
	}
	var memory, iterations uint32
	var threads uint8
	for _, kv := range strings.Split(parts[2], ",") {
		pair := strings.SplitN(kv, "=", 2)
		if len(pair) != 2 {
			return false
		}
		v, err := strconv.ParseUint(pair[1], 10, 32)
		if err != nil {
			return false
		}
		switch pair[0] {
		case "m":
			memory = uint32(v)
		case "t":
			iterations = uint32(v)
		case "p":
			threads = uint8(v)
		}
	}
	if memory == 0 || iterations == 0 || threads == 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(salt) == 0 {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(pin), salt, iterations, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// AdminPINConfigured reports whether an administrator PIN has been set.
func (c Config) AdminPINConfigured() bool {
	return strings.TrimSpace(c.AdminPINHash) != ""
}
