package lsp

import (
	"fmt"
	"log"
	"strings"

	"go.lsp.dev/protocol"

	"github.com/mcncl/buildkite-ls/internal/context"
	"github.com/mcncl/buildkite-ls/internal/plugins"
)

// CompletionProvider handles context-aware completion
type CompletionProvider struct {
	pluginRegistry *plugins.Registry
	analyzer       *context.Analyzer
	logger         *log.Logger
}

// NewCompletionProvider creates a new completion provider
func NewCompletionProvider(pluginRegistry *plugins.Registry, logger *log.Logger) *CompletionProvider {
	return &CompletionProvider{
		pluginRegistry: pluginRegistry,
		analyzer:       context.NewAnalyzer(),
		logger:         logger,
	}
}

// GetContextAnalyzer returns the context analyzer for use by other components
func (cp *CompletionProvider) GetContextAnalyzer() *context.Analyzer {
	return cp.analyzer
}

// GetCompletions returns context-aware completions for the given position
func (cp *CompletionProvider) GetCompletions(posCtx *context.PositionContext) []protocol.CompletionItem {
	if posCtx == nil {
		cp.logger.Printf("GetCompletions called with nil position context")
		return []protocol.CompletionItem{}
	}

	cp.logger.Printf("GetCompletions - URI: %s, Line: %d, Char: %d",
		posCtx.URI, posCtx.Position.Line, posCtx.Position.Character)
	cp.logger.Printf("GetCompletions - Current line: '%s'", posCtx.CurrentLine)

	// Analyze the context at the cursor position
	contextInfo := cp.analyzer.AnalyzeContext(posCtx)

	cp.logger.Printf("Context detected - Type: %d, PluginName: '%s', ParentKeys: %v, IndentLevel: %d",
		contextInfo.Type, contextInfo.PluginName, contextInfo.ParentKeys, contextInfo.IndentLevel)

	// Return completions based on context
	switch contextInfo.Type {
	case context.ContextTopLevel:
		cp.logger.Printf("Returning top-level completions")
		return cp.getTopLevelCompletions()
	case context.ContextStep:
		cp.logger.Printf("Returning step completions")
		return cp.getStepCompletions()
	case context.ContextPlugins:
		cp.logger.Printf("Returning plugin completions")
		return cp.getPluginCompletions(posCtx, contextInfo)
	case context.ContextPluginConfig:
		cp.logger.Printf("Returning plugin config completions for plugin: %s", contextInfo.PluginName)
		return cp.getPluginConfigCompletions(contextInfo)
	default:
		cp.logger.Printf("Returning default completions")
		return cp.getDefaultCompletions()
	}
}

// getTopLevelCompletions returns completions for top-level pipeline properties
func (cp *CompletionProvider) getTopLevelCompletions() []protocol.CompletionItem {
	return []protocol.CompletionItem{
		{
			Label:            "steps",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Pipeline steps array",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "An array of build steps to be run"},
			InsertText:       "steps:\n  - $0",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
		{
			Label:            "env",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Environment variables",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Environment variables for the pipeline"},
			InsertText:       "env:\n  $0",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
		{
			Label:            "agents",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Agent requirements",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Requirements for agents to run this pipeline"},
			InsertText:       "agents:\n  $0",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
		{
			Label:         "timeout_in_minutes",
			Kind:          protocol.CompletionItemKindProperty,
			Detail:        "Pipeline timeout",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "The maximum number of minutes a job created by this step will run"},
		},
		{
			Label:         "cancel_running_branch_builds",
			Kind:          protocol.CompletionItemKindProperty,
			Detail:        "Cancel running builds",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Cancel running builds for the same branch when a new build is created"},
		},
		{
			Label:         "skip_intermediate_builds",
			Kind:          protocol.CompletionItemKindProperty,
			Detail:        "Skip intermediate builds",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Skip intermediate builds and only run the latest"},
		},
		{
			Label:         "skip",
			Kind:          protocol.CompletionItemKindProperty,
			Detail:        "Skip entire pipeline",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Skip this pipeline entirely"},
		},
		{
			Label:            "notify",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Build notifications",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Configure Slack, email, or webhook notifications"},
			InsertText:       "notify:\n  - ${1|slack,email,webhook|}: \"$2\"",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
		{
			Label:            "group",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Group steps together",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Group related steps together in the pipeline UI"},
			InsertText:       "group: \"$1\"",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
		// Advanced pipeline features
		{
			Label:            "x-buildkite-repository-provider",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Repository provider settings",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Configure repository provider specific settings"},
			InsertText:       "x-buildkite-repository-provider:\n  webhook_url: \"${1:https://api.buildkite.com/v2/webhooks/}\"\n  build_branches: ${2|true,false|}",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
		{
			Label:            "x-buildkite-plugins",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Global plugin configuration",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Define plugins that apply to all steps"},
			InsertText:       "x-buildkite-plugins:\n  - ${1:plugin-name}#${2:version}:\n      ${3:config}: \"${4:value}\"",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
		{
			Label:            "repository_provider_settings",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Repository provider configuration",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Settings specific to your repository provider (GitHub, GitLab, etc.)"},
			InsertText:       "repository_provider_settings:\n  build_pull_requests: ${1|true,false|}\n  build_branches: ${2|true,false|}\n  publish_commit_status: ${3|true,false|}",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
	}
}

// getStepCompletions returns completions for step properties
func (cp *CompletionProvider) getStepCompletions() []protocol.CompletionItem {
	return []protocol.CompletionItem{
		{
			Label:         "label",
			Kind:          protocol.CompletionItemKindProperty,
			Detail:        "Step label",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "The label that will be displayed in the pipeline"},
		},
		{
			Label:         "command",
			Kind:          protocol.CompletionItemKindProperty,
			Detail:        "Command to run",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "The command/script to be executed by this step"},
		},
		{
			Label:            "commands",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Multiple commands",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "An array of commands to run in sequence"},
			InsertText:       "commands:\n  - \"$0\"",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
		{
			Label:            "agents",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Agent requirements for step",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Agent query rules to target specific agents"},
			InsertText:       "agents:\n  $0",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
		{
			Label:         "artifact_paths",
			Kind:          protocol.CompletionItemKindProperty,
			Detail:        "Paths to upload as artifacts",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Glob patterns for files to upload as build artifacts"},
		},
		{
			Label:         "branches",
			Kind:          protocol.CompletionItemKindProperty,
			Detail:        "Branch filtering",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Restrict step to certain branches"},
		},
		{
			Label:         "if",
			Kind:          protocol.CompletionItemKindProperty,
			Detail:        "Conditional step",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "A boolean condition to determine if the step should run"},
		},
		{
			Label:         "depends_on",
			Kind:          protocol.CompletionItemKindProperty,
			Detail:        "Step dependencies",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "A list of step keys that this step depends on"},
		},
		{
			Label:            "retry",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Retry configuration",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "The conditions for retrying this step"},
			InsertText:       "retry:\n  automatic: ${1:true}\n  manual: ${2:true}",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
		{
			Label:         "timeout_in_minutes",
			Kind:          protocol.CompletionItemKindProperty,
			Detail:        "Step timeout",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "The number of minutes to time out a job"},
		},
		{
			Label:         "skip",
			Kind:          protocol.CompletionItemKindProperty,
			Detail:        "Skip this step",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Whether to skip this step"},
		},
		{
			Label:            "plugins",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Build step plugins",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "List of plugins to run for this step"},
			InsertText:       "plugins:\n  - $0",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
		{
			Label:         "key",
			Kind:          protocol.CompletionItemKindProperty,
			Detail:        "Step key identifier",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "A unique identifier for this step, used for dependencies"},
		},
		{
			Label:         "concurrency",
			Kind:          protocol.CompletionItemKindProperty,
			Detail:        "Concurrency limit",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Number of concurrent jobs allowed for this step"},
		},
		{
			Label:         "concurrency_group",
			Kind:          protocol.CompletionItemKindProperty,
			Detail:        "Concurrency group name",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Name of the concurrency group to limit parallel execution"},
		},
		{
			Label:            "concurrency_method",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Concurrency method",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "How to handle concurrency limits (eager or ordered)"},
			InsertText:       "concurrency_method: ${1|eager,ordered|}",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
		{
			Label:         "parallelism",
			Kind:          protocol.CompletionItemKindProperty,
			Detail:        "Number of parallel jobs",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Number of parallel jobs to run for this step"},
		},
		{
			Label:            "soft_fail",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Allow step to fail",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Allow the step to fail without failing the entire build"},
			InsertText:       "soft_fail: ${1|true,false|}",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
		{
			Label:         "priority",
			Kind:          protocol.CompletionItemKindProperty,
			Detail:        "Step priority",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Priority of this step (-5 to 5, higher values run first)"},
		},
		{
			Label:            "matrix",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Matrix build configuration",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Create multiple variations of this step with different variable combinations"},
			InsertText:       "matrix:\n  setup:\n    ${1:environment}: [\"${2:production}\", \"${3:staging}\"]\n    ${4:node_version}: [\"${5:16}\", \"${6:18}\", \"${7:20}\"]\n  adjustments:\n    - with:\n        ${8:environment}: \"${9:production}\"\n      ${10|skip,soft_fail|}: ${11|true,false|}",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
		{
			Label:            "notify",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Step-specific notifications",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Configure notifications for this specific step"},
			InsertText:       "notify:\n  - ${1|slack,email,webhook|}: \"$2\"",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
		// Special step types
		{
			Label:            "wait",
			Kind:             protocol.CompletionItemKindKeyword,
			Detail:           "Wait step",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Wait for all previous steps to complete"},
			InsertText:       "wait: ${1|~,null|}",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
		{
			Label:         "block",
			Kind:          protocol.CompletionItemKindKeyword,
			Detail:        "Block/manual step",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Manual approval step that blocks pipeline execution"},
		},
		{
			Label:         "input",
			Kind:          protocol.CompletionItemKindKeyword,
			Detail:        "Input step",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Collect user input before continuing"},
		},
		{
			Label:         "trigger",
			Kind:          protocol.CompletionItemKindKeyword,
			Detail:        "Trigger step",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Trigger another pipeline"},
		},
		{
			Label:            "group",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Group step",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Group multiple steps together"},
			InsertText:       "group: \"$1\"\nsteps:\n  - $0",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
		// Additional properties for special step types
		{
			Label:         "prompt",
			Kind:          protocol.CompletionItemKindProperty,
			Detail:        "Block step prompt",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Prompt text shown for manual approval steps"},
		},
		{
			Label:            "fields",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Input step fields",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Fields to collect from user input"},
			InsertText:       "fields:\n  - ${1|text,select,boolean|}: \"$2\"",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
		{
			Label:         "pipeline",
			Kind:          protocol.CompletionItemKindProperty,
			Detail:        "Pipeline to trigger",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Pipeline slug to trigger"},
		},
		{
			Label:            "build",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Trigger build configuration",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Build configuration for triggered pipeline"},
			InsertText:       "build:\n  message: \"$1\"\n  commit: \"${2:HEAD}\"\n  branch: \"${3:main}\"",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
		{
			Label:         "async",
			Kind:          protocol.CompletionItemKindProperty,
			Detail:        "Async trigger",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Don't wait for triggered pipeline to complete"},
		},
		// Advanced features
		{
			Label:         "group",
			Kind:          protocol.CompletionItemKindProperty,
			Detail:        "Step group",
			Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Group steps together in the UI"},
		},
		{
			Label:            "parallelism",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Parallel job count",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Number of parallel jobs to run for this step"},
			InsertText:       "parallelism: ${1:5}",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
		{
			Label:            "signature",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Step signature",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Digital signature for step verification"},
			InsertText:       "signature:\n  algorithm: \"${1:sha256}\"\n  signed_fields:\n    - \"${2:command}\"",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
		{
			Label:            "cache",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Step-level cache",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Cache configuration for this specific step"},
			InsertText:       "cache:\n  - \"${1:.cache}\"",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
		{
			Label:            "concurrency_method",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Concurrency control method",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "How to handle concurrency conflicts"},
			InsertText:       "concurrency_method: ${1|eager,ordered|}",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
		{
			Label:            "cancel_on_build_failing",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Cancel on build failure",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Cancel this step if any other step fails"},
			InsertText:       "cancel_on_build_failing: ${1|true,false|}",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
	}
}

// getPluginCompletions returns completions for plugin names
func (cp *CompletionProvider) getPluginCompletions(posCtx *context.PositionContext, contextInfo *context.ContextInfo) []protocol.CompletionItem {
	var items []protocol.CompletionItem

	// Check if we need to suggest adding a list item first
	if cp.needsListItemSuggestion(posCtx, contextInfo) {
		items = append(items, protocol.CompletionItem{
			Label:            "- (add plugin)",
			Kind:             protocol.CompletionItemKindSnippet,
			Detail:           "Add a plugin to the list",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Insert a list item for adding a plugin"},
			InsertText:       "- ${1:plugin-name}#${2:version}:\n    ${3:config}: \"${4:value}\"",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
			SortText:         "00-list-item", // Sort this to the top
		})
	}

	for _, plugin := range plugins.GetPopularPlugins() {
		fullName := plugin.Name + "#" + plugin.Version

		// Create smart snippet templates based on plugin type
		var insertText string
		switch plugin.Name {
		case "docker":
			insertText = fmt.Sprintf("%s:\n    image: \"${1:node:18}\"", fullName)
		case "docker-compose":
			insertText = fmt.Sprintf("%s:\n    run: \"${1:app}\"", fullName)
		case "cache":
			insertText = fmt.Sprintf("%s:\n    key: \"${1:v1-cache-key}\"\n    paths:\n      - \"${2:.cache}\"", fullName)
		case "artifacts":
			insertText = fmt.Sprintf("%s:\n    download: \"${1:build/*}\"", fullName)
		case "test-collector":
			insertText = fmt.Sprintf("%s:\n    files: \"${1:test-results.xml}\"", fullName)
		case "slack":
			insertText = fmt.Sprintf("%s:\n    message: \"${1:Build completed}\"", fullName)
		case "junit-annotate":
			insertText = fmt.Sprintf("%s:\n    artifacts: \"${1:test-results.xml}\"", fullName)
		case "shellcheck":
			insertText = fmt.Sprintf("%s:\n    files: \"${1:scripts/*.sh}\"", fullName)
		default:
			insertText = fmt.Sprintf("%s:\n    ${1:property}: \"${2:value}\"", fullName)
		}

		items = append(items, protocol.CompletionItem{
			Label:  fullName,
			Kind:   protocol.CompletionItemKindModule,
			Detail: plugin.Description,
			Documentation: &protocol.MarkupContent{
				Kind:  protocol.Markdown,
				Value: fmt.Sprintf("**%s Plugin**\n\n%s\n\n[Plugin Directory](https://buildkite.com/plugins)", plugin.Name, plugin.Description),
			},
			InsertText:       insertText,
			InsertTextFormat: protocol.InsertTextFormatSnippet,
			FilterText:       plugin.Name,                                           // This allows "dock" to match "docker#v5.13.0"
			SortText:         fmt.Sprintf("%02d-%s", len(plugin.Name), plugin.Name), // Sort by name length, then alphabetically
		})
	}

	return items
}

// needsListItemSuggestion checks if we should suggest adding a list item (-)
func (cp *CompletionProvider) needsListItemSuggestion(posCtx *context.PositionContext, contextInfo *context.ContextInfo) bool {
	if posCtx == nil || contextInfo == nil {
		return false
	}

	// Check if current line doesn't already have a list item
	currentLine := posCtx.CurrentLine
	trimmedLine := strings.TrimSpace(currentLine)

	// If the line already has a dash, don't suggest another one
	if strings.HasPrefix(trimmedLine, "-") {
		return false
	}

	// If the line is empty or only has whitespace, and we're in plugins context,
	// suggest the list item
	if trimmedLine == "" && contextInfo.Type == context.ContextPlugins {
		return true
	}

	return false
}

// getPluginConfigCompletions returns completions for plugin configuration
func (cp *CompletionProvider) getPluginConfigCompletions(contextInfo *context.ContextInfo) []protocol.CompletionItem {
	if contextInfo.PluginName == "" {
		// No plugin name detected, return generic completions
		return cp.getGenericPluginConfigCompletions()
	}

	// Fetch plugin schema for the specific plugin
	schema, err := cp.pluginRegistry.GetPluginSchema(contextInfo.PluginName)
	if err != nil {
		// If we can't fetch the schema, return generic completions
		return cp.getGenericPluginConfigCompletions()
	}

	// Generate completions from the plugin schema
	return cp.generateCompletionsFromSchema(schema, contextInfo.PluginName, contextInfo.IndentLevel)
}

// getGenericPluginConfigCompletions returns fallback completions when plugin schema is unavailable
func (cp *CompletionProvider) getGenericPluginConfigCompletions() []protocol.CompletionItem {
	return []protocol.CompletionItem{
		{
			Label:            "enabled",
			Kind:             protocol.CompletionItemKindProperty,
			Detail:           "Enable/disable plugin",
			Documentation:    &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Whether this plugin should be enabled"},
			InsertText:       "enabled: ${1|true,false|}",
			InsertTextFormat: protocol.InsertTextFormatSnippet,
		},
	}
}

// generateCompletionsFromSchema creates completion items from a plugin's JSON schema
func (cp *CompletionProvider) generateCompletionsFromSchema(schema *plugins.PluginSchema, pluginName string, indentLevel int) []protocol.CompletionItem {
	var completions []protocol.CompletionItem

	// If the schema has no configuration section, return generic completions
	if schema.Configuration == nil {
		return cp.getGenericPluginConfigCompletions()
	}

	// Parse the configuration schema to generate completions
	if properties, ok := schema.Configuration["properties"].(map[string]interface{}); ok {
		for propName, propDef := range properties {
			completion := cp.createCompletionFromProperty(propName, propDef, pluginName, indentLevel)
			if completion != nil {
				completions = append(completions, *completion)
			}
		}
	}

	// If no completions were generated, return generic ones
	if len(completions) == 0 {
		return cp.getGenericPluginConfigCompletions()
	}

	return completions
}

// createCompletionFromProperty creates a completion item from a JSON schema property
func (cp *CompletionProvider) createCompletionFromProperty(propName string, propDef interface{}, pluginName string, indentLevel int) *protocol.CompletionItem {
	propMap, ok := propDef.(map[string]interface{})
	if !ok {
		return nil
	}

	completion := &protocol.CompletionItem{
		Label: propName,
		Kind:  protocol.CompletionItemKindProperty,
	}

	// Extract description for documentation
	if desc, ok := propMap["description"].(string); ok {
		completion.Detail = desc
		completion.Documentation = &protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: fmt.Sprintf("**%s Plugin - %s**\n\n%s", pluginName, propName, desc),
		}
	}

	// Generate insert text based on property type with proper indentation
	insertText := cp.generateInsertTextForProperty(propName, propMap, indentLevel)
	if insertText != "" {
		completion.InsertText = insertText
		completion.InsertTextFormat = protocol.InsertTextFormatSnippet
	}

	return completion
}

// generateInsertTextForProperty creates appropriate snippet text based on JSON schema property type
func (cp *CompletionProvider) generateInsertTextForProperty(propName string, propDef map[string]interface{}, indentLevel int) string {
	propType, hasType := propDef["type"].(string)

	// Create indentation string - for plugin configs, we're already at the right level
	// Just use the property name without extra indentation since completion will insert at cursor

	// Handle enum values
	if enumVal, hasEnum := propDef["enum"].([]interface{}); hasEnum && len(enumVal) > 0 {
		var options []string
		for _, val := range enumVal {
			if str, ok := val.(string); ok {
				options = append(options, str)
			}
		}
		if len(options) > 0 {
			return fmt.Sprintf("%s: ${1|%s|}", propName, fmt.Sprintf(`"%s"`, options[0]))
		}
	}

	if !hasType {
		return fmt.Sprintf("%s: \"${1}\"", propName)
	}

	switch propType {
	case "string":
		if defaultVal, ok := propDef["default"].(string); ok {
			return fmt.Sprintf("%s: \"${1:%s}\"", propName, defaultVal)
		}
		return fmt.Sprintf("%s: \"${1}\"", propName)
	case "boolean":
		return fmt.Sprintf("%s: ${1|true,false|}", propName)
	case "integer", "number":
		if defaultVal, ok := propDef["default"].(float64); ok {
			return fmt.Sprintf("%s: ${1:%.0f}", propName, defaultVal)
		}
		return fmt.Sprintf("%s: ${1:0}", propName)
	case "array":
		// For arrays, use relative indentation (2 spaces from current level)
		return fmt.Sprintf("%s:\n  - \"${1}\"", propName)
	case "object":
		// For objects, use relative indentation (2 spaces from current level)
		return fmt.Sprintf("%s:\n  ${1:key}: \"${2:value}\"", propName)
	default:
		return fmt.Sprintf("%s: ${1}", propName)
	}
}

// getDefaultCompletions returns fallback completions
func (cp *CompletionProvider) getDefaultCompletions() []protocol.CompletionItem {
	// Combine top-level and step completions as fallback
	items := cp.getTopLevelCompletions()
	items = append(items, cp.getStepCompletions()...)
	return items
}
