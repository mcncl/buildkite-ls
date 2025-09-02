package lsp

import (
	"context"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
)

func TestServer_SemanticTokensFull(t *testing.T) {
	server := newTestServer()
	ctx := context.Background()
	uri := "file:///test/.buildkite/pipeline.yml"

	tests := []struct {
		name                    string
		content                 string
		expectedTokens          int
		shouldContainTokenTypes []string
	}{
		{
			name: "basic pipeline with steps",
			content: `steps:
  - label: "Build"
    command: "make build"
    key: "build-step"
    `,
			expectedTokens:          8, // steps, :, -, label, :, "Build", command, :, "make build", key, :, "build-step"
			shouldContainTokenTypes: []string{"keyword", "string", "namespace", "property", "operator"},
		},
		{
			name: "wait step",
			content: `steps:
  - wait: "Deploy ready?"
    continue_on_failure: true
    `,
			expectedTokens:          6,
			shouldContainTokenTypes: []string{"keyword", "string", "property"},
		},
		{
			name: "step with plugins",
			content: `steps:
  - label: "Test"
    command: "npm test"
    plugins:
      - docker#v5.13.0:
          image: "node:18"
    `,
			expectedTokens:          10,
			shouldContainTokenTypes: []string{"keyword", "string", "property", "function"},
		},
		{
			name: "environment variables",
			content: `env:
  NODE_ENV: production
  DEBUG: "false"
steps:
  - command: "echo hello"
    `,
			expectedTokens:          9,
			shouldContainTokenTypes: []string{"variable", "property", "keyword", "string"},
		},
		{
			name: "comments and empty lines",
			content: `# This is a comment
steps:
  # Another comment
  - command: "build"
    # Inline comment after step
    `,
			expectedTokens:          5,
			shouldContainTokenTypes: []string{"comment", "keyword", "string"},
		},
		{
			name: "block step with input",
			content: `steps:
  - block: "Deploy to production?"
    prompt: "Are you sure?"
  - input: "Version"
    fields:
      - text: "version"
    `,
			expectedTokens:          10,
			shouldContainTokenTypes: []string{"keyword", "string", "property"},
		},
		{
			name: "trigger step",
			content: `steps:
  - trigger: "deploy-pipeline"
    async: true
    build:
      branch: "main"
    `,
			expectedTokens:          8,
			shouldContainTokenTypes: []string{"keyword", "string", "property"},
		},
		{
			name: "non-buildkite file",
			content: `version: "3"
services:
  app:
    image: nginx
    `,
			expectedTokens:          0,
			shouldContainTokenTypes: []string{},
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

			// Request semantic tokens
			semanticParams := &protocol.SemanticTokensParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentURI(testURI),
				},
			}

			result, err := server.SemanticTokensFull(ctx, semanticParams)
			if err != nil {
				t.Fatalf("SemanticTokensFull failed: %v", err)
			}

			// Check token count (each token is 5 uint32s)
			actualTokenCount := len(result.Data) / 5
			if actualTokenCount < tt.expectedTokens {
				t.Errorf("Expected at least %d tokens, got %d", tt.expectedTokens, actualTokenCount)
				t.Logf("Token data length: %d", len(result.Data))
				t.Logf("Content:\n%s", tt.content)
			}

			// Verify we have semantic tokens for non-empty files
			if tt.expectedTokens > 0 && len(result.Data) == 0 {
				t.Error("Expected semantic tokens but got empty array")
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

func TestServer_SemanticTokensRange(t *testing.T) {
	server := newTestServer()
	ctx := context.Background()
	uri := "file:///test/.buildkite/pipeline.yml"

	content := `env:
  NODE_ENV: production
steps:
  - label: "Build"
    command: "make build"
  - wait: "Continue?"
    `

	// Open document
	openParams := &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        protocol.DocumentURI(uri),
			LanguageID: "yaml",
			Version:    1,
			Text:       content,
		},
	}

	err := server.DidOpen(ctx, openParams)
	if err != nil {
		t.Fatalf("DidOpen failed: %v", err)
	}

	tests := []struct {
		name      string
		startLine uint32
		endLine   uint32
		minTokens int
	}{
		{
			name:      "env section only",
			startLine: 0,
			endLine:   1,
			minTokens: 2, // env:, NODE_ENV:, production
		},
		{
			name:      "first step only",
			startLine: 3,
			endLine:   4,
			minTokens: 4, // -, label:, "Build", command:, "make build"
		},
		{
			name:      "second step only",
			startLine: 5,
			endLine:   5,
			minTokens: 2, // -, wait:, "Continue?"
		},
		{
			name:      "full document",
			startLine: 0,
			endLine:   5,
			minTokens: 8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Request semantic tokens for range
			rangeParams := &protocol.SemanticTokensRangeParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentURI(uri),
				},
				Range: protocol.Range{
					Start: protocol.Position{Line: tt.startLine, Character: 0},
					End:   protocol.Position{Line: tt.endLine, Character: 999},
				},
			}

			result, err := server.SemanticTokensRange(ctx, rangeParams)
			if err != nil {
				t.Fatalf("SemanticTokensRange failed: %v", err)
			}

			// Check token count
			actualTokenCount := len(result.Data) / 5
			if actualTokenCount < tt.minTokens {
				t.Errorf("Expected at least %d tokens, got %d", tt.minTokens, actualTokenCount)
				t.Logf("Range: lines %d-%d", tt.startLine, tt.endLine)
				t.Logf("Token data length: %d", len(result.Data))
			}
		})
	}

	// Clean up
	closeParams := &protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: protocol.DocumentURI(uri),
		},
	}
	err = server.DidClose(ctx, closeParams)
	if err != nil {
		t.Errorf("DidClose failed: %v", err)
	}
}

func TestServer_SemanticTokensHelpers(t *testing.T) {
	server := newTestServer()

	t.Run("getTokenTypeIndex", func(t *testing.T) {
		tests := []struct {
			tokenType string
			expected  int
		}{
			{"keyword", 0},
			{"string", 1},
			{"property", 2},
			{"variable", 3},
			{"function", 4},
			{"namespace", 5},
			{"operator", 6},
			{"comment", 7},
			{"unknown", 0}, // should default to keyword
		}

		for _, tt := range tests {
			result := server.getTokenTypeIndex(tt.tokenType)
			if result != tt.expected {
				t.Errorf("getTokenTypeIndex(%q) = %d, want %d", tt.tokenType, result, tt.expected)
			}
		}
	})

	t.Run("getTokenModifierBits", func(t *testing.T) {
		tests := []struct {
			modifiers []string
			expected  int
		}{
			{[]string{}, 0},
			{[]string{"definition"}, 1},
			{[]string{"readonly"}, 2},
			{[]string{"deprecated"}, 4},
			{[]string{"definition", "readonly"}, 3},
			{[]string{"definition", "readonly", "deprecated"}, 7},
			{[]string{"unknown"}, 0},
		}

		for _, tt := range tests {
			result := server.getTokenModifierBits(tt.modifiers)
			if result != tt.expected {
				t.Errorf("getTokenModifierBits(%v) = %d, want %d", tt.modifiers, result, tt.expected)
			}
		}
	})

	t.Run("getKeyTokenType", func(t *testing.T) {
		tests := []struct {
			key      string
			inStep   bool
			expected string
		}{
			{"command", true, "keyword"},
			{"wait", true, "keyword"},
			{"block", true, "keyword"},
			{"label", true, "namespace"},
			{"key", true, "namespace"},
			{"env", false, "variable"},
			{"plugins", true, "function"},
			{"docker#v5.13.0", true, "function"},
			{"timeout_in_minutes", true, "property"},
			{"unknown_prop", true, "property"},
		}

		for _, tt := range tests {
			result := server.getKeyTokenType(tt.key, tt.inStep)
			if result != tt.expected {
				t.Errorf("getKeyTokenType(%q, %t) = %q, want %q", tt.key, tt.inStep, result, tt.expected)
			}
		}
	})

	t.Run("getValueTokenType", func(t *testing.T) {
		tests := []struct {
			key          string
			value        string
			inStep       bool
			expectedType string
		}{
			{"label", "\"Build App\"", true, "namespace"},
			{"key", "build-step", true, "namespace"},
			{"command", "\"make build\"", true, "string"},
			{"plugins", "docker#v5.13.0", true, "function"},
			{"timeout_in_minutes", "30", true, "keyword"},
			{"async", "true", true, "keyword"},
			{"wait", "null", true, "keyword"},
			{"continue_on_failure", "false", true, "keyword"},
			{"env", "production", false, "variable"},
			{"unknown", "some value", true, "string"},
		}

		for _, tt := range tests {
			resultType, _ := server.getValueTokenType(tt.key, tt.value, tt.inStep)
			if resultType != tt.expectedType {
				t.Errorf("getValueTokenType(%q, %q, %t) type = %q, want %q", tt.key, tt.value, tt.inStep, resultType, tt.expectedType)
			}
		}
	})
}

func TestServer_SemanticTokensContextTracking(t *testing.T) {
	server := newTestServer()

	// Test context tracking with a complex pipeline
	content := `env:
  NODE_ENV: production
agents:
  queue: default
steps:
  - label: "Build"
    command: "make build"
    key: "build-step"
    plugins:
      - docker#v5.13.0:
          image: "node:18"
  - wait: ~
  - block: "Deploy?"
    prompt: "Ready to deploy?"
  - trigger: "deploy"
    async: true
notify:
  - slack: "@here"
    `

	lines := strings.Split(content, "\n")
	tokens := server.generateSemanticTokens(lines)

	if len(tokens.Data) == 0 {
		t.Error("Expected semantic tokens for complex pipeline, got none")
	}

	// Verify we have a reasonable number of tokens
	tokenCount := len(tokens.Data) / 5
	if tokenCount < 15 {
		t.Errorf("Expected at least 15 tokens for complex pipeline, got %d", tokenCount)
	}
}
