//! Completion provider implementation

use tower_lsp::lsp_types::{CompletionItem, CompletionItemKind, Position};

use crate::parser::Document;
use crate::schema::BuildkiteSchema;

/// Generate completion items for the given document and position
pub fn provide_completion(
    document: &Document,
    position: Position,
    _schema: &BuildkiteSchema,
) -> Vec<CompletionItem> {
    // Get the context at the current position
    let _context = document.context_at_position(position.line, position.character);

    // TODO: Generate completions based on the context and schema
    vec![]
}

/// Create a completion item for a property
fn create_property_completion(name: &str, documentation: Option<String>) -> CompletionItem {
    CompletionItem {
        label: name.to_string(),
        kind: Some(CompletionItemKind::PROPERTY),
        detail: Some("Buildkite pipeline property".to_string()),
        documentation: documentation.map(|doc| {
            tower_lsp::lsp_types::Documentation::MarkupContent(MarkupContent {
                kind: tower_lsp::lsp_types::MarkupKind::Markdown,
                value: doc,
            })
        }),
        ..CompletionItem::default()
    }
}

use tower_lsp::lsp_types::{Documentation, MarkupContent, MarkupKind};