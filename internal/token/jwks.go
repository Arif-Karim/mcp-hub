package token

import (
	"encoding/base64"
	"net/http"

	"github.com/Arif-Karim/mcp-hub/internal/httpx"
)

type jwk struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
}

// JWKSHandler serves the public key as an RFC 7517/8037 JWK Set.
func (i *Issuer) JWKSHandler() http.Handler {
	doc := map[string][]jwk{"keys": {{
		Kty: "OKP",
		Crv: "Ed25519",
		X:   base64.RawURLEncoding.EncodeToString(i.pub),
		Kid: i.kid,
		Alg: "EdDSA",
		Use: "sig",
	}}}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "max-age=300")
		httpx.WriteJSON(w, http.StatusOK, doc)
	})
}
