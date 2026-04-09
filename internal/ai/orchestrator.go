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

// Orchestrator manages Ollama inference with persona-driven system prompts
// and implements the Reasoning-to-Silence pattern.
type Orchestrator struct {
	mu      sync.RWMutex
	persona Persona
	history []ChatMessage
	onReply func(name, content string)
}

func NewOrchestrator(onReply func(name, content string)) *Orchestrator {
	return &Orchestrator{
		persona: Persona{Name: "AI", Style: "friendly and casual", Model: "deepseek-r1:8b"},
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

// AddToHistory appends a message to the sliding context window.
func (o *Orchestrator) AddToHistory(msg ChatMessage) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.history = append(o.history, msg)
	if len(o.history) > maxHistory {
		o.history = o.history[len(o.history)-maxHistory:]
	}
}

// Process is called by the Batcher when a trigger fires.
// newMessages are appended to the history, then the full context is sent to Ollama.
func (o *Orchestrator) Process(newMessages []ChatMessage) {
	o.mu.Lock()
	o.history = append(o.history, newMessages...)
	if len(o.history) > maxHistory {
		o.history = o.history[len(o.history)-maxHistory:]
	}
	history := make([]ChatMessage, len(o.history))
	copy(history, o.history)
	persona := o.persona
	o.mu.Unlock()

	if len(history) == 0 {
		return
	}

	systemPrompt := buildSystemPrompt(persona)
	userPrompt := buildUserPrompt(history)

	raw, err := callOllama(persona.Model, systemPrompt, userPrompt)
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

// ── Ollama HTTP call ──

type ollamaRequest struct {
	Model    string           `json:"model"`
	Messages []ollamaMessage  `json:"messages"`
	Stream   bool             `json:"stream"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

func callOllama(model, systemPrompt, userPrompt string) (string, error) {
	body, err := json.Marshal(ollamaRequest{
		Model: model,
		Messages: []ollamaMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Stream: false,
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
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
		return "", fmt.Errorf("decode response: %w", err)
	}
	return result.Message.Content, nil
}
