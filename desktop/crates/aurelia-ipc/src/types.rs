use serde::{Deserialize, Serialize};

/// Default TUI local DM chat id. The desktop client shares this namespace so a
/// conversation started in the TUI can continue here.
pub const RESERVED_TUI_CHAT_ID: i64 = -9_000_001;

/// Lowest reserved TUI local chat id.
pub const RESERVED_TUI_CHAT_ID_FLOOR: i64 = -9_000_009;

/// Returns true when `chat_id` belongs to the reserved local TUI namespace.
pub fn is_reserved_tui_id(chat_id: i64) -> bool {
    (RESERVED_TUI_CHAT_ID_FLOOR..=RESERVED_TUI_CHAT_ID).contains(&chat_id)
}

// Client -> daemon message types.
pub const MSG_SEND: &str = "send";
pub const MSG_COMMAND: &str = "command";
pub const MSG_HISTORY: &str = "history";
pub const MSG_SESSIONS: &str = "sessions";
pub const MSG_SESSION_CREATE: &str = "session_create";
pub const MSG_SESSION_OPEN: &str = "session_open";
pub const MSG_SESSION_DELETE: &str = "session_delete";
pub const MSG_SESSION_RENAME: &str = "session_rename";
pub const MSG_PROJECT_STATE: &str = "project_state";
pub const MSG_MODELS: &str = "models";

// Daemon -> client event types.
pub const EVENT_ACK: &str = "ack";
pub const EVENT_MESSAGE: &str = "message";
pub const EVENT_STREAM_CHUNK: &str = "stream_chunk";
pub const EVENT_STREAM_END: &str = "stream_end";
pub const EVENT_ERROR: &str = "error";
pub const EVENT_HISTORY: &str = "history";
pub const EVENT_SESSIONS: &str = "sessions";
pub const EVENT_SESSION_CREATED: &str = "session_created";
pub const EVENT_SESSION_OPENED: &str = "session_opened";
pub const EVENT_SESSION_DELETED: &str = "session_deleted";
pub const EVENT_SESSION_RENAMED: &str = "session_renamed";
pub const EVENT_PROJECT_STATE: &str = "project_state";
pub const EVENT_MODELS: &str = "models";
pub const EVENT_PROGRESS: &str = "progress";

/// Image attachment sent to the daemon. Local IPC uses `path`; `data` is the
/// base64 fallback accepted by the daemon but not used by this client.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct IpcImage {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub path: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub data: Option<String>,
    pub media_type: String,
}

/// Document attachment sent to the daemon. The daemon copies the file into the
/// active project's `uploads/` directory, so `path` must be absolute.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct IpcAttachment {
    pub path: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,
}

/// Request sent from the desktop client to the daemon.
#[derive(Debug, Clone, Default, Serialize)]
pub struct IpcMessage {
    #[serde(rename = "type")]
    pub msg_type: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub chat_id: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub thread_id: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub user_id: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub text: Option<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub images: Vec<IpcImage>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub attachments: Vec<IpcAttachment>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub request_id: Option<String>,
}

impl IpcMessage {
    fn new(msg_type: &str, request_id: String) -> Self {
        Self {
            msg_type: msg_type.to_owned(),
            request_id: Some(request_id),
            ..Self::default()
        }
    }

    pub fn send(
        chat_id: i64,
        user_id: i64,
        text: impl Into<String>,
        images: Vec<IpcImage>,
        attachments: Vec<IpcAttachment>,
        request_id: impl Into<String>,
    ) -> Self {
        Self {
            chat_id: Some(chat_id),
            user_id: Some(user_id),
            text: Some(text.into()),
            images,
            attachments,
            ..Self::new(MSG_SEND, request_id.into())
        }
    }

    pub fn command(
        chat_id: i64,
        user_id: i64,
        text: impl Into<String>,
        request_id: impl Into<String>,
    ) -> Self {
        Self {
            chat_id: Some(chat_id),
            user_id: Some(user_id),
            text: Some(text.into()),
            ..Self::new(MSG_COMMAND, request_id.into())
        }
    }

    pub fn history(chat_id: i64, user_id: i64, request_id: impl Into<String>) -> Self {
        Self {
            chat_id: Some(chat_id),
            user_id: Some(user_id),
            ..Self::new(MSG_HISTORY, request_id.into())
        }
    }

    pub fn sessions(request_id: impl Into<String>) -> Self {
        Self::new(MSG_SESSIONS, request_id.into())
    }

    pub fn session_create(name: impl Into<String>, request_id: impl Into<String>) -> Self {
        Self {
            text: Some(name.into()),
            ..Self::new(MSG_SESSION_CREATE, request_id.into())
        }
    }

    pub fn session_open(chat_id: i64, request_id: impl Into<String>) -> Self {
        Self {
            chat_id: Some(chat_id),
            ..Self::new(MSG_SESSION_OPEN, request_id.into())
        }
    }

    pub fn session_delete(chat_id: i64, request_id: impl Into<String>) -> Self {
        Self {
            chat_id: Some(chat_id),
            ..Self::new(MSG_SESSION_DELETE, request_id.into())
        }
    }

    pub fn session_rename(
        chat_id: i64,
        name: impl Into<String>,
        request_id: impl Into<String>,
    ) -> Self {
        Self {
            chat_id: Some(chat_id),
            text: Some(name.into()),
            ..Self::new(MSG_SESSION_RENAME, request_id.into())
        }
    }

    pub fn project_state(chat_id: i64, user_id: i64, request_id: impl Into<String>) -> Self {
        Self {
            chat_id: Some(chat_id),
            user_id: Some(user_id),
            ..Self::new(MSG_PROJECT_STATE, request_id.into())
        }
    }

    pub fn models(user_id: i64, request_id: impl Into<String>) -> Self {
        Self {
            user_id: Some(user_id),
            ..Self::new(MSG_MODELS, request_id.into())
        }
    }
}

/// Event sent from the daemon to the desktop client.
#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub struct IpcEvent {
    #[serde(rename = "type")]
    pub msg_type: String,
    #[serde(default)]
    pub body: String,
    #[serde(default)]
    pub done: bool,
    #[serde(default)]
    pub request_id: String,
    #[serde(default)]
    pub error: String,
}

/// JSON payload of a `project_state` event.
#[derive(Debug, Clone, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct ProjectStatePayload {
    pub cwd: String,
    pub binding_source: String,
    pub binding_from: String,
    pub active_agent: String,
    pub model: String,
    pub bridge_status: String,
    pub memory_layers: Vec<ProjectStateMemoryLayer>,
    pub checkpoint_layer: String,
    pub latest_run: Option<ProjectStateRun>,
    pub session_input_tokens: i64,
    pub session_output_tokens: i64,
    pub session_cost_usd: f64,
    pub session_context_pct: f64,
    pub session_context_tokens: i64,
    pub session_context_window: i64,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct ProjectStateMemoryLayer {
    pub name: String,
    pub scope: String,
    pub exists: bool,
    pub file_count: i64,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct ProjectStateRun {
    pub status: String,
    pub checkpoint: String,
    pub agent_name: String,
    pub started_at: String,
    pub duration_ms: i64,
}

/// JSON payload of a `progress` event.
#[derive(Debug, Clone, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct ProgressPayload {
    pub state: String,
    pub detail: String,
    pub tool_name: String,
    pub tool_done: bool,
    pub elapsed_ms: i64,
}

/// One provider/model entry returned by the `models` event.
#[derive(Debug, Clone, Default, Deserialize, Serialize)]
pub struct TuiModelEntry {
    pub provider: String,
    pub id: String,
}

/// One local session returned by the `sessions` event.
#[derive(Debug, Clone, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct SessionInfo {
    pub chat_id: i64,
    pub name: String,
    pub message_count: i64,
}

/// One user/assistant message returned by the `history` event.
#[derive(Debug, Clone, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct HistoryMessage {
    pub sender: String,
    pub text: String,
    pub timestamp: String,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn send_message_uses_wire_field_names() {
        let message = IpcMessage::send(
            RESERVED_TUI_CHAT_ID,
            501,
            "olá",
            vec![IpcImage {
                path: Some("/tmp/a.png".into()),
                data: None,
                media_type: "image/png".into(),
            }],
            vec![],
            "req-1",
        );

        let json = serde_json::to_value(&message).unwrap();
        assert_eq!(json["type"], MSG_SEND);
        assert_eq!(json["chat_id"], RESERVED_TUI_CHAT_ID);
        assert_eq!(json["user_id"], 501);
        assert_eq!(json["images"][0]["media_type"], "image/png");
        assert!(json.get("attachments").is_none());
    }

    #[test]
    fn reserved_namespace_matches_daemon_range() {
        assert!(is_reserved_tui_id(RESERVED_TUI_CHAT_ID));
        assert!(is_reserved_tui_id(RESERVED_TUI_CHAT_ID_FLOOR));
        assert!(!is_reserved_tui_id(RESERVED_TUI_CHAT_ID_FLOOR - 1));
        assert!(!is_reserved_tui_id(0));
    }
}
