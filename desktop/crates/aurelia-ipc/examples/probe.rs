//! Small manual probe for the real daemon socket.
//!
//! Run with:
//!   cargo run -p aurelia-ipc --example probe

use aurelia_ipc::{
    IpcClient, IpcMessage, ProjectStatePayload, RESERVED_TUI_CHAT_ID, TuiModelEntry,
};

#[tokio::main(flavor = "current_thread")]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let socket_path = IpcClient::default_path()?;
    let client = IpcClient::new(&socket_path);
    println!("socket: {}", socket_path.display());

    let events = client
        .request(&IpcMessage::sessions("probe-sessions"))
        .await?;

    for event in events {
        println!("{} {}", event.msg_type, event.body);
    }

    let events = client
        .request(&IpcMessage::project_state(
            RESERVED_TUI_CHAT_ID,
            0,
            "probe-project-state",
        ))
        .await?;

    for event in events {
        if event.msg_type == "project_state" {
            let state: ProjectStatePayload = serde_json::from_str(&event.body)?;
            println!(
                "project: cwd={:?} model={:?} bridge={:?}",
                state.cwd, state.model, state.bridge_status
            );
        }
    }

    let events = client
        .request(&IpcMessage::models(0, "probe-models"))
        .await?;

    for event in events {
        if event.msg_type == "models" {
            let models: Vec<TuiModelEntry> = serde_json::from_str(&event.body)?;
            println!("models: {} entries", models.len());
        }
    }

    let events = client
        .request(&IpcMessage::command(
            RESERVED_TUI_CHAT_ID,
            0,
            "/status",
            "probe-status",
        ))
        .await?;

    for event in events {
        if event.msg_type == "message" {
            println!("status:\n{}", event.body);
        }
    }

    let events = client
        .request(&IpcMessage::history(
            RESERVED_TUI_CHAT_ID,
            0,
            "probe-history",
        ))
        .await?;

    for event in events {
        if event.msg_type == "history" {
            println!("history: {}", event.body);
        }
    }

    Ok(())
}
