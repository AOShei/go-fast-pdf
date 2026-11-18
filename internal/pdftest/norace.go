//go:build !race

package pdftest

// RaceEnabled reports whether the test binary was built with the race
// detector, which changes how much a program allocates. Tests that hold
// code to an allocation budget skip themselves when it is on.
const RaceEnabled = false
