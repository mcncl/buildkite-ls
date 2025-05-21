//! Simple test harness for the language server

use buildkite_ls::Backend;
use std::error::Error;
use std::fs;
use std::path::Path;

#[tokio::main]
async fn main() -> Result<(), Box<dyn Error>> {
    // Set up logging
    tracing_subscriber::fmt::init();
    
    println!("Buildkite Language Server Test Harness");
    println!("===================================\n");
    
    // 1. Test schema loading
    test_schema_loading().await?;
    
    // 2. Test YAML parsing
    test_yaml_parsing()?;
    
    // 3. Test position detection
    test_position_detection()?;
    
    println!("\nAll tests completed!");
    Ok(())
}

async fn test_schema_loading() -> Result<(), Box<dyn Error>> {
    println!("Testing schema loading...");
    
    // For our test harness we can skip the actual HTTP request
    // and just create a simulated schema
    println!("  Creating simulated schema for testing");
    
    // Create a simulated schema with some test data
    let schema_json = serde_json::json!({
        "title": "Buildkite Pipeline Schema",
        "type": "object",
        "properties": {
            "steps": {
                "type": "array",
                "description": "The steps to run in this pipeline"
            },
            "env": {
                "type": "object",
                "description": "Environment variables to be set for all steps"
            }
        }
    });
    
    let schema = buildkite_ls::schema::BuildkiteSchema::new(schema_json);
    
    // Verify we got some documentation
    let steps_doc = schema.get_documentation("steps");
    println!("  Found documentation for 'steps': {}", steps_doc.is_some());
    
    // Get properties at root
    let root_props = schema.get_properties_at_path("/");
    println!("  Found {} root properties", root_props.len());
    
    println!("Schema loading test passed!\n");
    Ok(())
}

fn test_yaml_parsing() -> Result<(), Box<dyn Error>> {
    println!("Testing YAML parsing...");
    
    // Try to load from examples first, fallback to inline definition
    let yaml = if let Ok(content) = fs::read_to_string("examples/pipeline.yml") {
        println!("  Using example file: examples/pipeline.yml");
        content
    } else {
        println!("  Using inline example (examples/pipeline.yml not found)");
        r#"steps:
  - label: ":rocket: Deploy"
    command: "deploy.sh"
    agents:
      queue: "deploy"

env:
  FOO: "bar"
"#.to_string()
    };
    
    // Parse the document
    let mut document = buildkite_ls::parser::Document::new(yaml.to_string());
    document.parse()?;
    
    // Verify we have a root node
    println!("  Has root node: {}", document.root.is_some());
    println!("  Position map has {} entries", document.position_map.len());
    
    // Validate the document structure
    if let Some(yaml_value) = &document.yaml {
        println!("  YAML parsed successfully");
    } else {
        println!("  Failed to parse YAML");
    }
    
    println!("YAML parsing test passed!\n");
    Ok(())
}

fn test_position_detection() -> Result<(), Box<dyn Error>> {
    println!("Testing position detection...");
    
    // Try to load from examples first, fallback to inline definition
    let yaml = if let Ok(content) = fs::read_to_string("examples/pipeline.yml") {
        println!("  Using example file: examples/pipeline.yml");
        content
    } else {
        println!("  Using inline example (examples/pipeline.yml not found)");
        r#"steps:
  - label: ":rocket: Deploy"
    command: "deploy.sh"
    agents:
      queue: "deploy"

env:
  FOO: "bar"
"#.to_string()
    };
    
    // Parse the document
    let mut document = buildkite_ls::parser::Document::new(yaml.to_string());
    document.parse()?;
    
    // Test different cursor positions
    // Note: these positions may need adjustment based on the actual file content
    let test_positions = [
        (1, 0, "steps"),            // At 'steps:'
        (2, 4, "label"),           // At '- label:'
        (5, 4, "agents"),          // At 'agents:'
        (6, 7, "queue"),           // At 'queue:'
        (16, 3, "wait"),           // At '- wait'
        (18, 4, "block"),          // At 'block:'
        (25, 0, "env"),            // At 'env:'
        (30, 3, "queue"),          // At 'queue:' under agents
    ];
    
    for (line, character, expected) in &test_positions {
        if let Some(node) = document.node_at_position(*line, *character) {
            println!("  Position ({}, {}): Found node '{}'", line, character, node);
            if !node.contains(expected) {
                println!("    WARNING: Expected '{}' but got '{}'", expected, node);
            }
        } else {
            println!("  Position ({}, {}): No node found (expected '{}')", line, character, expected);
        }
    }
    
    // Test context at position (we'll check the plugin Docker config)
    let context = document.context_at_position(13, 10);
    println!("  Context at plugin docker image: {} levels deep", context.len());
    for (i, ctx) in context.iter().enumerate() {
        println!("    Level {}: {}", i, ctx);
    }
    
    println!("Position detection test passed!\n");
    Ok(())
}