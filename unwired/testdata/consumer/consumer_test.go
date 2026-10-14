package consumer

import (
	"testing"

	"sample"
	"sample/support"
)

func TestUse(t *testing.T) {
	if support.OnlyConsumerTestHelper() != 3 {
		t.Fatal("no")
	}
	if sample.OnlyConsumerTestUses() != 4 {
		t.Fatal("no")
	}
}
