package lsp

import (
	"context"
	"encoding/json"
	"log"
	"strings"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"

	"github.com/mcncl/buildkite-ls/internal/parser"
	"github.com/mcncl/buildkite-ls/internal/schema"
)

type Server struct {
	client       protocol.Client
	logger       *log.Logger
	schemaLoader *schema.Loader
}

func NewServer() *Server {
	return &Server{
		logger:       log.New(log.Writer(), "[buildkite-ls] ", log.LstdFlags),
		schemaLoader: schema.NewLoader(),
	}
}

func (s *Server) SetClient(client protocol.Client) {
	s.client = client
}

func (s *Server) Logger() *log.Logger {
	return s.logger
}

func (s *Server) Initialize(ctx context.Context, params *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	s.logger.Printf("Initializing buildkite-ls server")
	
	return &protocol.InitializeResult{
		Capabilities: protocol.ServerCapabilities{
			TextDocumentSync: &protocol.TextDocumentSyncOptions{
				OpenClose: true,
				Change:    protocol.TextDocumentSyncKindFull,
			},
			HoverProvider: true,
			CompletionProvider: &protocol.CompletionOptions{
				TriggerCharacters: []string{" ", ":", "-"},
			},
		},
		ServerInfo: &protocol.ServerInfo{
			Name:    "buildkite-ls",
			Version: "0.1.0",
		},
	}, nil
}

func (s *Server) Initialized(ctx context.Context, params *protocol.InitializedParams) error {
	s.logger.Printf("Server initialized")
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
	s.validateDocument(ctx, params.TextDocument.URI, params.TextDocument.Text)
	return nil
}

func (s *Server) DidChange(ctx context.Context, params *protocol.DidChangeTextDocumentParams) error {
	s.logger.Printf("Document changed: %s", params.TextDocument.URI)
	
	if len(params.ContentChanges) > 0 {
		lastChange := params.ContentChanges[len(params.ContentChanges)-1]
		s.validateDocument(ctx, params.TextDocument.URI, lastChange.Text)
	}
	return nil
}

func (s *Server) DidClose(ctx context.Context, params *protocol.DidCloseTextDocumentParams) error {
	s.logger.Printf("Document closed: %s", params.TextDocument.URI)
	return nil
}

func (s *Server) Hover(ctx context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: "Buildkite pipeline hover information",
		},
	}, nil
}

func (s *Server) Completion(ctx context.Context, params *protocol.CompletionParams) (*protocol.CompletionList, error) {
	return &protocol.CompletionList{
		IsIncomplete: false,
		Items: []protocol.CompletionItem{
			{
				Label:  "steps",
				Kind:   protocol.CompletionItemKindProperty,
				Detail: "Pipeline steps array",
			},
		},
	}, nil
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

	if err := s.schemaLoader.ValidateJSON(pipeline.JSONBytes); err != nil {
		s.sendDiagnostics(ctx, uri, []protocol.Diagnostic{
			{
				Range: protocol.Range{
					Start: protocol.Position{Line: 0, Character: 0},
					End:   protocol.Position{Line: 0, Character: 0},
				},
				Severity: protocol.DiagnosticSeverityError,
				Message:  "Schema validation error: " + err.Error(),
			},
		})
		return
	}

	s.sendDiagnostics(ctx, uri, []protocol.Diagnostic{})
}

func (s *Server) isBuildkiteFile(uri string) bool {
	return strings.Contains(uri, "pipeline.yml") || 
		   strings.Contains(uri, "pipeline.yaml") ||
		   strings.Contains(uri, ".buildkite/")
}

func (s *Server) sendDiagnostics(ctx context.Context, uri protocol.DocumentURI, diagnostics []protocol.Diagnostic) {
	s.logger.Printf("Diagnostics for %s: %d issues", uri, len(diagnostics))
	for _, diag := range diagnostics {
		s.logger.Printf("  - %s: %s", diag.Severity, diag.Message)
	}
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
			s.Exit(ctx)
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
			
		default:
			return jsonrpc2.MethodNotFoundHandler(ctx, reply, req)
		}
	}
}