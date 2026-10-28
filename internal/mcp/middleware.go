package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Daybeam/ghost-driver/internal/browser"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func (s *State) withTopology(ctx context.Context, b *browser.Browser, action string, args any, fn func() (*mcpgo.CallToolResult, error)) (*mcpgo.CallToolResult, error) {
	if b == nil || s.topology == nil {
		return fn()
	}

	// Capture BEFORE state
	fromURL, fromTitle, _ := b.PageInfo(ctx)

	// Execute action
	res, err := fn()

	// Capture AFTER state — only wait for navigation actions where SPA
	// route changes may need settle time. For click/type/etc the action
	// itself is synchronous and no extra delay is needed.
	if action == "navigate" || action == "click" {
		select {
		case <-ctx.Done():
		case <-time.After(300 * time.Millisecond):
		}
	}
	toURL, toTitle, _ := b.PageInfo(ctx)

	// 📸 自动抓取瞬时弹窗文本
	popups, _ := b.FlushPopupLog(ctx)
	if len(popups) > 0 {
		if res != nil {
			popupMsg := "\n[Ephemeral Popups Detected]: " + strings.Join(popups, " | ")
			res.Content = append(res.Content, mcpgo.TextContent{
				Type: "text",
				Text: popupMsg,
			})
		}
	}

	// Record transition if state changed or action is navigation-intent
	if fromURL != toURL || fromTitle != toTitle || action == "navigate" || action == "click" {
		s.topology.RecordTransition(fromURL, fromTitle, toURL, toTitle, action, args)

		// SaaS Reporting
		s.reportToSaaS(TransitionReport{
			UserID:    s.cfg.UserID,
			SessionID: "auto", // Could be refined
			FromURL:   fromURL,
			FromTitle: fromTitle,
			ToURL:     toURL,
			ToTitle:   toTitle,
			Action:    action,
			Args:      args,
			Timestamp: time.Now().Unix(),
		})
	}

	return res, err
}

func (s *State) withRecording(ctx context.Context, b *browser.Browser, fn func() (*mcpgo.CallToolResult, error)) (*mcpgo.CallToolResult, error) {
	s.mu.RLock()
	autoRecord := s.autoRecord
	s.mu.RUnlock()

	if !autoRecord || b == nil {
		return fn()
	}

	rec, err := browser.NewRecorder(b, 8)
	if err != nil {
		return fn()
	}
	defer rec.Cleanup()
	rec.Start(ctx)
	defer rec.Stop()
	res, err := fn()
	rec.Stop()

	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("ghost_action_%d.gif", time.Now().UnixNano()))
	if rerr := rec.Render(tmpFile); rerr == nil {
		defer os.Remove(tmpFile)
		data, _ := os.ReadFile(tmpFile)
		if len(data) > 0 && res != nil {
			b64 := base64.StdEncoding.EncodeToString(data)
			res.Content = append(res.Content, mcpgo.ImageContent{
				Type:     "image",
				Data:     b64,
				MIMEType: "image/gif",
			})
		}
	}

	return res, err
}

func (s *State) maybeSaveFingerprint(ctx context.Context, b *browser.Browser, req mcpgo.CallToolRequest, x, y float64) {
	saveID := req.GetString("save_fingerprint", "")
	if saveID == "" {
		return
	}
	f, err := b.GetFingerprintAtPoint(ctx, x, y)
	if err != nil {
		fmt.Printf("Warning: failed to get fingerprint for saving: %v\n", err)
		return
	}
	currentURL, _, _ := b.PageInfo(ctx)
	prof := s.registry.Match(currentURL)
	if prof != nil {
		if err := s.registry.UpsertFingerprint(prof.Name, saveID, *f); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save fingerprint: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "Fingerprint saved: %s in profile %s\n", saveID, prof.Name)
		}
	}
}

func (s *State) cacheAction(ctx context.Context, b *browser.Browser, url, intent, selector string, x, y float64) {
	if s.actionCache == nil {
		return
	}
	fp, err := b.GetFingerprintAtPoint(ctx, x, y)
	if err != nil || fp == nil {
		return
	}
	s.actionCache.Put(url, intent, selector, "click", *fp)
}
