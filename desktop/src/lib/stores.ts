import { get, writable } from "svelte/store";
import type {
  ChatMessage,
  DaemonStatus,
  ModelEntry,
  Notice,
  PendingAttachment,
  ProjectState,
  SessionInfo,
} from "./types";

export const sessions = writable<SessionInfo[]>([]);
export const activeSession = writable<SessionInfo | null>(null);
export const messages = writable<ChatMessage[]>([]);
export const running = writable(false);
export const runStatus = writable("");
export const projectState = writable<ProjectState | null>(null);
export const models = writable<ModelEntry[]>([]);
export const daemon = writable<DaemonStatus | null>(null);
export const notice = writable<Notice | null>(null);
export const pendingAttachments = writable<PendingAttachment[]>([]);
export const loadingHistory = writable(false);
export const loadingProject = writable(false);

let noticeTimer: ReturnType<typeof setTimeout> | undefined;

export function notify(kind: Notice["kind"], text: string) {
  notice.set({ kind, text });
  clearTimeout(noticeTimer);
  noticeTimer = setTimeout(() => notice.set(null), kind === "error" ? 9000 : 5000);
}

export function clearNotice() {
  clearTimeout(noticeTimer);
  notice.set(null);
}

const IMAGE_MIME: Record<string, string> = {
  png: "image/png",
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
  gif: "image/gif",
  webp: "image/webp",
};

function basename(path: string): string {
  return path.split("/").pop() || path;
}

export function addPendingPaths(paths: string[]) {
  const current = get(pendingAttachments);
  const next = [...current];

  for (const path of paths) {
    if (next.some((attachment) => attachment.path === path)) continue;

    const extension = path.split(".").pop()?.toLowerCase() ?? "";
    const mediaType = IMAGE_MIME[extension];

    next.push({
      path,
      name: basename(path),
      kind: mediaType ? "image" : "document",
      media_type: mediaType ?? "",
    });
  }

  if (next.length > 10) {
    notify("error", "Máximo de 10 anexos por mensagem.");
  }

  pendingAttachments.set(next.slice(0, 10));
}

export function removePending(index: number) {
  pendingAttachments.update((current) => current.filter((_, i) => i !== index));
}

export function clearPending() {
  pendingAttachments.set([]);
}
