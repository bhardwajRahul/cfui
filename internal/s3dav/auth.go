package s3dav

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func HashPassword(password string) (string, error) {
	password = strings.TrimSpace(password)
	if password == "" {
		return "", fmt.Errorf("webdav password is required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func VerifyPassword(hash, password string) bool {
	if strings.TrimSpace(hash) == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func basicAuthOK(r *http.Request, username, passwordHash string) bool {
	gotUser, gotPassword, ok := r.BasicAuth()
	if !ok {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(gotUser), []byte(username)) != 1 {
		return false
	}
	return VerifyPassword(passwordHash, gotPassword)
}
