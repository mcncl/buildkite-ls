package context

import (
	"strings"

	"go.lsp.dev/protocol"
)

// CompletionContext represents the type of completion context at a cursor position
type CompletionContext int

const (
	ContextUnknown      CompletionContext = iota
	ContextTopLevel                       // Top-level pipeline properties (steps, env, agents)
	ContextStep                           // Inside a step object (label, command, plugins, etc.)
	ContextPlugins                        // Inside a plugins array (plugin names)
	ContextPluginConfig                   // Inside a specific plugin configuration
)

// ContextInfo provides detailed information about the completion context
type ContextInfo struct {
	Type             CompletionContext
	IndentLevel      int
	InArray          bool
	ArrayContext     string // e.g., "steps", "plugins"
	ParentKeys       []string
	CurrentKey       string
	PluginName       string // The plugin name when in ContextPluginConfig (e.g., "docker#v5.13.0")
	NearestStepIndex int
}

// Analyzer analyzes YAML context at cursor positions
type Analyzer struct{}

// NewAnalyzer creates a new context analyzer
func NewAnalyzer() *Analyzer {
	return &Analyzer{}
}

// PositionContext provides context information about a cursor position
type PositionContext struct {
	URI          protocol.DocumentURI
	Position     protocol.Position
	CurrentLine  string
	CharIndex    int
	ContextLines []string // Lines up to current position
	FullContent  string   // Full document content
}

// AnalyzeContext determines the completion context at the given position
func (a *Analyzer) AnalyzeContext(posCtx *PositionContext) *ContextInfo {
	if posCtx == nil {
		return &ContextInfo{Type: ContextUnknown}
	}

	// Parse the YAML structure up to the current position
	lines := posCtx.ContextLines
	currentLine := posCtx.CurrentLine
	charIndex := posCtx.CharIndex

	// Build context by analyzing indentation and keys
	return a.analyzeYAMLStructure(lines, currentLine, charIndex)
}

// analyzeYAMLStructure analyzes the YAML structure to determine context
func (a *Analyzer) analyzeYAMLStructure(lines []string, currentLine string, charIndex int) *ContextInfo {
	context := &ContextInfo{
		Type:             ContextUnknown,
		ParentKeys:       make([]string, 0),
		NearestStepIndex: -1,
	}

	// Track the key stack and indentation levels
	keyStack := make([]KeyInfo, 0)

	// Analyze each line up to current position
	for i, line := range lines {
		isCurrentLine := i == len(lines)-1

		// Skip empty lines and comments, but not if it's the current line
		trimmed := strings.TrimSpace(line)
		if (trimmed == "" || strings.HasPrefix(trimmed, "#")) && !isCurrentLine {
			continue
		}

		indent := getIndentLevel(line)

		// Pop keys that are at higher or equal indentation levels
		keyStack = popKeysAtOrAboveIndent(keyStack, indent)

		// For current line, just record indentation but don't parse keys
		if isCurrentLine {
			context.IndentLevel = indent
		} else {
			// Parse the line for key information (only for non-current lines with content)
			if keyInfo := parseKeyFromLine(line, indent); keyInfo != nil {
				keyStack = append(keyStack, *keyInfo)

				// Track step indices
				if keyInfo.Key == "steps" && keyInfo.IsArray {
					context.NearestStepIndex = 0
				}
			}
		}

		// Handle array items
		if strings.Contains(trimmed, "- ") && len(keyStack) > 0 {
			lastKey := keyStack[len(keyStack)-1]
			if lastKey.Key == "steps" {
				context.NearestStepIndex++
			}
		}

		// If this is the current line, analyze the specific position
		if isCurrentLine {
			context = a.determineContextFromStack(context, keyStack, currentLine, charIndex)
		}
	}

	return context
}

// KeyInfo represents information about a YAML key
type KeyInfo struct {
	Key         string
	IndentLevel int
	IsArray     bool
	HasValue    bool
}

// parseKeyFromLine extracts key information from a YAML line
func parseKeyFromLine(line string, indent int) *KeyInfo {
	trimmed := strings.TrimSpace(line)

	// Handle array items that contain keys (e.g., "- docker#v5.13.0:")
	if strings.HasPrefix(trimmed, "- ") {
		arrayItemContent := strings.TrimSpace(trimmed[2:]) // Remove "- " prefix

		// Check if the array item has a key: pattern
		if colonIndex := strings.Index(arrayItemContent, ":"); colonIndex != -1 {
			key := strings.TrimSpace(arrayItemContent[:colonIndex])
			afterColon := strings.TrimSpace(arrayItemContent[colonIndex+1:])

			return &KeyInfo{
				Key:         key,
				IndentLevel: indent,
				IsArray:     afterColon == "" || afterColon == "[]",
				HasValue:    afterColon != "" && afterColon != "[]",
			}
		}

		// Array item without a key, skip it
		return nil
	}

	// Look for key: value pattern in regular lines
	if colonIndex := strings.Index(trimmed, ":"); colonIndex != -1 {
		key := strings.TrimSpace(trimmed[:colonIndex])
		afterColon := strings.TrimSpace(trimmed[colonIndex+1:])

		return &KeyInfo{
			Key:         key,
			IndentLevel: indent,
			IsArray:     afterColon == "" || afterColon == "[]",
			HasValue:    afterColon != "" && afterColon != "[]",
		}
	}

	return nil
}

// determineContextFromStack determines the completion context from the key stack
func (a *Analyzer) determineContextFromStack(context *ContextInfo, keyStack []KeyInfo, currentLine string, charIndex int) *ContextInfo {
	// Build parent keys list
	context.ParentKeys = make([]string, 0, len(keyStack))
	for _, key := range keyStack {
		context.ParentKeys = append(context.ParentKeys, key.Key)
	}

	// Determine context based on the key stack
	if len(keyStack) == 0 {
		context.Type = ContextTopLevel
		return context
	}

	// Check if we're in a plugins context
	for i := len(keyStack) - 1; i >= 0; i-- {
		key := keyStack[i]
		if key.Key == "plugins" {
			context.Type = ContextPlugins
			context.InArray = true
			context.ArrayContext = "plugins"

			// If there's a plugin key after "plugins", we're in plugin config
			if i < len(keyStack)-1 {
				context.Type = ContextPluginConfig
				// The plugin name is the key right after "plugins"
				context.PluginName = keyStack[i+1].Key
			}

			return context
		}

		// If we find "steps", we know we're in a step context
		if key.Key == "steps" {
			context.Type = ContextStep
			return context
		}
	}

	// Check if we're at top level (no nesting)
	if len(keyStack) <= 1 {
		context.Type = ContextTopLevel
		return context
	}

	// Default to step context if we're nested
	context.Type = ContextStep
	return context
}

// getIndentLevel calculates the indentation level of a line
func getIndentLevel(line string) int {
	indent := 0
	for _, char := range line {
		switch char {
		case ' ':
			indent++
		case '\t':
			indent += 2 // Count tabs as 2 spaces
		default:
			goto done
		}
	}
done:
	return indent
}

// popKeysAtOrAboveIndent removes keys from stack that are at or above the given indent level
func popKeysAtOrAboveIndent(keyStack []KeyInfo, indent int) []KeyInfo {
	result := make([]KeyInfo, 0, len(keyStack))

	for _, key := range keyStack {
		if key.IndentLevel < indent {
			result = append(result, key)
		}
	}

	return result
}

// IsInPluginsArray checks if the cursor is positioned inside a plugins array
func (info *ContextInfo) IsInPluginsArray() bool {
	return info.Type == ContextPlugins
}

// IsAtTopLevel checks if the cursor is at the top level of the document
func (info *ContextInfo) IsAtTopLevel() bool {
	return info.Type == ContextTopLevel
}

// IsInStepContext checks if the cursor is inside a step object
func (info *ContextInfo) IsInStepContext() bool {
	return info.Type == ContextStep
}

// GetKeyPath returns the full key path as a string
func (info *ContextInfo) GetKeyPath() string {
	if len(info.ParentKeys) == 0 {
		return ""
	}
	return strings.Join(info.ParentKeys, ".")
}
