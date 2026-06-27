package anthropic_test

import (
	"encoding/json"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// TestDocumentContentBlockWithFileID tests that document content blocks
// can be marshaled/unmarshaled with file_id references.
func TestDocumentContentBlockWithFileID(t *testing.T) {
	t.Run("marshal document block with file_id", func(t *testing.T) {
		block := anthropic.ContentBlock{
			Type: "document",
			DocumentSource: &anthropic.DocumentSource{
				Type:   "file",
				FileID: "file_011CNha8iCJcU1wXNR6q4V8w",
			},
		}

		data, err := json.Marshal(block)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var result map[string]any
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if result["type"] != "document" {
			t.Errorf("expected type document, got %v", result["type"])
		}

		// Verify source structure
		source, ok := result["source"].(map[string]any)
		if !ok {
			t.Fatal("expected source to be present")
		}

		if source["type"] != "file" {
			t.Errorf("expected source type file, got %v", source["type"])
		}

		if source["file_id"] != "file_011CNha8iCJcU1wXNR6q4V8w" {
			t.Errorf("expected file_id file_011CNha8iCJcU1wXNR6q4V8w, got %v", source["file_id"])
		}
	})

	t.Run("unmarshal document block from JSON", func(t *testing.T) {
		jsonData := `{
			"type": "document",
			"source": {
				"type": "file",
				"file_id": "file_011CNha8iCJcU1wXNR6q4V8w"
			},
			"title": "Test Document"
		}`

		var block anthropic.ContentBlock
		if err := json.Unmarshal([]byte(jsonData), &block); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if block.Type != "document" {
			t.Errorf("expected type document, got %s", block.Type)
		}

		// Verify source was properly unmarshaled
		if block.DocumentSource == nil {
			t.Fatal("expected DocumentSource to be present after unmarshal")
		}

		if block.DocumentSource.Type != "file" {
			t.Errorf("expected source type file, got %s", block.DocumentSource.Type)
		}

		if block.DocumentSource.FileID != "file_011CNha8iCJcU1wXNR6q4V8w" {
			t.Errorf("expected file_id file_011CNha8iCJcU1wXNR6q4V8w, got %s", block.DocumentSource.FileID)
		}
	})

	t.Run("document block with optional fields", func(t *testing.T) {
		jsonData := `{
			"type": "document",
			"source": {
				"type": "file",
				"file_id": "file_abc123"
			},
			"title": "Annual Report",
			"context": "Financial analysis document",
			"citations": {"enabled": true}
		}`

		var block anthropic.ContentBlock
		if err := json.Unmarshal([]byte(jsonData), &block); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if block.Title != "Annual Report" {
			t.Errorf("expected title 'Annual Report', got %s", block.Title)
		}

		if block.Context != "Financial analysis document" {
			t.Errorf("expected context 'Financial analysis document', got %s", block.Context)
		}

		if block.Citations == nil || !block.Citations.Enabled {
			t.Error("expected citations to be enabled")
		}
	})
}

// TestMessageContentWithDocument tests that messages can contain
// document content blocks alongside text.
func TestMessageContentWithDocument(t *testing.T) {
	jsonData := `{
		"role": "user",
		"content": [
			{"type": "text", "text": "Analyze this document:"},
			{
				"type": "document",
				"source": {
					"type": "file",
					"file_id": "file_12345"
				},
				"title": "Analysis Report"
			}
		]
	}`

	var msg anthropic.Message
	if err := json.Unmarshal([]byte(jsonData), &msg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(msg.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(msg.Content))
	}

	// First block should be text
	if msg.Content[0].Type != "text" {
		t.Errorf("expected first block type text, got %s", msg.Content[0].Type)
	}

	// Second block should be document
	if msg.Content[1].Type != "document" {
		t.Errorf("expected second block type document, got %s", msg.Content[1].Type)
	}

	if msg.Content[1].DocumentSource == nil {
		t.Fatal("expected document source to be present")
	}

	if msg.Content[1].DocumentSource.FileID != "file_12345" {
		t.Errorf("expected file_id file_12345, got %s", msg.Content[1].DocumentSource.FileID)
	}
}
