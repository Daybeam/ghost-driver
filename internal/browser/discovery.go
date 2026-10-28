package browser

import (
	"context"
	"encoding/json"
)

type DiscoveryResult struct {
	Inputs  []SelectorCandidate `json:"inputs"`
	Buttons []SelectorCandidate `json:"buttons"`
}

type SelectorCandidate struct {
	Selector   string `json:"selector"`
	Type       string `json:"type"`
	AriaLabel  string `json:"aria_label"`
	Text       string `json:"text"`
	Confidence int    `json:"confidence"`
}

func (b *Browser) DiscoverSiteSelectors(ctx context.Context) (*DiscoveryResult, error) {
	js := `
(function(){
	var results = { inputs: [], buttons: [] };

	// Find potential inputs
	var inputEls = document.querySelectorAll('textarea, input[type="text"], [role="textbox"], [contenteditable="true"]');
	inputEls.forEach(el => {
		var r = el.getBoundingClientRect();
		if (r.width < 10 || r.height < 10) return;

		results.inputs.push({
			selector: el.id ? "#" + el.id : (el.name ? 'input[name="' + el.name + '"]' : el.tagName.toLowerCase()),
			aria_label: el.getAttribute("aria-label") || "",
			text: el.placeholder || "",
			confidence: el.tagName === "TEXTAREA" ? 90 : 70
		});
	});

	// Find potential buttons
	var btnEls = document.querySelectorAll('button, [role="button"], input[type="submit"]');
	btnEls.forEach(el => {
		var r = el.getBoundingClientRect();
		if (r.width < 5 || r.height < 5) return;

		var text = el.innerText.trim();
		var label = el.getAttribute("aria-label") || "";
		var conf = 50;
		if (text.match(/send|发送|submit|确定|保存|save/i) || label.match(/send|发送|submit/i)) {
			conf = 95;
		}

		results.buttons.push({
			selector: el.id ? "#" + el.id : (el.getAttribute("data-name") ? '[data-name="' + el.getAttribute("data-name") + '"]' : "button"),
			aria_label: label,
			text: text,
			confidence: conf
		});
	});

	return results;
})()
`
	raw, err := b.EvalJS(ctx, js)
	if err != nil {
		return nil, err
	}

	var res DiscoveryResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	return &res, nil
}
