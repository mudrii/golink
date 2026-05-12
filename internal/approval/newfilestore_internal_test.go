package approval

import "testing"

func TestNewFileStoreUsesDefaultPathWhenEmpty(t *testing.T) {
	store := NewFileStore("")
	if store.dir != ResolvePath() {
		t.Fatalf("dir = %q, want default path %q", store.dir, ResolvePath())
	}
}
