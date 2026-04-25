package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

type Pipeline struct {
	Engine    TTSEngine
	OutputDir string
	Workers   int
	State     *State
}

func (p *Pipeline) ScanExisting(ctx context.Context, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("scan error: %v", err)
		return
	}
	for _, e := range entries {
		if ctx.Err() != nil {
			return
		}
		if e.IsDir() || filepath.Ext(e.Name()) != ".epub" {
			continue
		}
		epubPath := filepath.Join(dir, e.Name())
		hash, err := fileHash(epubPath)
		if err != nil {
			continue
		}
		if p.State.IsProcessed(epubPath, hash) {
			log.Printf("skipping (already processed): %s", e.Name())
			continue
		}
		log.Printf("found unprocessed: %s", e.Name())
		p.Process(ctx, epubPath)
	}
}

func (p *Pipeline) Process(ctx context.Context, epubPath string) {
	log.Printf("processing: %s", filepath.Base(epubPath))

	hash, err := fileHash(epubPath)
	if err != nil {
		log.Printf("error hashing %s: %v", filepath.Base(epubPath), err)
		return
	}
	if p.State.IsProcessed(epubPath, hash) {
		log.Printf("already processed: %s", filepath.Base(epubPath))
		return
	}

	book, err := ParseEPUB(epubPath)
	if err != nil {
		log.Printf("parse error for %s: %v", filepath.Base(epubPath), err)
		return
	}

	log.Printf("book: %q by %s (%d chapters)", book.Title, book.Author, len(book.Chapters))

	bookDir := filepath.Join(p.OutputDir, book.Title)
	if err := os.MkdirAll(bookDir, 0755); err != nil {
		log.Printf("mkdir error: %v", err)
		return
	}

	type result struct {
		idx int
		err error
	}

	total := len(book.Chapters)
	results := make([]error, total)
	var wg sync.WaitGroup
	sem := make(chan struct{}, p.Workers)

	for i, ch := range book.Chapters {
		if ctx.Err() != nil {
			log.Printf("cancelled during %s", book.Title)
			return
		}

		wg.Add(1)
		go func(idx int, ch Chapter) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if ctx.Err() != nil {
				return
			}

			filename := fmt.Sprintf("%02d - %s.mp3", idx+1, sanitizeFilename(ch.Title))
			outPath := filepath.Join(bookDir, filename)

			if _, err := os.Stat(outPath); err == nil {
				log.Printf("  [%d/%d] exists, skipping: %s", idx+1, total, ch.Title)
				return
			}

			log.Printf("  [%d/%d] synthesizing: %s", idx+1, total, ch.Title)
			meta := MP3Meta{
				Title:  ch.Title,
				Album:  book.Title,
				Artist: book.Author,
				Track:  fmt.Sprintf("%d/%d", idx+1, total),
			}
			if err := p.Engine.Synthesize(ch.Text, outPath, meta); err != nil {
				log.Printf("  [%d/%d] error: %v", idx+1, total, err)
				os.Remove(outPath)
				results[idx] = err
				return
			}
			log.Printf("  [%d/%d] done: %s", idx+1, total, ch.Title)
		}(i, ch)
	}
	wg.Wait()

	if ctx.Err() != nil {
		log.Printf("cancelled: %s", book.Title)
		return
	}

	var missing int
	for i, ch := range book.Chapters {
		filename := fmt.Sprintf("%02d - %s.mp3", i+1, sanitizeFilename(ch.Title))
		outPath := filepath.Join(bookDir, filename)
		if _, err := os.Stat(outPath); err != nil {
			missing++
		}
	}
	for _, err := range results {
		if err != nil {
			missing++
		}
	}

	if missing == 0 {
		p.State.MarkProcessed(epubPath, hash, book.Title, total)
		p.State.Save()
		log.Printf("completed: %s (%d chapters)", book.Title, total)
	} else {
		log.Printf("incomplete: %s (%d/%d chapters missing)", book.Title, missing, total)
	}
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
