package auth

import (
	"encoding/hex"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMakeRefreshToken(t *testing.T) {
	first := MakeRefreshToken()
	second := MakeRefreshToken()

	if len(first) != 64 || len(second) != 64 {
		t.Fatalf("MakeRefreshToken() length = %d and %d, want 64", len(first), len(second))
	}
	if _, err := hex.DecodeString(first); err != nil {
		t.Fatalf("MakeRefreshToken() returned non-hex token: %v", err)
	}
	if first == second {
		t.Fatal("MakeRefreshToken() returned the same token twice")
	}
}

func TestMakeAndValidateJWT(t *testing.T) {
	userID := uuid.New()
	secret := "test-secret"

	token, err := MakeJWT(userID, secret, time.Hour)
	if err != nil {
		t.Fatalf("MakeJWT() error = %v", err)
	}

	validatedUserID, err := ValidateJWT(token, secret)
	if err != nil {
		t.Fatalf("ValidateJWT() error = %v", err)
	}

	if validatedUserID != userID {
		t.Fatalf("ValidateJWT() user ID = %v, want %v", validatedUserID, userID)
	}
}

func TestValidateJWTRejectsExpiredToken(t *testing.T) {
	token, err := MakeJWT(uuid.New(), "test-secret", -time.Hour)
	if err != nil {
		t.Fatalf("MakeJWT() error = %v", err)
	}

	if _, err := ValidateJWT(token, "test-secret"); err == nil {
		t.Fatal("ValidateJWT() accepted an expired token")
	}
}

func TestValidateJWTRejectsWrongSecret(t *testing.T) {
	token, err := MakeJWT(uuid.New(), "test-secret", time.Hour)
	if err != nil {
		t.Fatalf("MakeJWT() error = %v", err)
	}

	if _, err := ValidateJWT(token, "wrong-secret"); err == nil {
		t.Fatal("ValidateJWT() accepted a token signed with the wrong secret")
	}
}

func TestGetBearerToken(t *testing.T) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer test-token")

	token, err := GetBearerToken(headers)
	if err != nil {
		t.Fatalf("GetBearerToken() error = %v", err)
	}

	if token != "test-token" {
		t.Fatalf("GetBearerToken() token = %q, want %q", token, "test-token")
	}
}

func TestGetBearerTokenRejectsMissingHeader(t *testing.T) {
	if _, err := GetBearerToken(http.Header{}); err == nil {
		t.Fatal("GetBearerToken() accepted a request without an Authorization header")
	}
}

func TestGetAPIKey(t *testing.T) {
	headers := http.Header{}
	headers.Set("Authorization", "ApiKey test-key")

	apiKey, err := GetAPIKey(headers)
	if err != nil {
		t.Fatalf("GetAPIKey() error = %v", err)
	}

	if apiKey != "test-key" {
		t.Fatalf("GetAPIKey() key = %q, want %q", apiKey, "test-key")
	}
}

func TestGetAPIKeyRejectsInvalidHeader(t *testing.T) {
	for _, authorization := range []string{"", "Bearer test-key", "ApiKey", "ApiKey "} {
		headers := http.Header{}
		headers.Set("Authorization", authorization)

		if _, err := GetAPIKey(headers); err == nil {
			t.Fatalf("GetAPIKey() accepted invalid Authorization header %q", authorization)
		}
	}
}
