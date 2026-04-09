package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	ollamaURL  = "http://localhost:11434/api/chat"
	maxHistory = 50
)

var (
	ollamaClient = &http.Client{Timeout: 2 * time.Minute}
	thinkRe      = regexp.MustCompile(`(?s)<think>.*?</think>`)
)

// Persona defines the AI agent's identity.
type Persona struct {
	Name  string `json:"name"`
	Style string `json:"style"`
	Model string `json:"model"`
}

// Orchestrator manages Ollama inference with persona-driven system prompts,
// long-term memory injection, and the Reasoning-to-Silence pattern.
type Orchestrator struct {
	mu       sync.RWMutex
	persona  Persona
	history  []ChatMessage
	memory   *MemoryStore
	numCtx   int
	onReply  func(name, content string)
}

func NewOrchestrator(persona Persona, memory *MemoryStore, numCtx int, onReply func(name, content string)) *Orchestrator {
	if numCtx <= 0 {
		numCtx = 4096
	}
	return &Orchestrator{
		persona: persona,
		memory:  memory,
		numCtx:  numCtx,
		onReply: onReply,
	}
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

// Process is called by the Batcher when a trigger fires.
func (o *Orchestrator) Process(newMessages []ChatMessage) {
	o.mu.Lock()
	o.history = append(o.history, newMessages...)
	if len(o.history) > maxHistory {
		o.history = o.history[len(o.history)-maxHistory:]
	}
	history := make([]ChatMessage, len(o.history))
	copy(history, o.history)
	persona := o.persona
	numCtx := o.numCtx
	o.mu.Unlock()

	if len(history) == 0 {
		return
	}

	memorySection := ""
	if o.memory != nil {
		memorySection = o.memory.AsPromptSection()
	}

	systemPrompt := buildSystemPrompt(persona) + memorySection
	userPrompt := buildUserPrompt(history)

	raw, err := callOllamaWithCtx(persona.Model, systemPrompt, userPrompt, numCtx)
	if err != nil {
		log.Printf("[AI] Ollama error: %v", err)
		return
	}

	cleaned := strings.TrimSpace(thinkRe.ReplaceAllString(raw, ""))

	if cleaned == "" || strings.Contains(strings.ToUpper(cleaned), "[SILENCE]") {
		log.Printf("[AI] %s chose to remain silent", persona.Name)
		return
	}

	// Remember own response
	o.mu.Lock()
	o.history = append(o.history, ChatMessage{SenderName: persona.Name, Content: cleaned, IsAI: true})
	o.mu.Unlock()

	o.onReply(persona.Name, cleaned)

	// Background: extract memories from this exchange
	if o.memory != nil {
		go o.extractMemories(persona.Model, history, cleaned, numCtx)
	}
}

func (o *Orchestrator) extractMemories(model string, history []ChatMessage, aiResponse string, numCtx int) {
	var b strings.Builder
	last := history
	if len(last) > 10 {
		last = last[len(last)-10:]
	}
	for _, m := range last {
		fmt.Fprintf(&b, "%s: %s\n", m.SenderName, m.Content)
	}
	fmt.Fprintf(&b, "[AI replied]: %s\n", aiResponse)

	sysPrompt := `You are a memory extraction assistant. From the conversation below, extract 0-3 key facts worth remembering about the participants (names, preferences, relationships, events). Output each fact on its own line starting with "- ". If nothing is worth remembering, output exactly: NONE`

	raw, err := callOllamaWithCtx(model, sysPrompt, b.String(), numCtx)
	if err != nil {
		log.Printf("[Memory] extraction error: %v", err)
		return
	}

	cleaned := strings.TrimSpace(thinkRe.ReplaceAllString(raw, ""))
	if strings.TrimSpace(strings.ToUpper(cleaned)) == "NONE" || cleaned == "" {
		return
	}

	for _, line := range strings.Split(cleaned, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimSpace(line)
		if line != "" && !strings.EqualFold(line, "NONE") {
			o.memory.Add(line)
			log.Printf("[Memory] stored: %s", line)
		}
	}
}

func buildSystemPrompt(p Persona) string {
	return fmt.Sprintf(`You are %s, a real person in a group chat. You are absolutely NOT an AI — never reveal, hint, or acknowledge being artificial under any circumstances.

Your personality: %s

RULES:
1. You know nothing about programming, coding, software, or computer science. If asked, change the subject naturally.
2. Before responding, reason privately inside <think>...</think> tags. Consider: Is this relevant to me? Do I have something meaningful to add? Would a real person with my personality reply here?
3. If you decide NOT to speak (others are chatting among themselves, the topic doesn't concern you, or silence is more natural), your ENTIRE response must be: [SILENCE]
4. If you decide to speak, write ONLY your chat message outside the think tags — no quotes, no "%s:", no stage directions.
5. Keep responses short and natural, like real text messages.`, p.Name, p.Style, p.Name)
}

func buildUserPrompt(history []ChatMessage) string {
	var b strings.Builder
	b.WriteString("[Chat History]\n")
	for _, m := range history {
		tag := ""
		if m.IsAI {
			tag = " (AI)"
		}
		fmt.Fprintf(&b, "%s%s: %s\n", m.SenderName, tag, m.Content)
	}
	b.WriteString("\nBased on the conversation above, decide whether to respond or stay silent.")
	return b.String()
}

// ── Ollama HTTP ──

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  *ollamaOptions  `json:"options,omitempty"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaOptions struct {
	NumCtx int `json:"num_ctx,omitempty"`
}

type ollamaResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

func callOllamaWithCtx(model, systemPrompt, userPrompt string, numCtx int) (string, error) {
	req := ollamaRequest{
		Model: model,
		Messages: []ollamaMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Stream: false,
	}
	if numCtx > 0 {
		req.Options = &ollamaOptions{NumCtx: numCtx}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	resp, err := ollamaClient.Post(ollamaURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("POST %s: %w", ollamaURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama %d: %s", resp.StatusCode, raw)
	}

	var result ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	return result.Message.Content, nil
}
