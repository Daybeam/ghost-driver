//go:build lite
package browser

import (
	"context"
	"fmt"
)

type Recorder struct{}

func NewRecorder(b *Browser, fps int) (*Recorder, error) {
	return nil, fmt.Errorf("recording is not available in Lite version")
}

func (r *Recorder) Start(ctx context.Context) {}
func (r *Recorder) Stop() {}
func (r *Recorder) Render(outPath string) error { return fmt.Errorf("not implemented") }

var FFmpegPath = ""
