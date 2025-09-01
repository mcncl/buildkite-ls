package parser

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

type Pipeline struct {
	Content   []byte
	JSONBytes []byte
}

func ParseYAML(content []byte) (*Pipeline, error) {
	var yamlData interface{}
	if err := yaml.Unmarshal(content, &yamlData); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	jsonBytes, err := json.Marshal(yamlData)
	if err != nil {
		return nil, fmt.Errorf("failed to convert YAML to JSON: %w", err)
	}

	return &Pipeline{
		Content:   content,
		JSONBytes: jsonBytes,
	}, nil
}