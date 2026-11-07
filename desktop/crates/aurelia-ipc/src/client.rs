use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};

use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use tokio::net::UnixStream;
use tokio::time::timeout;

use crate::error::IpcError;
use crate::types::{IpcEvent, IpcMessage, EVENT_ERROR, EVENT_STREAM_END};

/// Maximum size accepted for a single daemon event line.
///
/// The TUI uses a 64 KiB scanner, but `get-session-history` payloads can reach
/// 256 KiB, so the desktop client allows a comfortable margin.
pub const MAX_EVENT_LINE_BYTES: usize = 1024 * 1024;

/// Default timeout for connecting to the daemon.
pub const DEFAULT_DIAL_TIMEOUT: Duration = Duration::from_secs(5);

fn default_path_in(home: &Path) -> PathBuf {
    let root = home.join(".aurelia");
    if root.join("nas-tunnel.enabled").exists() {
        root.join("nas.sock")
    } else {
        root.join("aurelia.sock")
    }
}

/// A thin client for the Aurelia daemon's Unix-socket IPC.
#[derive(Debug, Clone)]
pub struct IpcClient {
    socket_path: PathBuf,
    dial_timeout: Duration,
}

impl IpcClient {
    pub fn new(socket_path: impl Into<PathBuf>) -> Self {
        Self {
            socket_path: socket_path.into(),
            dial_timeout: DEFAULT_DIAL_TIMEOUT,
        }
    }

    pub fn with_timeout(socket_path: impl Into<PathBuf>, dial_timeout: Duration) -> Self {
        Self {
            socket_path: socket_path.into(),
            dial_timeout,
        }
    }

    /// Resolves the daemon socket path using `$HOME`.
    ///
    /// Resolution order:
    /// 1. `AURELIA_IPC_SOCKET` for an explicit path (tests, custom setups).
    /// 2. `~/.aurelia/nas.sock` when the NAS tunnel marker
    ///    `~/.aurelia/nas-tunnel.enabled` exists.
    /// 3. `~/.aurelia/aurelia.sock` for a local daemon.
    pub fn default_path() -> Result<PathBuf, IpcError> {
        if let Some(path) = std::env::var_os("AURELIA_IPC_SOCKET") {
            return Ok(PathBuf::from(path));
        }
        let home = std::env::var_os("HOME").ok_or(IpcError::NoHome)?;
        Ok(default_path_in(Path::new(&home)))
    }

    pub fn socket_path(&self) -> &Path {
        &self.socket_path
    }

    /// Opens a new connection and validates that the socket is reachable.
    pub async fn connect(&self) -> Result<UnixStream, IpcError> {
        let path = self.socket_path.display().to_string();
        let result = timeout(self.dial_timeout, UnixStream::connect(&self.socket_path))
            .await
            .map_err(|_| IpcError::Connect {
                path: path.clone(),
                source: std::io::Error::new(
                    std::io::ErrorKind::TimedOut,
                    "tempo de conexão esgotado",
                ),
            })?;

        result.map_err(|source| IpcError::Connect { path, source })
    }

    /// Connects and immediately disconnects. Returns the observed latency.
    pub async fn ping(&self) -> Result<Duration, IpcError> {
        let started = Instant::now();
        let stream = self.connect().await?;
        drop(stream);
        Ok(started.elapsed())
    }

    /// Sends one request and returns a stream of events for that connection.
    pub async fn send(&self, message: &IpcMessage) -> Result<EventStream, IpcError> {
        let mut stream = self.connect().await?;
        let mut data = serde_json::to_vec(message)?;
        data.push(b'\n');
        stream.write_all(&data).await?;
        stream.flush().await?;
        Ok(EventStream {
            reader: BufReader::new(stream),
        })
    }

    /// Sends one request and collects every event until the terminal event.
    ///
    /// Returns an error when the daemon emits an `error` event.
    pub async fn request(&self, message: &IpcMessage) -> Result<Vec<IpcEvent>, IpcError> {
        let mut stream = self.send(message).await?;
        let mut events = Vec::new();

        while let Some(event) = stream.next().await? {
            let is_error = event.msg_type == EVENT_ERROR;
            let is_end = event.msg_type == EVENT_STREAM_END;
            events.push(event);

            if is_error {
                let message = events
                    .last()
                    .map(|event| event.error.clone())
                    .unwrap_or_default();
                return Err(IpcError::Server(message));
            }
            if is_end {
                break;
            }
        }

        Ok(events)
    }
}

/// Event reader for a single request/response connection.
pub struct EventStream {
    reader: BufReader<UnixStream>,
}

impl EventStream {
    /// Reads the next event. Returns `None` when the daemon closes the socket.
    pub async fn next(&mut self) -> Result<Option<IpcEvent>, IpcError> {
        loop {
            let mut buffer = Vec::new();
            let read = self.reader.read_until(b'\n', &mut buffer).await?;
            if read == 0 {
                return Ok(None);
            }
            if buffer.len() > MAX_EVENT_LINE_BYTES {
                return Err(IpcError::LineTooLarge {
                    limit: MAX_EVENT_LINE_BYTES,
                    actual: buffer.len(),
                });
            }

            let end = if buffer.last() == Some(&b'\n') {
                buffer.len() - 1
            } else {
                buffer.len()
            };
            if end == 0 || buffer[..end].iter().all(|byte| byte.is_ascii_whitespace()) {
                continue;
            }

            return Ok(Some(serde_json::from_slice(&buffer[..end])?));
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::types::{IpcMessage, EVENT_ACK, EVENT_SESSIONS};
    use std::fs;
    use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
    use tokio::net::UnixListener;

    #[test]
    fn default_path_uses_nas_socket_only_with_tunnel_marker() {
        let dir = tempfile::tempdir().unwrap();
        let root = dir.path().join(".aurelia");
        fs::create_dir_all(&root).unwrap();

        assert_eq!(default_path_in(dir.path()), root.join("aurelia.sock"));

        fs::write(root.join("nas-tunnel.enabled"), b"").unwrap();
        assert_eq!(default_path_in(dir.path()), root.join("nas.sock"));
    }

    #[tokio::test]
    async fn request_reads_events_until_stream_end() {
        let dir = tempfile::tempdir().unwrap();
        let socket_path = dir.path().join("aurelia.sock");
        let listener = UnixListener::bind(&socket_path).unwrap();

        let server = tokio::spawn(async move {
            let (connection, _) = listener.accept().await.unwrap();
            let mut reader = BufReader::new(connection);
            let mut line = String::new();
            reader.read_line(&mut line).await.unwrap();

            let request: serde_json::Value = serde_json::from_str(&line).unwrap();
            assert_eq!(request["type"].as_str(), Some("sessions"));

            let mut connection = reader.into_inner();
            connection
                .write_all(b"{\"type\":\"ack\"}\n")
                .await
                .unwrap();
            connection
                .write_all(b"{\"type\":\"sessions\",\"body\":\"[]\"}\n")
                .await
                .unwrap();
            connection
                .write_all(b"{\"type\":\"stream_end\",\"done\":true}\n")
                .await
                .unwrap();
        });

        let client = IpcClient::new(&socket_path);
        let events = client
            .request(&IpcMessage::sessions("test-request"))
            .await
            .unwrap();

        assert_eq!(events.len(), 3);
        assert_eq!(events[0].msg_type, EVENT_ACK);
        assert_eq!(events[1].msg_type, EVENT_SESSIONS);
        assert_eq!(events[1].body, "[]");
        assert!(events[2].done);

        server.await.unwrap();
    }

    #[tokio::test]
    async fn request_surfaces_server_errors() {
        let dir = tempfile::tempdir().unwrap();
        let socket_path = dir.path().join("aurelia.sock");
        let listener = UnixListener::bind(&socket_path).unwrap();

        let server = tokio::spawn(async move {
            let (mut connection, _) = listener.accept().await.unwrap();
            let mut line = String::new();
            BufReader::new(&mut connection)
                .read_line(&mut line)
                .await
                .unwrap();
            connection
                .write_all(b"{\"type\":\"error\",\"error\":\"boom\"}\n")
                .await
                .unwrap();
        });

        let client = IpcClient::new(&socket_path);
        let error = client
            .request(&IpcMessage::sessions("test-request"))
            .await
            .unwrap_err();

        assert!(error.to_string().contains("boom"));
        server.await.unwrap();
    }
}
