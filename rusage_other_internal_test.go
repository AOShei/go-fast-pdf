//go:build !unix

package gofastpdf

import "time"

// processorTime is not measured on this system.
func processorTime() time.Duration { return 0 }
