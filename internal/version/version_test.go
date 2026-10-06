package version

import (
	"errors"
	"testing"
)

func TestDraftConflict(t *testing.T) {
	if err := CheckRevision(3, 3); err != nil {
		t.Fatalf("same revision should be accepted: %v", err)
	}
	err := CheckRevision(3, 4)
	if err == nil {
		t.Fatal("stale revision must conflict")
	}
	var conflict *Conflict
	if !errors.As(err, &conflict) {
		t.Fatalf("expected Conflict, got %T", err)
	}
	if conflict.YourRevision != 3 || conflict.Current != 4 {
		t.Fatalf("conflict revisions wrong: %+v", conflict)
	}
	if NextPublishedVersion(7) != 8 {
		t.Fatal("published versions must increase by one")
	}
}
