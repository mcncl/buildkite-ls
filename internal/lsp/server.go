package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"

	bkcontext "github.com/mcncl/buildkite-ls/internal/context"
	"github.com/mcncl/buildkite-ls/internal/parser"
	"github.com/mcncl/buildkite-ls/internal/plugins"
	"github.com/mcncl/buildkite-ls/internal/schema"
)

type Server struct {
	client             protocol.Client
	logger             *log.Logger
	schemaLoader       *schema.Loader
	pluginRegistry     *plugins.Registry
	documentManager    *DocumentManager
	completionProvider *CompletionProvider
	conn               jsonrpc2.Conn
}

func NewServer() *Server {
	pluginRegistry := plugins.NewRegistry()

	// Create debug log file
	debugFile, err := os.OpenFile("/tmp/buildkite-ls-debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
	if err != nil {
		debugFile = os.Stderr // Fallback to stderr
	}

	logger := log.New(debugFile, "[buildkite-ls] ", log.LstdFlags|log.Lshortfile)

	return &Server{
		logger:             logger,
		schemaLoader:       schema.NewLoader(),
		pluginRegistry:     pluginRegistry,
		documentManager:    NewDocumentManager(),
		completionProvider: NewCompletionProvider(pluginRegistry, logger),
	}
}

func (s *Server) SetClient(client protocol.Client) {
	s.client = client
}

func (s *Server) SetConnection(conn jsonrpc2.Conn) {
	s.conn = conn
}

func (s *Server) Logger() *log.Logger {
	return s.logger
}

func (s *Server) Initialize(ctx context.Context, params *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	s.logger.Printf("Initializing buildkite-ls server")

	completionOptions := &protocol.CompletionOptions{
		TriggerCharacters: []string{" ", ":", "-"},
	}

	s.logger.Printf("Advertising completion capabilities with triggers: %v", completionOptions.TriggerCharacters)

	return &protocol.InitializeResult{
		Capabilities: protocol.ServerCapabilities{
			TextDocumentSync: &protocol.TextDocumentSyncOptions{
				OpenClose: true,
				Change:    protocol.TextDocumentSyncKindFull,
			},
			HoverProvider:          true,
			CompletionProvider:     completionOptions,
			DocumentSymbolProvider: true,
			SignatureHelpProvider: &protocol.SignatureHelpOptions{
				TriggerCharacters: []string{":", " ", "\n"},
			},
			DefinitionProvider: true,
			CodeActionProvider: &protocol.CodeActionOptions{
				CodeActionKinds: []protocol.CodeActionKind{
					protocol.QuickFix,
					protocol.Refactor,
					protocol.RefactorRewrite,
				},
			},
			SemanticTokensProvider: map[string]interface{}{
				"legend": map[string]interface{}{
					"tokenTypes": []string{
						"keyword",   // step types (command, wait, block, etc.)
						"string",    // labels, commands, values
						"property",  // YAML property keys
						"variable",  // environment variables
						"function",  // plugin names
						"namespace", // step keys for reference
						"operator",  // YAML operators like :, -, |
						"comment",   // YAML comments
					},
					"tokenModifiers": []string{
						"definition", // when defining a step or plugin
						"readonly",   // for immutable values
						"deprecated", // for deprecated properties
					},
				},
				"range": true,
				"full":  true,
			},
		},
		ServerInfo: &protocol.ServerInfo{
			Name:    "buildkite-ls",
			Version: "0.1.0",
		},
	}, nil
}

func (s *Server) Initialized(ctx context.Context, params *protocol.InitializedParams) error {
	s.logger.Printf("Server initialized - ready to receive document events")
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Printf("Server shutting down")
	return nil
}

func (s *Server) Exit(ctx context.Context) error {
	s.logger.Printf("Server exiting")
	return nil
}

func (s *Server) DidOpen(ctx context.Context, params *protocol.DidOpenTextDocumentParams) error {
	s.logger.Printf("Document opened: %s", params.TextDocument.URI)
	s.logger.Printf("Document language: %s", params.TextDocument.LanguageID)
	s.logger.Printf("Is Buildkite file: %t", s.isBuildkiteFile(string(params.TextDocument.URI)))

	// Store document content
	s.documentManager.OpenDocument(params.TextDocument.URI, params.TextDocument.Version, params.TextDocument.Text)

	// Validate the document
	s.validateDocument(ctx, params.TextDocument.URI, params.TextDocument.Text)
	return nil
}

func (s *Server) DidChange(ctx context.Context, params *protocol.DidChangeTextDocumentParams) error {
	s.logger.Printf("Document changed: %s", params.TextDocument.URI)

	if len(params.ContentChanges) > 0 {
		lastChange := params.ContentChanges[len(params.ContentChanges)-1]

		// Update document content
		s.documentManager.UpdateDocument(params.TextDocument.URI, params.TextDocument.Version, lastChange.Text)

		// Validate the updated document
		s.validateDocument(ctx, params.TextDocument.URI, lastChange.Text)
	}
	return nil
}

func (s *Server) DidClose(ctx context.Context, params *protocol.DidCloseTextDocumentParams) error {
	s.logger.Printf("Document closed: %s", params.TextDocument.URI)

	// Remove document from cache
	s.documentManager.CloseDocument(params.TextDocument.URI)
	return nil
}

func (s *Server) Hover(ctx context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	if !s.isBuildkiteFile(string(params.TextDocument.URI)) {
		return nil, nil
	}

	// Get position context to provide smart hover
	posCtx, err := s.documentManager.GetContentAtPosition(params.TextDocument.URI, params.Position)
	if err != nil {
		s.logger.Printf("Failed to get position context for hover: %v", err)
		return nil, nil
	}

	hoverContent := s.getContextualHoverContent(posCtx)
	if hoverContent == "" {
		return nil, nil // No hover content available
	}

	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: hoverContent,
		},
	}, nil
}

func (s *Server) getContextualHoverContent(posCtx *bkcontext.PositionContext) string {
	if posCtx == nil {
		return ""
	}

	// Analyze context to determine what we're hovering over
	contextInfo := s.completionProvider.GetContextAnalyzer().AnalyzeContext(posCtx)

	// Extract the word/property at cursor position
	currentWord := s.extractWordAtPosition(posCtx)
	if currentWord == "" {
		return ""
	}

	// Check if hovering over a plugin reference
	if strings.Contains(currentWord, "#") && contextInfo.IsInPluginsArray() {
		return s.getPluginHoverContent(currentWord)
	}

	// Provide property-specific documentation
	return s.getPropertyHoverContent(currentWord, contextInfo)
}

func (s *Server) extractWordAtPosition(posCtx *bkcontext.PositionContext) string {
	currentLine := posCtx.CurrentLine
	charIndex := posCtx.CharIndex

	if charIndex >= len(currentLine) {
		return ""
	}

	// Find word boundaries around the cursor position
	start := charIndex
	end := charIndex

	// Move start back to beginning of word
	for start > 0 && (isAlphanumeric(currentLine[start-1]) || currentLine[start-1] == '_' || currentLine[start-1] == '-' || currentLine[start-1] == '#') {
		start--
	}

	// Move end forward to end of word
	for end < len(currentLine) && (isAlphanumeric(currentLine[end]) || currentLine[end] == '_' || currentLine[end] == '-' || currentLine[end] == '#') {
		end++
	}

	if start >= end {
		return ""
	}

	word := currentLine[start:end]
	// Remove trailing colon if present (for YAML keys)
	return strings.TrimSuffix(word, ":")
}

func isAlphanumeric(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '.' || b == '@'
}

func (s *Server) getPluginHoverContent(pluginName string) string {
	schema, err := s.pluginRegistry.GetPluginSchema(pluginName)
	if err != nil {
		return fmt.Sprintf("Plugin: %s\n\nUnable to load plugin information.", pluginName)
	}

	content := fmt.Sprintf("# %s Plugin\n\n%s\n\n", schema.Name, schema.Description)

	if schema.Author != "" {
		content += fmt.Sprintf("**Author**: %s\n\n", schema.Author)
	}

	if len(schema.Requirements) > 0 {
		content += "**Requirements**:\n"
		for _, req := range schema.Requirements {
			content += fmt.Sprintf("- %s\n", req)
		}
		content += "\n"
	}

	if schema.Configuration != nil {
		content += "**Configuration Options**: Available via schema validation\n\n"
	}

	content += "[Plugin Documentation](https://buildkite.com/plugins)"
	return content
}

func (s *Server) getPropertyHoverContent(property string, contextInfo *bkcontext.ContextInfo) string {
	// Create comprehensive documentation for Buildkite properties
	propertyDocs := map[string]string{
		// Pipeline-level properties
		"steps":  "**steps** - Array of build steps to be executed\n\nDefines the sequence of operations for your build pipeline. Each step can be a command step, wait step, block step, input step, or trigger step.\n\n[Steps Documentation](https://buildkite.com/docs/pipelines/defining-steps)",
		"env":    "**env** - Environment variables for the pipeline\n\nDefines environment variables that will be available to all steps in the pipeline unless overridden at the step level.\n\nExample:\n```yaml\nenv:\n  NODE_ENV: production\n  DEBUG: \"false\"\n```",
		"agents": "**agents** - Agent requirements for running steps\n\nSpecifies which agents can run this pipeline or step using key-value pairs for targeting.\n\nExample:\n```yaml\nagents:\n  queue: \"default\"\n  os: \"linux\"\n```",

		// Step properties
		"label":   "**label** - Human-readable name for the step\n\nDisplayed in the Buildkite UI and used to identify the step. Supports emoji and can include environment variable substitutions.\n\nExample: `label: \":rocket: Deploy to production\"`",
		"command": "**command** - Shell command(s) to execute\n\nCan be a single command or multiple commands. Supports multiline YAML syntax for complex scripts.\n\nExample:\n```yaml\ncommand: |\n  echo \"Building...\"\n  make build\n  make test\n```",
		"plugins": "**plugins** - List of plugins to enhance the step\n\nEach plugin provides additional functionality like Docker support, caching, or artifact management. Plugins are specified with their name and version.\n\n[Plugin Directory](https://buildkite.com/plugins)",

		// Advanced step properties
		"depends_on":         "**depends_on** - Step dependencies\n\nSpecifies which steps must complete before this step runs. Can reference steps by label or use step keys.\n\nExample:\n```yaml\ndepends_on:\n  - \"build\"\n  - step: \"test\"\n    allow_failure: true\n```",
		"if":                 "**if** - Conditional execution\n\nStep will only run if the condition evaluates to true. Supports environment variables and build metadata.\n\nExample: `if: build.branch == \"main\"`",
		"retry":              "**retry** - Automatic and manual retry configuration\n\nDefines how the step should be retried on failure.\n\nExample:\n```yaml\nretry:\n  automatic:\n    - exit_status: -1\n      limit: 2\n  manual:\n    allowed: true\n```",
		"timeout_in_minutes": "**timeout_in_minutes** - Step timeout\n\nMaximum time the step can run before being cancelled. Defaults to no timeout.\n\nExample: `timeout_in_minutes: 30`",
		"artifact_paths":     "**artifact_paths** - Glob patterns for build artifacts\n\nSpecifies which files/directories to upload as build artifacts after the step completes.\n\nExample: `artifact_paths: \"dist/**/*\"`",
		"branches":           "**branches** - Branch filtering\n\nControls which branches this step runs on. Supports glob patterns and negation.\n\nExample: `branches: \"main release/*\"`",
		"concurrency":        "**concurrency** - Parallel execution limit\n\nLimits how many instances of this step can run simultaneously across all agents.\n\nExample: `concurrency: 1`",
		"concurrency_group":  "**concurrency_group** - Concurrency grouping\n\nGroups steps together for concurrency limiting. Steps in the same group share concurrency limits.\n\nExample: `concurrency_group: \"deploy\"`",

		// Special step types
		"wait":    "**wait** - Wait step\n\nPauses the pipeline until all previous steps have completed. Useful for creating pipeline phases.\n\nExample: `wait: ~` or `wait: \"Continue to deploy?\"`",
		"block":   "**block** - Manual approval step\n\nPauses the pipeline and waits for manual approval before continuing.\n\nExample: `block: \"Deploy to production?\"`",
		"input":   "**input** - Input step\n\nCollects input from users before continuing the pipeline.\n\nExample:\n```yaml\ninput: \"Release details\"\nfields:\n  - text: \"version\"\n    required: true\n```",
		"trigger": "**trigger** - Trigger another pipeline\n\nTriggers another pipeline and optionally waits for it to complete.\n\nExample:\n```yaml\ntrigger: \"my-deployment-pipeline\"\nbuild:\n  message: \"Triggered from ${BUILDKITE_MESSAGE}\"\n```",

		// Plugin-specific (common ones)
		"image":   "**image** - Docker image to use\n\nSpecifies the Docker image for the docker plugin.\n\nExample: `image: \"node:18\"`",
		"volumes": "**volumes** - Docker volume mounts\n\nMounts host directories or volumes into the Docker container.\n\nExample:\n```yaml\nvolumes:\n  - \".:/app\"\n  - \"./cache:/cache\"\n```",
		"key":     "**key** - Cache key\n\nUnique identifier for the cache entry in the cache plugin.\n\nExample: `key: \"v1-{{ checksum 'package-lock.json' }}\"`",
		"paths":   "**paths** - Cache paths\n\nDirectories or files to cache.\n\nExample:\n```yaml\npaths:\n  - \"node_modules\"\n  - \".cache\"\n```",
	}

	// Get documentation for the property
	if doc, exists := propertyDocs[property]; exists {
		return doc
	}

	// For unknown properties, provide basic context-aware help
	contextType := "unknown"
	if contextInfo.IsAtTopLevel() {
		contextType = "pipeline-level"
	} else if contextInfo.IsInStepContext() {
		contextType = "step-level"
	} else if contextInfo.IsInPluginsArray() {
		contextType = "plugin"
	}

	return fmt.Sprintf("**%s** - %s property\n\nNo specific documentation available for this property.\n\n[Buildkite Documentation](https://buildkite.com/docs)", property, contextType)
}

func (s *Server) Completion(ctx context.Context, params *protocol.CompletionParams) (*protocol.CompletionList, error) {
	s.logger.Printf("Completion requested for URI: %s, Position: %d:%d", params.TextDocument.URI, params.Position.Line, params.Position.Character)

	if !s.isBuildkiteFile(string(params.TextDocument.URI)) {
		s.logger.Printf("File is not a Buildkite file, skipping completion")
		return &protocol.CompletionList{IsIncomplete: false, Items: []protocol.CompletionItem{}}, nil
	}

	// Get document content and position context
	positionContext, err := s.documentManager.GetContentAtPosition(params.TextDocument.URI, params.Position)
	if err != nil {
		s.logger.Printf("Failed to get position context: %v", err)
		return &protocol.CompletionList{IsIncomplete: false, Items: []protocol.CompletionItem{}}, nil
	}

	s.logger.Printf("Position context - Current line: '%s', Char index: %d", positionContext.CurrentLine, positionContext.CharIndex)

	// Get context-aware completions
	items := s.completionProvider.GetCompletions(positionContext)

	s.logger.Printf("Generated %d completion items", len(items))

	return &protocol.CompletionList{
		IsIncomplete: false,
		Items:        items,
	}, nil
}

func (s *Server) SignatureHelp(ctx context.Context, params *protocol.SignatureHelpParams) (*protocol.SignatureHelp, error) {
	s.logger.Printf("SignatureHelp requested for URI: %s, Position: %d:%d", params.TextDocument.URI, params.Position.Line, params.Position.Character)

	if !s.isBuildkiteFile(string(params.TextDocument.URI)) {
		s.logger.Printf("File is not a Buildkite file, skipping signature help")
		return nil, nil
	}

	// Get document content and position context
	positionContext, err := s.documentManager.GetContentAtPosition(params.TextDocument.URI, params.Position)
	if err != nil {
		s.logger.Printf("Failed to get position context: %v", err)
		return nil, nil
	}

	s.logger.Printf("SignatureHelp context - Current line: '%s', Char index: %d", positionContext.CurrentLine, positionContext.CharIndex)

	// Get signature help based on context
	signatures := s.getSignatureHelp(positionContext)

	if len(signatures) == 0 {
		return nil, nil
	}

	return &protocol.SignatureHelp{
		Signatures:      signatures,
		ActiveSignature: 0,
		ActiveParameter: s.getActiveParameter(positionContext, signatures[0]),
	}, nil
}

func (s *Server) getSignatureHelp(ctx *bkcontext.PositionContext) []protocol.SignatureInformation {
	var signatures []protocol.SignatureInformation

	// Detect context - plugin configuration, step properties, etc.
	if s.isInPluginContext(ctx) {
		signatures = append(signatures, s.getPluginSignatures(ctx)...)
	} else if s.isInStepContext(ctx) {
		signatures = append(signatures, s.getStepSignatures(ctx)...)
	}

	return signatures
}

func (s *Server) isInPluginContext(ctx *bkcontext.PositionContext) bool {
	// Check if we're in a plugin configuration context
	// Look for "plugins:" section and check if we're inside it
	lines := strings.Split(ctx.FullContent, "\n")
	currentLine := int(ctx.Position.Line)

	// Go backwards to find if we're in a plugins section
	inStepsSection := false
	inPluginsSection := false

	for i := currentLine; i >= 0; i-- {
		if i >= len(lines) {
			continue
		}
		line := strings.TrimSpace(lines[i])

		// Check for plugins: at step level
		if strings.HasPrefix(line, "plugins:") && s.getIndentLevel(lines[i]) > 0 {
			inPluginsSection = true
			break
		}

		// Check if we're in a step
		if strings.HasPrefix(strings.TrimLeft(lines[i], " \t"), "- ") && s.getIndentLevel(lines[i]) == 2 {
			inStepsSection = true
		}

		// Check for top-level sections
		if len(line) > 0 && lines[i][0] != ' ' && lines[i][0] != '\t' {
			break
		}
	}

	return inStepsSection && inPluginsSection
}

func (s *Server) isInStepContext(ctx *bkcontext.PositionContext) bool {
	// Check if we're configuring step properties
	lines := strings.Split(ctx.FullContent, "\n")
	currentLine := int(ctx.Position.Line)

	// Go backwards to find if we're in a step
	for i := currentLine; i >= 0; i-- {
		if i >= len(lines) {
			continue
		}
		line := lines[i]

		// Check if we're in a step (starts with "- " at 2-space indentation)
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), "- ") && s.getIndentLevel(line) == 2 {
			return true
		}

		// Check for top-level sections
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 0 && line[0] != ' ' && line[0] != '\t' {
			break
		}
	}

	return false
}

func (s *Server) getPluginSignatures(ctx *bkcontext.PositionContext) []protocol.SignatureInformation {
	var signatures []protocol.SignatureInformation

	// Detect which plugin we're configuring
	pluginName := s.detectPluginName(ctx)
	if pluginName == "" {
		return signatures
	}

	// Get plugin configuration from registry
	if pluginSchema, err := s.pluginRegistry.GetPluginSchema(pluginName); err == nil && pluginSchema != nil {
		signature := protocol.SignatureInformation{
			Label: fmt.Sprintf("%s plugin configuration", pluginName),
			Documentation: &protocol.MarkupContent{
				Kind:  protocol.Markdown,
				Value: fmt.Sprintf("Configuration options for the **%s** plugin", pluginName),
			},
			Parameters: s.getPluginParameters(pluginSchema),
		}
		signatures = append(signatures, signature)
	}

	return signatures
}

func (s *Server) getStepSignatures(ctx *bkcontext.PositionContext) []protocol.SignatureInformation {
	var signatures []protocol.SignatureInformation

	// Detect step type
	stepType := s.detectStepType(ctx)
	if stepType == "" {
		return signatures
	}

	signature := s.getStepTypeSignature(stepType)
	if signature != nil {
		signatures = append(signatures, *signature)
	}

	return signatures
}

func (s *Server) detectPluginName(ctx *bkcontext.PositionContext) string {
	lines := strings.Split(ctx.FullContent, "\n")
	currentLine := int(ctx.Position.Line)

	// Go backwards to find the plugin name
	for i := currentLine; i >= 0; i-- {
		if i >= len(lines) {
			continue
		}
		line := strings.TrimSpace(lines[i])

		// Look for plugin name (key before configuration)
		if strings.Contains(line, ":") && !strings.HasPrefix(line, "-") {
			parts := strings.Split(line, ":")
			if len(parts) > 0 {
				pluginName := strings.TrimSpace(parts[0])
				// Remove quotes if present
				pluginName = strings.Trim(pluginName, `"'`)
				return pluginName
			}
		}
	}

	return ""
}

func (s *Server) detectStepType(ctx *bkcontext.PositionContext) string {
	lines := strings.Split(ctx.FullContent, "\n")
	currentLine := int(ctx.Position.Line)

	// Look for step type in current step
	for i := currentLine; i >= 0; i-- {
		if i >= len(lines) {
			continue
		}

		// Stop at step boundary
		if strings.HasPrefix(strings.TrimLeft(lines[i], " \t"), "- ") && s.getIndentLevel(lines[i]) == 2 {
			// Look forward in this step for the type
			for j := i; j < len(lines) && j <= currentLine+10; j++ {
				stepLine := strings.TrimSpace(lines[j])
				if stepLine == "" {
					continue
				}

				// Stop if we hit another step or top-level property
				if j > i && (strings.HasPrefix(strings.TrimLeft(lines[j], " \t"), "- ") || (len(lines[j]) > 0 && lines[j][0] != ' ' && lines[j][0] != '\t')) {
					break
				}

				// Check for step type keywords (can be on step line with "- " or on separate line)
				if strings.Contains(stepLine, "command:") || strings.Contains(stepLine, "commands:") {
					return "command"
				}
				if strings.Contains(stepLine, "wait:") {
					return "wait"
				}
				if strings.Contains(stepLine, "block:") {
					return "block"
				}
				if strings.Contains(stepLine, "input:") {
					return "input"
				}
				if strings.Contains(stepLine, "trigger:") {
					return "trigger"
				}
				if strings.Contains(stepLine, "group:") {
					return "group"
				}
			}
			break
		}
	}

	return ""
}

func (s *Server) getPluginParameters(pluginSchema interface{}) []protocol.ParameterInformation {
	var parameters []protocol.ParameterInformation

	// This would be populated based on actual plugin schemas
	// For now, return generic parameters
	parameters = append(parameters, protocol.ParameterInformation{
		Label:         "configuration",
		Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Plugin configuration object"},
	})

	return parameters
}

func (s *Server) getStepTypeSignature(stepType string) *protocol.SignatureInformation {
	switch stepType {
	case "command":
		return &protocol.SignatureInformation{
			Label: "Command Step",
			Documentation: &protocol.MarkupContent{
				Kind:  protocol.Markdown,
				Value: "A command step runs shell commands on an agent",
			},
			Parameters: []protocol.ParameterInformation{
				{Label: "command", Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Shell command(s) to execute"}},
				{Label: "label", Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Display name for the step"}},
				{Label: "agents", Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Agent targeting rules"}},
				{Label: "env", Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Environment variables"}},
				{Label: "plugins", Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Plugins to use"}},
			},
		}
	case "wait":
		return &protocol.SignatureInformation{
			Label: "Wait Step",
			Documentation: &protocol.MarkupContent{
				Kind:  protocol.Markdown,
				Value: "A wait step waits for previous steps to complete",
			},
			Parameters: []protocol.ParameterInformation{
				{Label: "wait", Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Wait message or duration"}},
				{Label: "continue_on_failure", Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Continue if previous steps fail"}},
				{Label: "depends_on", Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Specific step dependencies"}},
			},
		}
	case "block":
		return &protocol.SignatureInformation{
			Label: "Block Step",
			Documentation: &protocol.MarkupContent{
				Kind:  protocol.Markdown,
				Value: "A block step pauses the pipeline and waits for manual approval",
			},
			Parameters: []protocol.ParameterInformation{
				{Label: "block", Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Block message shown to users"}},
				{Label: "prompt", Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Additional prompt text"}},
				{Label: "fields", Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Input fields for user interaction"}},
			},
		}
	case "input":
		return &protocol.SignatureInformation{
			Label: "Input Step",
			Documentation: &protocol.MarkupContent{
				Kind:  protocol.Markdown,
				Value: "An input step collects information from users",
			},
			Parameters: []protocol.ParameterInformation{
				{Label: "input", Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Input prompt message"}},
				{Label: "fields", Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Input field definitions"}},
			},
		}
	case "trigger":
		return &protocol.SignatureInformation{
			Label: "Trigger Step",
			Documentation: &protocol.MarkupContent{
				Kind:  protocol.Markdown,
				Value: "A trigger step starts another pipeline",
			},
			Parameters: []protocol.ParameterInformation{
				{Label: "trigger", Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Pipeline slug to trigger"}},
				{Label: "build", Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Build configuration"}},
				{Label: "async", Documentation: &protocol.MarkupContent{Kind: protocol.Markdown, Value: "Don't wait for completion"}},
			},
		}
	default:
		return nil
	}
}

func (s *Server) getActiveParameter(ctx *bkcontext.PositionContext, signature protocol.SignatureInformation) uint32 {
	// Simple implementation - could be enhanced to detect which parameter we're currently editing
	return 0
}

func (s *Server) getIndentLevel(line string) int {
	count := 0
	for _, char := range line {
		switch char {
		case ' ':
			count++
		case '\t':
			count += 4 // Treat tab as 4 spaces
		default:
			goto done
		}
	}
done:
	return count
}

func (s *Server) Definition(ctx context.Context, params *protocol.DefinitionParams) ([]protocol.Location, error) {
	s.logger.Printf("Definition requested for URI: %s, Position: %d:%d", params.TextDocument.URI, params.Position.Line, params.Position.Character)

	if !s.isBuildkiteFile(string(params.TextDocument.URI)) {
		s.logger.Printf("File is not a Buildkite file, skipping definition")
		return nil, nil
	}

	// Get document content and position context
	positionContext, err := s.documentManager.GetContentAtPosition(params.TextDocument.URI, params.Position)
	if err != nil {
		s.logger.Printf("Failed to get position context: %v", err)
		return nil, nil
	}

	s.logger.Printf("Definition context - Current line: '%s', Char index: %d", positionContext.CurrentLine, positionContext.CharIndex)

	// Find definitions based on what's under the cursor
	locations := s.findDefinitions(positionContext)

	s.logger.Printf("Found %d definition locations", len(locations))
	return locations, nil
}

func (s *Server) findDefinitions(ctx *bkcontext.PositionContext) []protocol.Location {
	var locations []protocol.Location

	// Get the word/identifier under the cursor
	word := s.getWordAtPosition(ctx)
	if word == "" {
		return locations
	}

	s.logger.Printf("Looking for definition of: '%s'", word)

	// Check if we're in a context where this could be a step reference
	if s.isStepReference(ctx, word) {
		if stepLocation := s.findStepDefinition(ctx, word); stepLocation != nil {
			locations = append(locations, *stepLocation)
		}
	}

	// Check if this could be a plugin reference
	if s.isPluginReference(ctx, word) {
		if pluginLocations := s.findPluginDefinitions(ctx, word); len(pluginLocations) > 0 {
			locations = append(locations, pluginLocations...)
		}
	}

	return locations
}

func (s *Server) getWordAtPosition(ctx *bkcontext.PositionContext) string {
	line := ctx.CurrentLine
	charIndex := ctx.CharIndex

	if charIndex >= len(line) {
		return ""
	}

	// Find word boundaries
	start := charIndex
	end := charIndex

	// Go backwards to find start of word
	for start > 0 && (isAlphaNumeric(line[start-1]) || line[start-1] == '-' || line[start-1] == '_') {
		start--
	}

	// Go forwards to find end of word
	for end < len(line) && (isAlphaNumeric(line[end]) || line[end] == '-' || line[end] == '_') {
		end++
	}

	// Extract the word, but also handle quoted strings
	word := line[start:end]

	// If the word is quoted, extract the content
	if len(word) >= 2 && (word[0] == '"' || word[0] == '\'') {
		quote := word[0]
		if word[len(word)-1] == quote {
			word = word[1 : len(word)-1]
		}
	}

	return word
}

func (s *Server) isStepReference(ctx *bkcontext.PositionContext, word string) bool {
	line := strings.TrimSpace(ctx.CurrentLine)

	// Check if we're in a depends_on array
	if strings.Contains(line, "depends_on") || strings.Contains(line, "- \""+word+"\"") || strings.Contains(line, "- '"+word+"'") {
		return true
	}

	// Check if we're in other step reference contexts
	// (could extend this for other places where step keys are referenced)

	return false
}

func (s *Server) isPluginReference(ctx *bkcontext.PositionContext, word string) bool {
	// Check if we're in a plugin configuration context
	// This could be in plugins array or plugin references
	lines := strings.Split(ctx.FullContent, "\n")
	currentLine := int(ctx.Position.Line)

	// Check if we're in a plugins section
	for i := currentLine; i >= 0; i-- {
		if i >= len(lines) {
			continue
		}
		line := strings.TrimSpace(lines[i])

		if strings.HasPrefix(line, "plugins:") {
			return true
		}

		// Stop at step boundary or top-level
		if strings.HasPrefix(strings.TrimLeft(lines[i], " \t"), "- ") && s.getIndentLevel(lines[i]) == 2 {
			break
		}
		if len(line) > 0 && lines[i][0] != ' ' && lines[i][0] != '\t' {
			break
		}
	}

	return false
}

func (s *Server) findStepDefinition(ctx *bkcontext.PositionContext, stepKey string) *protocol.Location {
	lines := strings.Split(ctx.FullContent, "\n")

	// Find all step definitions and look for one with matching key
	inSteps := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check for steps section
		if trimmed == "steps:" {
			inSteps = true
			continue
		}

		// Stop if we hit another top-level property
		if inSteps && len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			break
		}

		// Look for step definitions (- at step level indentation)
		if inSteps && strings.HasPrefix(strings.TrimLeft(line, " \t"), "- ") && s.getIndentLevel(line) == 2 {
			// Look ahead in this step for a key property
			foundStepKey := s.findStepKey(lines, i)
			if foundStepKey == stepKey {
				return &protocol.Location{
					URI: ctx.URI,
					Range: protocol.Range{
						Start: protocol.Position{Line: uint32(i), Character: 0},
						End:   protocol.Position{Line: uint32(i), Character: uint32(len(line))},
					},
				}
			}
		}
	}

	return nil
}

func (s *Server) findStepKey(lines []string, stepStartLine int) string {
	// Look through the step for a "key:" property
	for i := stepStartLine; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])

		// Stop if we hit another step or top-level property
		if i > stepStartLine && (strings.HasPrefix(strings.TrimLeft(lines[i], " \t"), "- ") || (len(lines[i]) > 0 && lines[i][0] != ' ' && lines[i][0] != '\t')) {
			break
		}

		// Look for key: property
		if strings.Contains(line, "key:") {
			// Find the key: part and extract the value
			keyStart := strings.Index(line, "key:")
			if keyStart != -1 {
				keyPart := line[keyStart:]
				parts := strings.SplitN(keyPart, ":", 2)
				if len(parts) == 2 {
					keyValue := strings.TrimSpace(parts[1])
					// Remove quotes
					keyValue = strings.Trim(keyValue, `"'`)
					return keyValue
				}
			}
		}
	}

	// If no explicit key, try to generate one from label
	for i := stepStartLine; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])

		// Stop if we hit another step or top-level property
		if i > stepStartLine && (strings.HasPrefix(strings.TrimLeft(lines[i], " \t"), "- ") || (len(lines[i]) > 0 && lines[i][0] != ' ' && lines[i][0] != '\t')) {
			break
		}

		// Look for label: property and generate key from it
		if strings.Contains(line, "label:") {
			// Find the label: part and extract the value
			labelStart := strings.Index(line, "label:")
			if labelStart != -1 {
				labelPart := line[labelStart:]
				parts := strings.SplitN(labelPart, ":", 2)
				if len(parts) == 2 {
					labelValue := strings.TrimSpace(parts[1])
					// Remove quotes
					labelValue = strings.Trim(labelValue, `"'`)
					// Convert to key format (lowercase, replace spaces with dashes)
					key := strings.ToLower(labelValue)
					key = strings.ReplaceAll(key, " ", "-")
					key = strings.ReplaceAll(key, ":", "")
					return key
				}
			}
		}
	}

	return ""
}

func (s *Server) findPluginDefinitions(ctx *bkcontext.PositionContext, pluginName string) []protocol.Location {
	var locations []protocol.Location

	// For now, return empty - this could be extended to:
	// 1. Find other uses of the same plugin in the pipeline
	// 2. Link to external plugin repositories
	// 3. Show plugin schema definitions

	s.logger.Printf("Plugin definition search for '%s' not yet implemented", pluginName)
	return locations
}

func isAlphaNumeric(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func (s *Server) CodeAction(ctx context.Context, params *protocol.CodeActionParams) ([]protocol.CodeAction, error) {
	s.logger.Printf("CodeAction requested for URI: %s, Range: %d:%d-%d:%d",
		params.TextDocument.URI,
		params.Range.Start.Line, params.Range.Start.Character,
		params.Range.End.Line, params.Range.End.Character)

	if !s.isBuildkiteFile(string(params.TextDocument.URI)) {
		s.logger.Printf("File is not a Buildkite file, skipping code actions")
		return nil, nil
	}

	var actions []protocol.CodeAction

	// Get document content
	doc, exists := s.documentManager.GetDocument(params.TextDocument.URI)
	if !exists {
		s.logger.Printf("Document not found: %s", params.TextDocument.URI)
		return nil, nil
	}

	// Generate code actions based on diagnostics in the range
	actions = append(actions, s.getQuickFixActions(params, doc)...)

	// Generate refactoring actions based on context
	actions = append(actions, s.getRefactorActions(params, doc)...)

	s.logger.Printf("Generated %d code actions", len(actions))
	return actions, nil
}

func (s *Server) getQuickFixActions(params *protocol.CodeActionParams, doc *Document) []protocol.CodeAction {
	var actions []protocol.CodeAction

	lines := doc.Lines

	// Check if we're in a step context
	stepInfo := s.analyzeStepAtRange(params.Range, lines)
	if stepInfo == nil {
		return actions
	}

	// Quick fix: Convert name to label
	if stepInfo.HasName && !stepInfo.HasLabel {
		actions = append(actions, s.createConvertNameToLabelAction(params.TextDocument.URI, stepInfo))
	}

	// Quick fix: Add missing label
	if !stepInfo.HasLabel && !stepInfo.HasName && stepInfo.IsCommandStep {
		actions = append(actions, s.createAddLabelAction(params.TextDocument.URI, stepInfo))
	}

	// Quick fix: Add missing key
	if !stepInfo.HasKey && (stepInfo.HasLabel || stepInfo.HasName) {
		actions = append(actions, s.createAddKeyAction(params.TextDocument.URI, stepInfo))
	}

	// Quick fix: Fix empty command
	if stepInfo.IsCommandStep && stepInfo.HasEmptyCommand {
		actions = append(actions, s.createFixEmptyCommandAction(params.TextDocument.URI, stepInfo))
	}

	// Quick fix: Add step type for steps missing type
	if !stepInfo.HasStepType {
		actions = append(actions, s.createAddStepTypeAction(params.TextDocument.URI, stepInfo))
	}

	return actions
}

func (s *Server) getRefactorActions(params *protocol.CodeActionParams, doc *Document) []protocol.CodeAction {
	var actions []protocol.CodeAction

	lines := doc.Lines

	// Check if we're in a step context
	stepInfo := s.analyzeStepAtRange(params.Range, lines)
	if stepInfo == nil {
		return actions
	}

	// Refactor: Convert single command to commands array
	if stepInfo.IsCommandStep && stepInfo.HasSingleCommand {
		actions = append(actions, s.createConvertToCommandsArrayAction(params.TextDocument.URI, stepInfo))
	}

	// Refactor: Extract step to separate step with dependency
	if stepInfo.IsCommandStep {
		actions = append(actions, s.createExtractStepAction(params.TextDocument.URI, stepInfo))
	}

	return actions
}

type StepInfo struct {
	StartLine        int
	EndLine          int
	IsCommandStep    bool
	HasLabel         bool
	HasKey           bool
	HasName          bool
	HasStepType      bool
	HasEmptyCommand  bool
	HasSingleCommand bool
	LabelLine        int
	CommandLine      int
	StepTypeLine     int
	NameLine         int
}

func (s *Server) analyzeStepAtRange(rang protocol.Range, lines []string) *StepInfo {
	startLine := int(rang.Start.Line)

	// Find the step that contains this range
	stepStart := -1
	stepEnd := -1
	inSteps := false

	// First, check if we're already on a step start line
	if startLine < len(lines) {
		currentLine := lines[startLine]
		if strings.HasPrefix(strings.TrimLeft(currentLine, " \t"), "- ") && s.getIndentLevel(currentLine) == 2 {
			// Check if we're in steps section by looking backwards for "steps:"
			for i := startLine - 1; i >= 0; i-- {
				line := strings.TrimSpace(lines[i])
				if line == "steps:" {
					inSteps = true
					stepStart = startLine
					break
				}
				// Stop at top-level properties
				if len(line) > 0 && lines[i][0] != ' ' && lines[i][0] != '\t' {
					break
				}
			}
		}
	}

	// If not on a step start line, go backwards to find one
	if stepStart == -1 {
		for i := startLine; i >= 0; i-- {
			if i >= len(lines) {
				continue
			}
			line := strings.TrimSpace(lines[i])

			// Check for steps section
			if line == "steps:" {
				inSteps = true
				continue
			}

			// Check if this is a step start
			if inSteps && strings.HasPrefix(strings.TrimLeft(lines[i], " \t"), "- ") && s.getIndentLevel(lines[i]) == 2 {
				stepStart = i
				break
			}

			// Stop at top-level properties
			if !inSteps && len(line) > 0 && lines[i][0] != ' ' && lines[i][0] != '\t' {
				break
			}
		}
	}

	if stepStart == -1 {
		return nil
	}

	// Find step end
	for i := stepStart + 1; i < len(lines); i++ {
		line := lines[i]

		// Stop at next step or top-level property
		if (strings.HasPrefix(strings.TrimLeft(line, " \t"), "- ") && s.getIndentLevel(line) == 2) ||
			(len(strings.TrimSpace(line)) > 0 && line[0] != ' ' && line[0] != '\t') {
			stepEnd = i - 1
			break
		}
	}

	if stepEnd == -1 {
		stepEnd = len(lines) - 1
	}

	// Analyze step content
	info := &StepInfo{
		StartLine: stepStart,
		EndLine:   stepEnd,
	}

	for i := stepStart; i <= stepEnd && i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])

		if strings.Contains(line, "label:") {
			info.HasLabel = true
			info.LabelLine = i
		}

		if strings.Contains(line, "name:") {
			info.HasName = true
			info.NameLine = i
		}

		if strings.Contains(line, "key:") {
			info.HasKey = true
		}

		if strings.Contains(line, "command:") {
			info.IsCommandStep = true
			info.HasStepType = true
			info.CommandLine = i
			info.StepTypeLine = i

			// Check if command is empty
			if strings.Contains(line, "command:") {
				parts := strings.SplitN(line, "command:", 2)
				if len(parts) == 2 {
					cmdValue := strings.TrimSpace(parts[1])
					cmdValue = strings.Trim(cmdValue, `"'`)
					if cmdValue == "" {
						info.HasEmptyCommand = true
					} else {
						info.HasSingleCommand = true
					}
				}
			}
		}

		// Check other step types
		if strings.Contains(line, "wait:") || strings.Contains(line, "block:") ||
			strings.Contains(line, "input:") || strings.Contains(line, "trigger:") ||
			strings.Contains(line, "group:") {
			info.HasStepType = true
			info.StepTypeLine = i
		}
	}

	return info
}

func (s *Server) createConvertNameToLabelAction(uri protocol.DocumentURI, stepInfo *StepInfo) protocol.CodeAction {
	// Get the document to read the actual line content
	doc, exists := s.documentManager.GetDocument(uri)
	if !exists || stepInfo.NameLine >= len(doc.Lines) {
		// Fallback if we can't read the document
		return protocol.CodeAction{
			Title: "Convert 'name' to 'label'",
			Kind:  protocol.QuickFix,
			Edit: &protocol.WorkspaceEdit{
				Changes: map[protocol.DocumentURI][]protocol.TextEdit{
					uri: {
						{
							Range: protocol.Range{
								Start: protocol.Position{Line: uint32(stepInfo.NameLine), Character: 0},
								End:   protocol.Position{Line: uint32(stepInfo.NameLine), Character: 999},
							},
							NewText: "    label: \"TODO: Add label\"",
						},
					},
				},
			},
		}
	}

	// Read the actual line and replace 'name:' with 'label:'
	originalLine := doc.Lines[stepInfo.NameLine]
	newLine := strings.Replace(originalLine, "name:", "label:", 1)

	return protocol.CodeAction{
		Title: "Convert 'name' to 'label'",
		Kind:  protocol.QuickFix,
		Edit: &protocol.WorkspaceEdit{
			Changes: map[protocol.DocumentURI][]protocol.TextEdit{
				uri: {
					{
						Range: protocol.Range{
							Start: protocol.Position{Line: uint32(stepInfo.NameLine), Character: 0},
							End:   protocol.Position{Line: uint32(stepInfo.NameLine), Character: uint32(len(originalLine))},
						},
						NewText: newLine,
					},
				},
			},
		},
	}
}

func (s *Server) createAddLabelAction(uri protocol.DocumentURI, stepInfo *StepInfo) protocol.CodeAction {
	// Generate a suggested label based on the step content or position
	suggestedLabel := fmt.Sprintf("Step %d", stepInfo.StartLine)

	// Insert label after the step start line
	insertLine := stepInfo.StartLine + 1
	newText := fmt.Sprintf("    label: \"%s\"\n", suggestedLabel)

	return protocol.CodeAction{
		Title: "Add label to step",
		Kind:  protocol.QuickFix,
		Edit: &protocol.WorkspaceEdit{
			Changes: map[protocol.DocumentURI][]protocol.TextEdit{
				uri: {
					{
						Range: protocol.Range{
							Start: protocol.Position{Line: uint32(insertLine), Character: 0},
							End:   protocol.Position{Line: uint32(insertLine), Character: 0},
						},
						NewText: newText,
					},
				},
			},
		},
	}
}

func (s *Server) createAddKeyAction(uri protocol.DocumentURI, stepInfo *StepInfo) protocol.CodeAction {
	// Generate key from label if possible, otherwise use generic key
	suggestedKey := fmt.Sprintf("step-%d", stepInfo.StartLine)

	// Insert key after label line
	insertLine := stepInfo.LabelLine + 1
	newText := fmt.Sprintf("    key: \"%s\"\n", suggestedKey)

	return protocol.CodeAction{
		Title: "Add key to step",
		Kind:  protocol.QuickFix,
		Edit: &protocol.WorkspaceEdit{
			Changes: map[protocol.DocumentURI][]protocol.TextEdit{
				uri: {
					{
						Range: protocol.Range{
							Start: protocol.Position{Line: uint32(insertLine), Character: 0},
							End:   protocol.Position{Line: uint32(insertLine), Character: 0},
						},
						NewText: newText,
					},
				},
			},
		},
	}
}

func (s *Server) createFixEmptyCommandAction(uri protocol.DocumentURI, stepInfo *StepInfo) protocol.CodeAction {
	// Replace empty command with placeholder
	newText := `    command: "echo 'TODO: Add command'"`

	return protocol.CodeAction{
		Title: "Fix empty command",
		Kind:  protocol.QuickFix,
		Edit: &protocol.WorkspaceEdit{
			Changes: map[protocol.DocumentURI][]protocol.TextEdit{
				uri: {
					{
						Range: protocol.Range{
							Start: protocol.Position{Line: uint32(stepInfo.CommandLine), Character: 0},
							End:   protocol.Position{Line: uint32(stepInfo.CommandLine + 1), Character: 0},
						},
						NewText: newText + "\n",
					},
				},
			},
		},
	}
}

func (s *Server) createAddStepTypeAction(uri protocol.DocumentURI, stepInfo *StepInfo) protocol.CodeAction {
	// Add command as default step type
	insertLine := stepInfo.StartLine + 1
	newText := `    command: "echo 'TODO: Add command'"` + "\n"

	return protocol.CodeAction{
		Title: "Add command to step",
		Kind:  protocol.QuickFix,
		Edit: &protocol.WorkspaceEdit{
			Changes: map[protocol.DocumentURI][]protocol.TextEdit{
				uri: {
					{
						Range: protocol.Range{
							Start: protocol.Position{Line: uint32(insertLine), Character: 0},
							End:   protocol.Position{Line: uint32(insertLine), Character: 0},
						},
						NewText: newText,
					},
				},
			},
		},
	}
}

func (s *Server) createConvertToCommandsArrayAction(uri protocol.DocumentURI, stepInfo *StepInfo) protocol.CodeAction {
	// Convert single command to commands array
	newText := `    commands:
      - "echo 'TODO: Add first command'"
      - "echo 'TODO: Add second command'"`

	return protocol.CodeAction{
		Title: "Convert to commands array",
		Kind:  protocol.RefactorRewrite,
		Edit: &protocol.WorkspaceEdit{
			Changes: map[protocol.DocumentURI][]protocol.TextEdit{
				uri: {
					{
						Range: protocol.Range{
							Start: protocol.Position{Line: uint32(stepInfo.CommandLine), Character: 0},
							End:   protocol.Position{Line: uint32(stepInfo.CommandLine + 1), Character: 0},
						},
						NewText: newText + "\n",
					},
				},
			},
		},
	}
}

func (s *Server) createExtractStepAction(uri protocol.DocumentURI, stepInfo *StepInfo) protocol.CodeAction {
	// This is a more complex refactoring - for now, just provide a placeholder
	return protocol.CodeAction{
		Title: "Extract to separate step",
		Kind:  protocol.Refactor,
		// Would implement full step extraction logic here
	}
}

func (s *Server) DocumentSymbol(ctx context.Context, params *protocol.DocumentSymbolParams) ([]protocol.DocumentSymbol, error) {
	if !s.isBuildkiteFile(string(params.TextDocument.URI)) {
		return nil, nil
	}

	// Get document content
	doc, exists := s.documentManager.GetDocument(params.TextDocument.URI)
	if !exists {
		return nil, fmt.Errorf("document not found: %s", params.TextDocument.URI)
	}

	// Parse YAML to extract symbols
	symbols, err := s.extractDocumentSymbols(doc.Content, doc.Lines)
	if err != nil {
		s.logger.Printf("Failed to extract document symbols: %v", err)
		return nil, nil // Return nil instead of error to avoid disrupting the user
	}

	return symbols, nil
}

func (s *Server) extractDocumentSymbols(content string, lines []string) ([]protocol.DocumentSymbol, error) {
	// Parse YAML first to validate it
	_, err := parser.ParseYAML([]byte(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	var symbols []protocol.DocumentSymbol

	// Extract top-level pipeline symbols
	if envSymbol := s.extractEnvSymbol(lines); envSymbol != nil {
		symbols = append(symbols, *envSymbol)
	}

	if agentsSymbol := s.extractAgentsSymbol(lines); agentsSymbol != nil {
		symbols = append(symbols, *agentsSymbol)
	}

	// Extract steps (the most important part)
	if stepsSymbol := s.extractStepsSymbol(lines); stepsSymbol != nil {
		symbols = append(symbols, *stepsSymbol)
	}

	// Extract other top-level properties
	otherSymbols := s.extractOtherTopLevelSymbols(lines)
	symbols = append(symbols, otherSymbols...)

	return symbols, nil
}

func (s *Server) extractStepsSymbol(lines []string) *protocol.DocumentSymbol {
	stepsLine := -1

	// Find the steps: line
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "steps:" {
			stepsLine = i
			break
		}
	}

	if stepsLine == -1 {
		return nil
	}

	// Extract individual steps as children
	var children []protocol.DocumentSymbol
	currentStepStart := -1
	stepIndex := 0

	for i := stepsLine + 1; i < len(lines); i++ {
		line := lines[i]

		// Stop if we hit another top-level property
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			break
		}

		// Look for step indicators (- at step level indentation)
		// Steps should be at 2-space indentation level under "steps:"
		trimmedLeft := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmedLeft, "- ") {
			// Check if this is at the correct step indentation level (2 spaces)
			// Count leading spaces
			leadingSpaces := len(line) - len(strings.TrimLeft(line, " "))
			if leadingSpaces == 2 {
				// Finish previous step if exists
				if currentStepStart != -1 {
					stepSymbol := s.createStepSymbol(lines, stepIndex, currentStepStart, i-1)
					if stepSymbol != nil {
						children = append(children, *stepSymbol)
					}
					stepIndex++
				}
				currentStepStart = i
			}
		}
	}

	// Don't forget the last step
	if currentStepStart != -1 {
		stepSymbol := s.createStepSymbol(lines, stepIndex, currentStepStart, len(lines)-1)
		if stepSymbol != nil {
			children = append(children, *stepSymbol)
		}
	}

	// Calculate end line for steps section
	endLine := len(lines) - 1
	for i := stepsLine + 1; i < len(lines); i++ {
		line := lines[i]
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			endLine = i - 1
			break
		}
	}

	return &protocol.DocumentSymbol{
		Name: fmt.Sprintf("steps (%d)", len(children)),
		Kind: protocol.SymbolKindArray,
		Range: protocol.Range{
			Start: protocol.Position{Line: uint32(stepsLine), Character: 0},
			End:   protocol.Position{Line: uint32(endLine), Character: 0},
		},
		SelectionRange: protocol.Range{
			Start: protocol.Position{Line: uint32(stepsLine), Character: 0},
			End:   protocol.Position{Line: uint32(stepsLine), Character: uint32(len("steps:"))},
		},
		Children: children,
	}
}

func (s *Server) createStepSymbol(lines []string, index int, startLine, endLine int) *protocol.DocumentSymbol {
	if startLine >= len(lines) {
		return nil
	}

	// Determine step type and label
	stepType := "Step"
	stepLabel := fmt.Sprintf("Step %d", index+1)
	stepKind := protocol.SymbolKindObject

	// Analyze step content to get a better name
	for i := startLine; i <= endLine && i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])

		// Look for different step types
		if strings.HasPrefix(line, "label:") || strings.HasPrefix(line, "- label:") {
			if label := extractQuotedValue(line); label != "" {
				stepLabel = label
				stepType = "Command Step"
			}
		} else if strings.HasPrefix(line, "wait:") || line == "wait" || strings.HasPrefix(line, "- wait") {
			stepType = "Wait"
			stepLabel = "Wait Step"
			stepKind = protocol.SymbolKindEvent
			if value := extractQuotedValue(line); value != "" {
				stepLabel = fmt.Sprintf("Wait: %s", value)
			}
		} else if strings.HasPrefix(line, "block:") || strings.HasPrefix(line, "- block:") {
			stepType = "Block"
			stepLabel = "Manual Approval"
			stepKind = protocol.SymbolKindEvent
			if value := extractQuotedValue(line); value != "" {
				stepLabel = fmt.Sprintf("Block: %s", value)
			}
		} else if strings.HasPrefix(line, "input:") || strings.HasPrefix(line, "- input:") {
			stepType = "Input"
			stepLabel = "Input Step"
			stepKind = protocol.SymbolKindEvent
			if value := extractQuotedValue(line); value != "" {
				stepLabel = fmt.Sprintf("Input: %s", value)
			}
		} else if strings.HasPrefix(line, "trigger:") || strings.HasPrefix(line, "- trigger:") {
			stepType = "Trigger"
			stepLabel = "Trigger Step"
			stepKind = protocol.SymbolKindEvent
			if value := extractQuotedValue(line); value != "" {
				stepLabel = fmt.Sprintf("Trigger: %s", value)
			}
		}
	}

	return &protocol.DocumentSymbol{
		Name: stepLabel,
		Kind: stepKind,
		Range: protocol.Range{
			Start: protocol.Position{Line: uint32(startLine), Character: 0},
			End:   protocol.Position{Line: uint32(endLine), Character: 0},
		},
		SelectionRange: protocol.Range{
			Start: protocol.Position{Line: uint32(startLine), Character: 0},
			End:   protocol.Position{Line: uint32(startLine), Character: uint32(len(lines[startLine]))},
		},
		Detail: stepType,
	}
}

func (s *Server) extractEnvSymbol(lines []string) *protocol.DocumentSymbol {
	return s.extractTopLevelSymbol(lines, "env", protocol.SymbolKindObject, "Environment Variables")
}

func (s *Server) extractAgentsSymbol(lines []string) *protocol.DocumentSymbol {
	return s.extractTopLevelSymbol(lines, "agents", protocol.SymbolKindObject, "Agent Requirements")
}

func (s *Server) extractOtherTopLevelSymbols(lines []string) []protocol.DocumentSymbol {
	var symbols []protocol.DocumentSymbol

	properties := []struct {
		name   string
		kind   protocol.SymbolKind
		detail string
	}{
		{"notify", protocol.SymbolKindObject, "Notifications"},
		{"skip", protocol.SymbolKindString, "Skip Condition"},
		{"group", protocol.SymbolKindObject, "Pipeline Group"},
		{"timeout_in_minutes", protocol.SymbolKindNumber, "Pipeline Timeout"},
	}

	for _, prop := range properties {
		if symbol := s.extractTopLevelSymbol(lines, prop.name, prop.kind, prop.detail); symbol != nil {
			symbols = append(symbols, *symbol)
		}
	}

	return symbols
}

func (s *Server) extractTopLevelSymbol(lines []string, property string, kind protocol.SymbolKind, detail string) *protocol.DocumentSymbol {
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == property+":" || strings.HasPrefix(trimmed, property+":") {
			// Find end of this property section
			endLine := i
			for j := i + 1; j < len(lines); j++ {
				nextLine := lines[j]
				if len(nextLine) > 0 && nextLine[0] != ' ' && nextLine[0] != '\t' {
					endLine = j - 1
					break
				}
				if j == len(lines)-1 {
					endLine = j
				}
			}

			return &protocol.DocumentSymbol{
				Name: property,
				Kind: kind,
				Range: protocol.Range{
					Start: protocol.Position{Line: uint32(i), Character: 0},
					End:   protocol.Position{Line: uint32(endLine), Character: 0},
				},
				SelectionRange: protocol.Range{
					Start: protocol.Position{Line: uint32(i), Character: 0},
					End:   protocol.Position{Line: uint32(i), Character: uint32(len(property))},
				},
				Detail: detail,
			}
		}
	}
	return nil
}

// Helper function to extract quoted values from YAML lines
func extractQuotedValue(line string) string {
	// Look for quoted values after colon
	if colonIndex := strings.Index(line, ":"); colonIndex != -1 {
		afterColon := strings.TrimSpace(line[colonIndex+1:])
		if len(afterColon) >= 2 && (afterColon[0] == '"' || afterColon[0] == '\'') {
			quote := afterColon[0]
			if endQuote := strings.LastIndex(afterColon, string(quote)); endQuote > 0 {
				return afterColon[1:endQuote]
			}
		}
		// Handle unquoted values
		if afterColon != "" && !strings.HasPrefix(afterColon, "[") && !strings.HasPrefix(afterColon, "{") {
			return afterColon
		}
	}
	return ""
}

func (s *Server) validateDocument(ctx context.Context, uri protocol.DocumentURI, content string) {
	if !s.isBuildkiteFile(string(uri)) {
		return
	}

	pipeline, err := parser.ParseYAML([]byte(content))
	if err != nil {
		s.sendDiagnostics(ctx, uri, []protocol.Diagnostic{
			{
				Range: protocol.Range{
					Start: protocol.Position{Line: 0, Character: 0},
					End:   protocol.Position{Line: 0, Character: 0},
				},
				Severity: protocol.DiagnosticSeverityError,
				Message:  "YAML parse error: " + err.Error(),
			},
		})
		return
	}

	validationErr, err := s.schemaLoader.ValidateJSON(pipeline.JSONBytes)
	if err != nil {
		s.sendDiagnostics(ctx, uri, []protocol.Diagnostic{
			{
				Range: protocol.Range{
					Start: protocol.Position{Line: 0, Character: 0},
					End:   protocol.Position{Line: 0, Character: 0},
				},
				Severity: protocol.DiagnosticSeverityError,
				Message:  "Schema loading error: " + err.Error(),
			},
		})
		return
	}

	if validationErr != nil {
		line := pipeline.GetLineForError(validationErr.Message)
		s.sendDiagnostics(ctx, uri, []protocol.Diagnostic{
			{
				Range: protocol.Range{
					Start: protocol.Position{Line: uint32(line - 1), Character: 0},
					End:   protocol.Position{Line: uint32(line - 1), Character: 999},
				},
				Severity: protocol.DiagnosticSeverityError,
				Message:  "Schema validation error: " + validationErr.Message,
			},
		})
		return
	}

	// All basic schema validation passed, now validate plugins
	diagnostics := s.validatePlugins(pipeline)

	// If no validation errors, send empty diagnostics to clear any existing ones
	s.sendDiagnostics(ctx, uri, diagnostics)
}

func (s *Server) validatePlugins(pipeline *parser.Pipeline) []protocol.Diagnostic {
	var diagnostics []protocol.Diagnostic

	// Parse the pipeline JSON to extract steps with plugins
	var pipelineData map[string]interface{}
	if err := json.Unmarshal(pipeline.JSONBytes, &pipelineData); err != nil {
		return diagnostics
	}

	// Enhanced validation with multiple checks
	lines := strings.Split(string(pipeline.Content), "\n")
	diagnostics = append(diagnostics, s.validatePipelineStructure(pipelineData, lines)...)
	diagnostics = append(diagnostics, s.validateSteps(pipelineData, lines)...)
	diagnostics = append(diagnostics, s.validatePluginConfigurations(pipelineData, lines)...)

	return diagnostics
}

func (s *Server) validatePipelineStructure(pipelineData map[string]interface{}, lines []string) []protocol.Diagnostic {
	var diagnostics []protocol.Diagnostic

	// Check for required fields
	if _, hasSteps := pipelineData["steps"]; !hasSteps {
		// Find where to suggest adding steps
		lineNum := s.findLineForProperty("steps", lines)
		diagnostics = append(diagnostics, protocol.Diagnostic{
			Range: protocol.Range{
				Start: protocol.Position{Line: uint32(lineNum), Character: 0},
				End:   protocol.Position{Line: uint32(lineNum), Character: uint32(len(lines[lineNum]))},
			},
			Severity: protocol.DiagnosticSeverityError,
			Message:  "Pipeline must contain a 'steps' array",
			Source:   "buildkite-ls",
			Code:     "missing-steps",
		})
	}

	// Validate common properties
	if env, hasEnv := pipelineData["env"]; hasEnv {
		if _, ok := env.(map[string]interface{}); !ok {
			lineNum := s.findLineForProperty("env", lines)
			diagnostics = append(diagnostics, protocol.Diagnostic{
				Range: protocol.Range{
					Start: protocol.Position{Line: uint32(lineNum), Character: 0},
					End:   protocol.Position{Line: uint32(lineNum), Character: uint32(len(lines[lineNum]))},
				},
				Severity: protocol.DiagnosticSeverityError,
				Message:  "Environment variables must be an object with string keys and values",
				Source:   "buildkite-ls",
				Code:     "invalid-env",
			})
		}
	}

	return diagnostics
}

func (s *Server) validateSteps(pipelineData map[string]interface{}, lines []string) []protocol.Diagnostic {
	var diagnostics []protocol.Diagnostic

	steps, ok := pipelineData["steps"].([]interface{})
	if !ok {
		return diagnostics
	}

	stepLines := s.findStepLines(lines)

	for stepIndex, stepItem := range steps {
		stepData, ok := stepItem.(map[string]interface{})
		if !ok {
			continue
		}

		// Get the actual line number for this step
		lineNum := uint32(stepIndex)
		if stepIndex < len(stepLines) {
			lineNum = uint32(stepLines[stepIndex])
		}

		// Validate step structure
		diagnostics = append(diagnostics, s.validateSingleStep(stepData, lineNum, stepIndex+1)...)
	}

	return diagnostics
}

func (s *Server) validateSingleStep(stepData map[string]interface{}, lineNum uint32, stepNumber int) []protocol.Diagnostic {
	var diagnostics []protocol.Diagnostic

	// Check for step type - must have one of: command, wait, block, input, trigger, group
	hasCommand := stepData["command"] != nil || stepData["commands"] != nil
	_, hasWait := stepData["wait"] // wait key exists (value can be nil)
	hasBlock := stepData["block"] != nil
	hasInput := stepData["input"] != nil
	hasTrigger := stepData["trigger"] != nil
	hasGroup := stepData["group"] != nil

	stepTypeCount := 0
	if hasCommand {
		stepTypeCount++
	}
	if hasWait {
		stepTypeCount++
	}
	if hasBlock {
		stepTypeCount++
	}
	if hasInput {
		stepTypeCount++
	}
	if hasTrigger {
		stepTypeCount++
	}
	if hasGroup {
		stepTypeCount++
	}

	if stepTypeCount == 0 {
		// Check if step has plugins that might provide command execution
		hasPlugins := stepData["plugins"] != nil

		if hasPlugins {
			// If step has plugins, this is just a warning since plugins might provide commands via hooks
			diagnostics = append(diagnostics, protocol.Diagnostic{
				Range: protocol.Range{
					Start: protocol.Position{Line: lineNum, Character: 2},
					End:   protocol.Position{Line: lineNum + 2, Character: 0},
				},
				Severity: protocol.DiagnosticSeverityInformation,
				Message:  fmt.Sprintf("Step %d has no explicit step type, but plugins may provide command execution via hooks", stepNumber),
				Source:   "buildkite-ls",
				Code:     "no-step-type-with-plugins",
			})
		} else {
			// No plugins, so missing step type is an error
			diagnostics = append(diagnostics, protocol.Diagnostic{
				Range: protocol.Range{
					Start: protocol.Position{Line: lineNum, Character: 2},
					End:   protocol.Position{Line: lineNum + 2, Character: 0},
				},
				Severity: protocol.DiagnosticSeverityError,
				Message:  fmt.Sprintf("Step %d must specify a step type: command, wait, block, input, trigger, or group", stepNumber),
				Source:   "buildkite-ls",
				Code:     "missing-step-type",
			})
		}
	} else if stepTypeCount > 1 {
		diagnostics = append(diagnostics, protocol.Diagnostic{
			Range: protocol.Range{
				Start: protocol.Position{Line: lineNum, Character: 2},
				End:   protocol.Position{Line: lineNum + 3, Character: 0},
			},
			Severity: protocol.DiagnosticSeverityError,
			Message:  fmt.Sprintf("Step %d has multiple step types - only one is allowed per step", stepNumber),
			Source:   "buildkite-ls",
			Code:     "multiple-step-types",
		})
	}

	// Validate command steps
	if hasCommand {
		if command, ok := stepData["command"].(string); ok && strings.TrimSpace(command) == "" {
			// Check if step has plugins that might provide command execution (like hooks/command)
			hasPlugins := stepData["plugins"] != nil

			if hasPlugins {
				// If step has plugins, this is just an info message since plugins might provide commands
				diagnostics = append(diagnostics, protocol.Diagnostic{
					Range: protocol.Range{
						Start: protocol.Position{Line: lineNum + 1, Character: 4},
						End:   protocol.Position{Line: lineNum + 1, Character: 999},
					},
					Severity: protocol.DiagnosticSeverityInformation,
					Message:  "Command is empty, but plugins may provide command execution via hooks",
					Source:   "buildkite-ls",
					Code:     "empty-command-with-plugins",
				})
			} else {
				// No plugins, so empty command is likely an error
				diagnostics = append(diagnostics, protocol.Diagnostic{
					Range: protocol.Range{
						Start: protocol.Position{Line: lineNum + 1, Character: 4},
						End:   protocol.Position{Line: lineNum + 1, Character: 999},
					},
					Severity: protocol.DiagnosticSeverityWarning,
					Message:  "Command should not be empty",
					Source:   "buildkite-ls",
					Code:     "empty-command",
				})
			}
		}

		// Check for 'name' field instead of 'label'
		if stepData["name"] != nil && stepData["label"] == nil {
			diagnostics = append(diagnostics, protocol.Diagnostic{
				Range: protocol.Range{
					Start: protocol.Position{Line: lineNum, Character: 2},
					End:   protocol.Position{Line: lineNum + 2, Character: 0},
				},
				Severity: protocol.DiagnosticSeverityInformation,
				Message:  "Use 'label' instead of 'name' - 'label' is the standard Buildkite field for step display names",
				Source:   "buildkite-ls",
				Code:     "use-label-not-name",
			})
		}

		// Warn about missing labels (only if no name field exists)
		if stepData["label"] == nil && stepData["name"] == nil {
			diagnostics = append(diagnostics, protocol.Diagnostic{
				Range: protocol.Range{
					Start: protocol.Position{Line: lineNum, Character: 2},
					End:   protocol.Position{Line: lineNum, Character: 999},
				},
				Severity: protocol.DiagnosticSeverityInformation,
				Message:  "Consider adding a 'label' to make this step easier to identify in the UI",
				Source:   "buildkite-ls",
				Code:     "missing-label",
			})
		}
	}

	// Validate wait steps
	if hasWait {
		// wait can be null, string, or number - anything else is invalid
		wait := stepData["wait"]
		switch v := wait.(type) {
		case string:
			// Valid - wait message
		case float64:
			// Valid - wait time in seconds
		case nil:
			// Valid - basic wait
		default:
			diagnostics = append(diagnostics, protocol.Diagnostic{
				Range: protocol.Range{
					Start: protocol.Position{Line: lineNum + 1, Character: 4},
					End:   protocol.Position{Line: lineNum + 1, Character: 999},
				},
				Severity: protocol.DiagnosticSeverityError,
				Message:  fmt.Sprintf("Wait value must be null, a string message, or a number of seconds, got %T", v),
				Source:   "buildkite-ls",
				Code:     "invalid-wait-value",
			})
		}
	}

	// Validate block steps
	if hasBlock {
		if block, ok := stepData["block"].(string); !ok || strings.TrimSpace(block) == "" {
			diagnostics = append(diagnostics, protocol.Diagnostic{
				Range: protocol.Range{
					Start: protocol.Position{Line: lineNum + 1, Character: 4},
					End:   protocol.Position{Line: lineNum + 1, Character: 999},
				},
				Severity: protocol.DiagnosticSeverityError,
				Message:  "Block step must have a non-empty message",
				Source:   "buildkite-ls",
				Code:     "empty-block-message",
			})
		}
	}

	// Validate trigger steps
	if hasTrigger {
		if trigger, ok := stepData["trigger"].(string); !ok || strings.TrimSpace(trigger) == "" {
			diagnostics = append(diagnostics, protocol.Diagnostic{
				Range: protocol.Range{
					Start: protocol.Position{Line: lineNum + 1, Character: 4},
					End:   protocol.Position{Line: lineNum + 1, Character: 999},
				},
				Severity: protocol.DiagnosticSeverityError,
				Message:  "Trigger step must specify a pipeline slug",
				Source:   "buildkite-ls",
				Code:     "empty-trigger-pipeline",
			})
		}
	}

	// Validate input steps
	if hasInput {
		if input, ok := stepData["input"].(string); !ok || strings.TrimSpace(input) == "" {
			diagnostics = append(diagnostics, protocol.Diagnostic{
				Range: protocol.Range{
					Start: protocol.Position{Line: lineNum + 1, Character: 4},
					End:   protocol.Position{Line: lineNum + 1, Character: 999},
				},
				Severity: protocol.DiagnosticSeverityError,
				Message:  "Input step must have a non-empty prompt message",
				Source:   "buildkite-ls",
				Code:     "empty-input-prompt",
			})
		}
	}

	return diagnostics
}

func (s *Server) validatePluginConfigurations(pipelineData map[string]interface{}, lines []string) []protocol.Diagnostic {
	var diagnostics []protocol.Diagnostic

	steps, ok := pipelineData["steps"].([]interface{})
	if !ok {
		return diagnostics
	}

	stepLines := s.findStepLines(lines)

	for stepIndex, stepItem := range steps {
		stepData, ok := stepItem.(map[string]interface{})
		if !ok {
			continue
		}

		// Get the actual line number for this step
		lineNum := uint32(stepIndex)
		if stepIndex < len(stepLines) {
			lineNum = uint32(stepLines[stepIndex])
		}

		pluginRefs := plugins.ParsePluginFromStep(stepData)
		for _, pluginRef := range pluginRefs {
			if err := s.pluginRegistry.ValidatePluginConfig(pluginRef.Name, pluginRef.Config); err != nil {
				diagnostics = append(diagnostics, protocol.Diagnostic{
					Range: protocol.Range{
						Start: protocol.Position{Line: lineNum, Character: 0},
						End:   protocol.Position{Line: lineNum + 2, Character: 0},
					},
					Severity: protocol.DiagnosticSeverityError,
					Message:  fmt.Sprintf("Plugin '%s' configuration error: %s", pluginRef.Name, err.Error()),
					Source:   "buildkite-ls",
					Code:     "plugin-config-error",
				})
			}
		}
	}

	return diagnostics
}

// Helper function to find the line number for a top-level property
func (s *Server) findLineForProperty(property string, lines []string) int {
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == property+":" {
			return i
		}
	}
	// If not found, suggest adding at the end
	return len(lines) - 1
}

// Helper function to find the line numbers where steps begin
func (s *Server) findStepLines(lines []string) []int {
	var stepLines []int
	inSteps := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Look for the steps: section
		if trimmed == "steps:" {
			inSteps = true
			continue
		}

		// Stop if we hit another top-level property
		if inSteps && len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			break
		}

		// Look for step indicators (- at step level indentation)
		if inSteps && strings.HasPrefix(strings.TrimLeft(line, " \t"), "- ") {
			// Check if this is at the correct step indentation level (2 spaces)
			leadingSpaces := len(line) - len(strings.TrimLeft(line, " "))
			if leadingSpaces == 2 {
				stepLines = append(stepLines, i)
			}
		}
	}

	return stepLines
}

func (s *Server) isBuildkiteFile(uri string) bool {
	// Convert URI to file path (remove file:// prefix if present)
	filePath := uri
	if strings.HasPrefix(uri, "file://") {
		filePath = strings.TrimPrefix(uri, "file://")
	}

	// Check if file is in .buildkite directory and is YAML
	if strings.Contains(filePath, ".buildkite/") {
		return strings.HasSuffix(filePath, ".yml") || strings.HasSuffix(filePath, ".yaml")
	}

	// Check for standalone pipeline files (common pattern)
	fileName := filepath.Base(filePath)
	return fileName == "pipeline.yml" || fileName == "pipeline.yaml" ||
		fileName == "buildkite.yml" || fileName == "buildkite.yaml"
}

func (s *Server) sendDiagnostics(ctx context.Context, uri protocol.DocumentURI, diagnostics []protocol.Diagnostic) {
	s.logger.Printf("Sending %d diagnostics for %s", len(diagnostics), uri)

	if s.conn == nil {
		s.logger.Printf("No connection available to send diagnostics")
		return
	}

	// Ensure diagnostics is never nil
	if diagnostics == nil {
		diagnostics = []protocol.Diagnostic{}
	}

	// Send diagnostics notification to client
	params := protocol.PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: diagnostics,
	}

	// Send the notification
	err := s.conn.Notify(ctx, "textDocument/publishDiagnostics", params)
	if err != nil {
		s.logger.Printf("Failed to send diagnostics: %v", err)
	}
}

func (s *Server) SemanticTokensFull(ctx context.Context, params *protocol.SemanticTokensParams) (*protocol.SemanticTokens, error) {
	s.logger.Printf("SemanticTokensFull requested for URI: %s", params.TextDocument.URI)

	if !s.isBuildkiteFile(string(params.TextDocument.URI)) {
		s.logger.Printf("File is not a Buildkite file, skipping semantic tokens")
		return &protocol.SemanticTokens{Data: []uint32{}}, nil
	}

	// Get document content
	doc, exists := s.documentManager.GetDocument(params.TextDocument.URI)
	if !exists {
		s.logger.Printf("Document not found: %s", params.TextDocument.URI)
		return &protocol.SemanticTokens{Data: []uint32{}}, nil
	}

	// Generate semantic tokens
	tokens := s.generateSemanticTokens(doc.Lines)

	s.logger.Printf("Generated %d semantic tokens", len(tokens.Data)/5)
	return tokens, nil
}

func (s *Server) SemanticTokensRange(ctx context.Context, params *protocol.SemanticTokensRangeParams) (*protocol.SemanticTokens, error) {
	s.logger.Printf("SemanticTokensRange requested for URI: %s, Range: %d:%d-%d:%d",
		params.TextDocument.URI,
		params.Range.Start.Line, params.Range.Start.Character,
		params.Range.End.Line, params.Range.End.Character)

	if !s.isBuildkiteFile(string(params.TextDocument.URI)) {
		s.logger.Printf("File is not a Buildkite file, skipping semantic tokens")
		return &protocol.SemanticTokens{Data: []uint32{}}, nil
	}

	// Get document content
	doc, exists := s.documentManager.GetDocument(params.TextDocument.URI)
	if !exists {
		s.logger.Printf("Document not found: %s", params.TextDocument.URI)
		return &protocol.SemanticTokens{Data: []uint32{}}, nil
	}

	// Extract lines in the specified range
	startLine := int(params.Range.Start.Line)
	endLine := int(params.Range.End.Line)

	if startLine < 0 || endLine >= len(doc.Lines) || startLine > endLine {
		return &protocol.SemanticTokens{Data: []uint32{}}, nil
	}

	rangeLines := doc.Lines[startLine : endLine+1]

	// Generate semantic tokens for the range
	tokens := s.generateSemanticTokensForRange(rangeLines, startLine)

	s.logger.Printf("Generated %d semantic tokens for range", len(tokens.Data)/5)
	return tokens, nil
}

func (s *Server) generateSemanticTokens(lines []string) *protocol.SemanticTokens {
	return s.generateSemanticTokensForRange(lines, 0)
}

func (s *Server) generateSemanticTokensForRange(lines []string, startLineOffset int) *protocol.SemanticTokens {
	var data []uint32

	// Track context
	inSteps := false
	inStep := false
	stepIndent := -1

	prevLine := uint32(0)
	prevStart := uint32(0)

	for lineIndex, line := range lines {
		actualLineNumber := uint32(lineIndex + startLineOffset)
		lineTokens := s.tokenizeLine(line, actualLineNumber, &inSteps, &inStep, &stepIndent)

		// Convert absolute positions to relative (LSP semantic tokens format)
		for i := 0; i < len(lineTokens); i += 5 {
			currentLine := lineTokens[i]
			currentStart := lineTokens[i+1]
			length := lineTokens[i+2]
			tokenType := lineTokens[i+3]
			tokenModifiers := lineTokens[i+4]

			// Calculate deltas
			deltaLine := currentLine - prevLine
			deltaStart := currentStart
			if deltaLine == 0 {
				deltaStart = currentStart - prevStart
			}

			data = append(data, deltaLine, deltaStart, length, tokenType, tokenModifiers)

			prevLine = currentLine
			prevStart = currentStart
		}
	}

	return &protocol.SemanticTokens{
		Data: data,
	}
}

func (s *Server) tokenizeLine(line string, lineNumber uint32, inSteps *bool, inStep *bool, stepIndent *int) []uint32 {
	var tokens []uint32

	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		// Handle comments
		if strings.HasPrefix(trimmed, "#") {
			commentStart := strings.Index(line, "#")
			tokens = append(tokens, s.createToken(lineNumber, uint32(commentStart), uint32(len(trimmed)), "comment", nil)...)
		}
		return tokens
	}

	indent := s.getIndentLevel(line)

	// Track context
	if trimmed == "steps:" {
		*inSteps = true
		*inStep = false
		// Highlight "steps" as a keyword
		tokens = append(tokens, s.createToken(lineNumber, 0, uint32(len("steps")), "keyword", nil)...)
		// Highlight ":" as operator
		tokens = append(tokens, s.createToken(lineNumber, uint32(len("steps")), 1, "operator", nil)...)
		return tokens
	}

	// Check if we're leaving the steps section
	if *inSteps && indent == 0 && !strings.HasPrefix(trimmed, "- ") {
		*inSteps = false
		*inStep = false
	}

	// Check if we're starting a new step
	if *inSteps && strings.HasPrefix(strings.TrimLeft(line, " \t"), "- ") && indent == 2 {
		*inStep = true
		*stepIndent = indent

		// Highlight the step dash as operator
		dashPos := strings.Index(line, "- ")
		tokens = append(tokens, s.createToken(lineNumber, uint32(dashPos), 1, "operator", nil)...)

		// Parse the rest of the step line
		stepContent := strings.TrimSpace(line[dashPos+2:])
		if stepContent != "" {
			stepTokens := s.parseStepContent(stepContent, lineNumber, uint32(dashPos+2))
			tokens = append(tokens, stepTokens...)
		}
		return tokens
	}

	// Check if we're leaving a step context
	if *inStep && indent <= *stepIndent && !strings.HasPrefix(trimmed, "- ") {
		*inStep = false
	}

	// Parse YAML key-value pairs
	if colonIndex := strings.Index(line, ":"); colonIndex != -1 {
		keyStart := 0
		for keyStart < len(line) && (line[keyStart] == ' ' || line[keyStart] == '\t') {
			keyStart++
		}

		key := strings.TrimSpace(line[keyStart:colonIndex])
		value := strings.TrimSpace(line[colonIndex+1:])

		// Determine token types based on context and key
		keyTokenType := s.getKeyTokenType(key, *inStep)
		keyModifiers := s.getKeyModifiers(key, *inStep)

		// Highlight the key
		tokens = append(tokens, s.createToken(lineNumber, uint32(keyStart), uint32(len(key)), keyTokenType, keyModifiers)...)

		// Highlight the colon
		tokens = append(tokens, s.createToken(lineNumber, uint32(colonIndex), 1, "operator", nil)...)

		// Highlight the value if present
		if value != "" {
			valueStart := colonIndex + 1
			for valueStart < len(line) && line[valueStart] == ' ' {
				valueStart++
			}

			valueTokenType, valueModifiers := s.getValueTokenType(key, value, *inStep)
			if valueTokenType != "" {
				tokens = append(tokens, s.createToken(lineNumber, uint32(valueStart), uint32(len(value)), valueTokenType, valueModifiers)...)
			}
		}
	}

	return tokens
}

func (s *Server) parseStepContent(content string, line uint32, startChar uint32) []uint32 {
	var tokens []uint32

	// Check if this is a step type definition on the same line (e.g., "- command: make build")
	if colonIndex := strings.Index(content, ":"); colonIndex != -1 {
		key := strings.TrimSpace(content[:colonIndex])
		value := strings.TrimSpace(content[colonIndex+1:])

		keyTokenType := s.getKeyTokenType(key, true)
		keyModifiers := s.getKeyModifiers(key, true)

		// Highlight the key
		tokens = append(tokens, s.createToken(line, startChar, uint32(len(key)), keyTokenType, keyModifiers)...)

		// Highlight the colon
		tokens = append(tokens, s.createToken(line, startChar+uint32(len(key)), 1, "operator", nil)...)

		// Highlight the value
		if value != "" {
			valueStart := startChar + uint32(colonIndex+1)
			for valueStart < startChar+uint32(len(content)) && content[colonIndex+1+int(valueStart-startChar-uint32(colonIndex+1))] == ' ' {
				valueStart++
			}

			valueTokenType, valueModifiers := s.getValueTokenType(key, value, true)
			if valueTokenType != "" {
				tokens = append(tokens, s.createToken(line, valueStart, uint32(len(value)), valueTokenType, valueModifiers)...)
			}
		}
	}

	return tokens
}

func (s *Server) getKeyTokenType(key string, inStep bool) string {
	// Step type keywords
	stepTypes := map[string]bool{
		"command": true, "commands": true, "wait": true, "block": true,
		"input": true, "trigger": true, "group": true,
	}

	if stepTypes[key] {
		return "keyword"
	}

	// Step properties that are like identifiers/namespaces
	if key == "key" || key == "label" {
		return "namespace"
	}

	// Environment variables
	if key == "env" {
		return "variable"
	}

	// Plugin-related
	if key == "plugins" || strings.Contains(key, "#") {
		return "function"
	}

	// Default to property
	return "property"
}

func (s *Server) getKeyModifiers(key string, inStep bool) []string {
	var modifiers []string

	// Step type keywords are definitions
	stepTypes := map[string]bool{
		"command": true, "commands": true, "wait": true, "block": true,
		"input": true, "trigger": true, "group": true,
	}

	if stepTypes[key] && inStep {
		modifiers = append(modifiers, "definition")
	}

	// Some properties are essentially readonly
	if key == "key" || key == "timeout_in_minutes" {
		modifiers = append(modifiers, "readonly")
	}

	return modifiers
}

func (s *Server) getValueTokenType(key string, value string, inStep bool) (string, []string) {
	var modifiers []string

	// Remove quotes from value for analysis
	cleanValue := strings.Trim(value, `"'`)

	// Plugin names (contain #)
	if strings.Contains(cleanValue, "#") {
		return "function", modifiers
	}

	// Environment variable values
	if key == "env" {
		return "variable", modifiers
	}

	// Step keys and labels are like namespaces/identifiers
	if key == "key" || key == "label" {
		return "namespace", modifiers
	}

	// Boolean and null values
	if cleanValue == "true" || cleanValue == "false" || cleanValue == "null" || cleanValue == "~" {
		modifiers = append(modifiers, "readonly")
		return "keyword", modifiers
	}

	// Numbers
	if _, err := strconv.Atoi(cleanValue); err == nil {
		modifiers = append(modifiers, "readonly")
		return "keyword", modifiers
	}

	// Default to string
	return "string", modifiers
}

func (s *Server) createToken(line, start, length uint32, tokenType string, modifiers []string) []uint32 {
	// LSP semantic tokens are encoded as [deltaLine, deltaStart, length, tokenType, tokenModifiers]
	// For now, return absolute positions; they'll be converted to deltas in generateSemanticTokensForRange

	tokenTypeIndex := s.getTokenTypeIndex(tokenType)
	tokenModifierBits := s.getTokenModifierBits(modifiers)

	return []uint32{line, start, length, uint32(tokenTypeIndex), uint32(tokenModifierBits)}
}

func (s *Server) getTokenTypeIndex(tokenType string) int {
	tokenTypes := []string{
		"keyword", "string", "property", "variable", "function", "namespace", "operator", "comment",
	}

	for i, t := range tokenTypes {
		if t == tokenType {
			return i
		}
	}
	return 0 // default to keyword
}

func (s *Server) getTokenModifierBits(modifiers []string) int {
	modifierMap := map[string]int{
		"definition": 1 << 0,
		"readonly":   1 << 1,
		"deprecated": 1 << 2,
	}

	bits := 0
	for _, modifier := range modifiers {
		if bit, ok := modifierMap[modifier]; ok {
			bits |= bit
		}
	}
	return bits
}

func (s *Server) Handler() jsonrpc2.Handler {
	return func(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
		s.logger.Printf("Received method: %s", req.Method())
		switch req.Method() {
		case "initialize":
			var params protocol.InitializeParams
			if err := json.Unmarshal(req.Params(), &params); err != nil {
				s.logger.Printf("Error unmarshaling params: %v", err)
				return reply(ctx, nil, err)
			}
			s.logger.Printf("Calling Initialize")

			// Client will be set up when needed for diagnostics

			result, err := s.Initialize(ctx, &params)
			s.logger.Printf("Initialize result: %+v, err: %v", result, err)
			return reply(ctx, result, err)

		case "initialized":
			var params protocol.InitializedParams
			if err := json.Unmarshal(req.Params(), &params); err != nil {
				return reply(ctx, nil, err)
			}
			err := s.Initialized(ctx, &params)
			return reply(ctx, nil, err)

		case "shutdown":
			err := s.Shutdown(ctx)
			return reply(ctx, nil, err)

		case "exit":
			_ = s.Exit(ctx)
			return nil

		case "textDocument/didOpen":
			var params protocol.DidOpenTextDocumentParams
			if err := json.Unmarshal(req.Params(), &params); err != nil {
				return reply(ctx, nil, err)
			}
			err := s.DidOpen(ctx, &params)
			return reply(ctx, nil, err)

		case "textDocument/didChange":
			var params protocol.DidChangeTextDocumentParams
			if err := json.Unmarshal(req.Params(), &params); err != nil {
				return reply(ctx, nil, err)
			}
			err := s.DidChange(ctx, &params)
			return reply(ctx, nil, err)

		case "textDocument/didClose":
			var params protocol.DidCloseTextDocumentParams
			if err := json.Unmarshal(req.Params(), &params); err != nil {
				return reply(ctx, nil, err)
			}
			err := s.DidClose(ctx, &params)
			return reply(ctx, nil, err)

		case "textDocument/hover":
			var params protocol.HoverParams
			if err := json.Unmarshal(req.Params(), &params); err != nil {
				return reply(ctx, nil, err)
			}
			result, err := s.Hover(ctx, &params)
			return reply(ctx, result, err)

		case "textDocument/completion":
			s.logger.Printf("Received textDocument/completion request")
			var params protocol.CompletionParams
			if err := json.Unmarshal(req.Params(), &params); err != nil {
				s.logger.Printf("Error unmarshaling completion params: %v", err)
				return reply(ctx, nil, err)
			}
			s.logger.Printf("Processing completion for URI: %s, Position: %d:%d",
				params.TextDocument.URI, params.Position.Line, params.Position.Character)
			result, err := s.Completion(ctx, &params)
			s.logger.Printf("Completion result: %d items, error: %v",
				len(result.Items), err)
			return reply(ctx, result, err)

		case "textDocument/documentSymbol":
			s.logger.Printf("Received textDocument/documentSymbol request")
			var params protocol.DocumentSymbolParams
			if err := json.Unmarshal(req.Params(), &params); err != nil {
				s.logger.Printf("Error unmarshaling document symbol params: %v", err)
				return reply(ctx, nil, err)
			}
			s.logger.Printf("Processing document symbols for URI: %s", params.TextDocument.URI)
			result, err := s.DocumentSymbol(ctx, &params)
			s.logger.Printf("DocumentSymbol result: %d symbols, error: %v",
				len(result), err)
			return reply(ctx, result, err)

		case "textDocument/signatureHelp":
			s.logger.Printf("Received textDocument/signatureHelp request")
			var params protocol.SignatureHelpParams
			if err := json.Unmarshal(req.Params(), &params); err != nil {
				s.logger.Printf("Error unmarshaling signature help params: %v", err)
				return reply(ctx, nil, err)
			}
			s.logger.Printf("Processing signature help for URI: %s", params.TextDocument.URI)
			result, err := s.SignatureHelp(ctx, &params)
			if result != nil {
				s.logger.Printf("SignatureHelp result: %d signatures, error: %v",
					len(result.Signatures), err)
			} else {
				s.logger.Printf("SignatureHelp result: nil, error: %v", err)
			}
			return reply(ctx, result, err)

		case "textDocument/definition":
			s.logger.Printf("Received textDocument/definition request")
			var params protocol.DefinitionParams
			if err := json.Unmarshal(req.Params(), &params); err != nil {
				s.logger.Printf("Error unmarshaling definition params: %v", err)
				return reply(ctx, nil, err)
			}
			s.logger.Printf("Processing definition for URI: %s", params.TextDocument.URI)
			result, err := s.Definition(ctx, &params)
			s.logger.Printf("Definition result: %d locations, error: %v",
				len(result), err)
			return reply(ctx, result, err)

		case "textDocument/codeAction":
			s.logger.Printf("Received textDocument/codeAction request")
			var params protocol.CodeActionParams
			if err := json.Unmarshal(req.Params(), &params); err != nil {
				s.logger.Printf("Error unmarshaling code action params: %v", err)
				return reply(ctx, nil, err)
			}
			s.logger.Printf("Processing code actions for URI: %s", params.TextDocument.URI)
			result, err := s.CodeAction(ctx, &params)
			s.logger.Printf("CodeAction result: %d actions, error: %v",
				len(result), err)
			return reply(ctx, result, err)

		case "textDocument/semanticTokens/full":
			s.logger.Printf("Received textDocument/semanticTokens/full request")
			var params protocol.SemanticTokensParams
			if err := json.Unmarshal(req.Params(), &params); err != nil {
				s.logger.Printf("Error unmarshaling semantic tokens params: %v", err)
				return reply(ctx, nil, err)
			}
			s.logger.Printf("Processing semantic tokens for URI: %s", params.TextDocument.URI)
			result, err := s.SemanticTokensFull(ctx, &params)
			if result != nil {
				s.logger.Printf("SemanticTokensFull result: %d tokens, error: %v",
					len(result.Data)/5, err)
			} else {
				s.logger.Printf("SemanticTokensFull result: nil, error: %v", err)
			}
			return reply(ctx, result, err)

		case "textDocument/semanticTokens/range":
			s.logger.Printf("Received textDocument/semanticTokens/range request")
			var params protocol.SemanticTokensRangeParams
			if err := json.Unmarshal(req.Params(), &params); err != nil {
				s.logger.Printf("Error unmarshaling semantic tokens range params: %v", err)
				return reply(ctx, nil, err)
			}
			s.logger.Printf("Processing semantic tokens range for URI: %s", params.TextDocument.URI)
			result, err := s.SemanticTokensRange(ctx, &params)
			if result != nil {
				s.logger.Printf("SemanticTokensRange result: %d tokens, error: %v",
					len(result.Data)/5, err)
			} else {
				s.logger.Printf("SemanticTokensRange result: nil, error: %v", err)
			}
			return reply(ctx, result, err)

		default:
			return jsonrpc2.MethodNotFoundHandler(ctx, reply, req)
		}
	}
}
