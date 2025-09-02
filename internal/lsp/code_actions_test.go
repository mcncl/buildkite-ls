package lsp

import (
	"context"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
)

func TestServer_CodeAction(t *testing.T) {
	server := newTestServer()
	ctx := context.Background()
	uri := "file:///test/.buildkite/pipeline.yml"

	tests := []struct {
		name            string
		content         string
		line            uint32
		char            uint32
		expectedActions int
		shouldContain   []string
	}{
		{
			name: "command step missing label",
			content: `steps:
  - command: "make build"
    `,
			line:            1,
			char:            10,
			expectedActions: 3, // Add label + Convert to commands + Extract step
			shouldContain:   []string{"Add label to step", "Convert to commands array"},
		},
		{
			name: "command step with label missing key",
			content: `steps:
  - label: "Build"
    command: "make build"
    `,
			line:            1,
			char:            10,
			expectedActions: 3, // Add key + Convert to commands + Extract step
			shouldContain:   []string{"Add key to step", "Convert to commands array"},
		},
		{
			name: "step with empty command",
			content: `steps:
  - label: "Build"
    command: ""
    `,
			line:            1,
			char:            10,
			expectedActions: 3, // Fix empty command + Add key + Extract step
			shouldContain:   []string{"Fix empty command", "Add key to step"},
		},
		{
			name: "step missing type",
			content: `steps:
  - label: "Build"
    `,
			line:            1,
			char:            10,
			expectedActions: 2, // Add command + Add key
			shouldContain:   []string{"Add command to step", "Add key to step"},
		},
		{
			name: "well formed step",
			content: `steps:
  - label: "Build"
    key: "build"
    command: "make build"
    `,
			line:            1,
			char:            10,
			expectedActions: 2, // Convert to commands + Extract step (refactors)
			shouldContain:   []string{"Convert to commands array", "Extract to separate step"},
		},
		{
			name: "step with 'name' instead of 'label'",
			content: `steps:
  - name: "Build App"
    command: "make build"
    `,
			line:            1,
			char:            10,
			expectedActions: 4, // Convert name + Add key + Convert to commands + Extract step
			shouldContain:   []string{"Convert 'name' to 'label'", "Add key to step"},
		},
		{
			name: "non-buildkite file",
			content: `version: "3"
services:
  app:
    image: nginx
    `,
			line:            2,
			char:            10,
			expectedActions: 0,
			shouldContain:   []string{},
		},
		{
			name: "outside step context",
			content: `env:
  NODE_ENV: production

steps:
  - command: "make build"
    `,
			line:            1,
			char:            10, // Position in env section
			expectedActions: 0,
			shouldContain:   []string{},
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

			// Request code actions
			actionParams := &protocol.CodeActionParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: protocol.DocumentURI(testURI),
				},
				Range: protocol.Range{
					Start: protocol.Position{Line: tt.line, Character: tt.char},
					End:   protocol.Position{Line: tt.line, Character: tt.char + 1},
				},
			}

			result, err := server.CodeAction(ctx, actionParams)
			if err != nil {
				t.Fatalf("CodeAction failed: %v", err)
			}

			// Check expectations
			if len(result) != tt.expectedActions {
				t.Errorf("Expected %d code actions, got %d", tt.expectedActions, len(result))
				t.Logf("Available actions:")
				for i, action := range result {
					t.Logf("  [%d] %s (%s)", i, action.Title, action.Kind)
				}
			}

			// Check that expected actions are present
			actionTitles := make([]string, len(result))
			for i, action := range result {
				actionTitles[i] = action.Title
			}

			for _, expectedTitle := range tt.shouldContain {
				found := false
				for _, title := range actionTitles {
					if title == expectedTitle {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected action '%s' not found. Available: %v", expectedTitle, actionTitles)
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

func TestServer_StepAnalysis(t *testing.T) {
	server := newTestServer()

	tests := []struct {
		name         string
		lines        []string
		line         int
		expectedInfo *StepInfo
	}{
		{
			name: "command step with label",
			lines: []string{
				"steps:",
				"  - label: \"Build\"",
				"    command: \"make build\"",
				"    key: \"build\"",
			},
			line: 1,
			expectedInfo: &StepInfo{
				StartLine:        1,
				EndLine:          3,
				IsCommandStep:    true,
				HasLabel:         true,
				HasKey:           true,
				HasStepType:      true,
				HasSingleCommand: true,
				LabelLine:        1,
				CommandLine:      2,
				StepTypeLine:     2,
			},
		},
		{
			name: "step missing label",
			lines: []string{
				"steps:",
				"  - command: \"make test\"",
			},
			line: 1,
			expectedInfo: &StepInfo{
				StartLine:        1,
				EndLine:          1,
				IsCommandStep:    true,
				HasLabel:         false,
				HasKey:           false,
				HasStepType:      true,
				HasSingleCommand: true,
				CommandLine:      1,
				StepTypeLine:     1,
			},
		},
		{
			name: "step with empty command",
			lines: []string{
				"steps:",
				"  - label: \"Build\"",
				"    command: \"\"",
			},
			line: 1,
			expectedInfo: &StepInfo{
				StartLine:       1,
				EndLine:         2,
				IsCommandStep:   true,
				HasLabel:        true,
				HasStepType:     true,
				HasEmptyCommand: true,
				LabelLine:       1,
				CommandLine:     2,
				StepTypeLine:    2,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rang := protocol.Range{
				Start: protocol.Position{Line: uint32(tt.line), Character: 0},
				End:   protocol.Position{Line: uint32(tt.line), Character: 1},
			}

			result := server.analyzeStepAtRange(rang, tt.lines)

			if tt.expectedInfo == nil {
				if result != nil {
					t.Errorf("Expected nil StepInfo, got %+v", result)
				}
				return
			}

			if result == nil {
				t.Fatal("Expected StepInfo, got nil")
			}

			// Compare key fields
			if result.StartLine != tt.expectedInfo.StartLine {
				t.Errorf("StartLine: expected %d, got %d", tt.expectedInfo.StartLine, result.StartLine)
			}
			if result.EndLine != tt.expectedInfo.EndLine {
				t.Errorf("EndLine: expected %d, got %d", tt.expectedInfo.EndLine, result.EndLine)
			}
			if result.IsCommandStep != tt.expectedInfo.IsCommandStep {
				t.Errorf("IsCommandStep: expected %t, got %t", tt.expectedInfo.IsCommandStep, result.IsCommandStep)
			}
			if result.HasLabel != tt.expectedInfo.HasLabel {
				t.Errorf("HasLabel: expected %t, got %t", tt.expectedInfo.HasLabel, result.HasLabel)
			}
			if result.HasKey != tt.expectedInfo.HasKey {
				t.Errorf("HasKey: expected %t, got %t", tt.expectedInfo.HasKey, result.HasKey)
			}
			if result.HasStepType != tt.expectedInfo.HasStepType {
				t.Errorf("HasStepType: expected %t, got %t", tt.expectedInfo.HasStepType, result.HasStepType)
			}
			if result.HasEmptyCommand != tt.expectedInfo.HasEmptyCommand {
				t.Errorf("HasEmptyCommand: expected %t, got %t", tt.expectedInfo.HasEmptyCommand, result.HasEmptyCommand)
			}
			if result.HasSingleCommand != tt.expectedInfo.HasSingleCommand {
				t.Errorf("HasSingleCommand: expected %t, got %t", tt.expectedInfo.HasSingleCommand, result.HasSingleCommand)
			}
		})
	}
}

func TestServer_CodeActionGeneration(t *testing.T) {
	server := newTestServer()
	uri := protocol.DocumentURI("file:///test/.buildkite/pipeline.yml")

	t.Run("Add label action", func(t *testing.T) {
		stepInfo := &StepInfo{
			StartLine: 1,
			EndLine:   2,
		}

		action := server.createAddLabelAction(uri, stepInfo)

		if action.Title != "Add label to step" {
			t.Errorf("Expected title 'Add label to step', got '%s'", action.Title)
		}
		if action.Kind != protocol.QuickFix {
			t.Errorf("Expected kind QuickFix, got %s", action.Kind)
		}
		if action.Edit == nil || action.Edit.Changes == nil {
			t.Fatal("Expected edit with changes")
		}

		changes := action.Edit.Changes[uri]
		if len(changes) != 1 {
			t.Fatalf("Expected 1 text edit, got %d", len(changes))
		}

		edit := changes[0]
		if !strings.Contains(edit.NewText, "label:") {
			t.Errorf("Expected edit to contain 'label:', got '%s'", edit.NewText)
		}
	})

	t.Run("Fix empty command action", func(t *testing.T) {
		stepInfo := &StepInfo{
			CommandLine: 2,
		}

		action := server.createFixEmptyCommandAction(uri, stepInfo)

		if action.Title != "Fix empty command" {
			t.Errorf("Expected title 'Fix empty command', got '%s'", action.Title)
		}
		if !strings.Contains(action.Edit.Changes[uri][0].NewText, "TODO: Add command") {
			t.Error("Expected edit to contain placeholder command")
		}
	})
}
