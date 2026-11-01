package main

import "testing"

func TestRequireLoopback(t *testing.T) {
	cases := []struct {
		addr string
		ok   bool
	}{
		{"127.0.0.1:7420", true},
		{"localhost:7420", true},
		{"[::1]:7420", true},
		{"0.0.0.0:7420", false},
		{":7420", false},
		{"10.1.2.3:7420", false},
		{"not-an-addr", false},
	}
	for _, c := range cases {
		err := requireLoopback(c.addr)
		if (err == nil) != c.ok {
			t.Errorf("requireLoopback(%q) err=%v, want ok=%v", c.addr, err, c.ok)
		}
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	if err := run([]string{"bogus"}); err == nil {
		t.Fatal("expected error for unknown command")
	}
	if err := run(nil); err == nil {
		t.Fatal("expected usage error with no args")
	}
}
