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
}
