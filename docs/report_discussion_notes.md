# Report Discussion Notes

這份文件整理課堂後想到的問題與報告方向。它不是最終簡報，而是後續討論、取捨與實作 demo 時的工作筆記。

## 一句話主軸

本專案不是單純做 AI 聊天，而是做一個「沒有中央伺服器的 P2P 群聊空間」，每位使用者可以把自己本機運行的 AI persona 帶進房間，形成多人與多 AI 的 decentralized social chat。

創新點可以聚焦在：

- 使用者不只是跟平台提供的 AI 對話，而是帶著自己的 local AI agent 進入別人的聊天室。
- 每個節點都是獨立 peer，負責自己的訊息驗證、排序、同步與 AI 推論。
- AI agent 不是中央 bot，而是 edge device 上的個人代理。

## 需要講清楚的系統問題

### 1. 三個以上節點時，訊息怎麼傳？

目前使用 libp2p GossipSub。

- 不是傳統 client-server。
- 也不是固定「A 經過 B 再到 C」的中繼鏈。
- 每個節點加入同一個 room topic。
- 節點把訊息 publish 到 topic，GossipSub mesh 會把訊息擴散到其他 subscribed peers。
- 三個以上節點時，訊息會沿著 GossipSub mesh 傳播；不要求所有人都直接連到所有人，但每個 honest peer 收到後都會自己驗證。

報告時可以說：

> The room is a GossipSub topic. A message is not sent to a central server. It is published into a peer-to-peer mesh, and honest peers independently validate it before accepting it.

### 2. Tailscale 是不是「真正跨網域」？

Tailscale 是 VPN / overlay network，不是完整 public Internet NAT traversal。

但 demo 上是合理的，因為：

- 它讓兩台不同實體電腦成為可互連 host。
- 比同一台電腦開兩個 port 更有說服力。
- 可以展示跨裝置的 P2P 行為、手動 peer connect、GossipSub 傳播、ledger sync。

需要誠實說：

> We use Tailscale as a private overlay network for the demo. It is not a full Internet-scale peer discovery solution, but it lets us test the distributed behavior across real machines.

### 3. 房間要用 room code 還是像 Minecraft server 用 IP？

現在其實有兩層：

- `room code`: 決定 GossipSub topic，等於聊天室名稱。
- `peer multiaddr`: 決定要連到哪個實體 peer，適合 Tailscale/manual connect。

建議報告說法：

- 一般使用者用 room code 加入同一個聊天主題。
- 在 LAN 上可用 mDNS 自動找 peer。
- 在 Tailscale / 跨網段 demo 時，用 manual multiaddr 連線，類似 Minecraft server address。

UI 目前已經支援 Manual Peer Connect，可以展示：

```text
/ip4/100.x.x.x/tcp/9000/p2p/12D3...
```

### 4. 時間是同步還是非同步？

不是 physical clock sync。

本系統使用 vector clock 做 logical time：

- 不依賴電腦時鐘是否一樣。
- 每個 sender 發訊息前增加自己的 counter。
- 收到訊息後 merge vector clock。
- 如果兩個訊息互相沒有 happened-before 關係，就是 concurrent。
- UI / AI history 會用 vector clock + deterministic tie-breaker 做一致排序。

報告時要避免說「時間同步」，改說：

> We do not synchronize wall clocks. We synchronize causal knowledge through vector clocks.

### 5. 如果兩個人同時 Enter，因果順序一樣怎麼辦？

兩則訊息可能是 concurrent，沒有真正的先後因果。

系統處理方式：

- vector clock 判斷它們 concurrent。
- 為了 UI 一致，使用 deterministic tie-breaker，例如 sender ID、message ID。
- 所有節點用同一套排序規則，所以畫面順序會一致。

重點：

> The order is deterministic, but not pretending to be physical time.

### 6. 如果有人偷改訊息，怎麼知道是誰改的？

目前能做到的是：

- 若訊息內容、sender_id、mentions、vector_clock、prev_hash 等簽章欄位被改，Ed25519 verification 會失敗。
- 如果封包是從某個 forwarding peer 送來，trust/quarantine 會記在 forwarding peer 上。
- 但在 gossip network 裡，若多個惡意節點合作重新散播壞封包，我們只能本地拒絕與 quarantine；不能全域證明最初源頭。

可以誠實說明：

> We can detect that a received packet is invalid and attribute local suspicion to the forwarding peer. We do not claim global Byzantine attribution or global reputation consensus.

這是合理的 decentralized 設計，不用假裝能做到全球封鎖。

### 7. 初始就攔截，還是進 app 後再攔截？

兩層都有：

- GossipSub topic validator：早期攔截 malformed、bad signature、spoofed sync、rate limit。
- Ledger：攔截「簽章合法但狀態不合理」的情況，例如 replay duplicate、clock regression、equivocation fork。

報告要強調分層防禦：

```text
Transport / GossipSub -> Validator -> Ledger -> UI / AI history
```

### 8. 兩個人合作散播假消息怎麼辦？

目前防的是 protocol-level fake message，不是 semantic misinformation。

可以擋：

- forged identity
- tampered signed fields
- replay
- malformed payload
- sync abuse
- clock manipulation
- equivocation fork
- flood behavior

不能擋：

- 兩個真人合作講謊話
- Sybil 大量建立新身份
- 全域 reputation consensus
- 語意 fact-checking

這要說清楚，避免被問倒。

## Demo 建議

### 主流程只展示 Core Demo

不要把所有 Attack Lab 按鈕都按一遍，會太亂。

建議主流程：

1. 兩台不同電腦透過 Tailscale/manual multiaddr 連線。
2. 正常聊天，證明 P2P room 可運作。
3. Tamper Signed Content，展示 Ed25519 驗簽與 Security Trace。
4. Replay Last Valid Message，展示 ledger dedupe。
5. Flood Spam Burst，展示 rate limit / quarantine。
6. 收尾說 advanced defenses 還包含 sync abuse、vector clock regression、hash-chain equivocation。

### Backup Demo / Q&A

如果老師追問再展示：

- Clock Regression：vector clock 不是裝飾。
- Equivocation Fork：hash chain 可偵測分叉。
- Forged Sync Response：sync repair 也會重新驗簽。
- Spoof Sync Identity：from_peer_id 必須與 forwarding peer 綁定。

## 視覺化呈現

需要一張系統架構圖，建議包含：

```text
Browser UI
  |
WebSocket / HTTP
  |
Go App State
  |-- Message Ledger
  |-- Vector Clock
  |-- AI Orchestrator / Ollama
  |
P2P Layer
  |-- libp2p Host
  |-- GossipSub Topic
  |-- mDNS / Manual Multiaddr
  |-- Topic Validator
  |
Remote Peers
```

還需要一張防禦流程圖：

```text
Incoming Packet
  -> JSON parse
  -> Payload size / kind check
  -> Sender / sync identity check
  -> Ed25519 signature check
  -> Rate limit / trust tracker
  -> Ledger dedupe / vector clock / hash-chain check
  -> UI + AI history
```

## 可以提的 AI 退火機制

AI 不是每句都回，原因是 decentralized multi-AI chat 容易失控。

目前想法：

- human mention / @all：高機率或必回。
- AI-to-AI chain：隨著 chain depth 逐漸降低回覆機率。
- 問句可提高回覆機率。
- 重複內容會被 suppress。

報告定位：

> The AI layer uses a decentralized invitation and cooling-down mechanism to prevent multiple local agents from endlessly responding to each other.

不要花太多時間講 prompt engineering，因為主題是分散式系統。

## 參考與可說明的技術來源

可以在簡報列：

- libp2p: peer identity, Noise transport, host connection
- GossipSub: decentralized pub/sub dissemination
- mDNS: LAN peer discovery
- Vector clocks: logical time and causal ordering
- Ed25519: application-level digital signatures
- Hash chain: tamper-evident local message history
- Ollama: local AI inference
- Tailscale: demo overlay network for cross-device testing

## 需要避免的說法

- 不要說「我們完全防止假消息」。
  - 改說：我們防 protocol-level fake messages。
- 不要說「Tailscale 就是真正 public P2P NAT traversal」。
  - 改說：Tailscale 是 demo overlay network，證明跨裝置 distributed behavior。
- 不要說「時間同步」。
  - 改說：logical time / causal ordering。
- 不要說「MITM 一定能攔截所有 GossipSub 封包」。
  - 改說：Byzantine peer can inject arbitrary packets into the room.

## 後續可加分項

- 做一張 architecture diagram。
- 做一張 packet validation pipeline diagram。
- 在 README 或簡報列出每個 defense 解決哪個 distributed-system problem。
- 加 GitHub Actions：push 後自動 `go test ./...`。
- 設計一個 3-node demo script：
  - A/B/C 三台節點進同一 room。
  - A 發訊息，B/C 都收到。
  - C 觸發 attack，A/B 各自 reject。
  - B 晚加入，透過 sync repair 補歷史。

## 目前最重要的報告結論

本專案的核心不是「做一個聊天 UI」或「接一個 AI 模型」，而是展示：

1. 去中心化通訊：沒有 central chat server。
2. Local AI agent：每個使用者帶自己的 AI 進入群聊。
3. Logical time：不用 physical clock sync 也能保持一致排序。
4. Local verification：每個 peer 自己驗證不可信封包。
5. Eventual consistency：晚加入節點可透過 sync repair 補資料。
6. Attack visibility：Security Trace 讓驗證流程可視化，不是黑箱按鈕。
