// Package vision provides VLM-based visual element detection.
package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/png" // register PNG decoder
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"
)

var ErrDelegateToCaller = errors.New("vision locating failed: delegate to caller")

// coordRe parses the [ymin, xmin, ymax, xmax] response from VLM.
var coordRe = regexp.MustCompile(`\[(\d+),\s*(\d+),\s*(\d+),\s*(\d+)\]`)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// Coord is a pixel position within the screenshot.
type Coord struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Provider defaults: model + base URL per provider.
var providerDefaults = map[string]struct{ model, url string }{
	"gemini":    {"gemini-2.0-flash-exp", "https://generativelanguage.googleapis.com/v1beta/models"},
	"openai":    {"gpt-4o", "https://api.openai.com/v1"},
	"anthropic": {"claude-3-5-sonnet-20241022", "https://api.anthropic.com/v1"},
}

// Finder locates UI elements by visual description using a VLM provider.
type Finder struct {
	provider string
	apiKey   string
	model    string
	baseURL  string
}

// NewFinder creates a Finder for the given VLM provider.
// Supported: "gemini" (default), "openai", "anthropic".
// apiKey falls back to provider-specific env var (GEMINI_API_KEY, OPENAI_API_KEY, ANTHROPIC_API_KEY).
// model and baseURL override provider defaults if non-empty.
func NewFinder(provider, apiKey, model, baseURL string) *Finder {
	if provider == "" {
		provider = "gemini"
	}
	if apiKey == "" {
		apiKey = defaultAPIKey(provider)
	}
	d, ok := providerDefaults[provider]
	if !ok {
		d = providerDefaults["gemini"]
	}
	if model == "" {
		model = d.model
	}
	if baseURL == "" {
		baseURL = d.url
	}
	return &Finder{provider: provider, apiKey: apiKey, model: model, baseURL: baseURL}
}

func defaultAPIKey(provider string) string {
	switch provider {
	case "openai":
		return os.Getenv("OPENAI_API_KEY")
	case "anthropic":
		return os.Getenv("ANTHROPIC_API_KEY")
	default:
		return os.Getenv("GEMINI_API_KEY")
	}
}

// Locate sends a PNG screenshot to the VLM and returns the pixel coordinate.
// If apiKey is empty, it returns ErrDelegateToCaller to trigger fallback.
func (f *Finder) Locate(ctx context.Context, png []byte, description string) (Coord, error) {
	if f.apiKey == "" {
		return Coord{}, ErrDelegateToCaller
	}

	imgCfg, _, imgErr := image.DecodeConfig(bytes.NewReader(png))
	if imgErr != nil {
		return Coord{}, fmt.Errorf("decode image dims: %w", imgErr)
	}

	b64 := base64.StdEncoding.EncodeToString(png)
	prompt := fmt.Sprintf(
		`Find the center of the following UI element in the image: "%s".
Return ONLY a JSON list containing the normalized coordinates [ymin, xmin, ymax, xmax] where each value is between 0 and 1000.
Example: [500, 200, 600, 400]
If not found, return [0, 0, 0, 0].`, description)

	var text string
	var err error
	switch f.provider {
	case "gemini":
		text, err = f.locateGemini(ctx, prompt, b64)
	case "openai":
		text, err = f.locateOpenAI(ctx, prompt, b64)
	case "anthropic":
		text, err = f.locateAnthropic(ctx, prompt, b64)
	default:
		return Coord{}, fmt.Errorf("unsupported VLM provider %q", f.provider)
	}
	if err != nil {
		return Coord{}, err
	}

	return parseCoords(text, description, imgCfg.Width, imgCfg.Height)
}

// parseCoords extracts [ymin,xmin,ymax,xmax] from VLM text and scales to pixels.
func parseCoords(text, description string, imgW, imgH int) (Coord, error) {
	matches := coordRe.FindStringSubmatch(text)
	if len(matches) != 5 {
		return Coord{}, fmt.Errorf("invalid coordinate format in response: %s", text)
	}
	ymin, _ := strconv.ParseFloat(matches[1], 64)
	xmin, _ := strconv.ParseFloat(matches[2], 64)
	ymax, _ := strconv.ParseFloat(matches[3], 64)
	xmax, _ := strconv.ParseFloat(matches[4], 64)
	if ymin == 0 && xmin == 0 && ymax == 0 && xmax == 0 {
		return Coord{}, fmt.Errorf("element %q not found in screenshot", description)
	}
	return Coord{
		X: (xmin+xmax)/2.0/1000.0*float64(imgW),
		Y: (ymin+ymax)/2.0/1000.0*float64(imgH),
	}, nil
}

// ── Gemini ────────────────────────────────────────────────────────────────────

func (f *Finder) locateGemini(ctx context.Context, prompt, b64 string) (string, error) {
	url := f.baseURL + "/" + f.model + ":generateContent"
	payload := map[string]any{
		"contents": []map[string]any{{
			"parts": []map[string]any{
				{"text": prompt},
				{"inline_data": map[string]any{"mime_type": "image/png", "data": b64}},
			},
		}},
		"generationConfig": map[string]any{"response_mime_type": "application/json"},
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", f.apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("gemini error (%d): %s", resp.StatusCode, string(b))
	}

	var r struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	if len(r.Candidates) == 0 || len(r.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("no response from Gemini")
	}
	return r.Candidates[0].Content.Parts[0].Text, nil
}

// ── OpenAI GPT-4o ─────────────────────────────────────────────────────────────

func (f *Finder) locateOpenAI(ctx context.Context, prompt, b64 string) (string, error) {
	url := f.baseURL + "/chat/completions"
	payload := map[string]any{
		"model": f.model,
		"messages": []map[string]any{{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": prompt},
				{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64," + b64}},
			},
		}},
		"max_tokens": 256,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("openai error (%d): %s", resp.StatusCode, string(b))
	}

	var r struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	if len(r.Choices) == 0 {
		return "", fmt.Errorf("no response from OpenAI")
	}
	return r.Choices[0].Message.Content, nil
}

// ── Anthropic Claude ──────────────────────────────────────────────────────────

func (f *Finder) locateAnthropic(ctx context.Context, prompt, b64 string) (string, error) {
	url := f.baseURL + "/messages"
	payload := map[string]any{
		"model":      f.model,
		"max_tokens": 256,
		"messages": []map[string]any{{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": prompt},
				{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": b64}},
			},
		}},
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", f.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("anthropic error (%d): %s", resp.StatusCode, string(b))
	}

	var r struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	if len(r.Content) == 0 {
		return "", fmt.Errorf("no response from Anthropic")
	}
	return r.Content[0].Text, nil
}
