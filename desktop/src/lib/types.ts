export const DM_CHAT_ID = -9000001;

export type SessionInfo = {
  chat_id: number;
  name: string;
  message_count: number;
};

export type HistoryMessage = {
  sender: string;
  text: string;
  timestamp: string;
};

export type ProjectStateMemoryLayer = {
  name: string;
  scope: string;
  exists: boolean;
  file_count: number;
};

export type ProjectStateRun = {
  status: string;
  checkpoint: string;
  agent_name: string;
  started_at: string;
  duration_ms: number;
};

export type ProjectState = {
  cwd: string;
  binding_source: string;
  binding_from: string;
  active_agent: string;
  model: string;
  bridge_status: string;
  memory_layers: ProjectStateMemoryLayer[];
  checkpoint_layer: string;
  latest_run: ProjectStateRun | null;
  session_input_tokens: number;
  session_output_tokens: number;
  session_cost_usd: number;
  session_context_pct: number;
  session_context_tokens: number;
  session_context_window: number;
};

export type ModelEntry = {
  provider: string;
  id: string;
};

export type ProgressPayload = {
  state: string;
  detail: string;
  tool_name: string;
  tool_done: boolean;
  elapsed_ms: number;
};

export type StreamPayload =
  | { kind: "chunk"; chat_id: number; text: string }
  | { kind: "message"; chat_id: number; text: string }
  | { kind: "progress"; chat_id: number; progress: ProgressPayload }
  | { kind: "done"; chat_id: number }
  | { kind: "cancelled"; chat_id: number }
  | { kind: "error"; chat_id: number; message: string };

export type DaemonStatus = {
  online: boolean;
  socket_path: string;
  latency_ms: number | null;
  error: string | null;
};

export type PendingAttachment = {
  path: string;
  name: string;
  kind: "image" | "document";
  media_type: string;
};

export type ChatMessage = {
  id: string;
  role: "user" | "assistant" | "system" | "error";
  text: string;
  timestamp?: string;
  streaming?: boolean;
};

export type Notice = {
  kind: "info" | "error";
  text: string;
};
