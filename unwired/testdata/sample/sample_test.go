package sample

import "testing"

// TestOnlyUsed exists so the fixture can prove that a use inside a test file is
// NOT a read by default: OnlyTestUsesThis must still report reads=0.
func TestOnlyUsed(t *testing.T) {
	if OnlyTestUsesThis() != 1 {
		t.Fatal("no")
	}
}
