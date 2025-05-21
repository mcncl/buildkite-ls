//! Buildkite Language Server implementation
//! Provides LSP features for Buildkite pipeline YAML files

mod server;
pub mod schema;
pub mod parser;
mod completion;
mod hover;
mod diagnostics;

// Re-export the modules needed for public API
pub use server::Backend;