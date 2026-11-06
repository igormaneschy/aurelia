import { invoke } from "@tauri-apps/api/core";
import type {
  DaemonStatus,
  HistoryMessage,
  ModelEntry,
  ProjectState,
  SessionInfo,
} from "./types";

export type ImageInput = {
  path: string;
  media_type: string;
};

export type AttachmentInput = {
  path: string;
  name?: string;
};

export const api = {
  daemonStatus: () => invoke<DaemonStatus>("daemon_status"),
  listSessions: () => invoke<SessionInfo[]>("list_sessions"),
  createSession: (name: string) => invoke<SessionInfo>("create_session", { name }),
  openSession: (chat_id: number) => invoke<SessionInfo>("open_session", { chat_id }),
  deleteSession: (chat_id: number) => invoke<void>("delete_session", { chat_id }),
  renameSession: (chat_id: number, name: string) =>
    invoke<SessionInfo>("rename_session", { chat_id, name }),
  loadHistory: (chat_id: number) =>
    invoke<HistoryMessage[]>("load_history", { chat_id }),
  getProjectState: (chat_id: number) =>
    invoke<ProjectState>("get_project_state", { chat_id }),
  getModels: () => invoke<ModelEntry[]>("get_models"),
  runCommand: (chat_id: number, text: string) =>
    invoke<string>("run_command", { chat_id, text }),
  sendMessage: (
    chat_id: number,
    text: string,
    images: ImageInput[],
    attachments: AttachmentInput[],
  ) => invoke<void>("send_message", { chat_id, text, images, attachments }),
  cancelRun: () => invoke<boolean>("cancel_run"),
};

export function errorMessage(error: unknown): string {
  if (typeof error === "string") return error;
  if (error instanceof Error) return error.message;
  return String(error);
}

export function friendlyError(error: unknown): string {
  const message = errorMessage(error);

  if (message.includes("no free TUI session slots")) {
    return "Limite de sessões da TUI atingido (8 nomeadas + DM). Apague uma sessão na TUI ou amplie o namespace no daemon.";
  }
  if (message.includes("não foi possível conectar ao daemon")) {
    return "O daemon do Aurelia não está acessível. Verifique se o serviço está a correr e tente novamente.";
  }

  return message;
}
