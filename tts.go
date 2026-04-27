package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type TTSEngine interface {
	Name() string
	Synthesize(text string, mp3Path string, metadata MP3Meta) error
}

type MP3Meta struct {
	Title  string
	Album  string
	Artist string
	Track  string
}

type EngineConfig struct {
	PiperBin   string
	PiperModel string
	Voice      string
}

func NewEngine(name string, cfg EngineConfig) (TTSEngine, error) {
	switch name {
	case "edge":
		return newEdgeEngine(cfg)
	case "piper":
		return newPiperEngine(cfg)
	case "say":
		return newSayEngine(cfg)
	case "auto":
		if e, err := newEdgeEngine(cfg); err == nil {
			return e, nil
		}
		if e, err := newPiperEngine(cfg); err == nil {
			return e, nil
		}
		return newSayEngine(cfg)
	default:
		return nil, fmt.Errorf("unknown engine: %s", name)
	}
}

type EdgeEngine struct {
	voice    string
	scriptPath string
}

func newEdgeEngine(cfg EngineConfig) (*EdgeEngine, error) {
	if out, err := exec.Command("python3", "-m", "edge_tts", "--list-voices").CombinedOutput(); err != nil || len(out) == 0 {
		return nil, fmt.Errorf("edge-tts not found: install via 'pip3 install edge-tts'")
	}
	voice := cfg.Voice
	if voice == "" {
		voice = "en-US-AndrewMultilingualNeural"
	}
	ex, err := os.Executable()
	if err != nil {
		return nil, err
	}
	scriptPath := filepath.Join(filepath.Dir(ex), "edgetts.py")
	if _, err := os.Stat(scriptPath); err != nil {
		scriptPath = filepath.Join(filepath.Dir(os.Args[0]), "edgetts.py")
		if _, err := os.Stat(scriptPath); err != nil {
			return nil, fmt.Errorf("edgetts.py not found next to binary")
		}
	}
	return &EdgeEngine{voice: voice, scriptPath: scriptPath}, nil
}

func (e *EdgeEngine) Name() string { return fmt.Sprintf("edge (%s)", e.voice) }

func (e *EdgeEngine) Synthesize(text string, mp3Path string, meta MP3Meta) error {
	prepared := prepareForTTS(text)

	tmpFile, err := os.CreateTemp("", "epub2audio-*.txt")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(prepared); err != nil {
		tmpFile.Close()
		return err
	}
	tmpFile.Close()

	tmpMP3 := mp3Path + ".tmp.mp3"
	defer os.Remove(tmpMP3)

	cmd := exec.Command("python3", e.scriptPath, e.voice, tmpFile.Name(), tmpMP3)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("edge-tts: %w", err)
	}

	return tagMP3(tmpMP3, mp3Path, meta)
}

func prepareForTTS(text string) string {
	cleaned := cleanText(text)
	paragraphs := splitParagraphs(cleaned)

	var sb strings.Builder
	for i, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		sb.WriteString(para)
		if i < len(paragraphs)-1 {
			sb.WriteString("\n\n")
		}
	}
	return sb.String()
}

var (
	footnoteRef    = regexp.MustCompile(`\[\d+\]`)
	multiSpace     = regexp.MustCompile(`[^\S\n]{2,}`)
	multiNewline   = regexp.MustCompile(`\n{3,}`)
	bulletNum      = regexp.MustCompile(`(?m)^(\d+)\.\s`)
	romanNumeral   = regexp.MustCompile(`(?m)^(I{1,3}|IV|VI{0,3}|IX|X{1,3})\.\s`)
	allCapsWord    = regexp.MustCompile(`\b([A-Z]{2,})\b`)
	ellipsisDots   = regexp.MustCompile(`\.{2,}`)
	dashRun        = regexp.MustCompile(`-{2,}`)
)

func cleanText(text string) string {
	text = footnoteRef.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = strings.ReplaceAll(text, "\u200b", "")
	text = strings.ReplaceAll(text, "\ufeff", "")
	text = ellipsisDots.ReplaceAllString(text, "...")
	text = dashRun.ReplaceAllString(text, " -- ")
	text = allCapsWord.ReplaceAllStringFunc(text, func(s string) string {
		if len(s) <= 4 {
			return s
		}
		runes := []rune(s)
		result := []rune{runes[0]}
		result = append(result, []rune(strings.ToLower(string(runes[1:])))...)
		return string(result)
	})
	text = multiSpace.ReplaceAllString(text, " ")
	text = multiNewline.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

func splitParagraphs(text string) []string {
	parts := strings.Split(text, "\n")
	var paragraphs []string
	var current strings.Builder

	for _, line := range parts {
		line = strings.TrimSpace(line)
		if line == "" {
			if current.Len() > 0 {
				paragraphs = append(paragraphs, current.String())
				current.Reset()
			}
			continue
		}
		if current.Len() > 0 {
			current.WriteString(" ")
		}
		current.WriteString(line)
	}
	if current.Len() > 0 {
		paragraphs = append(paragraphs, current.String())
	}
	return paragraphs
}

// Piper TTS - free, local, high-quality neural voices
type PiperEngine struct {
	bin    string
	model  string
	python bool
}

func newPiperEngine(cfg EngineConfig) (*PiperEngine, error) {
	bin := cfg.PiperBin
	if bin == "" {
		bin = "piper"
	}
	usePython := false
	if _, err := exec.LookPath(bin); err != nil {
		if out, err2 := exec.Command("python3", "-m", "piper", "--help").CombinedOutput(); err2 == nil && len(out) > 0 {
			usePython = true
		} else {
			return nil, fmt.Errorf("piper not found: install via 'pip3 install piper-tts' or download from https://github.com/rhasspy/piper/releases")
		}
	}
	if cfg.PiperModel == "" {
		return nil, fmt.Errorf("piper requires --piper-model flag")
	}
	if _, err := os.Stat(cfg.PiperModel); err != nil {
		return nil, fmt.Errorf("piper model not found: %s", cfg.PiperModel)
	}
	if usePython {
		bin = "python3"
	}
	return &PiperEngine{bin: bin, model: cfg.PiperModel, python: usePython}, nil
}

func (e *PiperEngine) Name() string { return "piper" }

func (e *PiperEngine) Synthesize(text string, mp3Path string, meta MP3Meta) error {
	tmpWav := mp3Path + ".wav"
	defer os.Remove(tmpWav)

	var cmd *exec.Cmd
	if e.python {
		cmd = exec.Command(e.bin, "-m", "piper", "--model", e.model, "--output_file", tmpWav)
	} else {
		cmd = exec.Command(e.bin, "--model", e.model, "--output_file", tmpWav)
	}
	cmd.Stdin = strings.NewReader(cleanText(text))
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("piper: %w", err)
	}

	return audioToMP3(tmpWav, mp3Path, meta)
}

// macOS say command - built-in, zero setup
type SayEngine struct {
	voice string
}

func newSayEngine(cfg EngineConfig) (*SayEngine, error) {
	if _, err := exec.LookPath("say"); err != nil {
		return nil, fmt.Errorf("say command not found (macOS only)")
	}
	voice := cfg.Voice
	if voice == "" {
		voice = "Samantha"
	}
	return &SayEngine{voice: voice}, nil
}

func (e *SayEngine) Name() string { return fmt.Sprintf("say (%s)", e.voice) }

func (e *SayEngine) Synthesize(text string, mp3Path string, meta MP3Meta) error {
	tmpFile, err := os.CreateTemp("", "epub2audio-*.txt")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(cleanText(text)); err != nil {
		tmpFile.Close()
		return err
	}
	tmpFile.Close()

	tmpAiff := mp3Path + ".aiff"
	defer os.Remove(tmpAiff)

	cmd := exec.Command("say", "-v", e.voice, "-f", tmpFile.Name(), "-o", tmpAiff)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("say: %w", err)
	}

	return audioToMP3(tmpAiff, mp3Path, meta)
}

func tagMP3(inputMP3, outputMP3 string, meta MP3Meta) error {
	args := []string{"-i", inputMP3, "-codec", "copy"}
	if meta.Title != "" {
		args = append(args, "-metadata", fmt.Sprintf("title=%s", meta.Title))
	}
	if meta.Album != "" {
		args = append(args, "-metadata", fmt.Sprintf("album=%s", meta.Album))
	}
	if meta.Artist != "" {
		args = append(args, "-metadata", fmt.Sprintf("artist=%s", meta.Artist))
		args = append(args, "-metadata", fmt.Sprintf("album_artist=%s", meta.Artist))
	}
	if meta.Track != "" {
		args = append(args, "-metadata", fmt.Sprintf("track=%s", meta.Track))
	}
	args = append(args, "-y", outputMP3)

	cmd := exec.Command("ffmpeg", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg tag: %w", err)
	}
	return nil
}

func audioToMP3(inputPath, mp3Path string, meta MP3Meta) error {
	args := []string{"-i", inputPath, "-codec:a", "libmp3lame", "-qscale:a", "2"}
	if meta.Title != "" {
		args = append(args, "-metadata", fmt.Sprintf("title=%s", meta.Title))
	}
	if meta.Album != "" {
		args = append(args, "-metadata", fmt.Sprintf("album=%s", meta.Album))
	}
	if meta.Artist != "" {
		args = append(args, "-metadata", fmt.Sprintf("artist=%s", meta.Artist))
		args = append(args, "-metadata", fmt.Sprintf("album_artist=%s", meta.Artist))
	}
	if meta.Track != "" {
		args = append(args, "-metadata", fmt.Sprintf("track=%s", meta.Track))
	}
	args = append(args, "-y", mp3Path)

	cmd := exec.Command("ffmpeg", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg: %w", err)
	}
	return nil
}
