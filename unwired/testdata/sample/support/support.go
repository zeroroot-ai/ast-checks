// Package support is a test-support package: sample_test.go imports it and no
// production file does.
package support

// Helper is called by sample_test.go. reads>=1 from tests.
func Helper() int { return 1 }

// Orphan is called by nobody, test or production. reads=0
func Orphan() int { return 2 }
