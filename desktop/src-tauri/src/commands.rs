use aurelia_ipc::{
    HistoryMessage, IpcAttachment, IpcClient, IpcEvent, IpcImage, IpcMessage, ProgressPayload,
    ProjectStatePayload, SessionInfo, TuiModelEntry, EVENT_ACK, EVENT_ERROR, EVENT_HISTORY,
    EVENT_MESSAGE, EVENT_MODELS, EVENT_PROGRESS, EVENT_PROJECT_STATE, EVENT_SESSIONS,
    EVENT_SESSION_CREATED, EVENT_SESSION_OPENED, EVENT_SESSION_RENAMED, EVENT_STREAM_CHUNK,
    EVENT_STREAM_END,
};
use serde::Serialize;
use tauri::{AppHandle, Emitter, Manager, State};
use tokio_util::sync::CancellationToken;

use crate::state::{AppState, RunState};

/// Tauri event used to stream assistant output to the frontend.
const STREAM_EVENT: &str = "aurelia://stream";

#[derive(Debug, Serialize)]
pub struct DaemonStatus {
    pub online: bool,
    pub socket_path: String,
    pub latency_ms: Option<u64>,
    pub error: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(tag = "kind", rename_all = "lowercase")]
enum StreamPayload {
    Chunk {
        chat_id: i64,
        text: String,
    },
    Message {
        chat_id: i64,
        text: String,
    },
    Progress {
        chat_id: i64,
        progress: ProgressPayload,
    },
    Done {
        chat_id: i64,
    },
    Cancelled {
        chat_id: i64,
    },
    Error {
        chat_id: i64,
        message: String,
    },
}

fn parse_event_body<T: serde::de::DeserializeOwned>(
    events: &[IpcEvent],
    event_type: &str,
) -> Result<T, String> {
    let event = events
        .iter()
        .find(|event| event.msg_type == event_type)
        .ok_or_else(|| format!("o daemon não retornou o evento {event_type}"))?;

    serde_json::from_str(&event.body)
        .map_err(|error| format!("resposta {event_type} inválida: {error}"))
}

#[tauri::command(rename_all = "snake_case")]
pub async fn daemon_status(state: State<'_, AppState>) -> Result<DaemonStatus, String> {
    let socket_path = state.client.socket_path().display().to_string();
    let result = state.client.ping().await;

    let status = match result {
        Ok(latency) => DaemonStatus {
            online: true,
            socket_path,
            latency_ms: Some(latency.as_millis() as u64),
            error: None,
        },
        Err(error) => DaemonStatus {
            online: false,
            socket_path,
            latency_ms: None,
            error: Some(error.to_string()),
        },
    };
    Ok(status)
}

#[tauri::command(rename_all = "snake_case")]
pub async fn list_sessions(state: State<'_, AppState>) -> Result<Vec<SessionInfo>, String> {
    let request_id = state.next_request_id();
    let events = state
        .client
        .request(&IpcMessage::sessions(request_id))
        .await
        .map_err(|error| error.to_string())?;

    parse_event_body(&events, EVENT_SESSIONS)
}

#[tauri::command(rename_all = "snake_case")]
pub async fn create_session(
    state: State<'_, AppState>,
    name: String,
) -> Result<SessionInfo, String> {
    let request_id = state.next_request_id();
    let events = state
        .client
        .request(&IpcMessage::session_create(name, request_id))
        .await
        .map_err(|error| error.to_string())?;

    parse_event_body(&events, EVENT_SESSION_CREATED)
}

#[tauri::command(rename_all = "snake_case")]
pub async fn open_session(
    state: State<'_, AppState>,
    chat_id: i64,
) -> Result<SessionInfo, String> {
    let request_id = state.next_request_id();
    let events = state
        .client
        .request(&IpcMessage::session_open(chat_id, request_id))
        .await
        .map_err(|error| error.to_string())?;

    parse_event_body(&events, EVENT_SESSION_OPENED)
}

#[tauri::command(rename_all = "snake_case")]
pub async fn delete_session(state: State<'_, AppState>, chat_id: i64) -> Result<(), String> {
    let request_id = state.next_request_id();
    state
        .client
        .request(&IpcMessage::session_delete(chat_id, request_id))
        .await
        .map_err(|error| error.to_string())?;
    Ok(())
}

#[tauri::command(rename_all = "snake_case")]
pub async fn rename_session(
    state: State<'_, AppState>,
    chat_id: i64,
    name: String,
) -> Result<SessionInfo, String> {
    let request_id = state.next_request_id();
    let events = state
        .client
        .request(&IpcMessage::session_rename(chat_id, name, request_id))
        .await
        .map_err(|error| error.to_string())?;

    parse_event_body(&events, EVENT_SESSION_RENAMED)
}

#[tauri::command(rename_all = "snake_case")]
pub async fn load_history(
    state: State<'_, AppState>,
    chat_id: i64,
) -> Result<Vec<HistoryMessage>, String> {
    let request_id = state.next_request_id();
    let events = state
        .client
        .request(&IpcMessage::history(chat_id, state.user_id, request_id))
        .await
        .map_err(|error| error.to_string())?;

    parse_event_body(&events, EVENT_HISTORY)
}

#[tauri::command(rename_all = "snake_case")]
pub async fn get_project_state(
    state: State<'_, AppState>,
    chat_id: i64,
) -> Result<ProjectStatePayload, String> {
    let request_id = state.next_request_id();
    let events = state
        .client
        .request(&IpcMessage::project_state(
            chat_id,
            state.user_id,
            request_id,
        ))
        .await
        .map_err(|error| error.to_string())?;

    parse_event_body(&events, EVENT_PROJECT_STATE)
}

#[tauri::command(rename_all = "snake_case")]
pub async fn get_models(state: State<'_, AppState>) -> Result<Vec<TuiModelEntry>, String> {
    let request_id = state.next_request_id();
    let events = state
        .client
        .request(&IpcMessage::models(state.user_id, request_id))
        .await
        .map_err(|error| error.to_string())?;

    parse_event_body(&events, EVENT_MODELS)
}

/// Runs a slash command (`/status`, `/cwd`, `/model`, `/agents`, `/reset`, …)
/// and returns the text the daemon produced.
#[tauri::command(rename_all = "snake_case")]
pub async fn run_command(
    state: State<'_, AppState>,
    chat_id: i64,
    text: String,
) -> Result<String, String> {
    let request_id = state.next_request_id();
    let events = state
        .client
        .request(&IpcMessage::command(
            chat_id,
            state.user_id,
            text,
            request_id,
        ))
        .await
        .map_err(|error| error.to_string())?;

    let mut output = String::new();
    for event in events {
        if event.msg_type == EVENT_MESSAGE {
            if !output.is_empty() {
                output.push('\n');
            }
            output.push_str(&event.body);
        }
    }
    Ok(output)
}

#[tauri::command(rename_all = "snake_case")]
pub async fn send_message(
    app: AppHandle,
    state: State<'_, AppState>,
    chat_id: i64,
    text: String,
    images: Vec<IpcImage>,
    attachments: Vec<IpcAttachment>,
) -> Result<(), String> {
    let (run_id, token) = {
        let mut run = state.run.lock().await;
        if let Some(current) = run.as_ref() {
            return Err(format!(
                "Já existe uma resposta em andamento na sessão {}.",
                current.chat_id
            ));
        }

        let run_id = state.next_run_id();
        let token = CancellationToken::new();
        *run = Some(RunState {
            id: run_id,
            chat_id,
            token: token.clone(),
        });
        (run_id, token)
    };

    let client = state.client.clone();
    let user_id = state.user_id;
    let request_id = state.next_request_id();

    tauri::async_runtime::spawn(async move {
        let result = stream_turn(
            &app,
            &client,
            user_id,
            chat_id,
            &text,
            images,
            attachments,
            &request_id,
            token,
        )
        .await;

        if let Err(error) = result {
            let _ = app.emit(
                STREAM_EVENT,
                StreamPayload::Error {
                    chat_id,
                    message: error,
                },
            );
        }

        let state = app.state::<AppState>();
        let mut run = state.run.lock().await;
        if run
            .as_ref()
            .is_some_and(|current| current.id == run_id)
        {
            *run = None;
        }
    });

    Ok(())
}

#[tauri::command(rename_all = "snake_case")]
pub async fn cancel_run(state: State<'_, AppState>) -> Result<bool, String> {
    let run = state.run.lock().await;
    match run.as_ref() {
        Some(run) => {
            run.token.cancel();
            Ok(true)
        }
        None => Ok(false),
    }
}

#[allow(clippy::too_many_arguments)]
async fn stream_turn(
    app: &AppHandle,
    client: &IpcClient,
    user_id: i64,
    chat_id: i64,
    text: &str,
    images: Vec<IpcImage>,
    attachments: Vec<IpcAttachment>,
    request_id: &str,
    token: CancellationToken,
) -> Result<(), String> {
    let message = IpcMessage::send(chat_id, user_id, text, images, attachments, request_id);
    let mut stream = client.send(&message).await.map_err(|error| error.to_string())?;

    loop {
        tokio::select! {
            _ = token.cancelled() => {
                let _ = app.emit(STREAM_EVENT, StreamPayload::Cancelled { chat_id });
                return Ok(());
            }
            next = stream.next() => {
                let event = match next.map_err(|error| error.to_string())? {
                    Some(event) => event,
                    None => {
                        let _ = app.emit(
                            STREAM_EVENT,
                            StreamPayload::Error {
                                chat_id,
                                message: "A conexão com o daemon foi encerrada.".to_owned(),
                            },
                        );
                        return Ok(());
                    }
                };

                match event.msg_type.as_str() {
                    EVENT_ACK => {}
                    EVENT_STREAM_CHUNK => {
                        let _ = app.emit(
                            STREAM_EVENT,
                            StreamPayload::Chunk {
                                chat_id,
                                text: event.body,
                            },
                        );
                    }
                    EVENT_MESSAGE => {
                        let _ = app.emit(
                            STREAM_EVENT,
                            StreamPayload::Message {
                                chat_id,
                                text: event.body,
                            },
                        );
                    }
                    EVENT_PROGRESS => {
                        if let Ok(progress) = serde_json::from_str::<ProgressPayload>(&event.body) {
                            let _ = app.emit(
                                STREAM_EVENT,
                                StreamPayload::Progress { chat_id, progress },
                            );
                        }
                    }
                    EVENT_STREAM_END => {
                        let _ = app.emit(STREAM_EVENT, StreamPayload::Done { chat_id });
                        return Ok(());
                    }
                    EVENT_ERROR => {
                        let message = if event.error.is_empty() {
                            "Erro desconhecido do daemon.".to_owned()
                        } else {
                            event.error
                        };
                        let _ = app.emit(
                            STREAM_EVENT,
                            StreamPayload::Error { chat_id, message },
                        );
                        return Ok(());
                    }
                    _ => {}
                }
            }
        }
    }
}
