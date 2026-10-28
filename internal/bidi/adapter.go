package bidi

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Translator maps CDP commands/events to WebDriver BiDi equivalents.
type Translator struct{}

func NewTranslator() *Translator { return &Translator{} }

// Command translates a CDP method+params to a BiDi method+params.
// Returns bidiMethod="" for no-op commands (CDP enable calls etc).
func (t *Translator) Command(cdpMethod string, params any, contextID string) (string, any, error) {
	p, _ := json.Marshal(params)
	var m map[string]any
	json.Unmarshal(p, &m)
	if m == nil {
		m = map[string]any{}
	}

	switch cdpMethod {

	// ── No-op enable/disable commands ──
	case "Page.enable", "Page.disable",
		"Runtime.enable", "Runtime.disable",
		"DOM.enable", "DOM.disable",
		"Network.enable", "Network.disable",
		"Accessibility.enable", "Accessibility.disable",
		"Target.setDiscoverTargets",
		"Target.setAutoAttach",
		"Emulation.clearDeviceMetricsOverride":
		return "", nil, nil

	// ── Navigation ──
	case "Page.navigate":
		return "browsingContext.navigate", map[string]any{
			"context": contextID,
			"url":     m["url"],
		}, nil
	case "Page.goBack":
		return "browsingContext.traverseHistory", map[string]any{
			"context":   contextID,
			"direction": "back",
		}, nil
	case "Page.goForward":
		return "browsingContext.traverseHistory", map[string]any{
			"context":   contextID,
			"direction": "forward",
		}, nil
	case "Page.reload":
		return "browsingContext.reload", map[string]any{
			"context": contextID,
		}, nil

	// ── Screenshot ──
	case "Page.captureScreenshot":
		format, _ := m["format"].(string)
		if format == "" {
			format = "png"
		}
		format = strings.TrimPrefix(format, "image/")
		params := map[string]any{
			"context": contextID,
			"format":  map[string]any{"type": format},
		}
		if beyondViewport, _ := m["captureBeyondViewport"].(bool); beyondViewport {
			params["origin"] = "document"
		}
		if clip, ok := m["clip"].(map[string]any); ok {
			biDiClip := map[string]any{"type": "box"}
			for _, k := range []string{"x", "y", "width", "height"} {
				if v, ok := clip[k]; ok {
					biDiClip[k] = v
				}
			}
			params["clip"] = biDiClip
		}
		return "browsingContext.captureScreenshot", params, nil

	// ── Script evaluation (Runtime.evaluate → script.evaluate) ──
	case "Runtime.evaluate":
		expr, _ := m["expression"].(string)
		await, _ := m["awaitPromise"].(bool)
		return "script.evaluate", map[string]any{
			"expression":  expr,
			"target":      map[string]any{"context": contextID},
			"awaitPromise": await,
		}, nil

	// ── Preload script (stealth injection) ──
	case "Page.addScriptToEvaluateOnNewDocument":
		source, _ := m["source"].(string)
		return "script.addPreloadScript", map[string]any{
			"functionSource": source,
		}, nil

	// ── Dialog handling ──
	case "Page.handleJavaScriptDialog":
		accept, _ := m["accept"].(bool)
		return "browsingContext.handleUserPrompt", map[string]any{
			"context": contextID,
			"accept":  accept,
		}, nil

	// ── Input events ──
	case "Input.dispatchMouseEvent":
		method, params := t.translateMouseEvent(m, contextID)
		return method, params, nil
	case "Input.dispatchKeyEvent":
		method, params := t.translateKeyEvent(m, contextID)
		return method, params, nil
	case "Input.insertText":
		text, _ := m["text"].(string)
		actions := []any{}
		for _, ch := range text {
			actions = append(actions, map[string]any{"type": "keyDown", "value": string(ch)})
			actions = append(actions, map[string]any{"type": "keyUp", "value": string(ch)})
		}
		return "input.performActions", map[string]any{
			"context": contextID,
			"actions": []any{map[string]any{"type": "key", "id": "keyboard", "actions": actions}},
		}, nil

	// ── Cookies ──
	case "Network.getCookies":
		return "storage.getCookies", map[string]any{}, nil
	case "Network.setCookie":
		return "storage.setCookie", m, nil

	// ── Target management ──
	case "Target.attachToTarget":
		return "", nil, nil
	case "Target.createTarget":
		url, _ := m["url"].(string)
		return "browsingContext.create", map[string]any{"type": "tab", "url": url}, nil
	case "Target.closeTarget":
		tid, _ := m["targetId"].(string)
		return "browsingContext.close", map[string]any{"context": tid}, nil

	// ── Emulation ──
	case "Emulation.setDeviceMetricsOverride":
		w, _ := m["width"].(float64)
		h, _ := m["height"].(float64)
		return "browsingContext.setViewport", map[string]any{
			"context": contextID,
			"viewport": map[string]any{"width": w, "height": h},
		}, nil

	// ── Unsupported ──
	case "Page.setInterceptFileChooserDialog":
		return "", nil, nil
	case "Page.handleFileChooser":
		return "", nil, fmt.Errorf("file upload not supported via Firefox BiDi (no file-chooser protocol); use Chrome, or set files on <input type=file> via eval_js")
	case "Network.getResponseBody":
		return "", nil, fmt.Errorf("network response body capture not supported via Firefox BiDi; use browser_fetch or eval_js with fetch() instead")
	case "Accessibility.getFullAXTree":
		return "", nil, fmt.Errorf("accessibility tree not supported via BiDi natively; use get_accessibility_tree which provides a JS polyfill")

	default:
		return "", nil, fmt.Errorf("unsupported CDP method: %s", cdpMethod)
	}
}

// Response transforms a BiDi response to CDP-compatible JSON.
func (t *Translator) Response(cdpMethod string, bidiResult json.RawMessage) json.RawMessage {
	if len(bidiResult) == 0 {
		return json.RawMessage("null")
	}

	switch cdpMethod {
	case "Runtime.evaluate":
		return unwrapScriptResult(bidiResult)
	case "Page.captureScreenshot":
		return unwrapScreenshot(bidiResult)
	case "Target.createTarget":
		return unwrapCreateTarget(bidiResult)
	default:
		return bidiResult
	}
}

// Event translates a BiDi event to a CDP event.
// Returns cdpEvent="" for unmapped events.
func (t *Translator) Event(bidiMethod string, bidiParams json.RawMessage) (string, json.RawMessage) {
	switch bidiMethod {
	case "browsingContext.navigationStarted":
		return "Page.frameNavigated", bidiParams
	case "browsingContext.domContentLoaded":
		return "Page.loadEventFired", bidiParams
	case "browsingContext.load":
		return "Page.loadEventFired", bidiParams
	case "browsingContext.userPromptOpened":
		return "Page.javascriptDialogOpening", bidiParams
	case "network.beforeRequestSent":
		return "Network.requestWillBeSent", bidiParams
	case "network.responseCompleted":
		return "Network.responseReceived", bidiParams
	default:
		return "", bidiParams
	}
}

// ── Helpers ──

func unwrapScriptResult(raw json.RawMessage) json.RawMessage {
	// BiDi script.evaluate result: {"type":"string","value":"foo"} or {"type":"number","value":42} etc.
	// CDP Runtime.evaluate result: {"result":{"type":"string","value":"foo"}}
	// We wrap the BiDi RemoteValue in a CDP-compatible structure for ALL types,
	// including null/undefined which have no "value" field.
	var rv struct {
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
	}
	if json.Unmarshal(raw, &rv) != nil {
		return raw
	}
	result := map[string]any{"type": rv.Type}
	if len(rv.Value) > 0 {
		result["value"] = json.RawMessage(rv.Value)
	}
	out, _ := json.Marshal(map[string]any{"result": result})
	return out
}

func unwrapScreenshot(raw json.RawMessage) json.RawMessage {
	var sr struct {
		Data string `json:"data"`
	}
	if json.Unmarshal(raw, &sr) != nil {
		return raw
	}
	out, _ := json.Marshal(map[string]any{"data": sr.Data})
	return out
}

func unwrapCreateTarget(raw json.RawMessage) json.RawMessage {
	var sr struct {
		Context string `json:"context"`
	}
	if json.Unmarshal(raw, &sr) != nil {
		return raw
	}
	out, _ := json.Marshal(map[string]any{"targetId": sr.Context})
	return out
}

func (t *Translator) translateMouseEvent(m map[string]any, contextID string) (string, any) {
	evType, _ := m["type"].(string)
	x, _ := m["x"].(float64)
	y, _ := m["y"].(float64)

	var action map[string]any
	switch evType {
	case "mouseMoved":
		action = map[string]any{"type": "pointerMove", "x": int(x), "y": int(y)}
	case "mousePressed":
		action = map[string]any{"type": "pointerDown", "button": 0}
	case "mouseReleased":
		action = map[string]any{"type": "pointerUp", "button": 0}
	case "mouseWheel":
		dx, _ := m["deltaX"].(float64)
		dy, _ := m["deltaY"].(float64)
		action = map[string]any{"type": "scroll", "x": int(x), "y": int(y), "deltaX": int(dx), "deltaY": int(dy)}
	default:
		action = map[string]any{"type": "pointerMove", "x": int(x), "y": int(y)}
	}

	return "input.performActions", map[string]any{
		"context": contextID,
		"actions": []any{map[string]any{
			"type":    "pointer",
			"id":      "mouse",
			"actions": []any{action},
		}},
	}
}

func (t *Translator) translateKeyEvent(m map[string]any, contextID string) (string, any) {
	evType, _ := m["type"].(string)
	key, _ := m["key"].(string)
	text, _ := m["text"].(string)

	var action map[string]any
	switch evType {
	case "keyDown", "rawKeyDown":
		v := key
		if v == "" {
			v = text
		}
		action = map[string]any{"type": "keyDown", "value": v}
	case "keyUp":
		action = map[string]any{"type": "keyUp", "value": key}
	default:
		action = map[string]any{"type": "keyDown", "value": key}
	}

	return "input.performActions", map[string]any{
		"context": contextID,
		"actions": []any{map[string]any{
			"type":    "key",
			"id":      "keyboard",
			"actions": []any{action},
		}},
	}
}
