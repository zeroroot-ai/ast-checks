package consumer

import (
	"testing"

	"sample"
)

func TestUse(t *testing.T) {
	if sample.OnlyConsumerTestUses() != 4 {
		t.Fatal("no")
	}
}
