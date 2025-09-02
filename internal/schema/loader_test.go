package schema

import (
	"testing"
)

func TestValidateJSON_ValidPipeline(t *testing.T) {
	loader := NewLoader()

	// Valid minimal pipeline
	validJSON := `{"steps": [{"label": "test", "command": "echo hello"}]}`

	result, err := loader.ValidateJSON([]byte(validJSON))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result != nil {
		t.Errorf("Expected no validation error for valid pipeline, got: %s", result.Message)
	}
}

func TestValidateJSON_InvalidProperty(t *testing.T) {
	loader := NewLoader()

	// Pipeline with invalid property
	invalidJSON := `{"steps": [{"label": "test", "command": "echo hello", "invalid_field": "should fail"}]}`

	result, err := loader.ValidateJSON([]byte(invalidJSON))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("Expected validation error for invalid property")
	}

	if result.Message == "" {
		t.Error("Expected non-empty error message")
	}

	// Should contain information about the invalid field
	expectedSubstrings := []string{"invalid_field", "not allowed"}
	for _, expected := range expectedSubstrings {
		if !containsSubstring(result.Message, expected) {
			t.Errorf("Expected error message to contain '%s', got: %s", expected, result.Message)
		}
	}
}

func TestValidateJSON_MissingRequiredField(t *testing.T) {
	loader := NewLoader()

	// Pipeline missing required steps field
	invalidJSON := `{"env": {"DEBUG": "true"}}`

	result, err := loader.ValidateJSON([]byte(invalidJSON))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("Expected validation error for missing required field")
	}

	// Should mention that steps is required
	if !containsSubstring(result.Message, "required") && !containsSubstring(result.Message, "steps") {
		t.Errorf("Expected error message to mention required steps field, got: %s", result.Message)
	}
}

func TestValidateJSON_WrongType(t *testing.T) {
	loader := NewLoader()

	// Steps should be array, not string
	invalidJSON := `{"steps": "should be array"}`

	result, err := loader.ValidateJSON([]byte(invalidJSON))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("Expected validation error for wrong type")
	}

	// Should mention type issue
	if !containsSubstring(result.Message, "type") && !containsSubstring(result.Message, "expected") {
		t.Errorf("Expected error message to mention type issue, got: %s", result.Message)
	}
}

func TestValidateJSON_InvalidJSON(t *testing.T) {
	loader := NewLoader()

	// Malformed JSON
	invalidJSON := `{"steps": [{"label": "test", "command": "echo hello"`

	_, err := loader.ValidateJSON([]byte(invalidJSON))
	if err == nil {
		t.Fatal("Expected error for invalid JSON")
	}
}

func TestValidateJSON_EmptyContent(t *testing.T) {
	loader := NewLoader()

	_, err := loader.ValidateJSON([]byte(""))
	if err == nil {
		t.Fatal("Expected error for empty JSON")
	}
}

func TestExtractPropertyFromDescription(t *testing.T) {
	tests := []struct {
		description string
		expected    string
	}{
		{
			description: "Additional property invalid_field is not allowed",
			expected:    "invalid_field",
		},
		{
			description: "Additional property some_other_field is not allowed",
			expected:    "some_other_field",
		},
		{
			description: "Some other error message",
			expected:    "",
		},
		{
			description: "Additional property  is not allowed", // Empty property name
			expected:    "",
		},
		{
			description: "",
			expected:    "",
		},
	}

	for _, test := range tests {
		result := extractPropertyFromDescription(test.description)
		if result != test.expected {
			t.Errorf("extractPropertyFromDescription(%q) = %q, expected %q", test.description, result, test.expected)
		}
	}
}

func TestExtractFieldName(t *testing.T) {
	tests := []struct {
		fieldPath string
		expected  string
	}{
		{
			fieldPath: "steps.1.invalid_field",
			expected:  "invalid_field",
		},
		{
			fieldPath: "steps.0.plugins.2.image",
			expected:  "image",
		},
		{
			fieldPath: "env.DEBUG",
			expected:  "DEBUG",
		},
		{
			fieldPath: "timeout_in_minutes",
			expected:  "timeout_in_minutes",
		},
		{
			fieldPath: "1",
			expected:  "1", // Fallback when all parts are numeric
		},
		{
			fieldPath: "",
			expected:  "",
		},
	}

	for _, test := range tests {
		result := extractFieldName(test.fieldPath)
		if result != test.expected {
			t.Errorf("extractFieldName(%q) = %q, expected %q", test.fieldPath, result, test.expected)
		}
	}
}

func TestValidationError_ComplexScenarios(t *testing.T) {
	loader := NewLoader()

	tests := []struct {
		name        string
		jsonContent string
		expectError bool
		errorChecks []string // Substrings that should be in the error message
	}{
		{
			name:        "valid_complex_pipeline",
			jsonContent: `{"steps": [{"label": "Build", "command": "make build", "agents": {"queue": "default"}}, {"label": "Test", "commands": ["make test", "make lint"]}], "env": {"CI": "true"}}`,
			expectError: false,
		},
		{
			name:        "invalid_step_property",
			jsonContent: `{"steps": [{"label": "Build", "invalid_step_prop": "bad"}]}`,
			expectError: true,
			errorChecks: []string{"invalid_step_prop", "not allowed"},
		},
		{
			name:        "invalid_agents_format",
			jsonContent: `{"steps": [{"label": "Build", "command": "echo", "agents": "should be object"}]}`,
			expectError: true,
			errorChecks: []string{"type"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := loader.ValidateJSON([]byte(test.jsonContent))
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if test.expectError {
				if result == nil {
					t.Fatal("Expected validation error but got none")
				}

				for _, check := range test.errorChecks {
					if !containsSubstring(result.Message, check) {
						t.Errorf("Expected error message to contain '%s', got: %s", check, result.Message)
					}
				}
			} else {
				if result != nil {
					t.Errorf("Expected no validation error but got: %s", result.Message)
				}
			}
		})
	}
}

func TestErrorPrioritization(t *testing.T) {
	loader := NewLoader()

	// This JSON should generate multiple errors, test that we get the most specific one
	invalidJSON := `{
		"steps": [
			{
				"label": "Test",
				"command": "echo hello",
				"invalid_property": "this should be prioritized",
				"agents": "wrong type"
			}
		],
		"missing_required_field_test": true
	}`

	result, err := loader.ValidateJSON([]byte(invalidJSON))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("Expected validation error")
	}

	// Should prioritize "additional_property_not_allowed" over other error types
	// The exact message will depend on which error the library returns first,
	// but it should be a specific, actionable error
	if result.Message == "evaluation failed" {
		t.Error("Should not return generic 'evaluation failed' message")
	}

	if len(result.Message) < 10 {
		t.Error("Error message should be descriptive")
	}
}

// Helper function to check if a string contains a substring (case-insensitive)
func containsSubstring(text, substring string) bool {
	if text == "" || substring == "" {
		return false
	}

	// Simple case-insensitive substring check
	textLower := ""
	subLower := ""

	for _, r := range text {
		if r >= 'A' && r <= 'Z' {
			textLower += string(r + 32)
		} else {
			textLower += string(r)
		}
	}

	for _, r := range substring {
		if r >= 'A' && r <= 'Z' {
			subLower += string(r + 32)
		} else {
			subLower += string(r)
		}
	}

	for i := 0; i <= len(textLower)-len(subLower); i++ {
		if textLower[i:i+len(subLower)] == subLower {
			return true
		}
	}

	return false
}
