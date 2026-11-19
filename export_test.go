package gofastpdf

// BoundedExtractor returns an extractor whose checks of DeepHidden
// follow at most shapes shapes on a page and do at most steps steps of
// work on it; 0 leaves a bound at its default. The first is the field
// MaxShapes; the second can be set only here. It is for the tests of
// those bounds, which a page of ordinary size does not reach.
func BoundedExtractor(shapes, steps int) *Extractor {
	return &Extractor{MaxShapes: shapes, maxDeepSteps: steps}
}
