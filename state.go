package main

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"
)

type State struct {
	path      string
	mu        sync.Mutex
	Processed map[string]ProcessedEntry `json:"processed"`
}

type ProcessedEntry struct {
	Hash      string    `json:"hash"`
	Title     string    `json:"title"`
	Chapters  int       `json:"chapters"`
	Completed time.Time `json:"completed"`
}

func LoadState(path string) *State {
	s := &State{
		path:      path,
		Processed: make(map[string]ProcessedEntry),
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	if err := json.Unmarshal(data, s); err != nil {
		log.Printf("state parse error (starting fresh): %v", err)
		s.Processed = make(map[string]ProcessedEntry)
	}
	return s
}

func (s *State) IsProcessed(path, hash string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.Processed[path]
	return ok && entry.Hash == hash
}

func (s *State) MarkProcessed(path, hash, title string, chapters int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Processed[path] = ProcessedEntry{
		Hash:      hash,
		Title:     title,
		Chapters:  chapters,
		Completed: time.Now(),
	}
}

func (s *State) Save() {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		log.Printf("state save error: %v", err)
		return
	}
	if err := os.WriteFile(s.path, data, 0644); err != nil {
		log.Printf("state save error: %v", err)
	}
}
