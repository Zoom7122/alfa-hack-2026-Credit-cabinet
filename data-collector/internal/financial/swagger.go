package financial

// These response shapes describe the JSON emitted by the inline document handlers.
// Route annotations live beside those handlers and are read with --parseFuncBody.
type documentListResponse struct {
	Items []Document `json:"items"`
}

type processingPendingResponse struct {
	State string `json:"state" example:"PENDING"`
}

type documentErrorResponse struct {
	Code   string `json:"code"`
	Type   string `json:"type,omitempty"`
	Title  string `json:"title,omitempty"`
	Status int    `json:"status,omitempty"`
}
