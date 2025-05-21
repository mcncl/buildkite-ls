//! Validation and diagnostics

use tower_lsp::lsp_types::{Diagnostic, DiagnosticSeverity, Position, Range};

use crate::parser::Document;
use crate::schema::BuildkiteSchema;

/// Generate diagnostics for the given document
pub fn validate_document(
    document: &Document,
    schema: &BuildkiteSchema,
) -> Vec<Diagnostic> {
    // Validate the document against the schema
    let errors = schema.validate(&document.text);

    // Convert errors to diagnostics
    errors
        .into_iter()
        .map(|error| {
            // TODO: Parse the error and get the correct position
            let range = Range {
                start: Position::new(0, 0),
                end: Position::new(0, 0),
            };

            Diagnostic {
                range,
                severity: Some(DiagnosticSeverity::ERROR),
                code: None,
                code_description: None,
                source: Some("buildkite-ls".to_string()),
                message: error,
                related_information: None,
                tags: None,
                data: None,
            }
        })
        .collect()
}