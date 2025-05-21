//! Hover information provider

use tower_lsp::lsp_types::{Hover, MarkedString, Position, Range};

use crate::parser::Document;
use crate::schema::BuildkiteSchema;

/// Generate hover information for the given document and position
pub fn provide_hover(
    document: &Document,
    position: Position,
    schema: &BuildkiteSchema,
) -> Option<Hover> {
    // Get the node at the current position
    let node = document.node_at_position(position.line, position.character)?;

    // Get documentation from the schema
    let documentation = schema.get_documentation(node)?;

    // Create hover information
    Some(Hover {
        contents: tower_lsp::lsp_types::HoverContents::Scalar(MarkedString::String(documentation)),
        range: Some(Range {
            start: position,
            end: Position {
                line: position.line,
                character: position.character + node.len() as u32,
            },
        }),
    })
}