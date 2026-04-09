(() => {
  "use strict";

  const $chat     = document.getElementById("chat");
  const $input    = document.getElementById("msg-input");
  const $send     = document.getElementById("btn-send");
  const $name     = document.getElementById("input-name");
  const $status   = document.getElementById("status");
  const $peers    = document.getElementById("peer-count");
  const $toggle   = document.getElementById("btn-toggle");
  const $sidebar  = document.getElementById("sidebar");
  const $force    = document.getElementById("btn-force-reply");
  const $aiName   = document.getElementById("ai-name");
  const $aiStyle  = document.getElementById("ai-style");
  const $aiModel  = document.getElementById("ai-model");

  let ws = null;
  let myPeerID = "";

  const messages = [];

  // ── Vector Clock comparison ──
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
    if (sumA !== sumB) return sumA - sumB;
    return 0;
  }

  // ── Sidebar toggle ──
  $toggle.addEventListener("click", () => $sidebar.classList.toggle("hidden"));

  // ── Name persistence ──
  $name.value  = localStorage.getItem("dmapc-name") || "";
  $name.addEventListener("input", () => {
    localStorage.setItem("dmapc-name", $name.value);
    updateSendState();
  });

  // ── Persona persistence + sync ──
  $aiName.value  = localStorage.getItem("dmapc-ai-name")  || "";
  $aiStyle.value = localStorage.getItem("dmapc-ai-style") || "";
  const savedModel = localStorage.getItem("dmapc-ai-model");
  if (savedModel) $aiModel.value = savedModel;

  let personaTimer = null;
  function onPersonaChange() {
    localStorage.setItem("dmapc-ai-name",  $aiName.value);
    localStorage.setItem("dmapc-ai-style", $aiStyle.value);
    localStorage.setItem("dmapc-ai-model", $aiModel.value);
    clearTimeout(personaTimer);
    personaTimer = setTimeout(sendPersona, 400);
  }
  $aiName.addEventListener("input",  onPersonaChange);
  $aiStyle.addEventListener("input", onPersonaChange);
  $aiModel.addEventListener("change", onPersonaChange);

  function sendPersona() {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    ws.send(JSON.stringify({
      type:     "persona",
      ai_name:  $aiName.value.trim(),
      ai_style: $aiStyle.value.trim(),
      ai_model: $aiModel.value,
    }));
  }

  function updateSendState() {
    const ready = ws && ws.readyState === WebSocket.OPEN && $name.value.trim() !== "";
    $input.disabled = !ready;
    $send.disabled  = !ready;
  }

  // ── WebSocket ──
  function connect() {
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    ws = new WebSocket(`${proto}//${location.host}/ws`);

    ws.addEventListener("open", () => {
      $status.textContent = "Connected";
      $status.style.color = "#4caf50";
      updateSendState();
      sendPersona();
    });

    ws.addEventListener("close", () => {
      $status.textContent = "Disconnected — reconnecting...";
      $status.style.color = "#e94560";
      updateSendState();
      setTimeout(connect, 2000);
    });

    ws.addEventListener("message", (evt) => {
      const msg = JSON.parse(evt.data);
      switch (msg.type) {
        case "chat":    insertChat(msg); break;
        case "system":  appendSystem(msg.content); break;
        case "peers":   $peers.textContent = msg.count; break;
      }
    });
  }

  // ── Insert a chat message in vector-clock order ──
  function insertChat(msg) {
    if (msg.id && messages.some(m => m.id === msg.id)) return;

    let idx = messages.length;
    for (let i = messages.length - 1; i >= 0; i--) {
      if (compareVC(messages[i].vector_clock, msg.vector_clock) <= 0) {
        idx = i + 1;
        break;
      }
      idx = i;
    }

    messages.splice(idx, 0, msg);

    const el = buildChatEl(msg);
    const chatMsgEls = $chat.querySelectorAll(".msg:not(.system)");
    const refEl = chatMsgEls[idx];
    if (refEl) {
      $chat.insertBefore(el, refEl);
    } else {
      $chat.appendChild(el);
    }
    $chat.scrollTop = $chat.scrollHeight;
  }

  function buildChatEl(msg) {
    const isSelf = msg.self === true;
    const isAI   = msg.is_ai === true;

    const wrapper = document.createElement("div");
    wrapper.className = "msg " + (isSelf ? "self" : "other") + (isAI ? " ai" : "");

    const meta = document.createElement("div");
    meta.className = "meta";
    meta.textContent = isSelf ? "You" : (msg.sender_name || msg.sender_id.slice(0, 12));
    if (isAI) meta.textContent += " 🤖";
    wrapper.appendChild(meta);

    const bubble = document.createElement("div");
    bubble.className = "bubble";
    bubble.textContent = msg.content;
    wrapper.appendChild(bubble);

    return wrapper;
  }

  function appendSystem(text) {
    const wrapper = document.createElement("div");
    wrapper.className = "msg system";
    const bubble = document.createElement("div");
    bubble.className = "bubble";
    bubble.textContent = text;
    wrapper.appendChild(bubble);
    $chat.appendChild(wrapper);
    $chat.scrollTop = $chat.scrollHeight;

    if (text.includes("PeerID:")) {
      myPeerID = text.split("PeerID: ")[1] || "";
    }
  }

  // ── Send ──
  function send() {
    const content = $input.value.trim();
    const name    = $name.value.trim();
    if (!content || !name || !ws || ws.readyState !== WebSocket.OPEN) return;
    ws.send(JSON.stringify({ type: "chat", sender_name: name, content: content }));
    $input.value = "";
    $input.focus();
  }

  $send.addEventListener("click", send);
  $input.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); send(); }
  });

  // ── Force Reply ──
  $force.addEventListener("click", () => {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    ws.send(JSON.stringify({ type: "force_reply" }));
  });

  // ── Boot ──
  connect();
})();
