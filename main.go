package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

func main() {
	watchDir := flag.String("watch", "", "directory to monitor for .epub files (required)")
	outputDir := flag.String("output", "", "output directory for .mp3 files, e.g. NAS mount (required)")
	engineName := flag.String("engine", "auto", "TTS engine: edge, piper, say, auto")
	piperBin := flag.String("piper-bin", "piper", "path to piper binary")
	piperModel := flag.String("piper-model", "", "path to piper voice model (.onnx)")
	voice := flag.String("voice", "", "voice name (e.g. en-US-GuyNeural for edge, Samantha for say)")
	workers := flag.Int("workers", 2, "concurrent chapter processors")
	flag.Parse()

	if *watchDir == "" || *outputDir == "" {
		fmt.Fprintf(os.Stderr, "Usage: epub2audio -watch <dir> -output <dir> [options]\n\n")
		fmt.Fprintf(os.Stderr, "Monitors a directory for .epub files, converts them to MP3 audiobooks\n")
		fmt.Fprintf(os.Stderr, "using text-to-speech, and saves them to an output directory.\n\n")
		fmt.Fprintf(os.Stderr, "Dependencies:\n")
		fmt.Fprintf(os.Stderr, "  - edge-tts (default): pip3 install edge-tts\n")
		fmt.Fprintf(os.Stderr, "  - ffmpeg (metadata): brew install ffmpeg\n\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	*watchDir, _ = filepath.Abs(*watchDir)
	*outputDir, _ = filepath.Abs(*outputDir)

	if _, err := os.Stat(*watchDir); os.IsNotExist(err) {
		log.Fatalf("watch directory does not exist: %s", *watchDir)
	}
	if _, err := os.Stat(*outputDir); os.IsNotExist(err) {
		log.Fatalf("output directory does not exist: %s", *outputDir)
	}

	engine, err := NewEngine(*engineName, EngineConfig{
		PiperBin:   *piperBin,
		PiperModel: *piperModel,
		Voice:      *voice,
	})
	if err != nil {
		log.Fatalf("TTS engine error: %v", err)
	}
	log.Printf("TTS engine: %s", engine.Name())

	state := LoadState(filepath.Join(*watchDir, ".epub2audio.json"))
	pipe := &Pipeline{
		Engine:    engine,
		OutputDir: *outputDir,
		Workers:   *workers,
		State:     state,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("shutting down...")
		cancel()
	}()

	pipe.ScanExisting(ctx, *watchDir)

	w, err := NewWatcher(*watchDir, pipe)
	if err != nil {
		log.Fatalf("watcher error: %v", err)
	}
	defer w.Close()

	log.Printf("watching %s for .epub files", *watchDir)
	log.Printf("output: %s", *outputDir)

	if err := w.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("watcher error: %v", err)
	}

	state.Save()
	log.Println("shutdown complete")
}
