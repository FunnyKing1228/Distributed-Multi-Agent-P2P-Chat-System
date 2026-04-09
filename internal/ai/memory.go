package ai

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Memory is a single long-term fact the AI remembers.
type Memory struct {
	ID        string `json:"id"`
	Fact      string `json:"fact"`
	CreatedAt string `json:"created_at"`
}

// MemoryStore persists AI memories to ~/.dmapc/personas/{name}/memory.json.
type MemoryStore struct {
	mu       sync.RWMutex
	memories []Memory
	path     string
	maxItems int
}

func NewMemoryStore(personaName string, maxItems int) (*MemoryStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("home dir: %w", err)
	}
	dir := filepath.Join(home, ".dmapc", "personas", sanitise(personaName))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}

	ms := &MemoryStore{
		path:     filepath.Join(dir, "memory.json"),
		maxItems: maxItems,
	}
	ms.load()
	return ms, nil
}

// Add stores a new fact. Oldest memories are evicted when the limit is reached.
func (ms *MemoryStore) Add(fact string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.memories = append(ms.memories, Memory{
		ID:        uuid.New().String(),
		Fact:      fact,
		CreatedAt: time.Now().Format(time.RFC3339),
	})
	if len(ms.memories) > ms.maxItems {
		ms.memories = ms.memories[len(ms.memories)-ms.maxItems:]
	}
	ms.save()
}

func (ms *MemoryStore) Delete(id string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	for i, m := range ms.memories {
		if m.ID == id {
			ms.memories = append(ms.memories[:i], ms.memories[i+1:]...)
			ms.save()
			return
		}
	}
}

func (ms *MemoryStore) All() []Memory {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	out := make([]Memory, len(ms.memories))
	copy(out, ms.memories)
	return out
}

// AsPromptSection returns a string suitable for injection into the system prompt.
func (ms *MemoryStore) AsPromptSection() string {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	if len(ms.memories) == 0 {
		return ""
	}
	s := "\n\nYou remember these facts from past conversations:\n"
	for _, m := range ms.memories {
		s += "- " + m.Fact + "\n"
	}
	return s
}

func (ms *MemoryStore) load() {
	data, err := os.ReadFile(ms.path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &ms.memories)
}

func (ms *MemoryStore) save() {
	data, err := json.MarshalIndent(ms.memories, "", "  ")
	if err != nil {
		log.Printf("[Memory] marshal: %v", err)
		return
	}
	_ = os.WriteFile(ms.path, data, 0o644)
}

func sanitise(name string) string {
	out := make([]byte, 0, len(name))
	for _, c := range []byte(name) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return "default"
	}
	return string(out)
}
