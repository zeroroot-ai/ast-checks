package support

import "testing"

// TestOrphanIsTwo gives support a test main of its own, which imports it.
func TestOrphanIsTwo(t *testing.T) {
	if Orphan() != 2 {
		t.Fatal("no")
	}
}
