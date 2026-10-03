package user

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
)

const hashPrefix = "$s$"

const saltBytes = 4

var ErrBadPassword = errors.New("incorrect password")

func hashWith(password, salt string) string {
	sum := sha256.Sum256([]byte(salt + password))
	return hashPrefix + salt + "$" + hex.EncodeToString(sum[:])
}

func Hash(password string) (string, error) {
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	return hashWith(password, hex.EncodeToString(salt)), nil
}

func VerifyHash(hash, password string) bool {
	rest, ok := strings.CutPrefix(hash, hashPrefix)
	if !ok {
		return false
	}
	salt, _, ok := strings.Cut(rest, "$")
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(hashWith(password, salt)), []byte(hash)) == 1
}
