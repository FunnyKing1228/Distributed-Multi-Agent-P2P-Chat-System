(() => {
  "use strict";

  // ── State ──
  let ws = null;
  let intentionalClose = false;
  let reconnectTimer = null;
  let userName = "";
  let roomCode = "";
  let installedModels = new Set();
  const messages = [];

  let personas = JSON.parse(localStorage.getItem("dmapc-personas") || "[]");
  let selectedPersonaId = localStorage.getItem("dmapc-selected-persona") || null;
  let editingPersonaId = null;

  // ── Setup DOM refs ──
  const $setup     = document.getElementById("view-setup");
  const $chatView  = document.getElementById("view-chat");
  const $sName     = document.getElementById("s-name");
  const $sRoom     = document.getElementById("s-room");
  const $sAiName   = document.getElementById("s-ai-name");
  const $sAiStyle  = document.getElementById("s-ai-style");
  const $sAiModel  = document.getElementById("s-ai-model");
  const $sMemLimit = document.getElementById("s-mem-limit");
  const $sMemVal   = document.getElementById("s-mem-val");
  const $sNumCtx   = document.getElementById("s-num-ctx");
  const $sCtxVal   = document.getElementById("s-ctx-val");
  const $btnJoin   = document.getElementById("btn-join");
  const $ollamaDot = document.getElementById("ollama-dot");
  const $ollamaStatus = document.getElementById("ollama-status");
  const $installedList = document.getElementById("installed-list");
  const $recommendedList = document.getElementById("recommended-list");
  const $pullProgress = document.getElementById("pull-progress");
  const $pullFill  = document.getElementById("pull-fill");
  const $pullStatus = document.getElementById("pull-status");
  const $personaGallery = document.getElementById("persona-gallery");
  const $personaEditor = document.getElementById("persona-editor");
  const $editorTitle = document.getElementById("editor-title");
  const $btnNewPersona = document.getElementById("btn-new-persona");
  const $btnSavePersona = document.getElementById("btn-save-persona");
  const $btnCancelPersona = document.getElementById("btn-cancel-persona");

  // ── Chat DOM refs ──
  const $chat       = document.getElementById("chat");
  const $input      = document.getElementById("msg-input");
  const $send       = document.getElementById("btn-send");
  const $status     = document.getElementById("status");
  const $peers      = document.getElementById("peer-count");
  const $toggle     = document.getElementById("btn-toggle");
  const $sidebar    = document.getElementById("sidebar");
  const $force      = document.getElementById("btn-force-reply");
  const $attackBtns = Array.from(document.querySelectorAll(".attack-btn"));
  const $attackResult = document.getElementById("attack-result");
  const $securityTrace = document.getElementById("security-trace");
  const $peerAddrs = document.getElementById("peer-addrs");
  const $peerConnectInput = document.getElementById("peer-connect-input");
  const $peerConnectStatus = document.getElementById("peer-connect-status");
  const $refreshPeer = document.getElementById("btn-refresh-peer");
  const $copyPeer = document.getElementById("btn-copy-peer");
  const $connectPeer = document.getElementById("btn-connect-peer");
  const $memberList = document.getElementById("member-list");
  const $memList    = document.getElementById("memory-list");
  const $chatRoom   = document.getElementById("chat-room");
  const $chatName   = document.getElementById("chat-name");
  const $roomBadge  = document.getElementById("room-badge");
  const $diagMessages = document.getElementById("diag-messages");
  const $diagVerified = document.getElementById("diag-verified");
  const $diagRejected = document.getElementById("diag-rejected");
  const $diagRateLimited = document.getElementById("diag-rate-limited");
  const $diagRepaired = document.getElementById("diag-repaired");
  const $diagDuplicates = document.getElementById("diag-duplicates");
  const $diagEquivocation = document.getElementById("diag-equivocation");
  const $diagClockBack = document.getElementById("diag-clock-back");
  const $diagQuarantined = document.getElementById("diag-quarantined");
  const $diagSync = document.getElementById("diag-sync");
  const $diagClock = document.getElementById("diag-clock");
  const $diagTime = document.getElementById("diag-time");
  const $diagLastReject = document.getElementById("diag-last-reject");

  // ── Restore saved values ──
  $sName.value = localStorage.getItem("dmapc-name") || "";
  $sRoom.value = localStorage.getItem("dmapc-room") || "";

  $sMemLimit.addEventListener("input", () => { $sMemVal.textContent = $sMemLimit.value; });
  $sNumCtx.addEventListener("input", () => { $sCtxVal.textContent = $sNumCtx.value; });

  function saveSetupLocals() {
    localStorage.setItem("dmapc-name", $sName.value);
    localStorage.setItem("dmapc-room", $sRoom.value);
  }

  function updateJoinState() {
    $btnJoin.disabled = !($sName.value.trim() && $sRoom.value.trim() && selectedPersonaId);
  }
  $sName.addEventListener("input", updateJoinState);
  $sRoom.addEventListener("input", updateJoinState);

  // ═══════════ PERSONA MANAGEMENT ═══════════

  function savePersonas() {
    localStorage.setItem("dmapc-personas", JSON.stringify(personas));
  }

  function getSelectedPersona() {
    return personas.find(p => p.id === selectedPersonaId) || null;
  }

  function selectPersona(id) {
    selectedPersonaId = id;
    localStorage.setItem("dmapc-selected-persona", id);
    renderPersonaGallery();
    updateJoinState();
  }

  function renderPersonaGallery() {
    $personaGallery.innerHTML = "";
    if (!personas.length) {
      $personaGallery.innerHTML = '<div class="no-personas">No personas yet — create one below!</div>';
      return;
    }
    personas.forEach(p => {
      const el = document.createElement("div");
      el.className = "persona-card" + (p.id === selectedPersonaId ? " selected" : "");
      const initials = (p.name || "?").slice(0, 2).toUpperCase();
      const colors = ["#6c63ff","#e94560","#4caf50","#ff9800","#2196f3","#9c27b0","#00bcd4"];
      const color = colors[Math.abs(hashStr(p.id)) % colors.length];
      el.innerHTML =
        `<div class="persona-avatar" style="background:${color}">${initials}</div>` +
        `<div class="persona-info">` +
          `<div class="pname">${escHtml(p.name)}</div>` +
          `<div class="pstyle">${escHtml(p.style || "No personality set")}</div>` +
          `<div class="pmodel">${p.model || "No model"} · ${p.memLimit || 50} memories · ctx ${p.numCtx || 4096}</div>` +
        `</div>` +
        `<div class="persona-actions">` +
          `<button class="edit" title="Edit">✎</button>` +
          `<button class="del" title="Delete">✕</button>` +
        `</div>`;
      el.addEventListener("click", (e) => {
        if (e.target.closest(".persona-actions")) return;
        selectPersona(p.id);
      });
      el.querySelector(".edit").addEventListener("click", () => openEditor(p.id));
      el.querySelector(".del").addEventListener("click", () => deletePersona(p.id));
      $personaGallery.appendChild(el);
    });
  }

  function hashStr(s) {
    let h = 0;
    for (let i = 0; i < s.length; i++) h = ((h << 5) - h + s.charCodeAt(i)) | 0;
    return h;
  }

  const $memSection = document.getElementById("persona-memory-section");

  function openEditor(id) {
    editingPersonaId = id || null;
    if (id) {
      const p = personas.find(x => x.id === id);
      if (!p) return;
      $editorTitle.textContent = "Edit Persona";
      $sAiName.value = p.name || "";
      $sAiStyle.value = p.style || "";
      $sAiModel.value = p.model || "";
      $sMemLimit.value = p.memLimit || 50;
      $sMemVal.textContent = $sMemLimit.value;
      $sNumCtx.value = p.numCtx || 4096;
      $sCtxVal.textContent = $sNumCtx.value;
      $memSection.classList.remove("hidden");
      loadMemoriesForPersona(p.name);
    } else {
      $editorTitle.textContent = "New Persona";
      $sAiName.value = "";
      $sAiStyle.value = "";
      $sAiModel.value = "";
      $sMemLimit.value = 50;
      $sMemVal.textContent = "50";
      $sNumCtx.value = 4096;
      $sCtxVal.textContent = "4096";
      $memSection.classList.add("hidden");
      $memList.innerHTML = "";
    }
    $personaEditor.classList.remove("hidden");
    $btnNewPersona.classList.add("hidden");
    $sAiName.focus();
  }

  function closeEditor() {
    $personaEditor.classList.add("hidden");
    $btnNewPersona.classList.remove("hidden");
    editingPersonaId = null;
  }

  function saveCurrentPersona() {
    const name = $sAiName.value.trim();
    if (!name) { $sAiName.focus(); return; }

    if (editingPersonaId) {
      const p = personas.find(x => x.id === editingPersonaId);
      if (p) {
        p.name = name;
        p.style = $sAiStyle.value.trim();
        p.model = $sAiModel.value;
        p.memLimit = parseInt($sMemLimit.value) || 50;
        p.numCtx = parseInt($sNumCtx.value) || 4096;
      }
    } else {
      const newP = {
        id: "p-" + Date.now() + "-" + Math.random().toString(36).slice(2, 6),
        name,
        style: $sAiStyle.value.trim(),
        model: $sAiModel.value,
        memLimit: parseInt($sMemLimit.value) || 50,
        numCtx: parseInt($sNumCtx.value) || 4096,
      };
      personas.push(newP);
      selectPersona(newP.id);
    }
    savePersonas();
    renderPersonaGallery();
    closeEditor();
  }

  function deletePersona(id) {
    const p = personas.find(x => x.id === id);
    if (!p || !confirm(`Delete persona "${p.name}"?`)) return;
    personas = personas.filter(x => x.id !== id);
    if (selectedPersonaId === id) {
      selectedPersonaId = personas.length ? personas[0].id : null;
      localStorage.setItem("dmapc-selected-persona", selectedPersonaId || "");
    }
    savePersonas();
    renderPersonaGallery();
    updateJoinState();
  }

  $btnNewPersona.addEventListener("click", () => openEditor(null));
  $btnSavePersona.addEventListener("click", saveCurrentPersona);
  $btnCancelPersona.addEventListener("click", closeEditor);

  renderPersonaGallery();
  updateJoinState();

  // ═══════════ MODEL MANAGEMENT ═══════════

  let recommendedData = [];

  const btnStyle = 'font-size:12px;padding:3px 12px;border-radius:4px;border:1px solid var(--accent);background:transparent;color:var(--accent);cursor:pointer;margin-left:8px';

  let detectedOS = "unknown";

  async function checkOllama() {
    try {
      const r = await fetch("/api/ollama/status");
      const d = await r.json();
      const running = !!d.running;
      const installed = !!d.installed;
      detectedOS = d.os || "unknown";
      $ollamaDot.className = "status-dot " + (running ? "on" : "off");

      if (running) {
        $ollamaStatus.innerHTML = '<span style="color:#4caf50">Running</span>';
      } else if (installed) {
        $ollamaStatus.innerHTML = 'Installed but not running' +
          `<button id="btn-start-ollama" style="${btnStyle}">Start Ollama</button>`;
        document.getElementById("btn-start-ollama")?.addEventListener("click", startOllama);
      } else {
        $ollamaStatus.innerHTML = 'Not installed' +
          `<button id="btn-install-ollama" style="${btnStyle}">Auto Install</button>` +
          `<a href="https://ollama.com/download" target="_blank" style="${btnStyle};text-decoration:none;display:inline-block">Manual Download</a>`;
        document.getElementById("btn-install-ollama")?.addEventListener("click", installOllama);
      }
      return running;
    } catch {
      $ollamaDot.className = "status-dot off";
      $ollamaStatus.innerHTML = 'Cannot reach server';
      return false;
    }
  }

  async function installOllama() {
    $ollamaDot.className = "status-dot pending";
    $ollamaStatus.innerHTML = '<span style="color:#ff9800">Installing Ollama... please wait</span>';
    const $installArea = document.getElementById("ollama-install-progress");
    $installArea.style.display = "block";
    $pullFill.style.width = "0%";
    $pullFill.style.background = "var(--accent)";
    $pullStatus.textContent = "Downloading Ollama...";

    try {
      const resp = await fetch("/api/ollama/install", { method: "POST" });
      const reader = resp.body.getReader();
      const decoder = new TextDecoder();
      let buf = "";

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });
        const lines = buf.split("\n");
        buf = lines.pop();
        for (const line of lines) {
          const text = line.replace(/^data:\s*/, "").trim();
          if (!text) continue;
          if (text === "[DONE] ok") {
            $pullFill.style.width = "100%";
            $pullStatus.textContent = "Ollama installed and running!";
            setTimeout(() => { $installArea.style.display = "none"; }, 3000);
            await checkOllama();
            await loadModels();
            return;
          } else if (text === "[DONE] manual_wait") {
            $pullFill.style.width = "100%";
            $pullFill.style.background = "#ff9800";
            $pullStatus.innerHTML = "Installer launched — please complete it, then <button onclick=\"location.reload()\" style=\"color:var(--accent);background:none;border:1px solid var(--accent);padding:2px 10px;border-radius:4px;cursor:pointer\">Refresh Page</button>";
            return;
          } else if (text.startsWith("[DONE] manual:")) {
            $pullStatus.innerHTML = text.replace("[DONE] manual:", "").trim();
            $pullFill.style.width = "100%";
            $pullFill.style.background = "#ff9800";
            return;
          } else if (text === "[DONE] error") {
            $pullFill.style.width = "100%";
            $pullFill.style.background = "#e94560";
            $pullStatus.innerHTML = 'Auto-install failed — <a href="https://ollama.com/download" target="_blank" style="color:var(--accent)">Download manually from ollama.com</a>';
            return;
          } else {
            $pullStatus.textContent = text;
            if (text.includes("%")) {
              const m = text.match(/(\d+)%/);
              if (m) $pullFill.style.width = m[1] + "%";
            } else {
              $pullFill.style.width = "50%";
            }
          }
        }
      }
    } catch (e) {
      $ollamaStatus.innerHTML = 'Install failed: ' + e.message;
      $pullStatus.textContent = "Error: " + e.message;
    }
  }

  async function startOllama() {
    $ollamaStatus.innerHTML = '<span style="color:#ff9800">Starting Ollama...</span>';
    try {
      const r = await fetch("/api/ollama/start", { method: "POST" });
      const d = await r.json();
      if (d.ok) {
        $ollamaDot.className = "status-dot on";
        $ollamaStatus.innerHTML = '<span style="color:#4caf50">Running</span>';
        await loadModels();
      } else {
        showStartFailed(d.error || "unknown error");
      }
    } catch (e) {
      showStartFailed(e.message);
    }
  }

  function showStartFailed(err) {
    $ollamaDot.className = "status-dot off";
    $ollamaStatus.innerHTML =
      `<span style="color:#e94560">Failed to start</span>` +
      `<button id="btn-retry-start" style="${btnStyle}">Retry</button>` +
      `<button id="btn-reinstall" style="${btnStyle}">Reinstall</button>` +
      `<br><span style="font-size:11px;color:var(--muted);margin-top:4px;display:inline-block">` +
        `Error: ${err}. You can also start Ollama manually and refresh this page.` +
      `</span>`;
    document.getElementById("btn-retry-start")?.addEventListener("click", startOllama);
    document.getElementById("btn-reinstall")?.addEventListener("click", installOllama);
  }

  async function loadModels() {
    try {
      const [modelsRes, recRes] = await Promise.all([
        fetch("/api/models"),
        fetch("/api/models/recommended")
      ]);
      const modelsData = await modelsRes.json();
      recommendedData = await recRes.json();

      installedModels = new Set((modelsData.models || []).map(m => m.name));
      renderInstalledModels(modelsData.models || []);
      renderRecommendedModels();
      populateModelDropdowns();
    } catch (e) {
      $installedList.innerHTML = '<p style="color:var(--muted);font-size:13px">Could not load models. Is Ollama running?</p>';
    }
  }

  function renderInstalledModels(models) {
    if (!models.length) {
      $installedList.innerHTML = '<p style="color:var(--muted);font-size:13px">No models installed yet.</p>';
      return;
    }
    $installedList.innerHTML = '<h3 style="font-size:12px;color:var(--muted);margin-bottom:8px;text-transform:uppercase;letter-spacing:1px">Installed</h3>';
    models.forEach(m => {
      const size = m.size ? (m.size / 1e9).toFixed(1) + " GB" : "";
      const el = document.createElement("div");
      el.className = "model-item";
      el.innerHTML = `<span class="name">${m.name}</span><span class="meta">${size}</span>` +
        `<button class="btn-delete" data-name="${m.name}">Delete</button>`;
      el.querySelector(".btn-delete").addEventListener("click", () => deleteModel(m.name));
      $installedList.appendChild(el);
    });
  }

  function rpStars(n) {
    return "★".repeat(n) + "☆".repeat(5 - n);
  }

  const MODELS_PER_PAGE = 2;
  let recPage = 0;
  let pullingModel = null;

  function renderRecommendedModels() {
    $recommendedList.innerHTML = "";
    const total = recommendedData.length;
    const totalPages = Math.ceil(total / MODELS_PER_PAGE);
    if (recPage >= totalPages) recPage = totalPages - 1;
    if (recPage < 0) recPage = 0;

    const start = recPage * MODELS_PER_PAGE;
    const pageModels = recommendedData.slice(start, start + MODELS_PER_PAGE);

    pageModels.forEach(m => {
      const installed = installedModels.has(m.name);
      const isPulling = pullingModel === m.name;
      const el = document.createElement("div");
      el.className = "model-card" + (m.best ? " best" : "");
      el.dataset.model = m.name;
      el.innerHTML =
        `<div class="model-header">` +
          `<span class="name">${m.name}</span>` +
          (m.best ? `<span class="badge-best">Recommended</span>` : "") +
        `</div>` +
        `<div class="model-desc">${m.desc}</div>` +
        `<div class="model-meta">` +
          `<span>${m.ram} RAM</span>` +
          `<span>${m.params} params</span>` +
          `<span class="rp-score" title="Roleplay quality">RP: ${rpStars(m.rp)}</span>` +
        `</div>` +
        `<div class="model-tags">${(m.tags || "").split(",").map(t => `<span class="tag">${t.trim()}</span>`).join("")}</div>` +
        `<div class="model-action">` +
          (installed
            ? `<button class="btn-installed">✓ Installed</button>`
            : isPulling
              ? `<button class="btn-download" disabled style="opacity:.5">Downloading...</button>`
              : `<button class="btn-download" data-name="${m.name}">Download</button>`) +
        `</div>` +
        `<div class="inline-progress" style="display:${isPulling ? "block" : "none"}">` +
          `<div class="progress-bar" style="display:block"><div class="fill" style="width:0%"></div></div>` +
          `<div class="inline-status" style="font-size:11px;color:var(--muted);margin-top:4px"></div>` +
        `</div>`;
      if (!installed && !isPulling) {
        el.querySelector(".btn-download").addEventListener("click", () => pullModel(m.name));
      }
      $recommendedList.appendChild(el);
    });

    if (totalPages > 1) {
      const nav = document.createElement("div");
      nav.className = "page-nav";
      nav.innerHTML =
        `<button class="pn-btn" id="rec-prev" ${recPage === 0 ? "disabled" : ""}>← Prev</button>` +
        `<span class="pn-info">${recPage + 1} / ${totalPages}</span>` +
        `<button class="pn-btn" id="rec-next" ${recPage >= totalPages - 1 ? "disabled" : ""}>Next →</button>`;
      nav.querySelector("#rec-prev").addEventListener("click", () => { recPage--; renderRecommendedModels(); });
      nav.querySelector("#rec-next").addEventListener("click", () => { recPage++; renderRecommendedModels(); });
      $recommendedList.appendChild(nav);
    }
  }

  function populateModelDropdowns() {
    const cur = $sAiModel.value;
    $sAiModel.innerHTML = '<option value="">— select —</option>';
    installedModels.forEach(name => {
      const opt = document.createElement("option");
      opt.value = name; opt.textContent = name;
      $sAiModel.appendChild(opt);
    });
    if (cur && installedModels.has(cur)) $sAiModel.value = cur;
  }

  function getCardEls(name) {
    const card = $recommendedList.querySelector(`.model-card[data-model="${name}"]`);
    if (!card) return null;
    return {
      card,
      progress: card.querySelector(".inline-progress"),
      fill: card.querySelector(".fill"),
      status: card.querySelector(".inline-status"),
      btn: card.querySelector(".btn-download"),
    };
  }

  async function pullModel(name) {
    pullingModel = name;
    renderRecommendedModels();

    const els = getCardEls(name);
    if (els) {
      els.progress.style.display = "block";
      els.status.textContent = "Starting download...";
    }

    try {
      const resp = await fetch("/api/models/pull", {
        method: "POST",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify({name})
      });
      const reader = resp.body.getReader();
      const decoder = new TextDecoder();
      let buf = "";

      while (true) {
        const {done, value} = await reader.read();
        if (done) break;
        buf += decoder.decode(value, {stream: true});
        const lines = buf.split("\n");
        buf = lines.pop();
        for (const line of lines) {
          const trimmed = line.replace(/^data:\s*/, "").trim();
          if (!trimmed) continue;
          try {
            const d = JSON.parse(trimmed);
            const c = getCardEls(name);
            if (!c) continue;
            if (d.total && d.completed) {
              const pct = Math.round(d.completed / d.total * 100);
              c.fill.style.width = pct + "%";
              c.status.textContent = `${pct}% — ${(d.completed / 1e9).toFixed(1)} / ${(d.total / 1e9).toFixed(1)} GB`;
            } else if (d.status) {
              c.status.textContent = d.status;
            }
          } catch {}
        }
      }
      const c = getCardEls(name);
      if (c) c.status.textContent = "Done!";
    } catch (e) {
      const c = getCardEls(name);
      if (c) c.status.textContent = "Error: " + e.message;
    }

    pullingModel = null;
    await loadModels();
  }

  async function deleteModel(name) {
    if (!confirm("Delete " + name + "?")) return;
    await fetch("/api/models", {
      method: "DELETE",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({name})
    });
    await loadModels();
  }

  // ═══════════ JOIN ROOM ═══════════

  $btnJoin.addEventListener("click", joinRoom);

  // Room member tracking
  const roomMembers = new Map(); // name → { type: "human"|"ai" }

  function addMember(name, type) {
    roomMembers.set(name, { type });
    renderMembers();
    updateMentionMenu();
  }

  function renderMembers() {
    $memberList.innerHTML = "";
    roomMembers.forEach((info, name) => {
      const el = document.createElement("div");
      el.className = "member-item";
      const isMe = name === userName;
      const isAI = info.type === "ai";
      el.innerHTML =
        `<span class="member-dot ${isAI ? "ai" : "human"}"></span>` +
        `<span class="member-name">${escHtml(name)}</span>` +
        (isMe ? '<span class="member-tag you">You</span>' : "") +
        (isAI ? '<span class="member-tag ai">AI</span>' : "");
      $memberList.appendChild(el);
    });
    updateMentionMenu();
  }

  function joinRoom() {
    saveSetupLocals();
    userName = $sName.value.trim();
    roomCode = $sRoom.value.trim();
    const persona = getSelectedPersona();
    if (!persona) return;

    $setup.classList.add("hidden");
    $chatView.style.display = "block";
    $chatRoom.textContent = roomCode;
    $chatName.textContent = userName;
    $roomBadge.textContent = roomCode;

    // Add self and own AI to member list
    roomMembers.clear();
    addMember(userName, "human");
    addMember(persona.name, "ai");

    connectWS();
  }

  // ═══════════ WEBSOCKET (Chat phase) ═══════════

  function connectWS() {
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    ws = new WebSocket(`${proto}//${location.host}/ws`);
    intentionalClose = false;

    ws.addEventListener("open", () => {
      $status.textContent = "Setting up...";
      $status.style.color = "#ff9800";
      const persona = getSelectedPersona() || {};
      ws.send(JSON.stringify({
        type: "setup",
        sender_name: userName,
        room: roomCode,
        ai_name: persona.name || "AI",
        ai_style: persona.style || "",
        ai_model: persona.model || "",
        memory_limit: persona.memLimit || 50,
        num_ctx: persona.numCtx || 4096,
      }));
    });

    ws.addEventListener("close", () => {
      if (intentionalClose) {
        intentionalClose = false;
        return;
      }
      $status.textContent = "Disconnected — reconnecting...";
      $status.style.color = "#e94560";
      $input.disabled = true; $send.disabled = true;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      reconnectTimer = setTimeout(connectWS, 2000);
    });

    ws.addEventListener("message", (evt) => {
      let msg;
      try {
        msg = JSON.parse(evt.data);
      } catch {
        appendSystem("Received malformed WS payload.");
        return;
      }
      switch (msg.type) {
        case "ready":
          $status.textContent = "Connected";
          $status.style.color = "#4caf50";
          $input.disabled = false;
          $send.disabled = false;
          appendSystem("Joined room: " + roomCode);
          loadPeerInfo();
          break;
        case "chat":          removeStreamBubble(msg.sender_name); insertChat(msg); break;
        case "ai_token":
          if (msg.content === "\x00SILENCE") { removeStreamBubble(msg.sender_name); }
          else { appendStreamToken(msg.sender_name, msg.content); }
          break;
        case "activity":     addActivity(msg.sender_name, msg.content); break;
        case "activity_stop": removeActivity(msg.sender_name); break;
        case "system":       appendSystem(msg.content); break;
        case "peers":        $peers.textContent = msg.count; break;
        case "diagnostics":  renderDiagnostics(msg); break;
        case "security_trace": renderSecurityTrace(msg); break;
        case "error":        appendSystem("Error: " + msg.content); break;
      }
    });
  }

  function renderDiagnostics(msg) {
    $diagMessages.textContent = msg.message_count ?? 0;
    $diagVerified.textContent = msg.verified_count ?? 0;
    $diagRejected.textContent = msg.rejected_count ?? 0;
    $diagRateLimited.textContent = msg.rate_limited_count ?? 0;
    $diagRepaired.textContent = msg.repaired_count ?? 0;
    $diagDuplicates.textContent = msg.duplicate_count ?? 0;
    $diagEquivocation.textContent = msg.equivocation_count ?? 0;
    $diagClockBack.textContent = msg.clock_back_count ?? 0;
    $diagQuarantined.textContent = msg.quarantined ?? 0;
    $diagSync.textContent = msg.sync_status || "idle";
    $diagClock.textContent = formatClock(msg.vector_clock || {});
    $diagTime.textContent = msg.time_model || "Vector Clock";
    $diagLastReject.textContent = msg.last_reject || "none";
  }

  function formatClock(clock) {
    const entries = Object.entries(clock);
    if (!entries.length) return "∅";
    return entries
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([peer, value]) => `${peer.slice(0, 4)}:${value}`)
      .join(" ");
  }

  // ── Chat rendering ──
  function compareVC(a, b) {
    if (!a || !b) return 0;
    const keys = new Set([...Object.keys(a), ...Object.keys(b)]);
    let aLess = false, bLess = false;
    for (const k of keys) {
      const va = a[k] || 0, vb = b[k] || 0;
      if (va < vb) aLess = true;
      if (vb < va) bLess = true;
    }
    if (aLess && !bLess) return -1;
    if (bLess && !aLess) return 1;
    return 0;
  }

  function compareMsg(a, b) {
    const vcCmp = compareVC(a.vector_clock, b.vector_clock);
    if (vcCmp !== 0) return vcCmp;
    const senderCmp = String(a.sender_id || "").localeCompare(String(b.sender_id || ""));
    if (senderCmp !== 0) return senderCmp;
    const idCmp = String(a.id || "").localeCompare(String(b.id || ""));
    if (idCmp !== 0) return idCmp;
    const nameCmp = String(a.sender_name || "").localeCompare(String(b.sender_name || ""));
    if (nameCmp !== 0) return nameCmp;
    return String(a.content || "").localeCompare(String(b.content || ""));
  }

  function insertChat(msg) {
    if (msg.sender_name) {
      removeActivity(msg.sender_name);
      if (!roomMembers.has(msg.sender_name)) {
        addMember(msg.sender_name, msg.is_ai ? "ai" : "human");
      }
    }
    if (msg.id && messages.some(m => m.id === msg.id)) return;
    let idx = messages.length;
    for (let i = messages.length - 1; i >= 0; i--) {
      if (compareMsg(messages[i], msg) <= 0) { idx = i + 1; break; }
      idx = i;
    }
    messages.splice(idx, 0, msg);
    const el = buildChatEl(msg);
    const chatMsgEls = $chat.querySelectorAll(".msg:not(.system)");
    const refEl = chatMsgEls[idx];
    refEl ? $chat.insertBefore(el, refEl) : $chat.appendChild(el);
    $chat.scrollTop = $chat.scrollHeight;
    updateMentionMenu();

  }

  function buildChatEl(msg) {
    const isAI = msg.is_ai === true;
    const isSelf = !isAI && msg.self === true;
    const w = document.createElement("div");
    w.className = "msg " + (isSelf ? "self" : "other") + (isAI ? " ai" : "");
    const meta = document.createElement("div");
    meta.className = "meta";
    meta.textContent = isAI
      ? (msg.sender_name || "AI") + " 🤖"
      : isSelf ? "You" : (msg.sender_name || "?");
    w.appendChild(meta);
    const bubble = document.createElement("div");
    bubble.className = "bubble";
    bubble.textContent = msg.content;
    w.appendChild(bubble);
    return w;
  }

  function appendSystem(text) {
    const w = document.createElement("div"); w.className = "msg system";
    const b = document.createElement("div"); b.className = "bubble"; b.textContent = text;
    w.appendChild(b); $chat.appendChild(w);
    $chat.scrollTop = $chat.scrollHeight;
  }

  // ── AI streaming bubble ──
  const streamBubbles = new Map();

  function appendStreamToken(name, token) {
    removeActivity(name);
    let el = streamBubbles.get(name);
    if (!el) {
      el = document.createElement("div");
      el.className = "msg other ai streaming";
      el.dataset.streamName = name;
      const meta = document.createElement("div");
      meta.className = "meta";
      meta.textContent = name + " 🤖";
      el.appendChild(meta);
      const bubble = document.createElement("div");
      bubble.className = "bubble";
      bubble.textContent = "";
      el.appendChild(bubble);
      $chat.appendChild(el);
      streamBubbles.set(name, el);
    }
    el.querySelector(".bubble").textContent += token;
    $chat.scrollTop = $chat.scrollHeight;
  }

  function removeStreamBubble(name) {
    const el = streamBubbles.get(name);
    if (el) {
      el.remove();
      streamBubbles.delete(name);
    }
  }

  // ── Activity bar (typing / thinking) ──
  const $activityBar = document.getElementById("activity-bar");
  const $mentionMenu = document.getElementById("mention-menu");
  const activeUsers = new Map(); // name → action ("typing" | "thinking")
  let mentionState = { start: -1, end: -1, items: [], activeIndex: 0 };

  function addActivity(name, action) {
    if (action === "join") { addMember(name, "human"); return; }
    if (action === "join_ai") { addMember(name, "ai"); return; }
    activeUsers.set(name, action);
    renderActivityBar();
  }

  function removeActivity(name) {
    activeUsers.delete(name);
    renderActivityBar();
  }

  function renderActivityBar() {
    if (activeUsers.size === 0) {
      $activityBar.innerHTML = "";
      return;
    }
    const parts = [];
    const typers = [];
    const thinkers = [];
    activeUsers.forEach((action, name) => {
      if (action === "thinking") thinkers.push(name);
      else typers.push(name);
    });

    if (typers.length) parts.push(`<strong>${typers.join(", ")}</strong> is typing`);
    if (thinkers.length) parts.push(`<strong>${thinkers.join(", ")}</strong> 🤖 is thinking`);

    $activityBar.innerHTML =
      '<span class="typing-dot"></span><span class="typing-dot"></span><span class="typing-dot"></span> ' +
      parts.join(" · ");
  }

  // ── Send + typing detection ──
  let typingTimer = null;
  let isTyping = false;

  function getMentionContext(inputEl = $input) {
    const value = inputEl.value;
    const caret = inputEl.selectionStart ?? value.length;
    const beforeCaret = value.slice(0, caret);
    const match = beforeCaret.match(/(?:^|[\s(])@([A-Za-z0-9_-]*)$/);
    if (!match) return null;
    const query = match[1] || "";
    return {
      query,
      start: caret - query.length - 1,
      end: caret,
    };
  }

  function collectMentionablePeople() {
    const items = [];
    const seen = new Set();
    const add = (name, kind) => {
      name = (name || "").trim();
      if (!name) return;
      const key = name.toLowerCase();
      if (seen.has(key)) return;
      seen.add(key);
      items.push({ name, kind });
    };

    add("all", "everyone");
    add(userName, "you");

    const persona = getSelectedPersona();
    if (persona?.name) add(persona.name, "your AI");

    roomMembers.forEach((info, name) => {
      add(name, info.type === "ai" ? "AI" : "human");
    });

    activeUsers.forEach((_, name) => add(name, "active"));
    streamBubbles.forEach((_, name) => add(name, "streaming"));

    messages.forEach((msg) => {
      add(msg.sender_name, msg.is_ai ? "AI" : "human");
    });

    document.querySelectorAll("#member-list .member-name").forEach((el) => {
      add(el.textContent || "", "member");
    });

    document.querySelectorAll("#chat .msg .meta").forEach((el) => {
      let text = (el.textContent || "").trim();
      if (!text || text === "You") return;
      text = text.replace(/\s*🤖\s*$/, "").trim();
      add(text, "chat");
    });

    return items;
  }

  function getMentionCandidates(query) {
    const lower = query.toLowerCase();
    return collectMentionablePeople()
      .filter(item => item.name.toLowerCase().startsWith(lower))
      .sort((a, b) => {
        if (a.name === "all") return -1;
        if (b.name === "all") return 1;
        return a.name.localeCompare(b.name);
      })
      .slice(0, 6);
  }

  function getEmergencyMentionCandidates(query) {
    const lower = query.toLowerCase();
    const names = new Set();
    document.querySelectorAll(".member-name, .meta").forEach((el) => {
      let text = (el.textContent || "").trim();
      if (!text || text === "You") return;
      text = text.replace(/\s*🤖\s*$/, "").trim();
      if (text.toLowerCase().startsWith(lower)) names.add(text);
    });
    return Array.from(names)
      .sort((a, b) => a.localeCompare(b))
      .slice(0, 6)
      .map(name => ({ name, kind: "fallback" }));
  }

  function hideMentionMenu() {
    mentionState = { start: -1, end: -1, items: [], activeIndex: 0 };
    $mentionMenu.classList.add("hidden");
    $mentionMenu.innerHTML = "";
  }

  function renderMentionMenu() {
    if (!mentionState.items.length) return hideMentionMenu();
    $mentionMenu.innerHTML = "";
    mentionState.items.forEach((item, idx) => {
      const el = document.createElement("div");
      el.className = "mention-item" + (idx === mentionState.activeIndex ? " active" : "");
      el.innerHTML = `<span class="mention-name">@${escHtml(item.name)}</span><span class="mention-kind">${escHtml(item.kind)}</span>`;
      el.addEventListener("mousedown", (e) => {
        e.preventDefault();
        applyMention(idx);
      });
      $mentionMenu.appendChild(el);
    });
    $mentionMenu.classList.remove("hidden");
  }

  function updateMentionMenu() {
    const ctx = getMentionContext();
    if (!ctx) {
      return hideMentionMenu();
    }
    const items = getMentionCandidates(ctx.query);
    if (!items.length) return hideMentionMenu();
    mentionState = {
      start: ctx.start,
      end: ctx.end,
      items,
      activeIndex: 0,
    };
    renderMentionMenu();
  }

  function applyMention(index = mentionState.activeIndex) {
    const item = mentionState.items[index];
    if (!item) return;
    const before = $input.value.slice(0, mentionState.start);
    const after = $input.value.slice(mentionState.end);
    const insert = `@${item.name} `;
    $input.value = before + insert + after;
    const caret = before.length + insert.length;
    $input.focus();
    $input.setSelectionRange(caret, caret);
    hideMentionMenu();
  }

  function handleMentionAutocomplete(e) {
    if (!$input || (e.target !== $input && document.activeElement !== $input)) return false;
    const ctx = getMentionContext($input);
    if (!ctx) return false;
    let items = getMentionCandidates(ctx.query);
    if (!items.length) items = getEmergencyMentionCandidates(ctx.query);
    if (!items.length) {
      e.preventDefault();
      e.stopPropagation();
      return true;
    }
    e.preventDefault();
    e.stopPropagation();
    if (typeof e.stopImmediatePropagation === "function") e.stopImmediatePropagation();
    if (
      mentionState.start !== ctx.start ||
      mentionState.end !== ctx.end ||
      mentionState.items.map(i => i.name).join("|") !== items.map(i => i.name).join("|")
    ) {
      mentionState = { start: ctx.start, end: ctx.end, items, activeIndex: 0 };
    } else if (items.length > 1) {
      mentionState.activeIndex = (mentionState.activeIndex + 1) % items.length;
    }
    renderMentionMenu();
    applyMention(mentionState.activeIndex);
    return true;
  }

  function sendTyping(active) {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    if (active === isTyping) return;
    isTyping = active;
    ws.send(JSON.stringify({ type: "typing", sender_name: userName, content: active ? "true" : "false" }));
  }

  $input.addEventListener("input", () => {
    if ($input.value.trim()) {
      sendTyping(true);
      clearTimeout(typingTimer);
      typingTimer = setTimeout(() => sendTyping(false), 2000);
    } else {
      sendTyping(false);
    }
    updateMentionMenu();
  });
  $input.addEventListener("focus", updateMentionMenu);
  $input.addEventListener("click", updateMentionMenu);
  document.addEventListener("click", (e) => {
    if (e.target === $input || $mentionMenu.contains(e.target)) return;
    hideMentionMenu();
  });

  function send() {
    const content = $input.value.trim();
    if (!content || !ws || ws.readyState !== WebSocket.OPEN) return;
    const { mentions, mentionAll } = parseMentions(content);
    sendTyping(false);
    clearTimeout(typingTimer);
    ws.send(JSON.stringify({
      type: "chat",
      sender_name: userName,
      content,
      mentions,
      mention_all: mentionAll,
    }));
    $input.value = ""; $input.focus();
    hideMentionMenu();
  }
  $send.addEventListener("click", send);
  document.addEventListener("keydown", (e) => {
    if (e.key === "Tab" && handleMentionAutocomplete(e)) {
      return;
    }
  }, true);
  $input.addEventListener("keydown", e => {
    const ctx = getMentionContext();
    if (e.key === "Tab" && ctx) {
      if (handleMentionAutocomplete(e)) return;
      return;
    }
    if (e.key === "@" || (ctx && (e.key.length === 1 || e.key === "Backspace" || e.key === "Delete"))) {
      setTimeout(updateMentionMenu, 0);
    }
    if (!$mentionMenu.classList.contains("hidden")) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        mentionState.activeIndex = (mentionState.activeIndex + 1) % mentionState.items.length;
        renderMentionMenu();
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        mentionState.activeIndex = (mentionState.activeIndex - 1 + mentionState.items.length) % mentionState.items.length;
        renderMentionMenu();
        return;
      }
      if (e.key === "Tab" || e.key === "Enter") {
        e.preventDefault();
        applyMention();
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        hideMentionMenu();
        return;
      }
    }
    if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); send(); }
  });

  // ── Sidebar ──
  $toggle.addEventListener("click", () => $sidebar.classList.toggle("hidden"));
  $force.addEventListener("click", () => { if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: "force_reply" })); });
  $attackBtns.forEach((btn) => {
    btn.addEventListener("click", () => runAttackDemo(btn));
  });
  $refreshPeer.addEventListener("click", loadPeerInfo);
  $copyPeer.addEventListener("click", copyPeerAddr);
  $connectPeer.addEventListener("click", connectManualPeer);

  document.getElementById("btn-leave").addEventListener("click", leaveRoom);

  function leaveRoom() {
    intentionalClose = true;
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: "leave" }));
      ws.close();
    }
    ws = null;

    messages.length = 0;
    $chat.innerHTML = "";
    activeUsers.clear();
    renderActivityBar();
    streamBubbles.clear();
    roomMembers.clear();
    $memberList.innerHTML = "";
    renderDiagnostics({});
    renderAttackResult({ note: "Choose an attack to see the defense result." });
    resetSecurityTrace();
    $peerAddrs.value = "";
    $peerConnectInput.value = "";
    $peerConnectStatus.textContent = "";
    $input.disabled = true;
    $send.disabled = true;
    isTyping = false;

    $chatView.style.display = "none";
    $setup.classList.remove("hidden");
  }

  async function runAttackDemo(btn) {
    const attackType = btn.dataset.attack;
    renderAttackResult({
      attack_type: attackType,
      defense_layer: "sending...",
      expected: "waiting for backend",
      note: "Publishing demo payload..."
    });
    $attackBtns.forEach((b) => { b.disabled = true; });
    try {
      const r = await fetch("/api/demo/attack", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ type: attackType })
      });
      const raw = await r.text();
      let parsed = null;
      try { parsed = JSON.parse(raw); } catch {}
      if (!r.ok) throw new Error(parsed?.note || raw || `HTTP ${r.status}`);
      renderAttackResult(parsed || { attack_type: attackType, note: raw });
    } catch (e) {
      renderAttackResult({
        attack_type: attackType,
        defense_layer: "not published",
        expected: "room must be ready",
        note: "Failed: " + (e?.message || "unknown error")
      });
    } finally {
      setTimeout(() => {
        $attackBtns.forEach((b) => { b.disabled = false; });
      }, 900);
    }
  }

  function renderAttackResult(result) {
    if (!$attackResult) return;
    let html =
      `<strong>Attack:</strong> ${escapeHTML(formatAttackType(result.attack_type || "unknown"))}\n` +
      `<strong>Defense:</strong> ${escapeHTML(result.defense_layer || "n/a")}\n` +
      `<strong>Expected:</strong> ${escapeHTML(result.expected || "n/a")}\n` +
      `<strong>Actual:</strong> ${escapeHTML(result.note || (result.published ? "published" : "not published"))}`;
    if (result.packet_summary) html += `\n<strong>Packet:</strong> ${escapeHTML(result.packet_summary)}`;
    if (result.explanation) html += `\n<strong>Why:</strong> ${escapeHTML(result.explanation)}`;
    if (result.packet_before) html += `\n<strong>Before:</strong><pre>${escapeHTML(result.packet_before)}</pre>`;
    if (result.packet_after) html += `\n<strong>After:</strong><pre>${escapeHTML(result.packet_after)}</pre>`;
    $attackResult.innerHTML = html;
  }

  function resetSecurityTrace() {
    if (!$securityTrace) return;
    $securityTrace.innerHTML = '<div class="trace-item"><div class="trace-summary">Waiting for packets...</div></div>';
  }

  function renderSecurityTrace(msg) {
    if (!$securityTrace) return;
    const placeholder = $securityTrace.querySelector(".trace-item .trace-summary");
    if (placeholder && placeholder.textContent === "Waiting for packets...") {
      $securityTrace.innerHTML = "";
    }
    const verdict = msg.trace_verdict || "info";
    const item = document.createElement("div");
    item.className = "trace-item " + verdict;
    const steps = Array.isArray(msg.trace_steps) ? msg.trace_steps : [];
    item.innerHTML =
      `<div class="trace-head"><span>${escapeHTML(formatAttackType(msg.trace_kind || "packet"))}</span><span class="trace-pill">${escapeHTML(verdict)}</span></div>` +
      `<div class="trace-summary">${escapeHTML(msg.trace_summary || "")}</div>` +
      steps.map(step => `<div class="trace-step">${escapeHTML(step)}</div>`).join("");
    $securityTrace.prepend(item);
    while ($securityTrace.children.length > 30) {
      $securityTrace.removeChild($securityTrace.lastChild);
    }
  }

  async function loadPeerInfo() {
    if (!$peerAddrs) return;
    try {
      const r = await fetch("/api/peer/info");
      const data = await r.json();
      if (!r.ok) throw new Error(data?.error || `HTTP ${r.status}`);
      const addrs = Array.isArray(data.addrs) ? data.addrs : [];
      const preferred = addrs
        .filter(addr => !addr.includes("/ip4/127.0.0.1/") && !addr.includes("/ip4/0.0.0.0/"));
      $peerAddrs.value = (preferred.length ? preferred : addrs).join("\n");
      $peerConnectStatus.textContent = "Share one multiaddr with the other computer.";
    } catch (e) {
      $peerConnectStatus.textContent = "Peer info unavailable until room is ready.";
    }
  }

  async function copyPeerAddr() {
    const text = ($peerAddrs.value || "").split("\n").find(Boolean) || "";
    if (!text) {
      $peerConnectStatus.textContent = "No multiaddr to copy yet.";
      return;
    }
    try {
      await navigator.clipboard.writeText(text);
      $peerConnectStatus.textContent = "Copied first multiaddr.";
    } catch {
      $peerAddrs.select();
      document.execCommand("copy");
      $peerConnectStatus.textContent = "Copied selected multiaddr.";
    }
  }

  async function connectManualPeer() {
    const multiaddr = ($peerConnectInput.value || "").trim();
    if (!multiaddr) {
      $peerConnectStatus.textContent = "Paste remote multiaddr first.";
      return;
    }
    $connectPeer.disabled = true;
    $peerConnectStatus.textContent = "Connecting...";
    try {
      const r = await fetch("/api/peer/connect", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ multiaddr })
      });
      const raw = await r.text();
      if (!r.ok) throw new Error(raw || `HTTP ${r.status}`);
      $peerConnectStatus.textContent = "Connected. GossipSub should sync shortly.";
      appendSystem("Manual peer connected.");
    } catch (e) {
      $peerConnectStatus.textContent = "Connect failed: " + (e?.message || "unknown error");
    } finally {
      $connectPeer.disabled = false;
    }
  }

  function formatAttackType(type) {
    return String(type)
      .split("_")
      .filter(Boolean)
      .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
      .join(" ");
  }

  function escapeHTML(value) {
    return String(value).replace(/[&<>"']/g, (ch) => ({
      "&": "&amp;",
      "<": "&lt;",
      ">": "&gt;",
      '"': "&quot;",
      "'": "&#39;"
    }[ch]));
  }

  // ── Memories (in persona editor) ──
  async function loadMemoriesForPersona(name) {
    try {
      const r = await fetch("/api/memory");
      const mems = await r.json();
      $memList.innerHTML = "";
      if (!mems || !mems.length) {
        $memList.innerHTML = '<p style="color:var(--muted);font-size:12px">No memories yet. Memories are created when the AI chats.</p>';
        return;
      }
      mems.forEach(m => {
        const el = document.createElement("div"); el.className = "memory-item";
        el.innerHTML = `<span class="fact">${escHtml(m.fact)}</span><button title="Delete">✕</button>`;
        el.querySelector("button").addEventListener("click", async () => {
          await fetch("/api/memory?id=" + m.id, { method: "DELETE" });
          loadMemoriesForPersona(name);
        });
        $memList.appendChild(el);
      });
    } catch {}
  }

  function escHtml(s) { const d = document.createElement("div"); d.textContent = s; return d.innerHTML; }

  function parseMentions(text) {
    const mentions = [];
    let mentionAll = false;
    const seen = new Set();
    const re = /(?:^|[\s(])@([A-Za-z0-9_][A-Za-z0-9_-]{0,29})\b/g;
    let match;
    while ((match = re.exec(text)) !== null) {
      const name = (match[1] || "").trim();
      if (!name) continue;
      if (name.toLowerCase() === "all") {
        mentionAll = true;
        continue;
      }
      const key = name.toLowerCase();
      if (seen.has(key)) continue;
      seen.add(key);
      mentions.push(name);
    }
    return { mentions, mentionAll };
  }

  // ═══════════ BOOT ═══════════
  let ollamaWasRunning = false;

  async function pollOllama() {
    const running = await checkOllama();
    if (running && !ollamaWasRunning) {
      await loadModels();
    }
    ollamaWasRunning = running;
  }

  pollOllama();
  setInterval(pollOllama, 5000);
})();
