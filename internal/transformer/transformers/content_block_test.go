package transformers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// TestOpenAITransformer_ContentBlockPreservation tests that the transformer
// preserves all content block types, not just text.
func TestOpenAITransformer_ContentBlockPreservation(t *testing.T) {
	t.Run("text only - should work", func(t *testing.T) {
		transformer := NewOpenAITransformer()

		req := &anthropic.Request{
			Model:     "gpt-4",
			MaxTokens: 4096,
			Messages: []anthropic.Message{
				{
					Role:    anthropic.RoleUser,
					Content: anthropic.MessageContent{{Type: "text", Text: "Hello"}},
				},
			},
		}

		httpReq, err := transformer.PrepareRequest(req, "https://api.openai.com/v1/chat/completions", "test-key", "gpt-4")
		if err != nil {
			t.Fatalf("failed to prepare request: %v", err)
		}

		// Parse request body to verify content was preserved
		var body map[string]any
		json.NewDecoder(httpReq.Body).Decode(&body)

		messages := body["messages"].([]any)
		if len(messages) == 0 {
			t.Fatal("expected at least one message")
		}

		firstMsg := messages[0].(map[string]any)
		content := firstMsg["content"].(string)
		if content != "Hello" {
			t.Errorf("expected content 'Hello', got '%s'", content)
		}

		t.Logf("Text content preserved: %s", content)
	})

	t.Run("text + image - image should be preserved", func(t *testing.T) {
		transformer := NewOpenAITransformer()

		req := &anthropic.Request{
			Model:     "gpt-4-vision-preview",
			MaxTokens: 4096,
			Messages: []anthropic.Message{
				{
					Role: anthropic.RoleUser,
					Content: anthropic.MessageContent{
						{Type: "text", Text: "What's in this image?"},
						{
							Type: "image",
							Source: &anthropic.ImageSource{
								Type:      "base64",
								MediaType: "image/jpeg",
								Data:      "fake_image_data",
							},
						},
					},
				},
			},
		}

		httpReq, err := transformer.PrepareRequest(req, "https://api.openai.com/v1/chat/completions", "test-key", "gpt-4-vision-preview")
		if err != nil {
			t.Fatalf("failed to prepare request: %v", err)
		}

		// Parse request body
		var body map[string]any
		json.NewDecoder(httpReq.Body).Decode(&body)

		messages := body["messages"].([]any)
		firstMsg := messages[0].(map[string]any)
		content := firstMsg["content"]

		// OpenAI GPT-4 Vision supports content as array with text and image_url
		contentArray, ok := content.([]any)
		if !ok {
			t.Errorf("expected content to be array for vision request, got %T", content)
			t.Logf("Content value: %v", content)
			return
		}

		if len(contentArray) < 2 {
			t.Errorf("expected at least 2 content items (text + image), got %d", len(contentArray))
			t.Logf("Content array: %v", contentArray)
			return
		}

		t.Logf("Content preserved with %d blocks", len(contentArray))
	})

	t.Run("text + document - document should be preserved or noted", func(t *testing.T) {
		transformer := NewOpenAITransformer()

		req := &anthropic.Request{
			Model:     "gpt-4",
			MaxTokens: 4096,
			Messages: []anthropic.Message{
				{
					Role: anthropic.RoleUser,
					Content: anthropic.MessageContent{
						{Type: "text", Text: "Analyze this document:"},
						{
							Type: "document",
							DocumentSource: &anthropic.DocumentSource{
								Type:   "file",
								FileID: "file_abc123",
							},
							Title: "Report.pdf",
						},
					},
				},
			},
		}

		httpReq, err := transformer.PrepareRequest(req, "https://api.openai.com/v1/chat/completions", "test-key", "gpt-4")
		if err != nil {
			t.Fatalf("failed to prepare request: %v", err)
		}

		// Parse request body
		var body map[string]any
		json.NewDecoder(httpReq.Body).Decode(&body)

		messages := body["messages"].([]any)
		firstMsg := messages[0].(map[string]any)
		content := firstMsg["content"]

		// Content can be either string or array depending on block types
		contentArray, ok := content.([]any)
		if !ok {
			t.Errorf("expected content to be array for multimodal request, got %T", content)
			return
		}

		if len(contentArray) < 2 {
			t.Errorf("expected at least 2 content items (text + document placeholder), got %d", len(contentArray))
			return
		}

		// First item should be text
		firstItem := contentArray[0].(map[string]any)
		if firstItem["type"] != "text" {
			t.Errorf("expected first item type text, got %v", firstItem["type"])
		}

		// Second item should be the document placeholder
		secondItem := contentArray[1].(map[string]any)
		secondText := secondItem["text"].(string)

		if !strings.Contains(secondText, "Report.pdf") {
			t.Errorf("expected document placeholder to mention the title, got: %s", secondText)
		}

		if !strings.Contains(secondText, "file_abc123") {
			t.Errorf("expected document placeholder to mention file_id, got: %s", secondText)
		}

		t.Logf("Document placeholder added: %s", secondText)
		t.Log("NEXT STEP: Implement file resolution to fetch actual document content (Task #13)")
	})
}
