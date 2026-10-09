// Package consumer is a first-party user of sample, for the consumer test.
package consumer

import "sample"

// Use calls the sample API. ConsumerOwn is never reported: the scan reports
// the declarations of the scanned module only.
func Use() int { return sample.OnlyConsumerUses() }

// ConsumerOwn is read by nothing.
func ConsumerOwn() int { return 0 }
