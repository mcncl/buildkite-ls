//! YAML parsing and document handling

use serde_json::Value as JsonValue;
use serde_yaml::Value as YamlValue;
use std::collections::HashMap;
use tower_lsp::lsp_types::{Position, Range};
use tracing::info;

/// Represents a node in the YAML document with position information
#[derive(Debug, Clone, PartialEq)]
pub struct Node {
    /// The type of node (scalar, mapping, sequence)
    pub node_type: NodeType,
    /// The key if this is a key-value pair in a mapping
    pub key: Option<String>,
    /// The value represented as a string
    pub value: String,
    /// The location of this node in the document
    pub range: Range,
    /// Child nodes if this is a mapping or sequence
    pub children: Vec<Node>,
    /// The path to this node in dot notation
    pub path: String,
}

/// Types of YAML nodes
#[derive(Debug, Clone, PartialEq)]
pub enum NodeType {
    /// A scalar value like a string, number, boolean
    Scalar,
    /// A mapping (key-value pairs)
    Mapping,
    /// A sequence (array)
    Sequence,
}

/// Represents a parsed YAML document with position information
#[derive(Clone)]
pub struct Document {
    /// The raw text of the document
    pub text: String,
    /// The parsed YAML as a serde_yaml Value
    pub yaml: Option<YamlValue>,
    /// Root node of the document with position information
    pub root: Option<Node>,
    /// Lines in the document for position lookup
    pub lines: Vec<String>,
    /// Map of position ranges to nodes for quick lookup
    pub position_map: HashMap<(u32, u32), Node>,
}

impl Document {
    /// Create a new document from the given text
    pub fn new(text: String) -> Self {
        Self { 
            text, 
            yaml: None,
            root: None,
            lines: Vec::new(),
            position_map: HashMap::new(),
        }
    }

    /// Parse the document and create position mappings
    pub fn parse(&mut self) -> Result<(), serde_yaml::Error> {
        // Split the document into lines for position tracking
        self.lines = self.text.lines().map(|s| s.to_string()).collect();
        
        // Parse YAML
        let yaml: YamlValue = serde_yaml::from_str(&self.text)?;
        self.yaml = Some(yaml.clone());
        
        // Create the position map and node tree
        self.build_position_map();
        
        Ok(())
    }

    /// Build the position map for the document
    fn build_position_map(&mut self) {
        // This is a simplified implementation. A full implementation would
        // need to parse the YAML AST and track positions more accurately.
        
        // For now, we'll do a simple line-based approach
        self.position_map.clear();
        
        if let Some(yaml) = &self.yaml {
            // Create a root node
            let root_range = Range {
                start: Position::new(0, 0),
                end: Position::new(self.lines.len() as u32, 0),
            };
            
            let mut root = Node {
                node_type: match yaml {
                    YamlValue::Mapping(_) => NodeType::Mapping,
                    YamlValue::Sequence(_) => NodeType::Sequence,
                    _ => NodeType::Scalar,
                },
                key: None,
                value: format!("{:?}", yaml),
                range: root_range,
                children: Vec::new(),
                path: "".to_string(),
            };
            
            // Process the YAML structure
            
            // Process lines as a simple approximation
            for (line_idx, line) in self.lines.iter().enumerate() {
                let line_num = line_idx as u32;
                let trimmed = line.trim();
                
                // Skip empty lines
                if trimmed.is_empty() {
                    continue;
                }
                
                // Simple key detection
                if let Some(colon_idx) = trimmed.find(':') {
                    let key = trimmed[..colon_idx].trim().to_string();
                    let value = if colon_idx + 1 < trimmed.len() {
                        trimmed[colon_idx + 1..].trim().to_string()
                    } else {
                        "".to_string()
                    };
                    
                    // Determine indentation level
                    let indent = line.chars().take_while(|c| c.is_whitespace()).count() as u32;
                    
                    // Create node for this key-value pair
                    let node = Node {
                        node_type: NodeType::Scalar,
                        key: Some(key.clone()),
                        value: value.clone(),
                        range: Range {
                            start: Position::new(line_num, indent),
                            end: Position::new(line_num, indent + (key.len() as u32) + (value.len() as u32) + 1),
                        },
                        children: Vec::new(),
                        path: key,
                    };
                    
                    // Add to the position map
                    self.position_map.insert((line_num, indent), node.clone());
                    root.children.push(node);
                }
                // Simple list item detection
                else if trimmed.starts_with('-') {
                    let indent = line.chars().take_while(|c| c.is_whitespace()).count() as u32;
                    let value = trimmed[1..].trim().to_string();
                    
                    // Create node for this list item
                    let node = Node {
                        node_type: NodeType::Scalar,
                        key: None,
                        value: value.clone(),
                        range: Range {
                            start: Position::new(line_num, indent),
                            end: Position::new(line_num, indent + (value.len() as u32) + 1),
                        },
                        children: Vec::new(),
                        path: format!("[{}]", root.children.len()),
                    };
                    
                    // Add to the position map
                    self.position_map.insert((line_num, indent), node.clone());
                    root.children.push(node);
                }
            }
            
            self.root = Some(root);
        }
    }

    /// Get the node at the given position
    pub fn node_at_position(&self, line: u32, character: u32) -> Option<String> {
        // Find the closest node in the position map
        let candidates: Vec<_> = self.position_map.iter()
            .filter(|((_line, _), node)| {
                let range = &node.range;
                range.start.line <= line && range.end.line >= line &&
                range.start.character <= character && range.end.character >= character
            })
            .collect();
        
        // Sort by specificity - prefer deeper nodes with smaller ranges
        if !candidates.is_empty() {
            // Find the most specific node (smallest range that contains the position)
            let mut best_match = candidates[0];
            let mut smallest_area = area_of_range(&best_match.1.range);
            
            for candidate in &candidates[1..] {
                let area = area_of_range(&candidate.1.range);
                if area < smallest_area {
                    best_match = *candidate;
                    smallest_area = area;
                }
            }
            
            // Return the path to this node
            return Some(best_match.1.path.clone());
        }
        
        None
    }

    /// Get the context at the given position (parent nodes)
    pub fn context_at_position(&self, line: u32, character: u32) -> Vec<String> {
        let mut context = Vec::new();
        
        if let Some(path) = self.node_at_position(line, character) {
            // Split the path and build context from parts
            let parts: Vec<&str> = path.split('/').collect();
            
            // Add increasingly specific parts of the path
            let mut current_path = String::new();
            for part in parts {
                if !part.is_empty() {
                    if !current_path.is_empty() {
                        current_path.push('/');
                    }
                    current_path.push_str(part);
                    context.push(current_path.clone());
                }
            }
        }
        
        context
    }
}

/// Calculate the area of a range (for finding the most specific node)
fn area_of_range(range: &Range) -> u64 {
    let width = range.end.character as u64 - range.start.character as u64;
    let height = range.end.line as u64 - range.start.line as u64 + 1;
    width * height
}