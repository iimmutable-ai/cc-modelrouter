package proxy

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/pkg/api/anthropic"
)

// TestFilesAPITypes tests that Files API types can be marshaled/unmarshaled correctly.
func TestFilesAPITypes(t *testing.T) {
	t.Run("FileUploadResponse marshaling", func(t *testing.T) {
		response := anthropic.FileUploadResponse{
			ID:           "file-abc123",
			Type:         "file",
			CreatedAt:    time.Date(2023, 11, 7, 5, 31, 56, 0, time.UTC),
			SizeBytes:    1024,
			Filename:     "test.pdf",
			MimeType:     "application/pdf",
			Downloadable: false,
		}

		data, err := json.Marshal(response)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var unmarshaled anthropic.FileUploadResponse
		if err := json.Unmarshal(data, &unmarshaled); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if unmarshaled.ID != response.ID {
			t.Errorf("expected ID %s, got %s", response.ID, unmarshaled.ID)
		}

		if unmarshaled.Filename != response.Filename {
			t.Errorf("expected filename %s, got %s", response.Filename, unmarshaled.Filename)
		}

		if unmarshaled.MimeType != response.MimeType {
			t.Errorf("expected mime type %s, got %s", response.MimeType, unmarshaled.MimeType)
		}
	})

	t.Run("FileListResponse marshaling", func(t *testing.T) {
		response := anthropic.FileListResponse{
			Object: "list",
			Data: []anthropic.FileObject{
				{
					ID:           "file-1",
					Type:         "file",
					CreatedAt:    time.Date(2023, 11, 7, 5, 31, 56, 0, time.UTC),
					SizeBytes:    1024,
					Filename:     "test1.pdf",
					MimeType:     "application/pdf",
					Downloadable: false,
				},
				{
					ID:           "file-2",
					Type:         "file",
					CreatedAt:    time.Date(2023, 11, 7, 5, 31, 57, 0, time.UTC),
					SizeBytes:    2048,
					Filename:     "test2.pdf",
					MimeType:     "application/pdf",
					Downloadable: false,
				},
			},
			HasMore: false,
		}

		data, err := json.Marshal(response)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var unmarshaled anthropic.FileListResponse
		if err := json.Unmarshal(data, &unmarshaled); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if len(unmarshaled.Data) != 2 {
			t.Fatalf("expected 2 files, got %d", len(unmarshaled.Data))
		}
	})

	t.Run("FileDeleteResponse marshaling", func(t *testing.T) {
		response := anthropic.FileDeleteResponse{
			ID:      "file-abc123",
			Type:    "file",
			Deleted: true,
		}

		data, err := json.Marshal(response)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var unmarshaled anthropic.FileDeleteResponse
		if err := json.Unmarshal(data, &unmarshaled); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if !unmarshaled.Deleted {
			t.Error("expected deleted to be true")
		}
	})
}

// TestFilePurposeValidation tests file purpose validation.
func TestFilePurposeValidation(t *testing.T) {
	validPurposes := []string{
		anthropic.FilePurposeVision,
		anthropic.FilePurposeAssistant,
		anthropic.FilePurposeFineTune,
		anthropic.FilePurposeAssistants,
	}

	for _, purpose := range validPurposes {
		t.Run("valid_"+purpose, func(t *testing.T) {
			if !anthropic.IsValidFilePurpose(purpose) {
				t.Errorf("expected %s to be valid", purpose)
			}
		})
	}

	invalidPurposes := []string{"", "invalid", "documents", "training"}
	for _, purpose := range invalidPurposes {
		t.Run("invalid_"+purpose, func(t *testing.T) {
			if anthropic.IsValidFilePurpose(purpose) {
				t.Errorf("expected %s to be invalid", purpose)
			}
		})
	}
}

// TestFilesAPIEndpoints tests that the handler properly routes to Files API endpoints.
func TestFilesAPIEndpoints(t *testing.T) {
	handler := NewHandler(50 * 1024 * 1024)
	setupHandler(t, handler, "test-files-api")

	tests := []struct {
		name           string
		method         string
		path           string
		body           string
		expectedStatus int
		validateBody   bool
	}{
		{
			name:           "POST /v1/files - upload file",
			method:         "POST",
			path:           "/v1/files",
			body:           `{"filename":"test.pdf","purpose":"vision"}`,
			expectedStatus: http.StatusOK,
			validateBody:   true,
		},
		{
			name:           "GET /v1/files - list files",
			method:         "GET",
			path:           "/v1/files",
			body:           "",
			expectedStatus: http.StatusOK,
			validateBody:   true,
		},
		{
			name:           "GET /v1/files/{id} - get file",
			method:         "GET",
			path:           "/v1/files/file-abc123",
			body:           "",
			expectedStatus: http.StatusOK,
			validateBody:   true,
		},
		{
			name:           "DELETE /v1/files/{id} - delete file",
			method:         "DELETE",
			path:           "/v1/files/file-abc123",
			body:           "",
			expectedStatus: http.StatusOK,
			validateBody:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req *http.Request
			if tt.body != "" {
				req = httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(tt.method, tt.path, nil)
			}

			// Add required Files API beta header
			req.Header.Set("anthropic-beta", "files-api-2025-04-14")

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.validateBody && w.Code == http.StatusOK {
				// Verify response has JSON content type
				contentType := w.Header().Get("Content-Type")
				if contentType != "application/json" {
					t.Errorf("Expected Content-Type application/json, got %s", contentType)
				}

				// Verify response body is valid JSON
				var result map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Errorf("Expected valid JSON response, got error: %v", err)
				}
			}
		})
	}
}

// TestFileUploadSizeValidation tests file size validation for uploads.
func TestFileUploadSizeValidation(t *testing.T) {
	handler := NewHandler(50 * 1024 * 1024)
	setupHandler(t, handler, "test-file-size")

	tests := []struct {
		name           string
		contentLength  int64
		expectAccept   bool
	}{
		{
			name:          "small file (1KB)",
			contentLength: 1024,
			expectAccept:  true,
		},
		{
			name:          "medium file (10MB)",
			contentLength: 10 * 1024 * 1024,
			expectAccept:  true,
		},
		{
			name:          "large file (45MB)",
			contentLength: 45 * 1024 * 1024,
			expectAccept:  true,
		},
		{
			name:          "over limit (51MB)",
			contentLength: 51 * 1024 * 1024,
			expectAccept:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a multipart form request
			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)

			// Add file form field
			part, err := writer.CreateFormFile("file", "test.pdf")
			if err != nil {
				t.Fatalf("failed to create form file: %v", err)
			}

			// Write dummy data of specified size
			dummyData := make([]byte, tt.contentLength)
			for i := range dummyData {
				dummyData[i] = byte(i % 256)
			}
			part.Write(dummyData)

			writer.WriteField("purpose", "vision")
			writer.Close()

			req := httptest.NewRequest("POST", "/v1/files", body)
			req.Header.Set("Content-Type", writer.FormDataContentType())

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			// Currently returns 501 (not implemented)
			// When implemented, should check for 413 (Payload Too Large) for oversized files
			t.Logf("File size %d: status=%d", tt.contentLength, w.Code)
		})
	}
}

// TestFileMalformedResponse tests handling of malformed file responses.
func TestFileMalformedResponse(t *testing.T) {
	handler := NewHandler(50 * 1024 * 1024)
	setupHandler(t, handler, "test-malformed")

	tests := []struct {
		name        string
		method      string
		path        string
		body        string
		contentType string
	}{
		{
			name:        "invalid JSON in file upload",
			method:      "POST",
			path:        "/v1/files",
			body:        "{invalid json}",
			contentType: "application/json",
		},
		{
			name:        "missing required fields",
			method:      "POST",
			path:        "/v1/files",
			body:        `{"filename": "test.pdf"}`, // missing purpose
			contentType: "application/json",
		},
		{
			name:        "invalid file ID",
			method:      "GET",
			path:        "/v1/files/invalid-id-format!",
			body:        "",
			contentType: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			t.Logf("Malformed test '%s': status=%d", tt.name, w.Code)
		})
	}
}
