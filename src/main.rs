use buildkite_ls::Backend;
use std::error::Error;
use tower_lsp::{LspService, Server};
use tracing_subscriber::{EnvFilter, FmtSubscriber};

#[tokio::main]
async fn main() -> Result<(), Box<dyn Error + Sync + Send>> {
    // Initialize the tracing subscriber for logging
    let subscriber = FmtSubscriber::builder()
        .with_env_filter(EnvFilter::from_default_env())
        .with_ansi(atty::is(atty::Stream::Stderr))
        .finish();

    tracing::subscriber::set_global_default(subscriber)
        .expect("Failed to set global default subscriber");

    // Create the transport for stdin/stdout communication
    let (stdin, stdout) = (tokio::io::stdin(), tokio::io::stdout());

    // Create the language server instance
    let (service, socket) = LspService::new(|client| Backend::new(client));
    Server::new(stdin, stdout, socket).serve(service).await;

    Ok(())
}