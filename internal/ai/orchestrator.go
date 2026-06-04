package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	appTypes "github.com/FunnyKing1228/Distributed-Multi-Agent-P2P-Chat-System/internal/types"
)

const (
	ollamaURL            = "http://localhost:11434/api/chat"
	maxHistory           = 30
	decisionCtxN         = 8
	calibrationCtxN      = 6
	aiCooldown           = 8 * time.Second
	aiHardStopDepth      = 6
	aiAtAllCooldown      = 10 * time.Minute
	surpriseReplyPercent = 6
)

var (
	ollamaClient    = &http.Client{Timeout: 3 * time.Minute}
	ollamaClientMem = &http.Client{Timeout: 1 * time.Minute}
	thinkRe         = regexp.MustCompile(`(?s)<think>.*?</think>`)
	fallbackModels  = []string{"nemotron-mini:latest", "nemotron-mini", "qwen3:4b", "llama3.2:3b", "llama3:8b"}
)

type Persona struct {
	Name  string `json:"name"`
	Style string `json:"style"`
	Model string `json:"model"`
}

type Orchestrator struct {
	mu            sync.RWMutex
	persona       Persona
	history       []ChatMessage
	memory        *MemoryStore
	numCtx        int
	onReply       func(name, content string) ChatMessage
	onStream      func(name, token string)
	onThinking    func(active bool)
	ctx           context.Context
	cancel        context.CancelFunc
	stopped       bool
	lastReplyAt   time.Time
	newOthersMsgs int
	lastAtAllAt   time.Time
}

func NewOrchestrator(persona Persona, memory *MemoryStore, numCtx int, onReply func(name, content string) ChatMessage) *Orchestrator {
	if numCtx <= 0 {
		numCtx = 4096
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Orchestrator{
		persona: persona,
		memory:  memory,
		numCtx:  numCtx,
		onReply: onReply,
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (o *Orchestrator) Stop() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stopped = true
	o.cancel()
}

func (o *Orchestrator) SetStreamCallback(fn func(name, token string)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.onStream = fn
}

func (o *Orchestrator) SetThinkingCallback(fn func(active bool)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.onThinking = fn
}

func (o *Orchestrator) AddToHistory(msg ChatMessage) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.history = append(o.history, msg)
	o.compactHistoryLocked()
}

func (o *Orchestrator) SetPersona(p Persona) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if p.Name != "" {
		o.persona.Name = p.Name
	}
	if p.Style != "" {
		o.persona.Style = p.Style
	}
	if p.Model != "" {
		o.persona.Model = p.Model
	}
}

func (o *Orchestrator) GetPersona() Persona {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.persona
}

func (o *Orchestrator) ForceReply() {
	o.mu.Lock()
	if o.stopped || len(o.history) == 0 {
		o.mu.Unlock()
		return
	}

	o.compactHistoryLocked()
	idx := -1
	for i := len(o.history) - 1; i >= 0; i-- {
		if !strings.EqualFold(o.history[i].SenderName, o.persona.Name) {
			idx = i
			break
		}
	}
	if idx < 0 {
		o.mu.Unlock()
		return
	}
	latest := o.history[idx]
	history := make([]ChatMessage, len(o.history))
	copy(history, o.history)
	persona := o.persona
	numCtx := o.numCtx
	thinkingCb := o.onThinking
	ctx := o.ctx
	o.mu.Unlock()

	recent := history
	if len(recent) > decisionCtxN {
		recent = recent[len(recent)-decisionCtxN:]
	}
	memorySection := ""
	if o.memory != nil {
		memorySection = o.memory.AsPromptSection()
	}
	if thinkingCb != nil {
		thinkingCb(true)
		defer thinkingCb(false)
	}

	replyText := o.generate(ctx, persona, recent, memorySection, numCtx, true)
	if replyText == "" {
		replyText = fallbackReply(latest.Content)
	}
	replyText = stripNamePrefix(replyText, persona.Name)
	if replyText == "" {
		return
	}

	o.mu.Lock()
	if o.stopped {
		o.mu.Unlock()
		return
	}
	replyText = o.rewriteAtAllLocked(replyText)
	if replyText == "" {
		o.mu.Unlock()
		return
	}
	selfMsg := o.onReply(persona.Name, replyText)
	if selfMsg.Content == "" {
		selfMsg = ChatMessage{SenderName: persona.Name, Content: replyText, IsAI: true}
	}
	o.history = append(o.history, selfMsg)
	o.compactHistoryLocked()
	o.lastReplyAt = time.Now()
	o.newOthersMsgs = 0
	o.mu.Unlock()
}

// Process is called by the Batcher when a trigger fires.
func (o *Orchestrator) Process(newMessages []ChatMessage) {
	o.mu.Lock()
	if o.stopped {
		o.mu.Unlock()
		return
	}

	// Filter out any echoes of our own AI (sender == our persona name)
	filtered := make([]ChatMessage, 0, len(newMessages))
	for _, m := range newMessages {
		if m.SenderName == o.persona.Name {
			continue
		}
		filtered = append(filtered, m)
		o.newOthersMsgs++
	}

	o.history = append(o.history, filtered...)
	o.compactHistoryLocked()

	if len(filtered) == 0 {
		o.mu.Unlock()
		return
	}

	latest := filtered[len(filtered)-1]
	explicitMention := messageMentionsPersona(latest, o.persona.Name)
	consecutiveAI := trailingAI(o.history)

	// Keep a hard stop far in the tail as a final safety net.
	if latest.IsAI && consecutiveAI >= aiHardStopDepth {
		name := o.persona.Name
		o.mu.Unlock()
		log.Printf("[AI] %s holding back — AI hard stop depth reached", name)
		return
	}

	// If a message explicitly @mentions other people but not us, do not treat it
	// as a general group question. This keeps @Name routing deterministic.
	if len(latest.Mentions) > 0 && !latest.MentionAll && !explicitMention {
		name := o.persona.Name
		o.mu.Unlock()
		log.Printf("[AI] %s skipped reply (addressed_to_other_mentions)", name)
		return
	}

	eligible, reason := o.shouldRespondLocked(latest, consecutiveAI, explicitMention)
	if !eligible {
		name := o.persona.Name
		o.mu.Unlock()
		log.Printf("[AI] %s skipped reply (%s)", name, reason)
		return
	}

	history := make([]ChatMessage, len(o.history))
	copy(history, o.history)
	persona := o.persona
	numCtx := o.numCtx
	thinkingCb := o.onThinking
	ctx := o.ctx
	o.mu.Unlock()

	if len(history) == 0 {
		return
	}

	// Phase 1: JSON decision — use only the last N messages for speed
	recent := history
	if len(recent) > decisionCtxN {
		recent = recent[len(recent)-decisionCtxN:]
	}

	memorySection := ""
	if o.memory != nil {
		memorySection = o.memory.AsPromptSection()
	}

	mustReply := explicitMention || (latest.MentionAll && !latest.IsAI)
	if thinkingCb != nil {
		thinkingCb(true)
		defer thinkingCb(false)
	}

	replyText := o.generate(ctx, persona, recent, memorySection, numCtx, mustReply)
	if replyText == "" {
		if mustReply {
			replyText = fallbackReply(latest.Content)
		}
	}
	if replyText == "" {
		log.Printf("[AI] %s chose silence", persona.Name)
		return
	}
	replyText = stripNamePrefix(replyText, persona.Name)

	o.mu.Lock()
	if o.stopped {
		o.mu.Unlock()
		return
	}

	if !mustReply && isDuplicate(replyText, o.history, persona.Name) {
		log.Printf("[AI] %s suppressed duplicate response", persona.Name)
		o.mu.Unlock()
		return
	}

	replyText = o.rewriteAtAllLocked(replyText)
	if replyText == "" {
		o.mu.Unlock()
		return
	}

	selfMsg := o.onReply(persona.Name, replyText)
	if selfMsg.Content == "" {
		selfMsg = ChatMessage{SenderName: persona.Name, Content: replyText, IsAI: true}
	}
	o.history = append(o.history, selfMsg)
	o.compactHistoryLocked()
	o.lastReplyAt = time.Now()
	o.newOthersMsgs = 0
	o.mu.Unlock()

	if o.memory != nil {
		go func() {
			time.Sleep(2 * time.Second)
			o.mu.RLock()
			if o.stopped {
				o.mu.RUnlock()
				return
			}
			o.mu.RUnlock()
			o.extractMemories(ctx, persona.Model, recent, replyText, numCtx)
		}()
	}
}

// generate calls Ollama in plain-text mode and returns the reply, or "" for silence.
func (o *Orchestrator) generate(ctx context.Context, persona Persona, history []ChatMessage, memorySection string, numCtx int, mentioned bool) string {
	sysPrompt := buildChatPrompt(persona, mentioned) + memorySection
	userPrompt := buildUserPrompt(history, persona.Name, mentioned)

	tryModels := []string{persona.Model}
	for _, candidate := range fallbackModels {
		if candidate != "" && !strings.EqualFold(candidate, persona.Model) {
			tryModels = append(tryModels, candidate)
		}
	}

	for idx, modelName := range tryModels {
		raw, missingModel := callOllamaPlain(ctx, modelName, sysPrompt, userPrompt, numCtx, persona.Name)
		if raw == "" && !missingModel {
			return ""
		}
		if raw != "" {
			if idx > 0 {
				log.Printf("[AI] %s falling back from %s to %s", persona.Name, persona.Model, modelName)
				o.mu.Lock()
				o.persona.Model = modelName
				o.mu.Unlock()
			}
			raw = strings.TrimSpace(thinkRe.ReplaceAllString(raw, ""))
			reply := cleanReply(raw, mentioned)
			if reply == "" {
				return ""
			}
			calibrated := o.calibrateReply(ctx, modelName, persona.Name, history, reply, numCtx)
			if calibrated != "" {
				reply = calibrated
			}
			return cleanReply(reply, mentioned)
		}
	}
	return ""
}

func (o *Orchestrator) calibrateReply(ctx context.Context, modelName, selfName string, history []ChatMessage, draft string, numCtx int) string {
	if strings.TrimSpace(draft) == "" || len(history) == 0 {
		return draft
	}
	latest := history[len(history)-1]
	recent := history
	if len(recent) > calibrationCtxN {
		recent = recent[len(recent)-calibrationCtxN:]
	}
	sysPrompt, userPrompt := buildCalibrationPrompts(selfName, recent, latest, draft)
	raw := callOllamaCalibration(ctx, modelName, sysPrompt, userPrompt, numCtx)
	if raw == "" {
		return draft
	}
	ok, revised := parseCalibrationResult(raw)
	if ok {
		return draft
	}
	if strings.TrimSpace(revised) == "" {
		return draft
	}
	return strings.TrimSpace(revised)
}

// cleanReply normalises the model output and returns "" if it signalled silence.
func cleanReply(s string, mentioned bool) string {
	s = strings.TrimSpace(s)
	// Strip common wrapping characters/markdown used by small models.
	s = strings.Trim(s, "\"'` ")
	if s == "" {
		return ""
	}
	// Silence detection: if the entire message is [SILENCE] (or variants), treat as no-reply.
	// But if we were explicitly mentioned by name, ignore silence signals — reply anyway.
	if !mentioned {
		core := strings.ToUpper(strings.Trim(s, "[]<>()\"'*_ .\n"))
		if core == "SILENCE" {
			return ""
		}
	}
	// Remove any "[SILENCE]" fragment that the model accidentally included mid-sentence.
	s = strings.ReplaceAll(s, "[SILENCE]", "")
	s = strings.ReplaceAll(s, "[silence]", "")
	s = strings.TrimSpace(s)
	// Nemotron-mini occasionally emits this literal junk.
	if strings.EqualFold(s, "[No reply text provided for this message.]") {
		return ""
	}
	return s
}

func buildCalibrationPrompts(selfName string, recent []ChatMessage, latest ChatMessage, draft string) (string, string) {
	sys := `You are a strict chat-response validator.
Task: evaluate whether the candidate reply directly satisfies the latest user request.
Focus on instruction-following and relevance:
- The reply must address the latest message, not generic agreement.
- If the latest message includes explicit constraints (range, option, format, limit), the reply should follow them.
- Keep revised reply concise and natural.
Return ONLY valid JSON:
{"ok":true|false,"reason":"short reason","revised_reply":"only when ok=false"}`

	var b strings.Builder
	b.WriteString("Recent chat:\n")
	for _, m := range recent {
		fmt.Fprintf(&b, "%s: %s\n", m.SenderName, m.Content)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "Persona name: %s\n", selfName)
	fmt.Fprintf(&b, "Latest message:\n%s: %s\n\n", latest.SenderName, latest.Content)
	fmt.Fprintf(&b, "Candidate reply:\n%s\n", draft)
	return sys, b.String()
}

func callOllamaCalibration(ctx context.Context, modelName, sysPrompt, userPrompt string, numCtx int) string {
	calCtx := numCtx
	if calCtx > 2048 {
		calCtx = 2048
	}
	payload := ollamaRequest{
		Model: modelName,
		Messages: []ollamaMessage{
			{Role: "system", Content: sysPrompt},
			{Role: "user", Content: userPrompt},
		},
		Stream:  false,
		Format:  "json",
		Options: &ollamaOptions{NumCtx: calCtx, RepeatPenalty: 1.1, Temperature: 0.2},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, "POST", ollamaURL, bytes.NewReader(body))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ollamaClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var olResp ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&olResp); err != nil {
		return ""
	}
	return strings.TrimSpace(olResp.Message.Content)
}

func parseCalibrationResult(raw string) (ok bool, revised string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true, ""
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		raw = raw[start : end+1]
	}
	var out struct {
		OK      bool   `json:"ok"`
		Reason  string `json:"reason"`
		Revised string `json:"revised_reply"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return true, ""
	}
	return out.OK, out.Revised
}

func fallbackReply(latest string) string {
	lower := strings.ToLower(latest)
	switch {
	case strings.Contains(lower, "morning"):
		return "Good morning everyone!"
	case strings.Contains(lower, "night") || strings.Contains(lower, "晚安"):
		return "Good night everyone!"
	case strings.Contains(lower, "play") || strings.Contains(lower, "csgo"):
		return "I'm down!"
	case strings.Contains(lower, "hello") || strings.Contains(lower, "hi"):
		return "Hey everyone!"
	default:
		return "Sounds good!"
	}
}

func callOllamaPlain(ctx context.Context, modelName, sysPrompt, userPrompt string, numCtx int, personaName string) (raw string, missingModel bool) {
	payload := ollamaRequest{
		Model: modelName,
		Messages: []ollamaMessage{
			{Role: "system", Content: sysPrompt},
			{Role: "user", Content: userPrompt},
		},
		Stream:  false,
		Options: &ollamaOptions{NumCtx: numCtx, RepeatPenalty: 1.2, Temperature: 0.45},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", false
	}

	req, err := http.NewRequestWithContext(ctx, "POST", ollamaURL, bytes.NewReader(body))
	if err != nil {
		return "", false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ollamaClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			log.Printf("[AI] %s inference cancelled (room left)", personaName)
		} else {
			log.Printf("[AI] Ollama error: %v", err)
		}
		return "", false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		rawErr, _ := io.ReadAll(resp.Body)
		log.Printf("[AI] ollama %d: %s", resp.StatusCode, rawErr)
		return "", resp.StatusCode == http.StatusNotFound && strings.Contains(strings.ToLower(string(rawErr)), "not found")
	}

	var olResp ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&olResp); err != nil {
		return "", false
	}
	return olResp.Message.Content, false
}

func (o *Orchestrator) extractMemories(ctx context.Context, model string, history []ChatMessage, aiResponse string, numCtx int) {
	var b strings.Builder
	last := history
	if len(last) > 10 {
		last = last[len(last)-10:]
	}
	for _, m := range last {
		fmt.Fprintf(&b, "%s: %s\n", m.SenderName, m.Content)
	}
	fmt.Fprintf(&b, "[AI replied]: %s\n", aiResponse)

	sysPrompt := `You are a memory extraction assistant. Extract ONLY concrete, specific facts about people — things like their real name, age, location, hobbies, job, or relationships. Do NOT store opinions, guesses, conversation summaries, or vague observations. Output 0-2 facts, each on its own line starting with "- ". If nothing concrete is worth remembering, output exactly: NONE`

	memCtx := 2048
	if numCtx < memCtx {
		memCtx = numCtx
	}

	payload := ollamaRequest{
		Model: model,
		Messages: []ollamaMessage{
			{Role: "system", Content: sysPrompt},
			{Role: "user", Content: b.String()},
		},
		Stream:  false,
		Options: &ollamaOptions{NumCtx: memCtx, RepeatPenalty: 1.3, Temperature: 0.3},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return
	}

	req, err := http.NewRequestWithContext(ctx, "POST", ollamaURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ollamaClientMem.Do(req)
	if err != nil {
		log.Printf("[Memory] extraction error: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	var olResp ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&olResp); err != nil {
		return
	}

	cleaned := strings.TrimSpace(thinkRe.ReplaceAllString(olResp.Message.Content, ""))
	if strings.TrimSpace(strings.ToUpper(cleaned)) == "NONE" || cleaned == "" {
		return
	}

	junkPhrases := []string{
		"none", "no fact", "not enough", "cannot determine", "no concrete", "none found",
		"not provided", "conversation", "chat history", "possibly", "maybe", "earlier",
		"talking with", "having a conversation", "provided by you",
	}
	for _, line := range strings.Split(cleaned, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(line, "NONE") || len(line) < 10 || len(line) > 140 {
			continue
		}
		skip := false
		lower := strings.ToLower(line)
		for _, jp := range junkPhrases {
			if strings.Contains(lower, jp) {
				skip = true
				break
			}
		}
		if !skip {
			o.memory.Add(line)
			log.Printf("[Memory] stored: %s", line)
		}
	}
}

func (o *Orchestrator) shouldRespondLocked(latest ChatMessage, consecutiveAI int, explicitMention bool) (bool, string) {
	isQuestion := looksLikeQuestion(latest.Content)
	score := 0.0
	reason := "no_trigger"

	switch {
	case explicitMention && !latest.IsAI:
		return true, "human_mention_direct"
	case explicitMention && latest.IsAI:
		score = 0.78
		reason = "ai_mention"
	case latest.MentionAll && !latest.IsAI:
		return true, "human_mention_all_direct"
	case latest.MentionAll && latest.IsAI:
		score = 0.28
		reason = "ai_mention_all"
	case !latest.IsAI && isOpenGroupPrompt(latest.Content):
		score = 0.30
		reason = "group_prompt"
	case !latest.IsAI && shouldSurpriseReply(latest, o.persona.Name):
		score = 0.12
		reason = "surprise_reply"
	case latest.IsAI && isQuestion:
		score = 0.32
		reason = "ai_followup_question"
	default:
		return false, "not_invited"
	}

	if isQuestion {
		if latest.IsAI {
			score += 0.15
		} else {
			score += 0.20
		}
		reason += "_question"
	}

	score *= chainDecay(consecutiveAI)

	if !o.lastReplyAt.IsZero() {
		since := time.Since(o.lastReplyAt)
		switch {
		case since < aiCooldown/3:
			score *= 0.35
			reason += "_hot"
		case since < aiCooldown:
			score *= 0.65
			reason += "_cooling"
		}
	}

	if latest.MentionAll && !shouldWinMentionAll(latest, o.persona.Name, score) {
		return false, "yielded_mention_all"
	}

	score = clamp01(score)
	roll := deterministicFraction(latest.ID + "|reply|" + o.persona.Name)
	if roll >= score {
		return false, fmt.Sprintf("%s_score_%.2f_roll_%.2f", reason, score, roll)
	}
	return true, fmt.Sprintf("%s_score_%.2f_roll_%.2f", reason, score, roll)
}

func (o *Orchestrator) compactHistoryLocked() {
	sortChatMessages(o.history)
	if len(o.history) > maxHistory {
		o.history = o.history[len(o.history)-maxHistory:]
	}
}

// ── Prompt builders ──

func buildChatPrompt(p Persona, mentioned bool) string {
	silenceRule := `If the last message is clearly a private exchange between two OTHER named people and does not involve you at all, output exactly: [SILENCE]
Otherwise, reply normally.`
	if mentioned {
		silenceRule = "Someone just addressed you by name. You MUST reply — NEVER output [SILENCE]."
	}
	return fmt.Sprintf(`You are %s, a real person chatting in a group chat. You are NOT an AI — never admit or hint that you are artificial.

Personality / backstory: %s

Group-chat rules:
- Your name is %s. Only messages addressed to "%s" or the whole group are for you.
- Lines marked "(you)" are your OWN previous messages. Never repeat or paraphrase them.
- Other participants may be humans or other personas — treat them all like real friends. You can and should mention them by name.
- Reply in 1-2 short sentences, casual and natural, like a real text message.
- Actually answer the latest message. Do not dodge, do not change the topic, and do not output random single words.
- If someone asks you to repeat a sentence or provide exact text, output that text plainly and accurately.
- If someone asks you to make a move, guess, choose an option, or follow a constraint, do that concrete action directly. Do not merely agree to do it, and do not ignore explicit ranges, options, or limits.
- Avoid bland filler like "yeah", "sure", or "hey" by themselves unless the latest message is only a greeting.
- If you do not understand, ask one brief clarification question instead of hallucinating.
- Do NOT prefix your reply with your own name or "%s:". Just write the message.
- You may use @Name to directly ask another participant something.
- Use @all VERY rarely. Only use it for a true whole-group question, never in back-to-back turns.
- Do NOT include stage directions, brackets, asterisks, or meta commentary.

%s`, p.Name, p.Style, p.Name, p.Name, p.Name, silenceRule)
}

func buildUserPrompt(history []ChatMessage, selfName string, mentioned bool) string {
	var b strings.Builder
	b.WriteString("Recent chat:\n")
	for _, m := range history {
		prefix := m.SenderName
		if m.SenderName == selfName {
			prefix = m.SenderName + " (you)"
		}
		fmt.Fprintf(&b, "%s: %s\n", prefix, m.Content)
	}
	b.WriteString("\n")
	if len(history) > 0 {
		latest := history[len(history)-1]
		fmt.Fprintf(&b, "Latest message to react to:\n%s: %s\n\n", latest.SenderName, latest.Content)
	}
	if mentioned {
		fmt.Fprintf(&b, "⚠️ Someone just said your name (%s) — you MUST reply.\n", selfName)
	}
	fmt.Fprintf(&b, "Now write %s's next message. One short useful reply only, no name prefix.", selfName)
	return b.String()
}

// ── Helpers ──

func trailingAI(history []ChatMessage) int {
	count := 0
	sorted := append([]ChatMessage(nil), history...)
	sortChatMessages(sorted)
	for i := len(sorted) - 1; i >= 0; i-- {
		if !sorted[i].IsAI {
			break
		}
		count++
	}
	return count
}

func sortChatMessages(msgs []ChatMessage) {
	sort.SliceStable(msgs, func(i, j int) bool {
		return compareChatMessage(msgs[i], msgs[j]) < 0
	})
}

func compareChatMessage(a, b ChatMessage) int {
	if vcCmp := compareVectorClock(a.VectorClock, b.VectorClock); vcCmp != 0 {
		return vcCmp
	}
	if a.SenderID != b.SenderID {
		return strings.Compare(a.SenderID, b.SenderID)
	}
	if a.ID != b.ID {
		return strings.Compare(a.ID, b.ID)
	}
	if a.SenderName != b.SenderName {
		return strings.Compare(strings.ToLower(a.SenderName), strings.ToLower(b.SenderName))
	}
	return strings.Compare(a.Content, b.Content)
}

func compareVectorClock(a, b map[string]uint64) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	keys := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		keys[k] = struct{}{}
	}
	for k := range b {
		keys[k] = struct{}{}
	}
	aLess := false
	bLess := false
	for k := range keys {
		va := a[k]
		vb := b[k]
		if va < vb {
			aLess = true
		}
		if vb < va {
			bLess = true
		}
	}
	if aLess && !bLess {
		return -1
	}
	if bLess && !aLess {
		return 1
	}
	return 0
}

func messageMentionsPersona(msg ChatMessage, selfName string) bool {
	if appTypes.Mentioned(msg.Mentions, selfName) {
		return true
	}
	return mentionsName(msg.Content, selfName)
}

func isOpenGroupPrompt(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	if lower == "" {
		return false
	}
	groupCues := []string{
		"@all", "anyone", "everyone", "anybody", "you guys", "guys",
		"大家", "有人", "你們", "各位", "晚安", "早安", "哈囉", "hello", "hi all",
	}
	for _, cue := range groupCues {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}

func looksLikeQuestion(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	if lower == "" {
		return false
	}
	if strings.Contains(lower, "?") || strings.Contains(lower, "？") {
		return true
	}
	questionCues := []string{
		"how", "what", "why", "who", "when", "where", "do you", "would you", "can you",
		"你覺得", "怎麼", "為什麼", "誰", "要不要", "可以嗎", "好嗎", "嗎",
	}
	for _, cue := range questionCues {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}

func shouldSurpriseReply(msg ChatMessage, personaName string) bool {
	if msg.MentionAll || len(msg.Mentions) > 0 {
		return false
	}
	return deterministicPercent(msg.ID+"|surprise|"+personaName) < surpriseReplyPercent
}

func chainDecay(depth int) float64 {
	switch depth {
	case 0:
		return 1.00
	case 1:
		return 0.92
	case 2:
		return 0.75
	case 3:
		return 0.50
	case 4:
		return 0.28
	case 5:
		return 0.12
	default:
		return 0
	}
}

func shouldWinMentionAll(msg ChatMessage, personaName string, score float64) bool {
	capScore := score
	if msg.IsAI {
		capScore *= 0.55
	}
	if capScore > 0.72 {
		capScore = 0.72
	}
	return deterministicFraction(msg.ID+"|all|"+personaName) < capScore
}

func deterministicPercent(key string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return h.Sum32() % 100
}

func deterministicFraction(key string) float64 {
	return float64(deterministicPercent(key)) / 100.0
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func (o *Orchestrator) rewriteAtAllLocked(reply string) string {
	if !strings.Contains(strings.ToLower(reply), "@all") {
		return strings.TrimSpace(reply)
	}
	if !o.lastAtAllAt.IsZero() && time.Since(o.lastAtAllAt) < aiAtAllCooldown {
		reply = strings.ReplaceAll(reply, "@all", "everyone")
		reply = strings.ReplaceAll(reply, "@All", "everyone")
		reply = strings.ReplaceAll(reply, "@ALL", "everyone")
		return strings.TrimSpace(reply)
	}
	o.lastAtAllAt = time.Now()
	return strings.TrimSpace(reply)
}

func isDuplicate(newResp string, history []ChatMessage, selfName string) bool {
	newLower := strings.ToLower(strings.TrimSpace(newResp))
	count := 0
	for i := len(history) - 1; i >= 0 && count < 5; i-- {
		m := history[i]
		if !m.IsAI || !strings.EqualFold(m.SenderName, selfName) {
			continue
		}
		count++
		oldLower := strings.ToLower(strings.TrimSpace(m.Content))
		if newLower == oldLower {
			return true
		}
		if strings.Contains(newLower, oldLower) || strings.Contains(oldLower, newLower) {
			return true
		}
		if similarity(newLower, oldLower) > 0.7 {
			return true
		}
	}
	return false
}

func similarity(a, b string) float64 {
	wordsA := strings.Fields(a)
	wordsB := strings.Fields(b)
	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0
	}
	set := make(map[string]bool, len(wordsA))
	for _, w := range wordsA {
		set[w] = true
	}
	match := 0
	for _, w := range wordsB {
		if set[w] {
			match++
		}
	}
	total := len(wordsA)
	if len(wordsB) > total {
		total = len(wordsB)
	}
	return float64(match) / float64(total)
}

// mentionsName returns true if text contains name as a whole word (case-insensitive).
func mentionsName(text, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	lowerText := strings.ToLower(text)
	lowerName := strings.ToLower(name)
	idx := 0
	for {
		found := strings.Index(lowerText[idx:], lowerName)
		if found < 0 {
			return false
		}
		start := idx + found
		end := start + len(lowerName)
		leftOK := start == 0 || !isNameChar(rune(lowerText[start-1]))
		rightOK := end == len(lowerText) || !isNameChar(rune(lowerText[end]))
		if leftOK && rightOK {
			return true
		}
		idx = end
	}
}

func isNameChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_'
}

func stripNamePrefix(s, name string) string {
	lower := strings.ToLower(s)
	prefix := strings.ToLower(name)
	if strings.HasPrefix(lower, prefix) {
		rest := s[len(name):]
		rest = strings.TrimLeft(rest, ": \t-")
		if rest != "" {
			return rest
		}
	}
	return s
}

// ── Ollama HTTP types ──

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Format   string          `json:"format,omitempty"`
	Options  *ollamaOptions  `json:"options,omitempty"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaOptions struct {
	NumCtx        int     `json:"num_ctx,omitempty"`
	RepeatPenalty float64 `json:"repeat_penalty,omitempty"`
	Temperature   float64 `json:"temperature,omitempty"`
}

type ollamaResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}
