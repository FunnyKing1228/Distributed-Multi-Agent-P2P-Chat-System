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

  let ws = null;
  let myPeerID = "";

  // Sorted message list (chat messages only, ordered by vector clock).
  const messages = [];

  // ── Vector Clock comparison ──
  // Returns -1 (a before b), +1 (a after b), or 0 (equal / concurrent).
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
    // Equal or concurrent — use clock sum as tiebreaker
    const sumA = Object.values(a).reduce((s, v) => s + v, 0);
    const sumB = Object.values(b).reduce((s, v) => s + v, 0);
    if (sumA !== sumB) return sumA - sumB;
    return 0;
  }

  // ── Sidebar toggle ──
  $toggle.addEventListener("click", () => $sidebar.classList.toggle("hidden"));

  // ── Name persistence ──
  const savedName = localStorage.getItem("dmapc-name") || "";
  $name.value = savedName;
  $name.addEventListener("input", () => {
    localStorage.setItem("dmapc-name", $name.value);
    updateSendState();
  });

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
    // Deduplicate by message ID
    if (msg.id && messages.some(m => m.id === msg.id)) return;

    // Find insertion index by scanning backwards (most messages append at end)
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

  // ── Force Reply placeholder (Step 4) ──
  $force.addEventListener("click", () => {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    ws.send(JSON.stringify({ type: "force_reply" }));
  });

  // ── Boot ──
  connect();
})();
