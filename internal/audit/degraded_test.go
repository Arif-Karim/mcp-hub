package audit_test

import "testing"

func TestWriteFailuresAreCounted(t *testing.T) {
	l, _ := newLog(t, 10)
	l.Record(ev(1))
	if n := l.WriteErrors(); n != 0 {
		t.Fatalf("WriteErrors = %d before failure", n)
	}
	_ = l.Close()
	l.Record(ev(2))
	l.Record(ev(3))
	if n := l.WriteErrors(); n != 2 {
		t.Fatalf("WriteErrors = %d, want 2", n)
	}
	if got := l.Recent(5); len(got) != 3 {
		t.Fatalf("ring len = %d, want 3", len(got))
	}
}
