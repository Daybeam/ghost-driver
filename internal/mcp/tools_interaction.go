package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/Daybeam/ghost-driver/internal/browser"
	"github.com/Daybeam/ghost-driver/internal/predict"
)

func (s *State) registerInteractionTools(srv *server.MCPServer) {
	srv.AddTool(mcpgo.NewTool("click",
		mcpgo.WithDescription(`Smooth cursor move + click.
HUMAN-LIKE HINT: Hover over the target for 1s before clicking to pass behavioral risk checks on external sites.
If click has no effect, reconnect with input_mode='l3'.`),
		mcpgo.WithNumber("x", mcpgo.Required()),
		mcpgo.WithNumber("y", mcpgo.Required()),
	), s.auditWrapper("click", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, input, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		x, _ := req.RequireFloat("x")
		y, _ := req.RequireFloat("y")
		return s.withTopology(ctx, b, "click", map[string]float64{"x": x, "y": y}, func() (*mcpgo.CallToolResult, error) {
			return s.withRecording(ctx, b, func() (*mcpgo.CallToolResult, error) {
				if err := input.Click(ctx, x, y); err != nil {
					return mcpgo.NewToolResultError(err.Error()), nil
				}
				return mcpgo.NewToolResultText(fmt.Sprintf("clicked (%.0f, %.0f)", x, y)), nil
			})
		})
	}))

	// ── hover ─────────────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("hover",
		mcpgo.WithDescription("Smooth cursor move to coordinates."),
		mcpgo.WithNumber("x", mcpgo.Required()),
		mcpgo.WithNumber("y", mcpgo.Required()),
	), s.auditWrapper("hover", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, input, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		return s.withRecording(ctx, b, func() (*mcpgo.CallToolResult, error) {
			x, err := req.RequireFloat("x")
			if err != nil {
				return mcpgo.NewToolResultError(err.Error()), nil
			}
			y, err := req.RequireFloat("y")
			if err != nil {
				return mcpgo.NewToolResultError(err.Error()), nil
			}
			if err := input.Hover(ctx, x, y); err != nil {
				return mcpgo.NewToolResultError(err.Error()), nil
			}
			return mcpgo.NewToolResultText(fmt.Sprintf("hovered (%.0f, %.0f)", x, y)), nil
		})
	}))

	// ── smart_click ──────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("smart_click",
		mcpgo.WithDescription(`Find and click an element.
If DOM strategies fail, it returns a screenshot for visual analysis.
HUMAN-LIKE HINT: Hover over the target for 1s before clicking to pass behavioral risk checks on external sites.
If click succeeds but the page doesn't react, reconnect with input_mode='l3'.`),
		mcpgo.WithString("description", mcpgo.Required()),
		mcpgo.WithString("selectors"),
		mcpgo.WithString("healing_id", mcpgo.Description("Identifier to load a saved fingerprint for self-healing.")),
		mcpgo.WithString("save_fingerprint", mcpgo.Description("ID to save the clicked element's fingerprint for future self-healing.")),
	), s.auditWrapper("smart_click", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, input, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}

		desc, err := req.RequireString("description")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}

		return s.withTopology(ctx, b, "smart_click", map[string]string{"description": desc}, func() (*mcpgo.CallToolResult, error) {
			return s.withRecording(ctx, b, func() (*mcpgo.CallToolResult, error) {
			selRaw := req.GetString("selectors", "")

			// Layer 0: Action Cache
			currentURL, _, _ := b.PageInfo(ctx)
			if s.actionCache != nil {
				if entry, found := s.actionCache.Get(currentURL, desc); found {
					if entry.Selector != "" {
						var cx, cy float64
						if ok, _ := getCoordBySelector(ctx, b, entry.Selector, &cx, &cy); ok {
							fp, _ := b.GetFingerprintAtPoint(ctx, cx, cy)
							if entry.FingerprintMatches(fp) {
								if err := input.Click(ctx, cx, cy); err == nil {
									s.maybeSaveFingerprint(ctx, b, req, cx, cy)
									s.actionCache.RecordSuccess(currentURL, desc)
									return mcpgo.NewToolResultText("clicked via Action Cache: " + entry.Selector), nil
								}
							} else {
								log.Printf("act_cache: fingerprint mismatch (selector=%s)", entry.Selector)
							}
						}
					}
					if entry.Fingerprint.TagName != "" {
						healedResult, score, err := b.FindHealedElement(ctx, entry.Fingerprint)
						if err == nil {
							if err := input.Click(ctx, healedResult.X, healedResult.Y); err == nil {
								s.maybeSaveFingerprint(ctx, b, req, healedResult.X, healedResult.Y)
								s.actionCache.RecordSuccess(currentURL, desc)
								return mcpgo.NewToolResultText(fmt.Sprintf(
									"clicked via Action Cache self-heal (score: %.2f)", score)), nil
							}
						}
					}
				} else {
					log.Printf("act_cache: miss url=%s desc=%s", currentURL, desc)
				}
			}

			// Layer 1: Jev Action Predictor (if configured)
			if s.actionPredictor != nil {
				if selRes, err := b.DiscoverSiteSelectors(ctx); err == nil && selRes != nil {
					table := predict.NewElementTable()
					for _, btn := range selRes.Buttons {
						label := btn.Text
						if label == "" {
							label = btn.AriaLabel
						}
						var x, y float64
						if ok, _ := getCoordBySelector(ctx, b, btn.Selector, &x, &y); ok {
							table.Add("button", "button", label, btn.Selector, x, y, 0, 0)
						}
					}
					for _, inp := range selRes.Inputs {
						label := inp.Text
						if label == "" {
							label = inp.AriaLabel
						}
						var x, y float64
						if ok, _ := getCoordBySelector(ctx, b, inp.Selector, &x, &y); ok {
							table.Add("input", "textbox", label, inp.Selector, x, y, 0, 0)
						}
					}
					if len(table.Items) > 0 {
						if action, pErr := s.actionPredictor.Predict(ctx, table.Format(), desc); pErr == nil {
							if target, ok := table.Get(action.TargetIdx); ok {
								if s.pacer != nil {
									_ = s.pacer.Pace(ctx)
								}
								if err := input.Click(ctx, target.X, target.Y); err == nil {
									s.maybeSaveFingerprint(ctx, b, req, target.X, target.Y)
									s.cacheAction(ctx, b, currentURL, desc, target.Selector, target.X, target.Y)
									return mcpgo.NewToolResultText(fmt.Sprintf(
										"clicked via Jev Action Predictor at (%.0f,%.0f) — %s", target.X, target.Y, target.Label)), nil
								}
							}
						}
					}
				}
			}

			// Layer 1.5: CSS selectors
			if selRaw != "" {
				for _, sel := range splitComma(selRaw) {
					if sel == "" {
						continue
					}
					var x, y float64
					if ok, _ := getCoordBySelector(ctx, b, sel, &x, &y); ok {
						if err := input.Click(ctx, x, y); err == nil {
							s.maybeSaveFingerprint(ctx, b, req, x, y)
							s.cacheAction(ctx, b, currentURL, desc, sel, x, y)
							return mcpgo.NewToolResultText("clicked via CSS: " + sel), nil
						}
					}
				}
			}

			// Layer 2: Semantic DOM
			domResult, err := b.FindElement(ctx, desc)
			if err == nil && domResult.Found {
				if err := input.Click(ctx, domResult.X, domResult.Y); err != nil {
					return mcpgo.NewToolResultError(err.Error()), nil
				}
				s.maybeSaveFingerprint(ctx, b, req, domResult.X, domResult.Y)
				s.cacheAction(ctx, b, currentURL, desc, "", domResult.X, domResult.Y)
				return mcpgo.NewToolResultText(fmt.Sprintf(
					"clicked via DOM semantic at (%.0f,%.0f) — %s", domResult.X, domResult.Y, domResult.Text)), nil
			}

			// Phase 2.5: Adaptive Healing (Scrapling style)
			healingID := req.GetString("healing_id", "")
			if healingID != "" {
				currentURL, _, _ := b.PageInfo(ctx)
				if prof := s.registry.Match(currentURL); prof != nil {
					if f, ok := prof.Fingerprints[healingID]; ok {
						healedResult, score, err := b.FindHealedElement(ctx, f)
						if err == nil {
							if err := input.Click(ctx, healedResult.X, healedResult.Y); err == nil {
								s.maybeSaveFingerprint(ctx, b, req, healedResult.X, healedResult.Y)
								s.cacheAction(ctx, b, currentURL, desc, "", healedResult.X, healedResult.Y)
								return mcpgo.NewToolResultText(fmt.Sprintf(
									"clicked via Adaptive Healing (score: %.2f) at (%.0f,%.0f)", score, healedResult.X, healedResult.Y)), nil
							}
						}
					}
				}
			}

			// Layer 3: Vision (if key available)
			png, _ := b.Screenshot(ctx, browser.ScreenshotOptions{
				Format:  "webp",
				Quality: 80,
			})
			if png != nil && s.visionFinder != nil {
				coord, err := s.visionFinder.Locate(ctx, png, desc)
				if err == nil {
					if err := input.Click(ctx, coord.X, coord.Y); err != nil {
						return mcpgo.NewToolResultError(fmt.Sprintf("VLM click failed: %v", err)), nil
					}
					s.maybeSaveFingerprint(ctx, b, req, coord.X, coord.Y)
					s.cacheAction(ctx, b, currentURL, desc, "", coord.X, coord.Y)
					return mcpgo.NewToolResultText(fmt.Sprintf("clicked via VLM at (%.0f,%.0f)", coord.X, coord.Y)), nil
				}
			}

			// Final Fallback: Return screenshot for LLM-side manual coordination
			if png == nil {
				return mcpgo.NewToolResultError("search failed and screenshot failed"), nil
			}

			res := mcpgo.NewToolResultText("Element not found automatically. Please analyze the attached screenshot and provide coordinates via pixel_click.")
			res.Content = append(res.Content, mcpgo.ImageContent{
				Type:     "image",
				Data:     base64.StdEncoding.EncodeToString(png),
				MIMEType: "image/webp",
			})
			return res, nil
		})
		})
	}))

	// ── save_site_profile ───────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("save_site_profile",
		mcpgo.WithDescription("Persist a site profile to the profiles/ directory."),
		mcpgo.WithString("name", mcpgo.Required()),
		mcpgo.WithString("yaml_content", mcpgo.Required()),
	), s.auditWrapper("save_site_profile", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		name, err := req.RequireString("name")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		content, err := req.RequireString("yaml_content")
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if err := s.registry.SaveProfile(name, content); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(fmt.Sprintf("Profile %s saved successfully", name)), nil
	}))

	// ── discover_site_selectors ──────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("discover_site_selectors",
		mcpgo.WithDescription("Heuristically discover input and button selectors for the current page."),
	), s.auditWrapper("discover_site_selectors", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}
		res, err := b.DiscoverSiteSelectors(ctx)
		if err != nil {
			return mcpgo.NewToolResultText(err.Error()), nil
		}
		data, _ := json.MarshalIndent(res, "", "  ")
		return mcpgo.NewToolResultText(string(data)), nil
	}))

	// ── upload_files ─────────────────────────────────────────────────────────
	srv.AddTool(mcpgo.NewTool("upload_files",
		mcpgo.WithDescription(`Provide files to an open file chooser dialog.
This tool should be called AFTER an action (like smart_click) has triggered a file picker.
On Chrome 131+ where Page.handleFileChooser is removed, falls back to DOM.setFileInputFiles.`),
		mcpgo.WithString("files", mcpgo.Required(), mcpgo.Description("Semicolon-separated list of local file paths to upload.")),
		mcpgo.WithString("selector", mcpgo.Description("Optional CSS selector for the file input (default: input[type=file]). Useful when multiple file inputs exist.")),
	), s.auditWrapper("upload_files", func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		b, _, err := s.ensureConnected(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if b == nil {
			return mcpgo.NewToolResultError("no browser connected"), nil
		}

		filesRaw := req.GetString("files", "")
		if filesRaw == "" {
			return mcpgo.NewToolResultError("no files provided"), nil
		}

		files := splitSemicolon(filesRaw)
		if len(files) == 0 {
			return mcpgo.NewToolResultError("no files provided"), nil
		}

		selector := req.GetString("selector", "")
		if err := b.UploadFiles(ctx, files, selector); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(fmt.Sprintf("Successfully provided %d file(s) to the file chooser.", len(files))), nil
	}))
}

func splitSemicolon(s string) []string {
	var res []string
	for _, p := range strings.Split(s, ";") {
		p = strings.TrimSpace(p)
		if p != "" {
			res = append(res, p)
		}
	}
	return res
}
