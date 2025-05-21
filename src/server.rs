//! Core language server implementation

use std::collections::HashMap;
use std::sync::{Arc, RwLock};

use tower_lsp::jsonrpc::Result;
use tower_lsp::lsp_types::*;
use tower_lsp::{Client, LanguageServer, LspService, Server};
use tracing::{debug, error, info, warn};

use crate::diagnostics;
use crate::parser::Document;
use crate::schema::BuildkiteSchema;

/// The main Backend struct for the Buildkite Language Server
pub struct Backend {
    /// LSP client to communicate with the editor
    client: Client,
    /// Schema for Buildkite pipelines
    schema: Arc<RwLock<Option<BuildkiteSchema>>>,
    /// Open documents managed by the server
    documents: Arc<RwLock<HashMap<Url, Document>>>,
}

impl Backend {
    pub fn new(client: Client) -> Self {
        Self {
            client,
            schema: Arc::new(RwLock::new(None)),
            documents: Arc::new(RwLock::new(HashMap::new())),
        }
    }

    /// Initialize the schema (to be called after server initialization)
    async fn initialize_schema(&self) {
        info!("Initializing Buildkite pipeline schema");
        match BuildkiteSchema::load().await {
            Ok(schema) => {
                // Load the schema
                {
                    let mut schema_lock = self.schema.write().unwrap();
                    *schema_lock = Some(schema);
                }
                
                // Log success
                info!("Buildkite pipeline schema loaded successfully");
                self.client
                    .log_message(MessageType::INFO, "Buildkite schema loaded successfully")
                    .await;
            }
            Err(e) => {
                // Log error
                let error_msg = format!("Failed to load schema: {}", e);
                error!("{}", error_msg);
                self.client
                    .show_message(MessageType::ERROR, error_msg)
                    .await;
            }
        }
    }

    /// Validate a document and publish diagnostics
    async fn validate_document(&self, uri: Url) {
        let (document, schema) = {
            let documents = self.documents.read().unwrap();
            let schema = self.schema.read().unwrap();
            
            let document = if let Some(document) = documents.get(&uri) {
                document
            } else {
                debug!("Document not found for validation: {}", uri);
                return;
            };
            
            let schema = if let Some(schema) = schema.as_ref() {
                schema
            } else {
                debug!("Schema not available for validation");
                return;
            };
            
            (document.clone(), schema.clone())
        };
        
        // Generate diagnostics
        let diagnostics = diagnostics::validate_document(&document, &schema);
        
        // Publish diagnostics
        self.client.publish_diagnostics(uri, diagnostics, None).await;
    }
}

#[tower_lsp::async_trait]
impl LanguageServer for Backend {
    async fn initialize(&self, _: InitializeParams) -> Result<InitializeResult> {
        info!("Initializing Buildkite Language Server");
        Ok(InitializeResult {
            capabilities: ServerCapabilities {
                hover_provider: Some(HoverProviderCapability::Simple(true)),
                completion_provider: Some(CompletionOptions {
                    resolve_provider: Some(false),
                    trigger_characters: Some(vec![".".to_string(), ":".to_string()]),
                    work_done_progress_options: Default::default(),
                    all_commit_characters: None,
                    completion_item: Default::default(),
                }),
                text_document_sync: Some(TextDocumentSyncCapability::Options(TextDocumentSyncOptions {
                    open_close: Some(true),
                    change: Some(TextDocumentSyncKind::INCREMENTAL),
                    will_save: None,
                    will_save_wait_until: None,
                    save: Some(SaveOptions::default().into()),
                })),
                diagnostic_provider: Some(DiagnosticServerCapabilities::Options(
                    DiagnosticOptions {
                        identifier: Some("buildkite-ls".to_string()),
                        inter_file_dependencies: false,
                        workspace_diagnostics: false,
                        work_done_progress_options: WorkDoneProgressOptions {
                            work_done_progress: None,
                        },
                    },
                )),
                ..ServerCapabilities::default()
            },
            server_info: Some(ServerInfo {
                name: "buildkite-ls".to_string(),
                version: Some(env!("CARGO_PKG_VERSION").to_string()),
            }),
        })
    }

    async fn initialized(&self, _: InitializedParams) {
        info!("Buildkite Language Server initialized");
        self.client
            .log_message(MessageType::INFO, "Buildkite Language Server initialized")
            .await;
        
        // Instead of calling initialize_schema, we'll do a simpler initialization here
        // to work around threading issues
        info!("Loading Buildkite schema");
        
        // For now, we'll use a simplified local schema for testing
        let schema_json = serde_json::json!({
            "title": "Buildkite Pipeline Schema",
            "type": "object",
            "properties": {
                "steps": {
                    "type": "array",
                    "description": "The steps to run in this pipeline"
                }
            }
        });
        
        // Create and store the schema
        let schema = BuildkiteSchema::new(schema_json);
        
        // Use a scope to limit the lifetime of the lock
        {
            let mut schema_lock = self.schema.write().unwrap();
            *schema_lock = Some(schema);
        }
        
        self.client
            .log_message(MessageType::INFO, "Loaded basic schema for testing")
            .await;
    }

    async fn shutdown(&self) -> Result<()> {
        info!("Shutting down Buildkite Language Server");
        Ok(())
    }

    async fn did_open(&self, params: DidOpenTextDocumentParams) {
        let uri = params.text_document.uri;
        let text = params.text_document.text;
        
        info!("Document opened: {}", uri);
        
        // Create and store the document
        let mut document = Document::new(text);
        if let Err(e) = document.parse() {
            error!("Failed to parse document: {}", e);
            self.client
                .show_message(MessageType::WARNING, format!("Failed to parse YAML: {}", e))
                .await;
        }
        
        // Store the document
        {
            let mut documents = self.documents.write().unwrap();
            documents.insert(uri.clone(), document);
        }
        
        // Validate the document
        self.validate_document(uri).await;
    }

    async fn did_change(&self, params: DidChangeTextDocumentParams) {
        let uri = params.text_document.uri;
        
        debug!("Document changed: {}", uri);
        
        // Update the document
        {
            let mut documents = self.documents.write().unwrap();
            // Apply changes to the document
            for change in params.content_changes {
                let mut updated_document = Document::new(change.text);
                
                // Parse the updated document
                if let Err(e) = updated_document.parse() {
                    error!("Failed to parse document: {}", e);
                }
                
                // Replace the document in our storage
                documents.insert(uri.clone(), updated_document);
            }
        }
        
        // Validate the document
        self.validate_document(uri).await;
    }

    async fn did_save(&self, params: DidSaveTextDocumentParams) {
        let uri = params.text_document.uri;
        
        info!("Document saved: {}", uri);
        
        // Re-validate the document
        self.validate_document(uri).await;
    }

    async fn did_close(&self, params: DidCloseTextDocumentParams) {
        let uri = params.text_document.uri;
        
        info!("Document closed: {}", uri);
        
        // Remove the document from storage
        {
            let mut documents = self.documents.write().unwrap();
            documents.remove(&uri);
        }
        
        // Clear diagnostics for the closed document
        self.client.publish_diagnostics(uri, vec![], None).await;
    }
}