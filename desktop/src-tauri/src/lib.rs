mod commands;
mod state;

use std::path::PathBuf;

use aurelia_ipc::IpcClient;
use state::AppState;
use tauri::Manager;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    // A missing HOME leaves the app in an offline state instead of failing to
    // start; the UI shows the resolved socket path and a clear error.
    let socket_path = IpcClient::default_path()
        .unwrap_or_else(|_| PathBuf::from("/nonexistent/aurelia.sock"));

    tauri::Builder::default()
        // Tauri's macOS default is already true, but making it explicit keeps
        // the desktop client in front when launched from a terminal or IDE.
        .activate_ignoring_other_apps(true)
        .plugin(tauri_plugin_dialog::init())
        .manage(AppState::new(IpcClient::new(socket_path)))
        .setup(|app| {
            if let Some(window) = app.get_webview_window("main") {
                let _ = window.show();
                let _ = window.set_focus();
            }
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            commands::daemon_status,
            commands::list_sessions,
            commands::create_session,
            commands::open_session,
            commands::delete_session,
            commands::rename_session,
            commands::load_history,
            commands::send_message,
            commands::cancel_run,
            commands::run_command,
            commands::get_project_state,
            commands::get_models,
        ])
        .run(tauri::generate_context!())
        .expect("erro ao iniciar o Aurelia Desktop");
}
