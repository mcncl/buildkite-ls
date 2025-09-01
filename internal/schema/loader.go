package schema

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/kaptinlin/jsonschema"
)

const SchemaURL = "https://raw.githubusercontent.com/buildkite/pipeline-schema/refs/heads/main/schema.json"

type Loader struct {
	mu     sync.RWMutex
	schema *jsonschema.Schema
}

func NewLoader() *Loader {
	return &Loader{}
}

func (l *Loader) GetSchema() (*jsonschema.Schema, error) {
	l.mu.RLock()
	if l.schema != nil {
		defer l.mu.RUnlock()
		return l.schema, nil
	}
	l.mu.RUnlock()

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.schema != nil {
		return l.schema, nil
	}

	resp, err := http.Get(SchemaURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch schema: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch schema: HTTP %d", resp.StatusCode)
	}

	schemaBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	schema, err := compiler.Compile(schemaBytes, SchemaURL)
	if err != nil {
		return nil, fmt.Errorf("failed to compile schema: %w", err)
	}

	l.schema = schema
	return schema, nil
}

func (l *Loader) ValidateJSON(jsonData []byte) error {
	schema, err := l.GetSchema()
	if err != nil {
		return fmt.Errorf("failed to get schema: %w", err)
	}

	var data interface{}
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	if err := schema.Validate(data); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	return nil
}