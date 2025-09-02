package schema

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/xeipuuv/gojsonschema"
)

const SchemaURL = "https://raw.githubusercontent.com/buildkite/pipeline-schema/refs/heads/main/schema.json"

type Loader struct {
	mu         sync.RWMutex
	schemaData []byte
}

func NewLoader() *Loader {
	return &Loader{}
}

func (l *Loader) GetSchemaData() ([]byte, error) {
	l.mu.RLock()
	if l.schemaData != nil {
		defer l.mu.RUnlock()
		return l.schemaData, nil
	}
	l.mu.RUnlock()

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.schemaData != nil {
		return l.schemaData, nil
	}

	resp, err := http.Get(SchemaURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch schema: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch schema: HTTP %d", resp.StatusCode)
	}

	schemaBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema: %w", err)
	}

	l.schemaData = schemaBytes
	return schemaBytes, nil
}

type ValidationError struct {
	Message string
	Path    string
	Line    int
}

func (l *Loader) ValidateJSON(jsonData []byte) (*ValidationError, error) {
	schemaData, err := l.GetSchemaData()
	if err != nil {
		return nil, fmt.Errorf("failed to get schema: %w", err)
	}

	schemaLoader := gojsonschema.NewBytesLoader(schemaData)
	documentLoader := gojsonschema.NewBytesLoader(jsonData)

	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	if err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	if !result.Valid() && len(result.Errors()) > 0 {
		// Find the most specific error - prioritize property-related errors over schema validation errors
		var bestError gojsonschema.ResultError
		bestError = result.Errors()[0] // fallback

		// Prioritize errors by specificity (most specific first)
		errorPriority := map[string]int{
			"additional_property_not_allowed": 1,  // Unknown property
			"required":                        2,  // Missing required field
			"invalid_type":                    3,  // Wrong data type
			"enum":                            4,  // Invalid enum value
			"string_gte":                      5,  // String too short
			"string_lte":                      6,  // String too long
			"array_min_items":                 7,  // Array too small
			"array_max_items":                 8,  // Array too large
			"number_gte":                      9,  // Number too small
			"number_lte":                      10, // Number too large
		}

		highestPriority := 999
		for _, err := range result.Errors() {
			if priority, exists := errorPriority[err.Type()]; exists && priority < highestPriority {
				bestError = err
				highestPriority = priority
			}
		}

		// Transform technical error messages into user-friendly ones
		message := l.friendlyErrorMessage(bestError)

		return &ValidationError{
			Message: message,
			Path:    bestError.Field(),
			Line:    1, // Will be set by caller
		}, nil
	}

	return nil, nil
}

func (l *Loader) friendlyErrorMessage(err gojsonschema.ResultError) string {
	switch err.Type() {
	case "additional_property_not_allowed":
		// Extract property name from description: "Additional property invalid_field is not allowed"
		if propertyName := extractPropertyFromDescription(err.Description()); propertyName != "" {
			return fmt.Sprintf("Unknown property '%s' is not allowed", propertyName)
		}
		return err.Description() // fallback to original
	case "required":
		return fmt.Sprintf("Missing required property '%s'", err.Field())
	case "invalid_type":
		return fmt.Sprintf("Property '%s' has wrong type (expected %s)", extractFieldName(err.Field()), err.Details()["expected"])
	case "enum":
		return fmt.Sprintf("Property '%s' must be one of: %v", extractFieldName(err.Field()), err.Details()["allowed"])
	case "string_gte":
		return fmt.Sprintf("Property '%s' is too short (minimum %v characters)", extractFieldName(err.Field()), err.Details()["min"])
	case "string_lte":
		return fmt.Sprintf("Property '%s' is too long (maximum %v characters)", extractFieldName(err.Field()), err.Details()["max"])
	case "array_min_items":
		return fmt.Sprintf("Array '%s' needs at least %v items", extractFieldName(err.Field()), err.Details()["min"])
	case "array_max_items":
		return fmt.Sprintf("Array '%s' can have at most %v items", extractFieldName(err.Field()), err.Details()["max"])
	default:
		// Fallback to original description for unknown error types
		return err.Description()
	}
}

func extractFieldName(fieldPath string) string {
	// Extract the last meaningful part of the field path
	// e.g., "steps.1.invalid_field" -> "invalid_field"
	// Skip numeric array indices
	parts := strings.Split(fieldPath, ".")

	// Go backwards through parts to find the first non-numeric part
	for i := len(parts) - 1; i >= 0; i-- {
		part := parts[i]
		// If it's not a number (array index), use it
		if !isNumeric(part) {
			return part
		}
	}

	// Fallback to the original field path if all parts are numeric (shouldn't happen)
	return fieldPath
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, char := range s {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func extractPropertyFromDescription(description string) string {
	// Extract property name from "Additional property invalid_field is not allowed"
	if strings.Contains(description, "Additional property ") && strings.Contains(description, " is not allowed") {
		start := strings.Index(description, "Additional property ") + len("Additional property ")
		end := strings.Index(description, " is not allowed")
		if start < end && end > start {
			return description[start:end]
		}
	}
	return ""
}
