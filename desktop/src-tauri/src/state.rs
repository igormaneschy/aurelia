use std::sync::atomic::{AtomicU64, Ordering};

use aurelia_ipc::IpcClient;
use tokio::sync::Mutex;
use tokio_util::sync::CancellationToken;

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
        Self {
            client,
            user_id: unsafe { libc::getuid() as i64 },
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
