package lsp

import (
	"sync"

	"go.lsp.dev/protocol"

	"github.com/mcncl/buildkite-ls/internal/context"
)

// DocumentManager handles document content caching and state management
type DocumentManager struct {
	mu        sync.RWMutex
	documents map[protocol.DocumentURI]*Document
}

// Document represents a cached document with its content and metadata
type Document struct {
	URI     protocol.DocumentURI
	Version int32
	Content string
	Lines   []string
}

// NewDocumentManager creates a new document manager
func NewDocumentManager() *DocumentManager {
	return &DocumentManager{
		documents: make(map[protocol.DocumentURI]*Document),
	}
}

// OpenDocument stores a newly opened document
func (dm *DocumentManager) OpenDocument(uri protocol.DocumentURI, version int32, content string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dm.documents[uri] = &Document{
		URI:     uri,
		Version: version,
		Content: content,
		Lines:   splitLines(content),
	}
}

// UpdateDocument updates an existing document with new content
func (dm *DocumentManager) UpdateDocument(uri protocol.DocumentURI, version int32, content string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if doc, exists := dm.documents[uri]; exists {
		doc.Version = version
		doc.Content = content
		doc.Lines = splitLines(content)
	} else {
		// Document doesn't exist, create it
		dm.documents[uri] = &Document{
			URI:     uri,
			Version: version,
			Content: content,
			Lines:   splitLines(content),
		}
	}
}

// CloseDocument removes a document from the cache
func (dm *DocumentManager) CloseDocument(uri protocol.DocumentURI) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	delete(dm.documents, uri)
}

// GetDocument retrieves a document by URI
func (dm *DocumentManager) GetDocument(uri protocol.DocumentURI) (*Document, bool) {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	doc, exists := dm.documents[uri]
	return doc, exists
}

// GetContentAtPosition returns the content and line information at a specific position
func (dm *DocumentManager) GetContentAtPosition(uri protocol.DocumentURI, position protocol.Position) (*context.PositionContext, error) {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	doc, exists := dm.documents[uri]
	if !exists {
		return nil, nil // Document not found
	}

	lineIndex := int(position.Line)
	charIndex := int(position.Character)

	if lineIndex >= len(doc.Lines) {
		return nil, nil // Position out of bounds
	}

	currentLine := doc.Lines[lineIndex]

	// Get surrounding context for analysis
	contextLines := make([]string, 0, len(doc.Lines))
	for i := 0; i < len(doc.Lines) && i <= lineIndex; i++ {
		contextLines = append(contextLines, doc.Lines[i])
	}

	return &context.PositionContext{
		URI:          uri,
		Position:     position,
		CurrentLine:  currentLine,
		CharIndex:    charIndex,
		ContextLines: contextLines,
		FullContent:  doc.Content,
	}, nil
}

// splitLines splits content into lines, preserving empty lines
func splitLines(content string) []string {
	if content == "" {
		return []string{}
	}

	lines := make([]string, 0)
	current := ""

	for _, char := range content {
		if char == '\n' {
			lines = append(lines, current)
			current = ""
		} else if char != '\r' { // Skip carriage returns
			current += string(char)
		}
	}

	// Add the last line if it doesn't end with newline
	if current != "" || len(lines) == 0 {
		lines = append(lines, current)
	}

	return lines
}
