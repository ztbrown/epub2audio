package main

import (
	"context"
	"log"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Watcher struct {
	dir     string
	pipe    *Pipeline
	watcher *fsnotify.Watcher
	pending map[string]time.Time
}

func NewWatcher(dir string, pipe *Pipeline) (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := w.Add(dir); err != nil {
		w.Close()
		return nil, err
	}
	return &Watcher{
		dir:     dir,
		pipe:    pipe,
		watcher: w,
		pending: make(map[string]time.Time),
	}, nil
}

func (w *Watcher) Close() error {
	return w.watcher.Close()
}

func (w *Watcher) Run(ctx context.Context) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case event, ok := <-w.watcher.Events:
			if !ok {
				return nil
			}
			if filepath.Ext(event.Name) != ".epub" {
				continue
			}
			if event.Op&(fsnotify.Create|fsnotify.Write) != 0 {
				w.pending[event.Name] = time.Now()
			}

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return nil
			}
			log.Printf("watcher error: %v", err)

		case <-ticker.C:
			now := time.Now()
			for epubPath, seen := range w.pending {
				if now.Sub(seen) > 3*time.Second {
					delete(w.pending, epubPath)
					log.Printf("detected: %s", filepath.Base(epubPath))
					w.pipe.Process(ctx, epubPath)
				}
			}
		}
	}
}
