//! Buildkite pipeline schema handling

use serde_json::{Map, Value};
use std::collections::HashMap;
use thiserror::Error;
use tracing::{debug, error, info};

/// URL to the official Buildkite pipeline JSON schema
pub const BUILDKITE_SCHEMA_URL: &str = 
    "https://raw.githubusercontent.com/buildkite/pipeline-schema/refs/heads/main/schema.json";

/// Errors that can occur when working with the schema
#[derive(Error, Debug)]
pub enum SchemaError {
    #[error("Failed to fetch schema: {0}")]
    FetchError(#[from] reqwest::Error),

    #[error("Failed to parse schema JSON: {0}")]
    ParseError(#[from] serde_json::Error),

    #[error("Schema validation error: {0}")]
    ValidationError(String),
}

/// Schema representation of Buildkite pipeline
#[derive(Clone)]
pub struct BuildkiteSchema {
    /// The raw JSON schema
    schema: Value,
    /// A mapping of JSON schema paths to their documentation
    documentation: HashMap<String, String>,
    /// Definitions from the schema
    definitions: HashMap<String, Value>,
}

impl BuildkiteSchema {
    /// Create a new schema instance from parsed JSON
    pub fn new(schema: Value) -> Self {
        let mut documentation = HashMap::new();
        let mut definitions = HashMap::new();

        // Extract definitions
        if let Value::Object(schema_obj) = &schema {
            if let Some(Value::Object(defs)) = schema_obj.get("definitions") {
                for (key, value) in defs.iter() {
                    definitions.insert(key.clone(), value.clone());
                }
            }
        }

        // Extract documentation from the schema
        extract_documentation(&schema, "", &mut documentation);

        Self {
            schema,
            documentation,
            definitions,
        }
    }

    /// Load the schema from the official Buildkite schema JSON
    pub async fn load() -> Result<Self, Box<dyn std::error::Error>> {
        info!("Downloading Buildkite pipeline schema from {}", BUILDKITE_SCHEMA_URL);
        
        // Create a client and fetch the schema
        let client = reqwest::Client::new();
        let response = client.get(BUILDKITE_SCHEMA_URL).send().await?
            .error_for_status()?;
        let schema_json = response.text().await?;
        
        info!("Parsing Buildkite pipeline schema");
        let schema: Value = serde_json::from_str(&schema_json)?;
        
        debug!("Schema title: {}", schema.get("title").and_then(|v| v.as_str()).unwrap_or("Unknown"));
        Ok(Self::new(schema))
    }

    /// Validate a pipeline document against the schema
    pub fn validate(&self, document: &str) -> Vec<String> {
        let mut errors = Vec::new();

        // Parse the document as YAML
        match serde_yaml::from_str::<Value>(document) {
            Ok(yaml) => {
                // For now, we'll do a basic validation check
                // In a full implementation, we would use a JSON Schema validator
                if let Value::Object(obj) = &yaml {
                    // Basic validation rules
                    self.validate_required_fields(obj, &mut errors);
                    self.validate_steps(obj, &mut errors);
                } else {
                    errors.push("Document root must be a YAML object".to_string());
                }
            }
            Err(e) => {
                errors.push(format!("Failed to parse pipeline YAML: {}", e));
            }
        }

        errors
    }

    /// Validate required fields in the pipeline
    fn validate_required_fields(&self, doc: &Map<String, Value>, errors: &mut Vec<String>) {
        // Check for required 'steps' field
        if !doc.contains_key("steps") {
            errors.push("Pipeline must contain a 'steps' array".to_string());
        }
    }

    /// Validate steps in the pipeline
    fn validate_steps(&self, doc: &Map<String, Value>, errors: &mut Vec<String>) {
        if let Some(Value::Array(steps)) = doc.get("steps") {
            if steps.is_empty() {
                errors.push("Pipeline must contain at least one step".to_string());
            }

            // Validate each step
            for (i, step) in steps.iter().enumerate() {
                if let Value::Object(step_obj) = step {
                    // Check for at least one step type
                    let has_command = step_obj.contains_key("command");
                    let has_trigger = step_obj.contains_key("trigger");
                    let has_wait = step_obj.contains_key("wait");
                    let has_block = step_obj.contains_key("block");
                    let has_group = step_obj.contains_key("group");
                    
                    if !has_command && !has_trigger && !has_wait && !has_block && !has_group {
                        errors.push(format!(
                            "Step {} must contain one of: 'command', 'trigger', 'wait', 'block', or 'group'", 
                            i + 1
                        ));
                    }
                } else {
                    errors.push(format!(
                        "Step {} must be an object", 
                        i + 1
                    ));
                }
            }
        }
    }

    /// Get documentation for a specific schema element
    pub fn get_documentation(&self, path: &str) -> Option<String> {
        self.documentation.get(path).cloned()
    }

    /// Get all possible properties at a specific path
    pub fn get_properties_at_path(&self, path: &str) -> Vec<String> {
        let mut properties = Vec::new();
        
        // For now, a simplified implementation
        if path.is_empty() || path == "/" {
            // Root level properties
            properties.extend_from_slice(&[
                "steps".to_string(),
                "env".to_string(),
                "agents".to_string(),
                "name".to_string(),
            ]);
        } else if path.ends_with("/steps") {
            // Step types
            properties.extend_from_slice(&[
                "command".to_string(),
                "trigger".to_string(),
                "wait".to_string(),
                "block".to_string(),
                "group".to_string(),
            ]);
        }
        
        properties
    }
}

/// Extract documentation from the schema
fn extract_documentation(value: &Value, path: &str, docs: &mut HashMap<String, String>) {
    match value {
        Value::Object(obj) => {
            // If this object has a description, add it to the documentation map
            if let Some(Value::String(desc)) = obj.get("description") {
                if !path.is_empty() {
                    docs.insert(path.to_string(), desc.clone());
                }
            }
            
            // Recursively process properties
            if let Some(Value::Object(props)) = obj.get("properties") {
                for (key, prop_val) in props.iter() {
                    let new_path = if path.is_empty() {
                        key.clone()
                    } else {
                        format!("{}/{}", path, key)
                    };
                    extract_documentation(prop_val, &new_path, docs);
                }
            }
            
            // Process items for arrays
            if let Some(items) = obj.get("items") {
                let new_path = if path.is_empty() {
                    "items".to_string()
                } else {
                    format!("{}/items", path)
                };
                extract_documentation(items, &new_path, docs);
            }
        },
        Value::Array(arr) => {
            // Process all items in the array
            for (i, item) in arr.iter().enumerate() {
                let new_path = format!("{}/{}", path, i);
                extract_documentation(item, &new_path, docs);
            }
        },
        _ => {}
    }
}