//go:build !lite
package browser

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Recorder captures a sequence of screenshots and synthesises a GIF or MP4
// via FFmpeg. Recording runs on a background goroutine so the caller's
// action loop is not blocked.
type Recorder struct {
	b       *Browser
	dir     string        // temp directory for PNG frames
	fps     int           // capture rate
	stop    chan struct{}
	stopped chan struct{}
	count   atomic.Int64
	once    sync.Once
	started atomic.Bool
}

// NewRecorder creates a Recorder but does not start it yet.
// fps: frames per second to capture (5–10 is usually enough for GIF).
func NewRecorder(b *Browser, fps int) (*Recorder, error) {
	dir, err := os.MkdirTemp("", "ghost-frames-*")
	if err != nil {
		return nil, fmt.Errorf("mktemp: %w", err)
	}
	if fps <= 0 {
		fps = 8
	}
	return &Recorder{
		b:       b,
		dir:     dir,
		fps:     fps,
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}, nil
}

// Start begins background screenshot capture.
func (r *Recorder) Start(ctx context.Context) {
	_ = r.b.SetIndicatorColor(ctx, "#ff2222")
	_ = r.b.SetIndicatorLabel(ctx, "⏺ recording")
	interval := time.Second / time.Duration(r.fps)
	r.started.Store(true)
	go func() {
		defer close(r.stopped)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-r.stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.captureFrame(ctx)
			}
		}
	}()
}

// Stop halts capture and returns after the background goroutine exits.
// Safe even if Start was never called (no deadlock).
func (r *Recorder) Stop() {
	r.once.Do(func() { close(r.stop) })
	if r.started.Load() {
		<-r.stopped
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = r.b.SetIndicatorColor(ctx, "#00d4ff")
	_ = r.b.SetIndicatorLabel(ctx, "● Ghost-Driver")
}

// Cleanup removes the temp frames directory. Idempotent — safe to call after
// Render (which already removed it). Prevents temp-dir leaks when recording
// is abandoned without a Render call.
func (r *Recorder) Cleanup() {
	_ = os.RemoveAll(r.dir)
}

// Render synthesises the captured frames into a GIF (or MP4 if outPath ends in .mp4).
// outPath: destination file path.  FFmpeg must be installed.
func (r *Recorder) Render(outPath string) error {
	defer os.RemoveAll(r.dir) // clean up frames regardless of outcome

	n := r.count.Load()
	if n == 0 {
		return fmt.Errorf("no frames captured")
	}

	ext := filepath.Ext(outPath)
	switch ext {
	case ".gif":
		return r.renderGIF(outPath)
	case ".mp4":
		return r.renderMP4(outPath)
	default:
		return fmt.Errorf("unsupported output format %q (use .gif or .mp4)", ext)
	}
}

func (r *Recorder) captureFrame(ctx context.Context) {
	png, err := r.b.Screenshot(ctx, ScreenshotOptions{Format: "png"})
	if err != nil {
		return
	}
	path := filepath.Join(r.dir, fmt.Sprintf("frame_%06d.png", r.count.Add(1)))
	if err := os.WriteFile(path, png, 0644); err != nil {
		// Disk full / permission denied: roll the counter back so FFmpeg's
		// frame sequence stays consistent with the files on disk.
		r.count.Add(-1)
		return
	}
}

// FFmpegPath is the path to the ffmpeg binary. It can be overridden at runtime.
var FFmpegPath = "ffmpeg"

func findFFmpeg() string {
	// 1. Check if the global variable is a full path and exists
	if filepath.IsAbs(FFmpegPath) {
		if _, err := os.Stat(FFmpegPath); err == nil {
			return FFmpegPath
		}
	}

	// 2. Check environment variable
	if env := os.Getenv("FFMPEG_PATH"); env != "" {
		return env
	}

	// 3. Try to find in system PATH
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		return path
	}

	return FFmpegPath // Fallback to default name
}

// renderGIF uses FFmpeg's palettegen + paletteuse filter chain for
// high-quality GIF output (much better than direct gif encoding).
func (r *Recorder) renderGIF(outPath string) error {
	ffmpeg := findFFmpeg()
	palette := filepath.Join(r.dir, "palette.png")

	// Step 1: generate optimal palette.
	genCmd := exec.Command(ffmpeg, "-y",
		"-framerate", fmt.Sprintf("%d", r.fps),
		"-i", filepath.Join(r.dir, "frame_%06d.png"),
		"-vf", "fps="+fmt.Sprintf("%d", r.fps)+",scale=1280:-1:flags=lanczos,palettegen=stats_mode=diff",
		palette,
	)
	if out, err := genCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg palettegen (path: %s): %w\n%s", ffmpeg, err, out)
	}

	// Step 2: encode GIF using the palette.
	encCmd := exec.Command(ffmpeg, "-y",
		"-framerate", fmt.Sprintf("%d", r.fps),
		"-i", filepath.Join(r.dir, "frame_%06d.png"),
		"-i", palette,
		"-lavfi", "fps="+fmt.Sprintf("%d", r.fps)+",scale=1280:-1:flags=lanczos[x];[x][1:v]paletteuse=dither=bayer:bayer_scale=5",
		outPath,
	)
	if out, err := encCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg gif encode (path: %s): %w\n%s", ffmpeg, err, out)
	}
	return nil
}

func (r *Recorder) renderMP4(outPath string) error {
	ffmpeg := findFFmpeg()
	cmd := exec.Command(ffmpeg, "-y",
		"-framerate", fmt.Sprintf("%d", r.fps),
		"-i", filepath.Join(r.dir, "frame_%06d.png"),
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-crf", "23",
		"-preset", "fast",
		outPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg mp4 encode (path: %s): %w\n%s", ffmpeg, err, out)
	}
	return nil
}
