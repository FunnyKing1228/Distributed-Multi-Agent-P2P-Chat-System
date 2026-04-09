(() => {
  "use strict";

  // ── State ──
  let ws = null;
  let userName = "";
  let roomCode = "";
  let installedModels = new Set();
  const messages = [];

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

  // ── Chat DOM refs ──
  const $chat      = document.getElementById("chat");
  const $input     = document.getElementById("msg-input");
  const $send      = document.getElementById("btn-send");
  const $status    = document.getElementById("status");
  const $peers     = document.getElementById("peer-count");
  const $toggle    = document.getElementById("btn-toggle");
  const $sidebar   = document.getElementById("sidebar");
  const $force     = document.getElementById("btn-force-reply");
  const $aiName    = document.getElementById("ai-name");
  const $aiStyle   = document.getElementById("ai-style");
  const $aiModel   = document.getElementById("ai-model");
  const $memList   = document.getElementById("memory-list");
  const $chatRoom  = document.getElementById("chat-room");
  const $chatName  = document.getElementById("chat-name");
  const $roomBadge = document.getElementById("room-badge");

  // ── Restore saved values ──
  $sName.value    = localStorage.getItem("dmapc-name") || "";
  $sRoom.value    = localStorage.getItem("dmapc-room") || "";
  $sAiName.value  = localStorage.getItem("dmapc-ai-name") || "";
  $sAiStyle.value = localStorage.getItem("dmapc-ai-style") || "";
  const savedModel = localStorage.getItem("dmapc-ai-model");

  $sMemLimit.addEventListener("input", () => { $sMemVal.textContent = $sMemLimit.value + " facts"; });
  $sNumCtx.addEventListener("input", () => { $sCtxVal.textContent = $sNumCtx.value; });

  function saveSetupLocals() {
    localStorage.setItem("dmapc-name", $sName.value);
    localStorage.setItem("dmapc-room", $sRoom.value);
    localStorage.setItem("dmapc-ai-name", $sAiName.value);
    localStorage.setItem("dmapc-ai-style", $sAiStyle.value);
    localStorage.setItem("dmapc-ai-model", $sAiModel.value);
  }

  function updateJoinState() {
    $btnJoin.disabled = !($sName.value.trim() && $sRoom.value.trim());
  }
  $sName.addEventListener("input", updateJoinState);
  $sRoom.addEventListener("input", updateJoinState);
  updateJoinState();

  // ═══════════ MODEL MANAGEMENT ═══════════

  let recommendedData = [];

  async function checkOllama() {
    try {
      const r = await fetch("/api/ollama/status");
      const d = await r.json();
      $ollamaDot.className = "status-dot " + (d.running ? "on" : "off");
      $ollamaStatus.textContent = d.running ? "Running" : "Not running — start with: ollama serve";
      return d.running;
    } catch {
      $ollamaDot.className = "status-dot off";
      $ollamaStatus.textContent = "Cannot reach server";
      return false;
    }
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

  function renderRecommendedModels() {
    $recommendedList.innerHTML = "";
    recommendedData.forEach(m => {
      const installed = installedModels.has(m.name);
      const el = document.createElement("div");
      el.className = "model-item";
      el.innerHTML = `<span class="name">${m.name}</span>` +
        `<span class="meta">${m.ram} RAM · ${m.params} params</span>` +
        (installed
          ? `<button class="btn-installed">Installed</button>`
          : `<button class="btn-download" data-name="${m.name}">Download</button>`);
      if (!installed) {
        el.querySelector(".btn-download").addEventListener("click", () => pullModel(m.name));
      }
      $recommendedList.appendChild(el);
    });
  }

  function populateModelDropdowns() {
    [$sAiModel, $aiModel].forEach($sel => {
      const cur = $sel.value;
      $sel.innerHTML = '<option value="">— select —</option>';
      installedModels.forEach(name => {
        const opt = document.createElement("option");
        opt.value = name; opt.textContent = name;
        $sel.appendChild(opt);
      });
      if (cur && installedModels.has(cur)) $sel.value = cur;
    });
    if (savedModel && installedModels.has(savedModel)) $sAiModel.value = savedModel;
  }

  async function pullModel(name) {
    $pullProgress.style.display = "block";
    $pullFill.style.width = "0%";
    $pullStatus.textContent = "Starting download: " + name + "...";

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
            if (d.total && d.completed) {
              const pct = Math.round(d.completed / d.total * 100);
              $pullFill.style.width = pct + "%";
              $pullStatus.textContent = `${name}: ${pct}%`;
            } else if (d.status) {
              $pullStatus.textContent = d.status;
            }
          } catch {}
        }
      }
      $pullStatus.textContent = name + " — done!";
    } catch (e) {
      $pullStatus.textContent = "Error: " + e.message;
    }

    setTimeout(() => { $pullProgress.style.display = "none"; }, 2000);
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

  function joinRoom() {
    saveSetupLocals();
    userName = $sName.value.trim();
    roomCode = $sRoom.value.trim();

    $setup.classList.add("hidden");
    $chatView.style.display = "block";
    $chatRoom.textContent = roomCode;
    $chatName.textContent = userName;
    $roomBadge.textContent = roomCode;
    $aiName.value = $sAiName.value;
    $aiStyle.value = $sAiStyle.value;
    if ($sAiModel.value) $aiModel.value = $sAiModel.value;

    connectWS();
  }

  // ═══════════ WEBSOCKET (Chat phase) ═══════════

  function connectWS() {
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    ws = new WebSocket(`${proto}//${location.host}/ws`);

    ws.addEventListener("open", () => {
      $status.textContent = "Setting up...";
      $status.style.color = "#ff9800";
      ws.send(JSON.stringify({
        type: "setup",
        sender_name: userName,
        room: roomCode,
        ai_name: $sAiName.value.trim() || "AI",
        ai_style: $sAiStyle.value.trim(),
        ai_model: $sAiModel.value,
        memory_limit: parseInt($sMemLimit.value) || 50,
        num_ctx: parseInt($sNumCtx.value) || 4096,
      }));
    });

    ws.addEventListener("close", () => {
      $status.textContent = "Disconnected — reconnecting...";
      $status.style.color = "#e94560";
      $input.disabled = true; $send.disabled = true;
      setTimeout(connectWS, 2000);
    });

    ws.addEventListener("message", (evt) => {
      const msg = JSON.parse(evt.data);
      switch (msg.type) {
        case "ready":
          $status.textContent = "Connected";
          $status.style.color = "#4caf50";
          $input.disabled = false;
          $send.disabled = false;
          appendSystem("Joined room: " + roomCode + " — PeerID: " + msg.peer_id);
          loadMemories();
          break;
        case "chat":    insertChat(msg); break;
        case "system":  appendSystem(msg.content); break;
        case "peers":   $peers.textContent = msg.count; break;
        case "error":   appendSystem("Error: " + msg.content); break;
      }
    });
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
    const sumA = Object.values(a).reduce((s, v) => s + v, 0);
    const sumB = Object.values(b).reduce((s, v) => s + v, 0);
    return sumA - sumB;
  }

  function insertChat(msg) {
    if (msg.id && messages.some(m => m.id === msg.id)) return;
    let idx = messages.length;
    for (let i = messages.length - 1; i >= 0; i--) {
      if (compareVC(messages[i].vector_clock, msg.vector_clock) <= 0) { idx = i + 1; break; }
      idx = i;
    }
    messages.splice(idx, 0, msg);
    const el = buildChatEl(msg);
    const chatMsgEls = $chat.querySelectorAll(".msg:not(.system)");
    const refEl = chatMsgEls[idx];
    refEl ? $chat.insertBefore(el, refEl) : $chat.appendChild(el);
    $chat.scrollTop = $chat.scrollHeight;

    if (msg.is_ai) setTimeout(loadMemories, 3000);
  }

  function buildChatEl(msg) {
    const isSelf = msg.self === true, isAI = msg.is_ai === true;
    const w = document.createElement("div");
    w.className = "msg " + (isSelf ? "self" : "other") + (isAI ? " ai" : "");
    const meta = document.createElement("div");
    meta.className = "meta";
    meta.textContent = (isSelf ? "You" : (msg.sender_name || "?")) + (isAI ? " 🤖" : "");
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

  // ── Send ──
  function send() {
    const content = $input.value.trim();
    if (!content || !ws || ws.readyState !== WebSocket.OPEN) return;
    ws.send(JSON.stringify({ type: "chat", sender_name: userName, content }));
    $input.value = ""; $input.focus();
  }
  $send.addEventListener("click", send);
  $input.addEventListener("keydown", e => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); send(); } });

  // ── Sidebar ──
  $toggle.addEventListener("click", () => $sidebar.classList.toggle("hidden"));
  $force.addEventListener("click", () => { if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: "force_reply" })); });

  let personaTimer = null;
  function onPersonaChange() {
    clearTimeout(personaTimer);
    personaTimer = setTimeout(() => {
      if (!ws || ws.readyState !== WebSocket.OPEN) return;
      ws.send(JSON.stringify({
        type: "persona",
        ai_name: $aiName.value.trim(),
        ai_style: $aiStyle.value.trim(),
        ai_model: $aiModel.value,
      }));
    }, 400);
  }
  $aiName.addEventListener("input", onPersonaChange);
  $aiStyle.addEventListener("input", onPersonaChange);
  $aiModel.addEventListener("change", onPersonaChange);

  // ── Memories ──
  async function loadMemories() {
    try {
      const r = await fetch("/api/memory");
      const mems = await r.json();
      $memList.innerHTML = "";
      if (!mems || !mems.length) {
        $memList.innerHTML = '<p style="color:var(--muted);font-size:12px">No memories yet.</p>';
        return;
      }
      mems.forEach(m => {
        const el = document.createElement("div"); el.className = "memory-item";
        el.innerHTML = `<span class="fact">${escHtml(m.fact)}</span><button title="Delete">✕</button>`;
        el.querySelector("button").addEventListener("click", async () => {
          await fetch("/api/memory?id=" + m.id, { method: "DELETE" });
          loadMemories();
        });
        $memList.appendChild(el);
      });
    } catch {}
  }

  function escHtml(s) { const d = document.createElement("div"); d.textContent = s; return d.innerHTML; }

  // ═══════════ BOOT ═══════════
  checkOllama().then(ok => { if (ok) loadModels(); });
  setInterval(() => { checkOllama().then(ok => { if (ok) loadModels(); }); }, 10000);
})();
