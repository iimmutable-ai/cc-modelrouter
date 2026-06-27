package anthropic_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// TestFileUploadResponseSpecCompliance tests that FileUploadResponse
// matches the official Anthropic Files API specification.
func TestFileUploadResponseSpecCompliance(t *testing.T) {
	t.Run("response has correct field names and types", func(t *testing.T) {
		// According to official spec:
		// {
		//   "created_at": "2023-11-07T05:31:56Z",  // RFC 3339 string
		//   "downloadable": false,
		//   "filename": "document.pdf",
		//   "id": "file_abc123",
		//   "mime_type": "application/pdf",
		//   "size_bytes": 1024,
		//   "type": "file"
		// }

		response := anthropic.FileUploadResponse{
			ID:           "file_abc123",
			Type:         "file",
			CreatedAt:    time.Date(2023, 11, 7, 5, 31, 56, 0, time.UTC),
			SizeBytes:    1024,
			Filename:     "document.pdf",
			MimeType:     "application/pdf",
			Downloadable: false,
		}

		data, err := json.Marshal(response)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var result map[string]any
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		// Check field names match spec
		if result["created_at"] == nil {
			t.Error("missing created_at field")
		} else if _, ok := result["created_at"].(string); !ok {
			t.Errorf("created_at should be string (RFC 3339), got %T", result["created_at"])
		}

		if result["downloadable"] == nil {
			t.Error("missing downloadable field")
		}

		if result["filename"] == nil {
			t.Error("missing filename field")
		}

		if result["id"] == nil {
			t.Error("missing id field")
		}

		if result["mime_type"] == nil {
			t.Error("missing mime_type field")
		}

		// Should be size_bytes, not bytes
		if result["size_bytes"] == nil {
			if result["bytes"] != nil {
				t.Error("field should be named size_bytes, not bytes")
			} else {
				t.Error("missing size_bytes field")
			}
		}

		if result["type"] != "file" {
			t.Errorf("type should be 'file', got %v", result["type"])
		}

		// Purpose field is NOT in Anthropic's spec (it's from OpenAI)
		if result["purpose"] != nil {
			t.Error("purpose field should not be present (not in Anthropic spec)")
		}
	})

	t.Run("created_at is RFC 3339 timestamp string", func(t *testing.T) {
		response := anthropic.FileUploadResponse{
			ID:        "file_test",
			CreatedAt: time.Date(2023, 11, 7, 5, 31, 56, 0, time.UTC),
		}

		data, err := json.Marshal(response)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var result map[string]any
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		createdAt, ok := result["created_at"].(string)
		if !ok {
			t.Errorf("created_at should be string, got %T", result["created_at"])
		}

		// Should be in RFC 3339 format
		if _, err := time.Parse(time.RFC3339, createdAt); err != nil {
			t.Errorf("created_at should be RFC 3339 format, got: %s (error: %v)", createdAt, err)
		}
	})
}
