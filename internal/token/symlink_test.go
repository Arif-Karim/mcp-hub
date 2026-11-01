package token_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Arif-Karim/mcp-hub/internal/token"
)

func TestSymlinkedKeyFileRejected(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.pem")
	if _, err := token.NewIssuer(real, iss, time.Now); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.pem")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := token.NewIssuer(link, iss, time.Now); !errors.Is(err, token.ErrInvalidKey) {
		t.Fatalf("err = %v, want ErrInvalidKey", err)
	}
}
