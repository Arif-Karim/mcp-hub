// Command devtoken stands in for mcp-hub during local development: it serves a
// throwaway Ed25519 JWKS on 127.0.0.1 and prints a matching short-lived token.
//
//	go run ./cmd/devtoken -aud service-directory
//
// The key lives only in memory; restart devtoken to rotate it. Never use in production.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:7499", "loopback address to serve the JWKS on")
	aud := flag.String("aud", "service-directory", "token audience (catalogue entry id)")
	iss := flag.String("iss", "mcp-hub", "token issuer")
	sub := flag.String("sub", "dev.user@example.test", "token subject")
	ttl := flag.Duration("ttl", time.Hour, "token lifetime")
	flag.Parse()

	if host, _, err := net.SplitHostPort(*addr); err != nil || host != "127.0.0.1" {
		log.Fatalf("devtoken: -addr must be 127.0.0.1:<port>")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	const kid = "devtoken-1"
	now := time.Now()
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{
		"sub": *sub, "iss": *iss, "aud": *aud,
		"iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(*ttl).Unix(),
	})
	tok.Header["kid"] = kid
	signed, err := tok.SignedString(priv)
	if err != nil {
		log.Fatal(err)
	}

	jwks, err := json.Marshal(map[string]any{"keys": []map[string]string{{
		"kty": "OKP", "crv": "Ed25519", "alg": "EdDSA", "use": "sig", "kid": kid,
		"x": base64.RawURLEncoding.EncodeToString(pub),
	}}})
	if err != nil {
		log.Fatal(err)
	}

	jwksURL := "http://" + *addr + "/.well-known/jwks.json"
	fmt.Fprintf(os.Stderr, "HUB_JWKS_URL=%s\nHUB_AUDIENCE=%s\nHUB_ISSUER=%s\n\n", jwksURL, *aud, *iss)
	fmt.Println(signed) // stdout carries only the token, so `TOKEN=$(...)` style use is easy

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwks)
	})
	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
