package sample

import (
	"testing"

	"sample/support"
)

// TestOnlyUsed exists so the fixture can prove that a use inside a test file is
// NOT a read by default: OnlyTestUsesThis must still report reads=0.
func TestOnlyUsed(t *testing.T) {
	if OnlyTestUsesThis() != 1 {
		t.Fatal("no")
	}
}

// TestUsesSupport makes support a test-support package.
func TestUsesSupport(t *testing.T) {
	if support.Helper() != 1 {
		t.Fatal("no")
	}
}
