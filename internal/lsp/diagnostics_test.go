package lsp

import (
	"testing"

	"go.lsp.dev/protocol"

	"github.com/mcncl/buildkite-ls/internal/parser"
)

func TestServer_EnhancedDiagnostics(t *testing.T) {
	server := newTestServer()

	tests := []struct {
		name                string
		content             string
		expectedDiagnostics []ExpectedDiagnostic
	}{
		{
			name: "missing steps",
			content: `env:
  NODE_ENV: production

agents:
  queue: "default"`,
			expectedDiagnostics: []ExpectedDiagnostic{
				{
					Code:     "missing-steps",
					Severity: protocol.DiagnosticSeverityError,
					Message:  "Pipeline must contain a 'steps' array",
				},
			},
		},
		{
			name: "invalid env format",
			content: `env: "not an object"
steps:
  - command: "test"`,
			expectedDiagnostics: []ExpectedDiagnostic{
				{
					Code:     "invalid-env",
					Severity: protocol.DiagnosticSeverityError,
					Message:  "Environment variables must be an object with string keys and values",
				},
				{
					Code:     "missing-label",
					Severity: protocol.DiagnosticSeverityInformation,
					Message:  "Consider adding a 'label' to make this step easier to identify in the UI",
				},
			},
		},
		{
			name: "step with no type",
			content: `steps:
  - label: "No Command"`,
			expectedDiagnostics: []ExpectedDiagnostic{
				{
					Code:     "missing-step-type",
					Severity: protocol.DiagnosticSeverityError,
					Message:  "Step 1 must specify a step type: command, wait, block, input, trigger, or group",
				},
			},
		},
		{
			name: "step with multiple types",
			content: `steps:
  - command: "test"
    wait: ~`,
			expectedDiagnostics: []ExpectedDiagnostic{
				{
					Code:     "multiple-step-types",
					Severity: protocol.DiagnosticSeverityError,
					Message:  "Step 1 has multiple step types - only one is allowed per step",
				},
				{
					Code:     "missing-label",
					Severity: protocol.DiagnosticSeverityInformation,
					Message:  "Consider adding a 'label' to make this step easier to identify in the UI",
				},
			},
		},
		{
			name: "empty command",
			content: `steps:
  - label: "Build"
    command: ""`,
			expectedDiagnostics: []ExpectedDiagnostic{
				{
					Code:     "empty-command",
					Severity: protocol.DiagnosticSeverityWarning,
					Message:  "Command should not be empty",
				},
			},
		},
		{
			name: "command step missing label",
			content: `steps:
  - command: "make build"`,
			expectedDiagnostics: []ExpectedDiagnostic{
				{
					Code:     "missing-label",
					Severity: protocol.DiagnosticSeverityInformation,
					Message:  "Consider adding a 'label' to make this step easier to identify in the UI",
				},
			},
		},
		{
			name: "invalid wait value",
			content: `steps:
  - wait: true`,
			expectedDiagnostics: []ExpectedDiagnostic{
				{
					Code:     "invalid-wait-value",
					Severity: protocol.DiagnosticSeverityError,
					Message:  "Wait value must be null, a string message, or a number of seconds, got bool",
				},
			},
		},
		{
			name: "empty block message",
			content: `steps:
  - block: ""`,
			expectedDiagnostics: []ExpectedDiagnostic{
				{
					Code:     "empty-block-message",
					Severity: protocol.DiagnosticSeverityError,
					Message:  "Block step must have a non-empty message",
				},
			},
		},
		{
			name: "empty trigger pipeline",
			content: `steps:
  - trigger: ""`,
			expectedDiagnostics: []ExpectedDiagnostic{
				{
					Code:     "empty-trigger-pipeline",
					Severity: protocol.DiagnosticSeverityError,
					Message:  "Trigger step must specify a pipeline slug",
				},
			},
		},
		{
			name: "empty input prompt",
			content: `steps:
  - input: ""`,
			expectedDiagnostics: []ExpectedDiagnostic{
				{
					Code:     "empty-input-prompt",
					Severity: protocol.DiagnosticSeverityError,
					Message:  "Input step must have a non-empty prompt message",
				},
			},
		},
		{
			name: "valid pipeline with multiple step types",
			content: `env:
  NODE_ENV: production

steps:
  - label: "Build"
    command: "make build"
  
  - wait: ~
  
  - label: "Test"
    command: "make test"
    
  - block: "Deploy to production?"
    
  - input: "Release version"
    fields:
      - text: "version"
        
  - trigger: "deploy-pipeline"`,
			expectedDiagnostics: []ExpectedDiagnostic{},
		},
		{
			name: "valid wait variations",
			content: `steps:
  - wait: ~
  - wait: "Waiting for deployment"
  - wait: 30`,
			expectedDiagnostics: []ExpectedDiagnostic{},
		},
		{
			name: "step with plugins but no command (should be info, not error)",
			content: `steps:
  - label: "Build with plugin"
    plugins:
      - docker#v5.13.0:
          image: "golang:1.19"`,
			expectedDiagnostics: []ExpectedDiagnostic{
				{
					Code:     "no-step-type-with-plugins",
					Severity: protocol.DiagnosticSeverityInformation,
					Message:  "Step 1 has no explicit step type, but plugins may provide command execution via hooks",
				},
			},
		},
		{
			name: "step with plugins and empty command (should be info, not warning)",
			content: `steps:
  - label: "Build with plugin"
    command: ""
    plugins:
      - docker#v5.13.0:
          image: "golang:1.19"`,
			expectedDiagnostics: []ExpectedDiagnostic{
				{
					Code:     "empty-command-with-plugins",
					Severity: protocol.DiagnosticSeverityInformation,
					Message:  "Command is empty, but plugins may provide command execution via hooks",
				},
			},
		},
		{
			name: "step with 'name' instead of 'label' (should suggest using 'label')",
			content: `steps:
  - name: "Build App"
    command: "make build"`,
			expectedDiagnostics: []ExpectedDiagnostic{
				{
					Code:     "use-label-not-name",
					Severity: protocol.DiagnosticSeverityInformation,
					Message:  "Use 'label' instead of 'name' - 'label' is the standard Buildkite field for step display names",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse content
			pipeline, err := parser.ParseYAML([]byte(tt.content))
			if err != nil {
				t.Fatalf("Failed to parse YAML: %v", err)
			}

			// Get diagnostics
			diagnostics := server.validatePlugins(pipeline)

			// Check expected diagnostics
			if len(tt.expectedDiagnostics) == 0 {
				if len(diagnostics) != 0 {
					t.Errorf("Expected no diagnostics, but got %d:", len(diagnostics))
					for i, d := range diagnostics {
						t.Errorf("  [%d] %s: %s", i, d.Code, d.Message)
					}
				}
				return
			}

			if len(diagnostics) != len(tt.expectedDiagnostics) {
				t.Errorf("Expected %d diagnostics, got %d:", len(tt.expectedDiagnostics), len(diagnostics))
				for i, d := range diagnostics {
					t.Errorf("  [%d] %s: %s", i, d.Code, d.Message)
				}
				return
			}

			// Check each diagnostic
			for i, expected := range tt.expectedDiagnostics {
				actual := diagnostics[i]

				if actual.Code != expected.Code {
					t.Errorf("Diagnostic %d: expected code %s, got %s", i, expected.Code, actual.Code)
				}

				if actual.Severity != expected.Severity {
					t.Errorf("Diagnostic %d: expected severity %v, got %v", i, expected.Severity, actual.Severity)
				}

				if actual.Message != expected.Message {
					t.Errorf("Diagnostic %d: expected message %q, got %q", i, expected.Message, actual.Message)
				}

				if actual.Source != "buildkite-ls" {
					t.Errorf("Diagnostic %d: expected source 'buildkite-ls', got %q", i, actual.Source)
				}
			}
		})
	}
}

func TestServer_DiagnosticsHelpers(t *testing.T) {
	server := newTestServer()

	t.Run("findLineForProperty", func(t *testing.T) {
		lines := []string{
			"env:",
			"  NODE_ENV: production",
			"",
			"steps:",
			"  - command: test",
		}

		// Test finding existing property
		line := server.findLineForProperty("env", lines)
		if line != 0 {
			t.Errorf("Expected line 0 for 'env', got %d", line)
		}

		line = server.findLineForProperty("steps", lines)
		if line != 3 {
			t.Errorf("Expected line 3 for 'steps', got %d", line)
		}

		// Test non-existent property
		line = server.findLineForProperty("agents", lines)
		if line != len(lines)-1 {
			t.Errorf("Expected line %d for non-existent property, got %d", len(lines)-1, line)
		}
	})

	t.Run("findStepLines", func(t *testing.T) {
		lines := []string{
			"steps:",
			"  - label: \"Build\"",
			"    command: \"make build\"",
			"  ",
			"  - wait: ~",
			"  ",
			"  - trigger: \"deploy\"",
			"    build:",
			"      message: \"Deploy\"",
		}

		stepLines := server.findStepLines(lines)
		expectedLines := []int{1, 4, 6}

		if len(stepLines) != len(expectedLines) {
			t.Errorf("Expected %d step lines, got %d: %v", len(expectedLines), len(stepLines), stepLines)
			return
		}

		for i, expected := range expectedLines {
			if stepLines[i] != expected {
				t.Errorf("Expected step line %d at index %d, got %d", expected, i, stepLines[i])
			}
		}
	})
}

type ExpectedDiagnostic struct {
	Code     string
	Severity protocol.DiagnosticSeverity
	Message  string
}
