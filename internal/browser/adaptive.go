package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/Daybeam/ghost-driver/internal/profile"
)

// GenerateFingerprintJS is the JavaScript snippet to extract an element's fingerprint.
const GenerateFingerprintJS = `
(function(el) {
  if (!el) return null;
  function getDepth(node) {
    var depth = 0;
    while (node && node.parentNode) {
      depth++;
      node = node.parentNode;
    }
    return depth;
  }
  var attrs = {};
  if (el.attributes) {
    for (var i = 0; i < el.attributes.length; i++) {
      var a = el.attributes[i];
      if (a.name !== 'class' && a.name !== 'id') {
        attrs[a.name] = a.value;
      }
    }
  }
  return {
    tagName: el.tagName ? el.tagName.toLowerCase() : "",
    id: el.id || "",
    classes: el.classList ? Array.from(el.classList).sort() : [],
    attributes: attrs,
    text: (el.innerText || el.textContent || "").trim().substring(0, 100),
    depth: getDepth(el)
  };
})`

// GetFingerprint returns the fingerprint for the element matched by selector.
func (b *Browser) GetFingerprint(ctx context.Context, selector string) (*profile.Fingerprint, error) {
	js := fmt.Sprintf("(%s)(document.querySelector(%q))", GenerateFingerprintJS, selector)
	raw, err := b.EvalJS(ctx, js)
	if err != nil {
		return nil, err
	}
	if string(raw) == "null" {
		return nil, fmt.Errorf("element not found: %s", selector)
	}
	var f profile.Fingerprint
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// GetFingerprintAtPoint returns the fingerprint for the element at coordinates (x, y).
func (b *Browser) GetFingerprintAtPoint(ctx context.Context, x, y float64) (*profile.Fingerprint, error) {
	js := fmt.Sprintf("(%s)(document.elementFromPoint(%f, %f))", GenerateFingerprintJS, x, y)
	raw, err := b.EvalJS(ctx, js)
	if err != nil {
		return nil, err
	}
	if string(raw) == "null" {
		return nil, fmt.Errorf("no element at point (%f, %f)", x, y)
	}
	var f profile.Fingerprint
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// FindHealedElement searches the entire page for the element most similar to the given fingerprint.
func (b *Browser) FindHealedElement(ctx context.Context, f profile.Fingerprint) (QueryResult, float64, error) {
	js := fmt.Sprintf(`
(function() {
  var candidates = document.querySelectorAll('button, a, input, [role="button"], [role="link"], summary');
  if (candidates.length < 5) {
    candidates = document.querySelectorAll('div, span, li, p');
  }
  var results = [];
  var fn = %s;
  for (var i = 0; i < Math.min(candidates.length, 500); i++) {
    var el = candidates[i];
    var r = el.getBoundingClientRect();
    results.push({
      fp: fn(el),
      x: r.left + r.width/2,
      y: r.top + r.height/2,
      width: r.width,
      height: r.height,
      text: (el.innerText || el.textContent || "").trim().substring(0, 120),
      selector: el.tagName.toLowerCase()
    });
  }
  return results;
})()`, GenerateFingerprintJS)

	raw, err := b.EvalJS(ctx, js)
	if err != nil {
		return QueryResult{}, 0, err
	}

	var candidates []struct {
		FP       profile.Fingerprint `json:"fp"`
		X        float64             `json:"x"`
		Y        float64             `json:"y"`
		Width    float64             `json:"width"`
		Height   float64             `json:"height"`
		Text     string              `json:"text"`
		Selector string              `json:"selector"`
	}
	if err := json.Unmarshal(raw, &candidates); err != nil {
		return QueryResult{}, 0, err
	}

	var bestScore float64
	var bestIndex int = -1

	for i, c := range candidates {
		score := CalculateSimilarity(f, c.FP)
		if score > bestScore {
			bestScore = score
			bestIndex = i
		}
	}

	if bestIndex == -1 || bestScore < 0.4 {
		return QueryResult{}, bestScore, fmt.Errorf("no suitable candidate found (best score: %.2f)", bestScore)
	}

	c := candidates[bestIndex]
	return QueryResult{
		Found:    true,
		X:        c.X,
		Y:        c.Y,
		Width:    c.Width,
		Height:   c.Height,
		Text:     c.Text,
		Selector: c.Selector,
	}, bestScore, nil
}

// CalculateSimilarity computes a score between 0.0 and 1.0 comparing two fingerprints.
func CalculateSimilarity(f1, f2 profile.Fingerprint) float64 {
	var score float64
	var totalWeight float64

	// 1. Tag Name (Weight: 2)
	totalWeight += 2.0
	if f1.TagName == f2.TagName {
		score += 2.0
	}

	// 2. ID (Weight: 3)
	if f1.ID != "" && f2.ID != "" {
		totalWeight += 3.0
		if f1.ID == f2.ID {
			score += 3.0
		}
	}

	// 3. Classes (Weight: 2)
	if len(f1.Classes) > 0 || len(f2.Classes) > 0 {
		totalWeight += 2.0
		intersection := 0
		matched := make(map[string]bool)
		for _, c := range f1.Classes {
			matched[c] = true
		}
		for _, c := range f2.Classes {
			if matched[c] {
				intersection++
			}
		}
		union := len(f1.Classes) + len(f2.Classes) - intersection
		if union > 0 {
			score += 2.0 * (float64(intersection) / float64(union))
		}
	}

	// 4. Text (Weight: 3) - Text is very important
	if f1.Text != "" || f2.Text != "" {
		totalWeight += 3.0
		if f1.Text == f2.Text && f1.Text != "" {
			score += 3.0
		} else if f1.Text != "" && f2.Text != "" {
			if strings.Contains(f1.Text, f2.Text) || strings.Contains(f2.Text, f1.Text) {
				score += 1.5
			}
		}
	}

	// 5. Attributes (Weight: 1.5)
	if len(f1.Attributes) > 0 || len(f2.Attributes) > 0 {
		totalWeight += 1.5
		matches := 0
		totalAttrs := 0
		for k, v1 := range f1.Attributes {
			totalAttrs++
			if v2, ok := f2.Attributes[k]; ok && v1 == v2 {
				matches++
			}
		}
		if totalAttrs > 0 {
			score += 1.5 * (float64(matches) / float64(totalAttrs))
		}
	}

	// 6. Depth (Weight: 0.5)
	totalWeight += 0.5
	depthDiff := math.Abs(float64(f1.Depth - f2.Depth))
	score += 0.5 * math.Max(0, 1.0-(depthDiff/10.0))

	return score / totalWeight
}
