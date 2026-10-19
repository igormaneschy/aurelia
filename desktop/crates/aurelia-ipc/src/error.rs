use thiserror::Error;

/// Errors raised while talking to the Aurelia daemon.
#[derive(Debug, Error)]
pub enum IpcError {
    #[error("HOME não definido; não foi possível localizar o socket do Aurelia")]
    NoHome,

    #[error("não foi possível conectar ao daemon em {path}: {source}")]
    Connect {
        path: String,
        #[source]
        source: std::io::Error,
    },

    #[error("falha de I/O no IPC: {0}")]
    Io(#[from] std::io::Error),

    #[error("resposta IPC inválida: {0}")]
    Json(#[from] serde_json::Error),

    #[error("daemon retornou erro: {0}")]
    Server(String),

    #[error("linha IPC excede {limit} bytes (recebidos {actual})")]
    LineTooLarge { limit: usize, actual: usize },
}
