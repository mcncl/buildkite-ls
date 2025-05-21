//! Buildkite Language Server implementation
//! Provides LSP features for Buildkite pipeline YAML files

mod server;
mod schema;
mod parser;
mod completion;
mod hover;
mod diagnostics;

// Re-export the modules needed for public API
pub use server::Backend;