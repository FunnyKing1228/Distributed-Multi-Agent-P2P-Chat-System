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
        case "chat":    renderChat(msg); break;
        case "system":  renderSystem(msg.content); break;
        case "peers":   $peers.textContent = msg.count; break;
      }
    });
  }

  // ── Render helpers ──
  function renderChat(msg) {
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

    $chat.appendChild(wrapper);
    $chat.scrollTop = $chat.scrollHeight;
  }

  function renderSystem(text) {
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
