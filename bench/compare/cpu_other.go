//go:build !unix

package main

import "time"

// processorTime is not known here; the report shows none.
func processorTime() time.Duration { return 0 }
