package lsp

import (
	"fmt"
	"testing"

	"go.lsp.dev/protocol"
)

func TestDocumentManager_OpenDocument(t *testing.T) {
	dm := NewDocumentManager()

	uri := protocol.DocumentURI("file:///tmp/test.yml")
	version := int32(1)
	content := "steps:\n  - label: \"test\""

	dm.OpenDocument(uri, version, content)

	doc, exists := dm.GetDocument(uri)
	if !exists {
		t.Fatal("Document should exist after opening")
	}

	if doc.URI != uri {
		t.Errorf("Expected URI %s, got %s", uri, doc.URI)
	}

	if doc.Version != version {
		t.Errorf("Expected version %d, got %d", version, doc.Version)
	}

	if doc.Content != content {
		t.Errorf("Expected content %q, got %q", content, doc.Content)
	}

	expectedLines := []string{"steps:", "  - label: \"test\""}
	if len(doc.Lines) != len(expectedLines) {
		t.Fatalf("Expected %d lines, got %d", len(expectedLines), len(doc.Lines))
	}

	for i, expected := range expectedLines {
		if doc.Lines[i] != expected {
			t.Errorf("Line %d: expected %q, got %q", i, expected, doc.Lines[i])
		}
	}
}

func TestDocumentManager_UpdateDocument(t *testing.T) {
	dm := NewDocumentManager()

	uri := protocol.DocumentURI("file:///tmp/test.yml")

	// Open initial document
	dm.OpenDocument(uri, 1, "steps:")

	// Update with new content
	newContent := "steps:\n  - label: \"updated\"\n    command: \"echo hello\""
	dm.UpdateDocument(uri, 2, newContent)

	doc, exists := dm.GetDocument(uri)
	if !exists {
		t.Fatal("Document should exist after update")
	}

	if doc.Version != 2 {
		t.Errorf("Expected version 2, got %d", doc.Version)
	}

	if doc.Content != newContent {
		t.Errorf("Expected updated content, got %q", doc.Content)
	}

	expectedLines := []string{"steps:", "  - label: \"updated\"", "    command: \"echo hello\""}
	if len(doc.Lines) != len(expectedLines) {
		t.Fatalf("Expected %d lines, got %d", len(expectedLines), len(doc.Lines))
	}
}

func TestDocumentManager_UpdateNonExistentDocument(t *testing.T) {
	dm := NewDocumentManager()

	uri := protocol.DocumentURI("file:///tmp/nonexistent.yml")
	content := "steps:\n  - label: \"test\""

	// Update document that doesn't exist - should create it
	dm.UpdateDocument(uri, 1, content)

	doc, exists := dm.GetDocument(uri)
	if !exists {
		t.Fatal("Document should be created when updating non-existent document")
	}

	if doc.Content != content {
		t.Errorf("Expected content %q, got %q", content, doc.Content)
	}
}

func TestDocumentManager_CloseDocument(t *testing.T) {
	dm := NewDocumentManager()

	uri := protocol.DocumentURI("file:///tmp/test.yml")
	dm.OpenDocument(uri, 1, "steps:")

	// Verify document exists
	_, exists := dm.GetDocument(uri)
	if !exists {
		t.Fatal("Document should exist before closing")
	}

	// Close document
	dm.CloseDocument(uri)

	// Verify document is removed
	_, exists = dm.GetDocument(uri)
	if exists {
		t.Error("Document should not exist after closing")
	}
}

func TestDocumentManager_GetContentAtPosition(t *testing.T) {
	dm := NewDocumentManager()

	uri := protocol.DocumentURI("file:///tmp/test.yml")
	content := `steps:
  - label: "test"
    command: "echo hello"
    plugins:
      - docker#v5.13.0:
          image: "node:18"`

	dm.OpenDocument(uri, 1, content)

	// Test getting context at plugins line
	position := protocol.Position{Line: 4, Character: 8} // "      - docker#v5.13.0:"

	posCtx, err := dm.GetContentAtPosition(uri, position)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if posCtx == nil {
		t.Fatal("Expected position context, got nil")
	}

	if posCtx.URI != uri {
		t.Errorf("Expected URI %s, got %s", uri, posCtx.URI)
	}

	if posCtx.Position.Line != 4 {
		t.Errorf("Expected line 4, got %d", posCtx.Position.Line)
	}

	if posCtx.CharIndex != 8 {
		t.Errorf("Expected char index 8, got %d", posCtx.CharIndex)
	}

	expectedCurrentLine := "      - docker#v5.13.0:"
	if posCtx.CurrentLine != expectedCurrentLine {
		t.Errorf("Expected current line %q, got %q", expectedCurrentLine, posCtx.CurrentLine)
	}

	// Context lines should include lines up to current position
	expectedContextLines := 5 // Lines 0-4
	if len(posCtx.ContextLines) != expectedContextLines {
		t.Errorf("Expected %d context lines, got %d", expectedContextLines, len(posCtx.ContextLines))
	}

	if posCtx.FullContent != content {
		t.Error("Full content should match original content")
	}
}

func TestDocumentManager_GetContentAtPosition_OutOfBounds(t *testing.T) {
	dm := NewDocumentManager()

	uri := protocol.DocumentURI("file:///tmp/test.yml")
	content := "steps:\n  - label: \"test\""

	dm.OpenDocument(uri, 1, content)

	// Test position beyond document end
	position := protocol.Position{Line: 10, Character: 0}

	posCtx, err := dm.GetContentAtPosition(uri, position)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if posCtx != nil {
		t.Error("Expected nil context for out-of-bounds position")
	}
}

func TestDocumentManager_GetContentAtPosition_NonExistentDocument(t *testing.T) {
	dm := NewDocumentManager()

	uri := protocol.DocumentURI("file:///tmp/nonexistent.yml")
	position := protocol.Position{Line: 0, Character: 0}

	posCtx, err := dm.GetContentAtPosition(uri, position)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if posCtx != nil {
		t.Error("Expected nil context for non-existent document")
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{
			input:    "",
			expected: []string{},
		},
		{
			input:    "single line",
			expected: []string{"single line"},
		},
		{
			input:    "line1\nline2",
			expected: []string{"line1", "line2"},
		},
		{
			input:    "line1\nline2\n",
			expected: []string{"line1", "line2"}, // Implementation doesn't add empty line at end
		},
		{
			input:    "line1\r\nline2", // Windows line endings
			expected: []string{"line1", "line2"},
		},
		{
			input:    "line1\n\nline3", // Empty line in middle
			expected: []string{"line1", "", "line3"},
		},
		{
			input:    "\n",         // Just newline
			expected: []string{""}, // Implementation returns single empty string
		},
	}

	for _, test := range tests {
		result := splitLines(test.input)

		if len(result) != len(test.expected) {
			t.Errorf("splitLines(%q): expected %d lines, got %d", test.input, len(test.expected), len(result))
			continue
		}

		for i, expected := range test.expected {
			if result[i] != expected {
				t.Errorf("splitLines(%q): line %d expected %q, got %q", test.input, i, expected, result[i])
			}
		}
	}
}

func TestDocumentManager_ConcurrentAccess(t *testing.T) {
	dm := NewDocumentManager()

	uri := protocol.DocumentURI("file:///tmp/concurrent.yml")

	// Test concurrent access
	done := make(chan bool, 2)

	// Goroutine 1: Open and update document
	go func() {
		for i := 0; i < 100; i++ {
			content := fmt.Sprintf("steps:\n  - label: \"test%d\"", i)
			if i == 0 {
				dm.OpenDocument(uri, int32(i+1), content)
			} else {
				dm.UpdateDocument(uri, int32(i+1), content)
			}
		}
		done <- true
	}()

	// Goroutine 2: Read document
	go func() {
		for i := 0; i < 100; i++ {
			dm.GetDocument(uri)
			position := protocol.Position{Line: 0, Character: 0}
			_, _ = dm.GetContentAtPosition(uri, position)
		}
		done <- true
	}()

	// Wait for both goroutines
	<-done
	<-done

	// Verify final state
	doc, exists := dm.GetDocument(uri)
	if !exists {
		t.Error("Document should exist after concurrent access")
	}

	if doc.Version != 100 {
		t.Errorf("Expected final version 100, got %d", doc.Version)
	}
}
