package context

import (
	"strings"
	"testing"

	"go.lsp.dev/protocol"
)

func TestAnalyzeContext_TopLevel(t *testing.T) {
	analyzer := NewAnalyzer()

	content := `steps:
  - label: "test"
env:`

	posCtx := &PositionContext{
		Position:     protocol.Position{Line: 2, Character: 4},
		CurrentLine:  "env:",
		CharIndex:    4,
		ContextLines: []string{"steps:", "  - label: \"test\"", "env:"},
		FullContent:  content,
	}

	result := analyzer.AnalyzeContext(posCtx)

	if result.Type != ContextTopLevel {
		t.Errorf("Expected ContextTopLevel, got %v", result.Type)
	}

	if !result.IsAtTopLevel() {
		t.Error("Expected IsAtTopLevel() to return true")
	}
}

func TestAnalyzeContext_StepLevel(t *testing.T) {
	analyzer := NewAnalyzer()

	content := `steps:
  - label: "test"
    command:`

	posCtx := &PositionContext{
		Position:     protocol.Position{Line: 2, Character: 12},
		CurrentLine:  "    command:",
		CharIndex:    12,
		ContextLines: []string{"steps:", "  - label: \"test\"", "    command:"},
		FullContent:  content,
	}

	result := analyzer.AnalyzeContext(posCtx)

	if result.Type != ContextStep {
		t.Errorf("Expected ContextStep, got %v", result.Type)
	}

	if !result.IsInStepContext() {
		t.Error("Expected IsInStepContext() to return true")
	}
}

func TestAnalyzeContext_PluginsArray(t *testing.T) {
	analyzer := NewAnalyzer()

	content := `steps:
  - label: "test"
    command: "echo hello"
    plugins:
      - `

	posCtx := &PositionContext{
		Position:     protocol.Position{Line: 4, Character: 8},
		CurrentLine:  "      - ",
		CharIndex:    8,
		ContextLines: []string{"steps:", "  - label: \"test\"", "    command: \"echo hello\"", "    plugins:", "      - "},
		FullContent:  content,
	}

	result := analyzer.AnalyzeContext(posCtx)

	if result.Type != ContextPlugins {
		t.Errorf("Expected ContextPlugins, got %v", result.Type)
	}

	if !result.IsInPluginsArray() {
		t.Error("Expected IsInPluginsArray() to return true")
	}

	if result.ArrayContext != "plugins" {
		t.Errorf("Expected ArrayContext 'plugins', got '%s'", result.ArrayContext)
	}
}

func TestAnalyzeContext_PluginConfig(t *testing.T) {
	analyzer := NewAnalyzer()

	content := `steps:
  - label: "test"
    plugins:
      - docker#v5.13.0:
          image:`

	posCtx := &PositionContext{
		Position:     protocol.Position{Line: 4, Character: 16},
		CurrentLine:  "          image:",
		CharIndex:    16,
		ContextLines: []string{"steps:", "  - label: \"test\"", "    plugins:", "      - docker#v5.13.0:", "          image:"},
		FullContent:  content,
	}

	result := analyzer.AnalyzeContext(posCtx)

	if result.Type != ContextPluginConfig {
		t.Errorf("Expected ContextPluginConfig, got %v", result.Type)
	}
}

func TestAnalyzeContext_ComplexNesting(t *testing.T) {
	analyzer := NewAnalyzer()

	content := `env:
  DEBUG: "true"
steps:
  - label: "Build"
    command: "make build"
    agents:
      queue: "default"
  - label: "Test with Docker"
    command: "make test"
    plugins:
      - docker#v5.13.0:
          image: "node:18"
          volumes:
            - ".:/app"
      - cache#v2.4.10:
          key: "cache-v1"
    retry:
      automatic: true`

	// Test inside docker plugin config
	posCtx := &PositionContext{
		Position:     protocol.Position{Line: 13, Character: 18},
		CurrentLine:  "            - \".:/app\"",
		CharIndex:    18,
		ContextLines: strings.Split(content, "\n")[:14], // Up to line 13
		FullContent:  content,
	}

	result := analyzer.AnalyzeContext(posCtx)

	if result.Type != ContextPluginConfig {
		t.Errorf("Expected ContextPluginConfig for docker volumes, got %v", result.Type)
	}

	// Test inside plugins array (cache plugin)
	posCtx2 := &PositionContext{
		Position:     protocol.Position{Line: 14, Character: 8},
		CurrentLine:  "      - cache#v2.4.10:",
		CharIndex:    8,
		ContextLines: strings.Split(content, "\n")[:15], // Up to line 14
		FullContent:  content,
	}

	result2 := analyzer.AnalyzeContext(posCtx2)

	if result2.Type != ContextPlugins {
		t.Errorf("Expected ContextPlugins for cache plugin, got %v", result2.Type)
	}
}

func TestAnalyzeContext_IndentationEdgeCases(t *testing.T) {
	analyzer := NewAnalyzer()

	// Test mixed spaces and content
	content := `steps:
  - label: "test"
    
    command: "echo"    # Comment
    plugins:
      # Plugin comment
      - docker#v5.13.0:
          image: "test"
          
          volumes: []`

	posCtx := &PositionContext{
		Position:     protocol.Position{Line: 6, Character: 8},
		CurrentLine:  "      - docker#v5.13.0:",
		CharIndex:    8,
		ContextLines: strings.Split(content, "\n")[:7], // Up to line 6
		FullContent:  content,
	}

	result := analyzer.AnalyzeContext(posCtx)

	if result.Type != ContextPlugins {
		t.Errorf("Expected ContextPlugins with mixed whitespace, got %v", result.Type)
	}
}

func TestGetIndentLevel(t *testing.T) {
	tests := []struct {
		line     string
		expected int
	}{
		{"steps:", 0},
		{"  - label: test", 2},
		{"    command: echo", 4},
		{"      - docker#v5.13.0:", 6},
		{"        image: node", 8},
		{"\t\timage: node", 4}, // 2 tabs = 4 spaces
		{"", 0},
		{"no-indent", 0},
	}

	for _, test := range tests {
		result := getIndentLevel(test.line)
		if result != test.expected {
			t.Errorf("getIndentLevel(%q) = %d, expected %d", test.line, result, test.expected)
		}
	}
}

func TestParseKeyFromLine(t *testing.T) {
	tests := []struct {
		line     string
		indent   int
		expected *KeyInfo
	}{
		{"steps:", 0, &KeyInfo{Key: "steps", IndentLevel: 0, IsArray: true, HasValue: false}},
		{"  plugins:", 2, &KeyInfo{Key: "plugins", IndentLevel: 2, IsArray: true, HasValue: false}},
		{"    command: echo hello", 4, &KeyInfo{Key: "command", IndentLevel: 4, IsArray: false, HasValue: true}},
		{"    retry: []", 4, &KeyInfo{Key: "retry", IndentLevel: 4, IsArray: true, HasValue: false}},
		{"  - docker#v5.13.0:", 2, &KeyInfo{Key: "docker#v5.13.0", IndentLevel: 2, IsArray: true, HasValue: false}}, // Array items with keys are parsed
		{"", 0, nil}, // Empty lines return nil
	}

	for _, test := range tests {
		result := parseKeyFromLine(test.line, test.indent)

		if test.expected == nil {
			if result != nil {
				t.Errorf("parseKeyFromLine(%q, %d) expected nil, got %v", test.line, test.indent, result)
			}
			continue
		}

		if result == nil {
			t.Errorf("parseKeyFromLine(%q, %d) expected %v, got nil", test.line, test.indent, test.expected)
			continue
		}

		if result.Key != test.expected.Key {
			t.Errorf("parseKeyFromLine(%q, %d) key = %q, expected %q", test.line, test.indent, result.Key, test.expected.Key)
		}

		if result.IsArray != test.expected.IsArray {
			t.Errorf("parseKeyFromLine(%q, %d) IsArray = %v, expected %v", test.line, test.indent, result.IsArray, test.expected.IsArray)
		}

		if result.HasValue != test.expected.HasValue {
			t.Errorf("parseKeyFromLine(%q, %d) HasValue = %v, expected %v", test.line, test.indent, result.HasValue, test.expected.HasValue)
		}
	}
}
