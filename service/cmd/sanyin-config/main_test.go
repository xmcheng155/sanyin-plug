package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestAuthenticationIsDisabledByDefaultAndOptionalWithPasswordFile(t *testing.T) {
	base := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	passwordless, err := withOptionalBasicAuth(base, "")
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	passwordless.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://speaker.local/", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("passwordless default returned %d", response.Code)
	}

	passwordFile := filepath.Join(t.TempDir(), "web-password")
	if err := os.WriteFile(passwordFile, []byte("long-local-password\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	protected, err := withOptionalBasicAuth(base, passwordFile)
	if err != nil {
		t.Fatal(err)
	}
	unauthorized := httptest.NewRecorder()
	protected.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "http://speaker.local/", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("optional password file did not enable auth: %d", unauthorized.Code)
	}
}

func TestBasicAuthProtectsPageAndAPI(t *testing.T) {
	handler := basicAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), "admin", "long-local-password")

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "http://speaker.local/", nil))
	if unauthorized.Code != http.StatusUnauthorized || unauthorized.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("unauthorized request was not challenged: %d %#v", unauthorized.Code, unauthorized.Header())
	}

	authorizedRequest := httptest.NewRequest(http.MethodGet, "http://speaker.local/", nil)
	authorizedRequest.SetBasicAuth("admin", "long-local-password")
	authorized := httptest.NewRecorder()
	handler.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusNoContent {
		t.Fatalf("valid credentials returned %d", authorized.Code)
	}
}

func TestCSRFProtectionRequiresSameOriginAndHeader(t *testing.T) {
	handler := csrfProtection(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))

	for name, item := range map[string]struct {
		origin   string
		header   string
		expected int
	}{
		"same origin":   {origin: "http://speaker.local:8787", header: "1", expected: http.StatusNoContent},
		"cross origin":  {origin: "http://attacker.invalid", header: "1", expected: http.StatusForbidden},
		"missing token": {origin: "http://speaker.local:8787", expected: http.StatusForbidden},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, "http://speaker.local:8787/api/v1/airplay/auto-recover", nil)
			request.Header.Set("Origin", item.origin)
			if item.header != "" {
				request.Header.Set("X-Sanyin-CSRF", item.header)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != item.expected {
				t.Fatalf("expected %d, got %d", item.expected, response.Code)
			}
		})
	}
}
