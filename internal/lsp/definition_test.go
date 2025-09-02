package lsp

import (
	"context"
	"strings"
	"testing"

	"go.lsp.dev/protocol"

	bkcontext "github.com/mcncl/buildkite-ls/internal/context"
)

func TestServer_Definition(t *testing.T) {
	server := newTestServer()
	ctx := context.Background()
	uri := "file:///test/.buildkite/pipeline.yml"

	tests := []struct {
		name         string
		content      string
		line         uint32
		char         uint32
		expectedLocs int
		shouldFind   bool
		targetLine   uint32 // Expected line of definition
	}{
		{
			name: "step reference in depends_on",
			content: `steps:
  - label: "Build"
    key: "build-step"
    command: "make build"
    
  - label: "Test" 
    key: "test-step"
    command: "make test"
    depends_on:
      - "build-step"
    `,
			line:         9,
			char:         10, // Position on "build-step" in depends_on
			expectedLocs: 1,
			shouldFind:   true,
			targetLine:   1, // Should point to the build step definition
		},
		{
			name: "step reference with generated key from label",
			content: `steps:
  - label: "Build App"
    command: "make build"
    
  - label: "Test App"
    command: "make test"
    depends_on:
      - "build-app"
    `,
			line:         7,
			char:         10, // Position on "build-app"
			expectedLocs: 1,
			shouldFind:   true,
			targetLine:   1, // Should point to the "Build App" step
		},
		{
			name: "non-existent step reference",
			content: `steps:
  - label: "Build"
    key: "build-step"
    command: "make build"
    
  - label: "Test"
    depends_on:
      - "non-existent-step"
    `,
			line:         6,
			char:         10,
			expectedLocs: 0,
			shouldFind:   false,
		},
		{
			name: "plugin reference",
			content: `steps:
  - label: "Build"
    command: "make build"
    plugins:
      - docker-compose#v4.7.0:
          run: app
    `,
			line:         5,
			char:         10, // Position on "docker-compose"
			expectedLocs: 0,  // Plugin definitions not implemented yet
			shouldFind:   false,
		},
		{
			name: "not in step reference context",
			content: `env:
  NODE_ENV: production
  
steps:
  - label: "random-text"
    command: "echo hello"
    `,
			line:         4,
			char:         15, // Position on "random-text" in label
			expectedLocs: 0,
			shouldFind:   false,
		},
		{
			name: "non-buildkite file",
			content: `version: "3"
services:
  app:
    image: nginx
    depends_on:
      - db
    `,
			line:         5,
			char:         10,
			expectedLocs: 0,
			shouldFind:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Determine URI based on test
			testURI := uri
			if tt.name == "non-buildkite file" {
				testURI = "file:///test/docker-compose.yml"
			}

			// Open document
			openParams := &protocol.DidOpenTextDocumentParams{
				TextDocument: protocol.TextDocumentItem{
					URI:        protocol.DocumentURI(testURI),
					LanguageID: "yaml",
					Version:    1,
					Text:       tt.content,
				},
			}

			err := server.DidOpen(ctx, openParams)
			if err != nil {
				t.Fatalf("DidOpen failed: %v", err)
			}

			// Request definition
			defParams := &protocol.DefinitionParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{
						URI: protocol.DocumentURI(testURI),
					},
					Position: protocol.Position{
						Line:      tt.line,
						Character: tt.char,
					},
				},
			}

			result, err := server.Definition(ctx, defParams)
			if err != nil {
				t.Fatalf("Definition failed: %v", err)
			}

			// Debug logging for failing cases
			if tt.shouldFind && len(result) != tt.expectedLocs {
				t.Logf("Content:\n%s", tt.content)
				t.Logf("Position: line %d, char %d", tt.line, tt.char)

				lines := strings.Split(tt.content, "\n")
				if int(tt.line) < len(lines) {
					currentLine := lines[tt.line]
					t.Logf("Current line: '%s'", currentLine)
					if int(tt.char) < len(currentLine) {
						t.Logf("Character at position: '%c'", currentLine[tt.char])
					}
				}
			}

			// Check expectations
			if len(result) != tt.expectedLocs {
				t.Errorf("Expected %d definition locations, got %d", tt.expectedLocs, len(result))
			}

			if tt.shouldFind && len(result) > 0 {
				// Check that we found a definition at the expected line
				found := false
				for _, loc := range result {
					if loc.Range.Start.Line == tt.targetLine {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected definition at line %d, but definitions were at lines: %v",
						tt.targetLine, getLocationLines(result))
				}
			}

			// Clean up
			closeParams := &protocol.DidCloseTextDocumentParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentURI(testURI),
				},
			}
			err = server.DidClose(ctx, closeParams)
			if err != nil {
				t.Errorf("DidClose failed: %v", err)
			}
		})
	}
}

func TestServer_DefinitionHelpers(t *testing.T) {
	server := newTestServer()

	t.Run("getWordAtPosition", func(t *testing.T) {
		tests := []struct {
			line     string
			charIdx  int
			expected string
		}{
			{"  - \"build-step\"", 6, "build-step"},
			{"  - 'test-step'", 6, "test-step"},
			{"  - build-step", 6, "build-step"},
			{"key: \"my-step\"", 6, "my-step"},
			{"depends_on:", 5, "depends_on"},
			{"", 0, ""},
			{"hello world", 6, "world"},
			{"hyphen-word", 2, "hyphen-word"},
			{"under_score", 2, "under_score"},
		}

		for _, tt := range tests {
			ctx := &bkcontext.PositionContext{
				CurrentLine: tt.line,
				CharIndex:   tt.charIdx,
			}
			result := server.getWordAtPosition(ctx)
			if result != tt.expected {
				t.Errorf("getWordAtPosition('%s', %d) = '%s', want '%s'",
					tt.line, tt.charIdx, result, tt.expected)
			}
		}
	})

	t.Run("findStepKey", func(t *testing.T) {
		lines := []string{
			"steps:",
			"  - label: \"Build App\"",
			"    key: \"explicit-key\"",
			"    command: \"make build\"",
			"  ",
			"  - label: \"Test App\"",
			"    command: \"make test\"",
			"    # no explicit key",
		}

		// Test explicit key
		key1 := server.findStepKey(lines, 1)
		if key1 != "explicit-key" {
			t.Errorf("Expected explicit-key, got '%s'", key1)
		}

		// Test generated key from label
		key2 := server.findStepKey(lines, 5)
		if key2 != "test-app" {
			t.Errorf("Expected test-app, got '%s'", key2)
		}
	})
}

// Helper function to extract line numbers from locations for debugging
func getLocationLines(locations []protocol.Location) []uint32 {
	lines := make([]uint32, len(locations))
	for i, loc := range locations {
		lines[i] = loc.Range.Start.Line
	}
	return lines
}
