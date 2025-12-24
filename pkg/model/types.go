package model

// Document represents the final output of the library.
type Document struct {
	Metadata Metadata `json:"metadata"`
	Pages    []Page   `json:"pages"`
}

// Metadata holds document-level information.
type Metadata struct {
	Title    string `json:"title,omitempty"`
	Author   string `json:"author,omitempty"`
	Creator  string `json:"creator,omitempty"`
	Producer string `json:"producer,omitempty"`
	// Encrypted indicates if the file was password protected
	Encrypted bool `json:"encrypted"`
}

// Page represents a single page in the PDF.
type Page struct {
	PageNumber int     `json:"page_number"`
	Content    string  `json:"content"` // Markdown/Formatted text
	CharCount  int     `json:"char_count"`
	Width      float64 `json:"width"`
	Height     float64 `json:"height"`
	Images     []Image `json:"images,omitempty"`
}

// Image represents an image reference on a page.
type Image struct {
	Type       string    `json:"type"`                // "image" or "inline_image"
	ID         string    `json:"id,omitempty"`        // e.g., "Im1" (empty for inline images)
	Rect       []float64 `json:"rect,omitempty"`      // [x, y, width, height]
	Width      float64   `json:"width,omitempty"`     // Image width in pixels
	Height     float64   `json:"height,omitempty"`    // Image height in pixels
	ColorSpace string    `json:"color_space,omitempty"` // e.g., "/DeviceRGB"
}
