package config

import (
	"errors"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func valid() map[string]string {
	return map[string]string{
		"PORT":         "7501",
		"HUB_JWKS_URL": "http://127.0.0.1:7420/.well-known/jwks.json",
		"HUB_AUDIENCE": "service-directory",
	}
}

func TestLoadValid(t *testing.T) {
	cfg, err := Load(env(valid()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		Addr:     "127.0.0.1:7501",
		JWKSURL:  "http://127.0.0.1:7420/.well-known/jwks.json",
		Audience: "service-directory",
		Issuer:   "mcp-hub",
	}
	if cfg != want {
		t.Fatalf("got %+v, want %+v", cfg, want)
	}
}

func TestLoadIssuerOverride(t *testing.T) {
	m := valid()
	m["HUB_ISSUER"] = "corp-idp"
	cfg, err := Load(env(m))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Issuer != "corp-idp" {
		t.Fatalf("issuer = %q, want corp-idp", cfg.Issuer)
	}
}

func TestLoadErrorsNameTheVariable(t *testing.T) {
	cases := []struct {
		name, key, value, wantVar string
	}{
		{"missing port", "PORT", "", "PORT"},
		{"non-numeric port", "PORT", "http", "PORT"},
		{"port zero", "PORT", "0", "PORT"},
		{"port too big", "PORT", "65536", "PORT"},
		{"missing jwks", "HUB_JWKS_URL", "", "HUB_JWKS_URL"},
		{"jwks not a url", "HUB_JWKS_URL", "not a url", "HUB_JWKS_URL"},
		{"jwks bad scheme", "HUB_JWKS_URL", "file:///etc/jwks.json", "HUB_JWKS_URL"},
		{"missing audience", "HUB_AUDIENCE", "  ", "HUB_AUDIENCE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := valid()
			m[tc.key] = tc.value
			_, err := Load(env(m))
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, ErrInvalid) {
				t.Errorf("error %v does not wrap ErrInvalid", err)
			}
			if !strings.Contains(err.Error(), tc.wantVar) {
				t.Errorf("error %q does not name %s", err, tc.wantVar)
			}
		})
	}
}
