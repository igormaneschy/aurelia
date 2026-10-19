//! Rust client for the Aurelia daemon's Unix-socket IPC.
//!
//! The wire protocol is newline-delimited JSON. A client opens one connection
//! per request and reads events until `stream_end` or `error`.

pub mod client;
pub mod error;
pub mod types;

pub use client::{EventStream, IpcClient, MAX_EVENT_LINE_BYTES};
pub use error::IpcError;
pub use types::*;
