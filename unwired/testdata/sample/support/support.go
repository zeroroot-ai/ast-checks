// Package support is a test-support package: sample_test.go imports it and no
// production file does.
package support

// Helper is called by sample_test.go. reads>=1 from tests.
func Helper() int { return 1 }

// Orphan is called by nobody, test or production. reads=0
func Orphan() int { return 2 }

// OnlyConsumerTestHelper is called by a test of the consumer module only.
// support is test support, so with the consumer loaded: reads>=1 from tests.
func OnlyConsumerTestHelper() int { return 3 }
