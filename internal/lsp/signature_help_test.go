package lsp

import (
	"context"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
)

func TestServer_SignatureHelp(t *testing.T) {
	server := newTestServer()
	ctx := context.Background()
	uri := "file:///test/.buildkite/pipeline.yml"

	tests := []struct {
		name     string
		content  string
		line     uint32
		char     uint32
		expected bool   // whether we expect signature help
		contains string // what the signature should contain
	}{
		{
			name: "command step context",
			content: `steps:
  - label: "Build"
    command: "make build"
    `,
			line:     2,
			char:     20,
			expected: true,
			contains: "Command Step",
		},
		{
			name: "wait step context",
			content: `steps:
  - wait: ~
    continue_on_failure: true
    `,
			line:     2,
			char:     20,
			expected: true,
			contains: "Wait Step",
		},
		{
			name: "block step context",
			content: `steps:
  - block: "Deploy?"
    prompt: "Ready?"
    `,
			line:     2,
			char:     15,
			expected: true,
			contains: "Block Step",
		},
		{
			name: "trigger step context",
			content: `steps:
  - trigger: "deploy"
    async: true
    `,
			line:     2,
			char:     10,
			expected: true,
			contains: "Trigger Step",
		},
		{
			name: "input step context",
			content: `steps:
  - input: "Version?"
    fields: []
    `,
			line:     2,
			char:     10,
			expected: true,
			contains: "Input Step",
		},
		{
			name: "non-buildkite file",
			content: `version: "3"
services:
  app:
    image: nginx
    `,
			line:     2,
			char:     10,
			expected: false,
			contains: "",
		},
		{
			name: "top-level context",
			content: `env:
  NODE_ENV: production
agents:
  queue: default
    `,
			line:     3,
			char:     10,
			expected: false,
			contains: "",
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

			// Request signature help
			sigParams := &protocol.SignatureHelpParams{
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

			result, err := server.SignatureHelp(ctx, sigParams)
			if err != nil {
				t.Fatalf("SignatureHelp failed: %v", err)
			}

			// Debug logging for failing cases
			if tt.expected && result == nil {
				t.Logf("Content:\n%s", tt.content)
				t.Logf("Position: line %d, char %d", tt.line, tt.char)

				// Test context detection manually
				lines := strings.Split(tt.content, "\n")
				t.Logf("Lines:")
				for i, line := range lines {
					marker := ""
					if i == int(tt.line) {
						marker = " <-- CURSOR"
					}
					t.Logf("  %d: '%s'%s", i, line, marker)
				}
			}

			// Check expectations
			if tt.expected {
				if result == nil {
					t.Error("Expected signature help but got nil")
					return
				}
				if len(result.Signatures) == 0 {
					t.Error("Expected signatures but got empty array")
					return
				}

				found := false
				for _, sig := range result.Signatures {
					if tt.contains != "" && sig.Label == tt.contains {
						found = true
						break
					}
				}

				if tt.contains != "" && !found {
					t.Errorf("Expected signature containing '%s', got signatures: %v", tt.contains, getSignatureLabels(result.Signatures))
				}
			} else {
				if result != nil && len(result.Signatures) > 0 {
					t.Errorf("Expected no signature help, but got %d signatures", len(result.Signatures))
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

func TestServer_SignatureHelpHelpers(t *testing.T) {
	server := newTestServer()

	t.Run("getIndentLevel", func(t *testing.T) {
		tests := []struct {
			line     string
			expected int
		}{
			{"no indent", 0},
			{"  two spaces", 2},
			{"    four spaces", 4},
			{"\ttab", 4},
			{"\t  tab and spaces", 6},
			{"", 0},
		}

		for _, tt := range tests {
			result := server.getIndentLevel(tt.line)
			if result != tt.expected {
				t.Errorf("getIndentLevel(%q) = %d, want %d", tt.line, result, tt.expected)
			}
		}
	})
}

func TestServer_StepTypeSignatures(t *testing.T) {
	server := newTestServer()

	tests := []struct {
		stepType       string
		hasSignature   bool
		expectedParams int
	}{
		{"command", true, 5},
		{"wait", true, 3},
		{"block", true, 3},
		{"input", true, 2},
		{"trigger", true, 3},
		{"group", false, 0},
		{"unknown", false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.stepType, func(t *testing.T) {
			sig := server.getStepTypeSignature(tt.stepType)

			if tt.hasSignature {
				if sig == nil {
					t.Errorf("Expected signature for %s, got nil", tt.stepType)
					return
				}
				if len(sig.Parameters) != tt.expectedParams {
					t.Errorf("Expected %d parameters for %s, got %d", tt.expectedParams, tt.stepType, len(sig.Parameters))
				}
			} else {
				if sig != nil {
					t.Errorf("Expected no signature for %s, got %+v", tt.stepType, sig)
				}
			}
		})
	}
}

// Helper function to extract signature labels for debugging
func getSignatureLabels(signatures []protocol.SignatureInformation) []string {
	labels := make([]string, len(signatures))
	for i, sig := range signatures {
		labels[i] = sig.Label
	}
	return labels
}
