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
	target := filepath.Join(dir, "real.pem")
	if _, err := token.NewIssuer(target, iss, time.Now); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.pem")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := token.NewIssuer(link, iss, time.Now); !errors.Is(err, token.ErrInvalidKey) {
		t.Fatalf("err = %v, want ErrInvalidKey", err)
	}
}
