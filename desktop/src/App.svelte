<script lang="ts">
  import { onMount } from "svelte";
  import { get } from "svelte/store";
  import { listen, type UnlistenFn } from "@tauri-apps/api/event";
  import { getCurrentWebview } from "@tauri-apps/api/webview";
  import { confirm as confirmDialog } from "@tauri-apps/plugin-dialog";

  import Sidebar from "./lib/components/Sidebar.svelte";
  import ChatHeader from "./lib/components/ChatHeader.svelte";
  import Transcript from "./lib/components/Transcript.svelte";
  import Composer from "./lib/components/Composer.svelte";
  import ProjectPanel from "./lib/components/ProjectPanel.svelte";
  import Modal from "./lib/components/Modal.svelte";

  import { api, friendlyError } from "./lib/api";
  import {
    activeSession,
    addPendingPaths,
    clearNotice,
    clearPending,
    daemon,
    loadingHistory,
    loadingProject,
    messages,
    models,
    notice,
    notify,
    pendingAttachments,
    projectState,
    running,
    runStatus,
    sessions,
  } from "./lib/stores";
  import {
    DM_CHAT_ID,
    type ChatMessage,
    type ProgressPayload,
    type SessionInfo,
    type StreamPayload,
  } from "./lib/types";

  let showProject = false;
  let modelsLoading = false;
  let modalTitle = "";
  let modalBody = "";
  let unlistenStream: UnlistenFn | undefined;
  let unlistenDrag: UnlistenFn | undefined;

  function newId(): string {
    if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
      return crypto.randomUUID();
    }
    return `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  }

  async function refreshDaemon() {
    try {
      daemon.set(await api.daemonStatus());
    } catch (error) {
      daemon.set({
        online: false,
        socket_path: "",
        latency_ms: null,
        error: friendlyError(error),
      });
    }
  }

  async function refreshSessions(): Promise<SessionInfo[]> {
    try {
      const list = await api.listSessions();
      const withDm = list.some((session) => session.chat_id === DM_CHAT_ID)
        ? list
        : [
            { chat_id: DM_CHAT_ID, name: "dm", message_count: 0 },
            ...list,
          ];
      sessions.set(withDm);
      return withDm;
    } catch (error) {
      notify("error", friendlyError(error));
      await refreshDaemon();
      return get(sessions);
    }
  }

  async function loadHistory(chatId: number) {
    const history = await api.loadHistory(chatId);
    messages.set(
      history.map((message, index) => ({
        id: `${chatId}-history-${index}`,
        role: message.sender === "Aurelia" ? "assistant" : "user",
        text: message.text,
        timestamp: message.timestamp,
      })),
    );
  }

  async function refreshProject(chatId: number) {
    loadingProject.set(true);
    try {
      projectState.set(await api.getProjectState(chatId));
    } catch (error) {
      notify("error", friendlyError(error));
    } finally {
      loadingProject.set(false);
    }
  }

  async function selectSession(session: SessionInfo) {
    if (get(running)) {
      notify("info", "Aguarde a resposta atual antes de trocar de sessão.");
      return;
    }

    activeSession.set(session);
    localStorage.setItem("aurelia.lastChatId", String(session.chat_id));
    messages.set([]);
    runStatus.set("");
    loadingHistory.set(true);

    try {
      await api.openSession(session.chat_id);
      await loadHistory(session.chat_id);
      await refreshProject(session.chat_id);
    } catch (error) {
      notify("error", friendlyError(error));
    } finally {
      loadingHistory.set(false);
    }
  }

  async function refreshAll() {
    await refreshDaemon();
    const list = await refreshSessions();
    const current = get(activeSession);
    if (current) {
      await refreshProject(current.chat_id);
      return;
    }
    const target = list[0];
    if (target) await selectSession(target);
  }

  async function boot() {
    await refreshDaemon();
    const list = await refreshSessions();
    const status = get(daemon);
    if (status?.online === false || list.length === 0) return;

    const saved = Number(localStorage.getItem("aurelia.lastChatId"));
    const savedSession = Number.isFinite(saved)
      ? list.find((session) => session.chat_id === saved)
      : undefined;
    const dmSession = list.find((session) => session.chat_id === DM_CHAT_ID);
    const target = savedSession ?? dmSession ?? list[0];

    if (target) await selectSession(target);
  }

  function formatProgress(progress: ProgressPayload): string {
    if (progress.tool_done) return "Ferramenta concluída";
    if (progress.tool_name) {
      return progress.detail
        ? `${progress.tool_name} · ${progress.detail}`
        : progress.tool_name;
    }

    switch (progress.state) {
      case "working":
        return "Trabalhando…";
      case "waiting":
        return "Aguardando o modelo…";
      case "stall_warning":
        return "O modelo está a demorar…";
      case "stall_urgent":
        return "Ainda a aguardar resposta…";
      case "done":
        return "Concluído";
      case "canceled":
        return "Cancelado";
      case "failed":
        return "Falhou";
      default:
        return progress.detail || "A processar…";
    }
  }

  function lastStreamingAssistantIndex(list: ChatMessage[]): number {
    for (let index = list.length - 1; index >= 0; index -= 1) {
      if (list[index].role === "assistant" && list[index].streaming) return index;
    }
    return -1;
  }

  function isToolChunk(text: string): boolean {
    const trimmed = text.trim();
    return trimmed.startsWith("🔧 ") || trimmed === "✅ tool_done";
  }

  function appendChunk(text: string) {
    if (isToolChunk(text)) return;

    messages.update((current) => {
      const next = [...current];
      const index = lastStreamingAssistantIndex(next);
      if (index === -1) {
        next.push({
          id: newId(),
          role: "assistant",
          text,
          streaming: true,
        });
      } else {
        next[index] = { ...next[index], text: next[index].text + text };
      }
      return next;
    });
  }

  function replaceAssistant(text: string) {
    messages.update((current) => {
      const next = [...current];
      const index = lastStreamingAssistantIndex(next);
      if (index === -1) {
        next.push({
          id: newId(),
          role: "assistant",
          text,
          streaming: true,
        });
      } else {
        next[index] = { ...next[index], text };
      }
      return next;
    });
  }

  function finishStreamingAssistant() {
    messages.update((current) =>
      current
        .filter(
          (message) =>
            !(
              message.role === "assistant" &&
              message.streaming &&
              message.text.trim() === ""
            ),
        )
        .map((message) =>
          message.streaming ? { ...message, streaming: false } : message,
        ),
    );
  }

  async function afterTurn(chatId: number) {
    await Promise.all([refreshDaemon(), refreshSessions(), refreshProject(chatId)]);
  }

  function handleStreamEvent(payload: StreamPayload) {
    const session = get(activeSession);
    if (!session || payload.chat_id !== session.chat_id) return;

    switch (payload.kind) {
      case "chunk":
        appendChunk(payload.text);
        break;
      case "message":
        replaceAssistant(payload.text);
        break;
      case "progress":
        runStatus.set(formatProgress(payload.progress));
        break;
      case "done":
        running.set(false);
        runStatus.set("");
        finishStreamingAssistant();
        void afterTurn(payload.chat_id);
        break;
      case "cancelled":
        running.set(false);
        runStatus.set("");
        finishStreamingAssistant();
        messages.update((current) => [
          ...current,
          {
            id: newId(),
            role: "system",
            text: "Resposta cancelada.",
          },
        ]);
        void afterTurn(payload.chat_id);
        break;
      case "error":
        running.set(false);
        runStatus.set("");
        finishStreamingAssistant();
        messages.update((current) => [
          ...current,
          {
            id: newId(),
            role: "error",
            text: payload.message,
          },
        ]);
        notify("error", payload.message);
        void afterTurn(payload.chat_id);
        break;
    }
  }

  async function send(text: string): Promise<boolean> {
    const session = get(activeSession);
    if (!session) {
      notify("error", "Nenhuma sessão ativa.");
      return false;
    }
    if (get(running)) {
      notify("info", "Aguarde a resposta atual.");
      return false;
    }
    if (get(daemon)?.online !== true) {
      notify("error", "O daemon está offline.");
      await refreshDaemon();
      return false;
    }

    const pending = get(pendingAttachments);
    const documents = pending.filter((attachment) => attachment.kind === "document");
    if (documents.length > 0 && !get(projectState)?.cwd) {
      notify(
        "error",
        "Defina um projeto antes de anexar documentos: eles são copiados para <projeto>/uploads.",
      );
      return false;
    }

    const images = pending
      .filter((attachment) => attachment.kind === "image")
      .map((attachment) => ({
        path: attachment.path,
        media_type: attachment.media_type,
      }));
    const attachments = documents.map((attachment) => ({
      path: attachment.path,
      name: attachment.name,
    }));

    messages.update((current) => [
      ...current,
      { id: newId(), role: "user", text },
      { id: newId(), role: "assistant", text: "", streaming: true },
    ]);
    running.set(true);
    runStatus.set("Pensando…");
    clearPending();

    try {
      await api.sendMessage(session.chat_id, text, images, attachments);
      return true;
    } catch (error) {
      running.set(false);
      finishStreamingAssistant();
      const message = friendlyError(error);
      messages.update((current) => [
        ...current,
        { id: newId(), role: "error", text: message },
      ]);
      notify("error", message);
      return false;
    }
  }

  async function cancel() {
    try {
      await api.cancelRun();
    } catch (error) {
      notify("error", friendlyError(error));
    }
  }

  async function createSession(name: string) {
    try {
      const created = await api.createSession(name);
      const list = await refreshSessions();
      const target = list.find((session) => session.chat_id === created.chat_id) ?? created;
      await selectSession(target);
    } catch (error) {
      notify("error", friendlyError(error));
    }
  }

  async function openProjectPanel() {
    showProject = true;
    const session = get(activeSession);
    if (!session) return;

    if (get(models).length === 0) {
      modelsLoading = true;
      try {
        models.set(await api.getModels());
      } catch (error) {
        notify("error", friendlyError(error));
      } finally {
        modelsLoading = false;
      }
    }

    await refreshProject(session.chat_id);
  }

  async function setCwd(path: string) {
    const session = get(activeSession);
    if (!session) return;

    try {
      const response = await api.runCommand(session.chat_id, `/cwd ${path}`);
      notify("info", response || "Projeto atualizado.");
      await refreshProject(session.chat_id);
    } catch (error) {
      notify("error", friendlyError(error));
    }
  }

  async function clearCwd() {
    const session = get(activeSession);
    if (!session) return;

    try {
      const response = await api.runCommand(session.chat_id, "/cwd clear");
      notify("info", response || "Projeto removido.");
      await refreshProject(session.chat_id);
    } catch (error) {
      notify("error", friendlyError(error));
    }
  }

  async function setModel(provider: string, id: string) {
    const session = get(activeSession);
    if (!session) return;

    try {
      const response = await api.runCommand(session.chat_id, `/model ${provider}/${id}`);
      notify("info", response || "Modelo atualizado.");
      await loadHistory(session.chat_id);
      await refreshProject(session.chat_id);
    } catch (error) {
      notify("error", friendlyError(error));
    }
  }

  async function setAutoModel() {
    const session = get(activeSession);
    if (!session) return;

    try {
      const response = await api.runCommand(session.chat_id, "/model auto");
      notify("info", response || "Modelo PI default ativado.");
      await loadHistory(session.chat_id);
      await refreshProject(session.chat_id);
    } catch (error) {
      notify("error", friendlyError(error));
    }
  }

  async function showStatus() {
    const session = get(activeSession);
    if (!session) return;
    try {
      modalTitle = "Status";
      modalBody = (await api.runCommand(session.chat_id, "/status")) || "Sem resposta.";
    } catch (error) {
      notify("error", friendlyError(error));
    }
  }

  async function showProfiles() {
    const session = get(activeSession);
    if (!session) return;
    try {
      modalTitle = "Perfis";
      modalBody = (await api.runCommand(session.chat_id, "/agents")) || "Sem resposta.";
    } catch (error) {
      notify("error", friendlyError(error));
    }
  }

  async function showHelp() {
    const session = get(activeSession);
    if (!session) return;
    try {
      modalTitle = "Ajuda";
      modalBody = (await api.runCommand(session.chat_id, "/help")) || "Sem resposta.";
    } catch (error) {
      notify("error", friendlyError(error));
    }
  }

  async function resetSession() {
    const session = get(activeSession);
    if (!session) return;

    const accepted = await confirmDialog(
      "Reiniciar a sessão? O contexto PI atual será apagado.",
      { title: "Reiniciar sessão", kind: "warning" },
    );
    if (!accepted) return;

    try {
      const response = await api.runCommand(session.chat_id, "/reset");
      notify("info", response || "Sessão reiniciada.");
      await loadHistory(session.chat_id);
      await refreshProject(session.chat_id);
    } catch (error) {
      notify("error", friendlyError(error));
    }
  }

  onMount(() => {
    listen<StreamPayload>("aurelia://stream", (event) =>
      handleStreamEvent(event.payload),
    )
      .then((unlisten) => {
        unlistenStream = unlisten;
      })
      .catch((error) => notify("error", friendlyError(error)));

    getCurrentWebview()
      .onDragDropEvent((event) => {
        if (event.payload.type === "drop") {
          addPendingPaths(event.payload.paths);
        }
      })
      .then((unlisten) => {
        unlistenDrag = unlisten;
      })
      .catch(() => {});

    void boot();

    return () => {
      unlistenStream?.();
      unlistenDrag?.();
    };
  });
</script>

<svelte:window
  on:keydown={(event) => {
    if (event.key !== "Escape") return;
    if (modalBody) {
      modalBody = "";
    } else if (showProject) {
      showProject = false;
    }
  }}
/>

<div class="app-shell">
  <Sidebar
    sessions={$sessions}
    activeSession={$activeSession}
    daemon={$daemon}
    project={$projectState}
    running={$running}
    onSelect={selectSession}
    onCreate={createSession}
    onRefresh={refreshAll}
    onShowProject={openProjectPanel}
    onShowStatus={showStatus}
    onShowProfiles={showProfiles}
    onShowHelp={showHelp}
    onReset={resetSession}
  />

  <main class="main-panel">
    <ChatHeader
      session={$activeSession}
      project={$projectState}
      daemon={$daemon}
      running={$running}
      runStatus={$runStatus}
    />

    {#if $daemon && !$daemon.online}
      <div class="offline-banner">
        <div>
          <strong>Daemon offline.</strong>
          O cliente não encontrou o socket em <code>{$daemon.socket_path || "~/.aurelia/aurelia.sock"}</code>.
          Inicie o serviço com <code>make deploy</code> no repositório ou
          <code>launchctl kickstart -k gui/$(id -u)/com.aurelia.agent</code>.
        </div>
        <button class="primary small" on:click={refreshAll}>Tentar novamente</button>
      </div>
    {/if}

    <Transcript
      messages={$messages}
      runStatus={$runStatus}
      running={$running}
      loading={$loadingHistory}
    />

    <Composer
      running={$running}
      disabled={$daemon?.online !== true || !$activeSession}
      onSend={send}
      onCancel={cancel}
    />
  </main>

  {#if showProject}
    <ProjectPanel
      project={$projectState}
      models={$models}
      modelsLoading={modelsLoading}
      loading={$loadingProject}
      running={$running}
      onSetCwd={setCwd}
      onClearCwd={clearCwd}
      onSetModel={setModel}
      onSetAutoModel={setAutoModel}
      onRefresh={() => {
        const session = get(activeSession);
        if (session) void refreshProject(session.chat_id);
      }}
      onClose={() => (showProject = false)}
    />
  {/if}

  {#if modalBody}
    <Modal title={modalTitle} body={modalBody} onClose={() => (modalBody = "")} />
  {/if}

  {#if $notice}
    <div class="notice {$notice.kind}" role="status">
      <span>{$notice.text}</span>
      <button class="icon-button" title="Fechar" on:click={clearNotice}>×</button>
    </div>
  {/if}
</div>
