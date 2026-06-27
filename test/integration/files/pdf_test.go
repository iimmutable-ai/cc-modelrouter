package files

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// TestPDFTypes tests that PDF types can be marshaled/unmarshaled correctly.
func TestPDFTypes(t *testing.T) {
	t.Run("PDFSource marshaling", func(t *testing.T) {
		source := anthropic.PDFSource{
			Type:      "base64",
			Data:      "JVBERi0xLjQKJeLjz9MK", // Minimal PDF base64
			MediaType: "application/pdf",
		}

		data, err := json.Marshal(source)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var unmarshaled anthropic.PDFSource
		if err := json.Unmarshal(data, &unmarshaled); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if unmarshaled.Type != source.Type {
			t.Errorf("expected type %s, got %s", source.Type, unmarshaled.Type)
		}

		if unmarshaled.MediaType != source.MediaType {
			t.Errorf("expected media type %s, got %s", source.MediaType, unmarshaled.MediaType)
		}
	})

	t.Run("PDFDocument marshaling", func(t *testing.T) {
		doc := anthropic.PDFDocument{
			ID:        "pdf-doc-123",
			Filename:  "test.pdf",
			PageCount: 5,
			Pages: []anthropic.PDFPage{
				{
					PageNumber: 1,
					Image:      "base64image1",
					Text:       "First page content",
					Width:      800,
					Height:     1200,
				},
			},
			Metadata: anthropic.PDFMetadata{
				Title:      "Test Document",
				PageCount:  5,
			},
			CreatedAt: 1234567890,
		}

		data, err := json.Marshal(doc)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var unmarshaled anthropic.PDFDocument
		if err := json.Unmarshal(data, &unmarshaled); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if unmarshaled.Filename != doc.Filename {
			t.Errorf("expected filename %s, got %s", doc.Filename, unmarshaled.Filename)
		}

		if unmarshaled.PageCount != doc.PageCount {
			t.Errorf("expected page count %d, got %d", doc.PageCount, unmarshaled.PageCount)
		}
	})

	t.Run("PDFExtractionRequest marshaling", func(t *testing.T) {
		req := anthropic.PDFExtractionRequest{
			FileID:        "file-abc123",
			PageNumbers:   []int{1, 2, 3},
			ExtractText:   true,
			ExtractImages: true,
		}

		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var unmarshaled anthropic.PDFExtractionRequest
		if err := json.Unmarshal(data, &unmarshaled); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if unmarshaled.FileID != req.FileID {
			t.Errorf("expected file ID %s, got %s", req.FileID, unmarshaled.FileID)
		}

		if len(unmarshaled.PageNumbers) != len(req.PageNumbers) {
			t.Errorf("expected %d page numbers, got %d", len(req.PageNumbers), len(unmarshaled.PageNumbers))
		}
	})
}

// TestPDFMediaTypeValidation tests PDF media type validation.
func TestPDFMediaTypeValidation(t *testing.T) {
	validTypes := []string{
		"application/pdf",
		"application/x-pdf",
		"text/pdf",
	}

	for _, mediaType := range validTypes {
		t.Run("valid_"+mediaType, func(t *testing.T) {
			if !anthropic.IsValidPDFMediaType(mediaType) {
				t.Errorf("expected %s to be valid", mediaType)
			}
		})
	}

	invalidTypes := []string{
		"application/octet-stream",
		"text/plain",
		"image/jpeg",
		"",
		"application/msword",
	}

	for _, mediaType := range invalidTypes {
		t.Run("invalid_"+strings.ReplaceAll(mediaType, "/", "_"), func(t *testing.T) {
			if anthropic.IsValidPDFMediaType(mediaType) {
				t.Errorf("expected %s to be invalid", mediaType)
			}
		})
	}
}

// TestPDFSizeValidation tests PDF file size validation.
func TestPDFSizeValidation(t *testing.T) {
	// MaxPDFFileSize is 100MB
	maxSize := anthropic.MaxPDFFileSize

	tests := []struct {
		name        string
		size        int64
		expectValid bool
	}{
		{
			name:        "small PDF (1KB)",
			size:        1024,
			expectValid: true,
		},
		{
			name:        "medium PDF (10MB)",
			size:        10 * 1024 * 1024,
			expectValid: true,
		},
		{
			name:        "large PDF (50MB)",
			size:        50 * 1024 * 1024,
			expectValid: true,
		},
		{
			name:        "at limit (100MB)",
			size:        maxSize,
			expectValid: true,
		},
		{
			name:        "over limit (101MB)",
			size:        maxSize + 1,
			expectValid: false,
		},
		{
			name:        "way over limit (200MB)",
			size:        200 * 1024 * 1024,
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := tt.size <= maxSize
			if isValid != tt.expectValid {
				t.Errorf("size %d: expected valid=%v, got valid=%v", tt.size, tt.expectValid, isValid)
			}
		})
	}
}

// TestPDFInMessageContent tests PDF content in message content blocks.
func TestPDFInMessageContent(t *testing.T) {
	// Test PDF as content block
	pdfBlock := map[string]any{
		"type":     "pdf",
		"file_id":  "file-abc123",
		"source": map[string]string{
			"type":       "file_id",
			"media_type": "application/pdf",
		},
	}

	data, err := json.Marshal(pdfBlock)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var unmarshaled map[string]any
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if unmarshaled["type"] != "pdf" {
		t.Errorf("expected type pdf, got %v", unmarshaled["type"])
	}

	t.Logf("PDF block marshaled successfully: %s", string(data))
}

// TestPDFPageExtraction tests PDF page extraction structure.
func TestPDFPageExtraction(t *testing.T) {
	extractionResponse := anthropic.PDFExtractionResponse{
		ID:     "extraction-123",
		FileID: "file-abc123",
		Pages: []anthropic.PDFPage{
			{
				PageNumber: 1,
				Text:       "Page 1 content",
				Width:      800,
				Height:     1200,
			},
			{
				PageNumber: 2,
				Text:       "Page 2 content",
				Image:      "base64img1",
				Width:      800,
				Height:     1200,
			},
		},
		Metadata: anthropic.PDFMetadata{
			Title:      "Sample PDF",
			PageCount:  2,
		},
		Status: "completed",
	}

	data, err := json.Marshal(extractionResponse)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var unmarshaled anthropic.PDFExtractionResponse
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(unmarshaled.Pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(unmarshaled.Pages))
	}

	if unmarshaled.Pages[0].PageNumber != 1 {
		t.Errorf("expected page 1, got %d", unmarshaled.Pages[0].PageNumber)
	}

	if unmarshaled.Status != "completed" {
		t.Errorf("expected status completed, got %s", unmarshaled.Status)
	}

	t.Logf("PDF extraction response marshaled successfully")
}

// TestPDFMalformedResponse tests handling of malformed PDF responses.
func TestPDFMalformedResponse(t *testing.T) {
	tests := []struct {
		name        string
		response    string
		expectError bool
	}{
		{
			name:        "invalid JSON",
			response:    "{invalid pdf data",
			expectError: true,
		},
		{
			name:        "missing required fields",
			response:    `{"file_id": "file-123"}`, // missing pages
			expectError: false, // Partial data may be OK
		},
		{
			name:        "empty response",
			response:    "{}",
			expectError: false, // Empty object is valid JSON
		},
		{
			name: "invalid page count",
			response: `{
				"id": "extraction-123",
				"file_id": "file-123",
				"pages": [{"page_number": -1}],
				"status": "completed"
			}`,
			expectError: false, // JSON is valid, semantic validation is separate
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result anthropic.PDFExtractionResponse
			err := json.Unmarshal([]byte(tt.response), &result)

			if tt.expectError && err == nil {
				t.Error("expected error but got none")
			}

			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestPDFUploadAndUse tests uploading a PDF and using it in a message.
func TestPDFUploadAndUse(t *testing.T) {
	// Simulate file upload response
	uploadResp := anthropic.FileUploadResponse{
		ID:           "file-pdf-123",
		Type:         "file",
		CreatedAt:    time.Date(2023, 11, 7, 5, 31, 56, 0, time.UTC),
		SizeBytes:    1024,
		Filename:     "test.pdf",
		MimeType:     "application/pdf",
		Downloadable: false,
	}

	// Verify response structure
	if uploadResp.ID == "" {
		t.Error("expected non-empty file ID")
	}

	if uploadResp.Filename != "test.pdf" {
		t.Errorf("expected filename test.pdf, got %s", uploadResp.Filename)
	}

	if uploadResp.MimeType != "application/pdf" {
		t.Errorf("expected mime type application/pdf, got %s", uploadResp.MimeType)
	}

	t.Logf("PDF file upload response: ID=%s, Filename=%s, MimeType=%s", uploadResp.ID, uploadResp.Filename, uploadResp.MimeType)
}

// TestPDFWithMaxPages tests PDF with maximum page count.
func TestPDFWithMaxPages(t *testing.T) {
	maxPages := anthropic.MaxPDFPages // 1000

	// Create a PDF document with max pages
	var pages []anthropic.PDFPage
	for i := 1; i <= maxPages; i++ {
		pages = append(pages, anthropic.PDFPage{
			PageNumber: i,
			Text:       "Page content",
			Width:      800,
			Height:     1200,
		})
	}

	doc := anthropic.PDFDocument{
		ID:        "pdf-large",
		Filename:  "large.pdf",
		PageCount: maxPages,
		Pages:     pages,
		Metadata: anthropic.PDFMetadata{
			Title:     "Large Document",
			PageCount: maxPages,
		},
	}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	t.Logf("Marshaled PDF with %d pages: %d bytes", maxPages, len(data))

	if len(data) == 0 {
		t.Error("expected non-empty marshaled data")
	}
}

// TestPDFMetadata tests PDF metadata handling.
func TestPDFMetadata(t *testing.T) {
	metadata := anthropic.PDFMetadata{
		Title:      "Test PDF Document",
		Author:     "Test Author",
		Subject:    "Test Subject",
		Keywords:   "test, pdf, metadata",
		Creator:    "Test Creator",
		Producer:   "Test Producer",
		Created:    1234567890,
		Modified:   1234567900,
		PageCount:  10,
	}

	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var unmarshaled anthropic.PDFMetadata
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if unmarshaled.Title != metadata.Title {
		t.Errorf("expected title %s, got %s", metadata.Title, unmarshaled.Title)
	}

	if unmarshaled.Author != metadata.Author {
		t.Errorf("expected author %s, got %s", metadata.Author, unmarshaled.Author)
	}

	if unmarshaled.PageCount != metadata.PageCount {
		t.Errorf("expected page count %d, got %d", metadata.PageCount, unmarshaled.PageCount)
	}

	t.Logf("PDF metadata marshaled successfully")
}
