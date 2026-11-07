use std::sync::atomic::{AtomicU64, Ordering};

use aurelia_ipc::IpcClient;
use tokio::sync::Mutex;
use tokio_util::sync::CancellationToken;

/// User ID sent in IPC requests.
///
/// Zero asks the daemon to resolve the canonical persona; the TUI handler
/// overrides every client-supplied ID anyway. The local OS UID must not be
/// used because the desktop can talk to a daemon on another machine (the NAS),
/// where the peer UID belongs to the remote account and would not match.
fn resolve_user_id(value: Option<&str>) -> i64 {
    value
        .and_then(|raw| raw.trim().parse::<i64>().ok())
        .unwrap_or(0)
}

/// Shared state for the Tauri commands.
pub struct AppState {
    pub client: IpcClient,
    pub user_id: i64,
    /// There is at most one in-flight agent turn, matching the daemon's global
    /// TUI run guard.
    pub run: Mutex<Option<RunState>>,
    next_request: AtomicU64,
    next_run: AtomicU64,
}

pub struct RunState {
    pub id: u64,
    pub chat_id: i64,
    pub token: CancellationToken,
}

impl AppState {
    pub fn new(client: IpcClient) -> Self {
        let user_id = resolve_user_id(std::env::var("AURELIA_IPC_USER_ID").ok().as_deref());
        Self {
            client,
            user_id,
            run: Mutex::new(None),
            next_request: AtomicU64::new(1),
            next_run: AtomicU64::new(1),
        }
    }

    pub fn next_request_id(&self) -> String {
        let sequence = self.next_request.fetch_add(1, Ordering::Relaxed);
        format!("desktop-{}-{}", std::process::id(), sequence)
    }

    pub fn next_run_id(&self) -> u64 {
        self.next_run.fetch_add(1, Ordering::Relaxed)
    }
}

#[cfg(test)]
mod tests {
    use super::resolve_user_id;

    #[test]
    fn user_id_defaults_to_persona_resolution() {
        assert_eq!(resolve_user_id(None), 0);
        assert_eq!(resolve_user_id(Some("   ")), 0);
        assert_eq!(resolve_user_id(Some("1000")), 1000);
        assert_eq!(resolve_user_id(Some("invalid")), 0);
    }
}
