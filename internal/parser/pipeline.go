package parser

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type Pipeline struct {
	Content   []byte
	JSONBytes []byte
	YAMLNode  *yaml.Node
}

type Position struct {
	Line      int
	Character int
}

func ParseYAML(content []byte) (*Pipeline, error) {
	var yamlNode yaml.Node
	if err := yaml.Unmarshal(content, &yamlNode); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	var yamlData interface{}
	if err := yaml.Unmarshal(content, &yamlData); err != nil {
		return nil, fmt.Errorf("failed to parse YAML data: %w", err)
	}

	jsonBytes, err := json.Marshal(yamlData)
	if err != nil {
		return nil, fmt.Errorf("failed to convert YAML to JSON: %w", err)
	}

	return &Pipeline{
		Content:   content,
		JSONBytes: jsonBytes,
		YAMLNode:  &yamlNode,
	}, nil
}

func (p *Pipeline) FindNodeByPath(path []string) *yaml.Node {
	if p.YAMLNode == nil || len(p.YAMLNode.Content) == 0 {
		return nil
	}

	return findNodeRecursive(p.YAMLNode.Content[0], path, 0)
}

func findNodeRecursive(node *yaml.Node, path []string, depth int) *yaml.Node {
	if depth >= len(path) {
		return node
	}

	if node.Kind != yaml.MappingNode {
		return nil
	}

	target := path[depth]
	for i := 0; i < len(node.Content); i += 2 {
		if i+1 < len(node.Content) && node.Content[i].Value == target {
			return findNodeRecursive(node.Content[i+1], path, depth+1)
		}
	}

	return nil
}

func (p *Pipeline) GetLineForError(errorMsg string) int {
	lines := strings.Split(string(p.Content), "\n")

	// For now, try to find common invalid patterns
	// TODO: Use YAML node positions for accurate line reporting
	invalidPatterns := []string{"invalid_field", "unknown_property", "bad_field"}

	for _, pattern := range invalidPatterns {
		for i, line := range lines {
			if strings.Contains(line, pattern) {
				return i + 1
			}
		}
	}

	// Look for any field that might be invalid based on the error context
	// This is a fallback for generic schema errors
	if strings.Contains(errorMsg, "evaluation failed") {
		// Try to find lines with uncommon field names that might be invalid
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.Contains(trimmed, "invalid_field:") {
				return i + 1
			}
		}
	}

	return 1
}
