package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// EnumOptionsConfig controls dropdown enumeration.
type EnumOptionsConfig struct {
	TriggerSelector   string
	ContainerSelector string
	ItemSelector      string
	Virtual           bool
	MaxItems          int
	Match             string
}

// EnumOption is one extracted choice.
type EnumOption struct {
	Value    string `json:"value"`
	Text     string `json:"text"`
	Disabled bool   `json:"disabled"`
	Index    int    `json:"index"`
}

// MatchedOption is an option with a fuzzy match score.
type MatchedOption struct {
	Value string  `json:"value"`
	Text  string  `json:"text"`
	Score float64 `json:"score"`
}

// EnumOptionsResult is the tool output.
type EnumOptionsResult struct {
	Options  []EnumOption    `json:"options"`
	Matched  []MatchedOption `json:"matched,omitempty"`
	RawCount int             `json:"raw_count"`
}

// EnumOptions implements the complex dropdown enumeration logic.
func (b *Browser) EnumOptions(ctx context.Context, cfg EnumOptionsConfig, input *Input) (*EnumOptionsResult, error) {
	// 1. Click Trigger to expand dropdown.
	x, y, found, err := b.getCoordBySelector(ctx, cfg.TriggerSelector)
	if err != nil || !found {
		return nil, fmt.Errorf("trigger not found: %s", cfg.TriggerSelector)
	}

	if err := input.Click(ctx, x, y); err != nil {
		return nil, fmt.Errorf("failed to click trigger: %w", err)
	}

	// 2. Wait for panel to render.
	b.SmartWait(ctx, DefaultWaitConfig())

	// 3. Collect options.
	var options []EnumOption
	if cfg.Virtual {
		vCfg := VirtualListConfig{
			ContainerSelector: cfg.ContainerSelector,
			ItemSelector:      cfg.ItemSelector,
			MaxItems:          cfg.MaxItems,
			ScrollStep:        300,
			SettleDelay:       400 * time.Millisecond,
			StopIfNoNew:       3,
		}
		items, err := b.ExtractVirtualList(ctx, vCfg)
		if err != nil {
			return nil, err
		}
		for i, item := range items {
			val := item.Attrs["data-value"]
			if val == "" {
				val = item.Attrs["value"]
			}
			if val == "" {
				val = item.Text
			}
			options = append(options, EnumOption{
				Value:    val,
				Text:     item.Text,
				Disabled: item.Attrs["disabled"] != "" || strings.Contains(item.Attrs["class"], "disabled"),
				Index:    i,
			})
		}
	} else {
		options, err = b.collectStaticOptions(ctx, cfg.ContainerSelector, cfg.ItemSelector)
		if err != nil {
			return nil, err
		}
	}

	res := &EnumOptionsResult{
		Options:  options,
		RawCount: len(options),
	}

	// 4. Fuzzy matching if requested.
	if cfg.Match != "" {
		res.Matched = b.fuzzyMatchOptions(options, cfg.Match)
	}

	return res, nil
}

func (b *Browser) collectStaticOptions(ctx context.Context, containerSel, itemSel string) ([]EnumOption, error) {
	if itemSel == "" {
		itemSel = "[role='option'], li[data-value], .el-select-dropdown__item, .ant-select-item-option"
	}
	js := fmt.Sprintf(`
(function(){
  var container = document.querySelector(%q) || document;
  var items = Array.from(container.querySelectorAll(%q));
  return items.map(function(el, i){
    return {
      value: el.getAttribute('data-value') || el.getAttribute('value') || el.innerText.trim(),
      text: el.innerText.trim(),
      disabled: el.classList.contains('is-disabled') || el.hasAttribute('disabled') || el.classList.contains('disabled'),
      index: i
    };
  });
})()`, containerSel, itemSel)

	raw, err := b.EvalJS(ctx, js)
	if err != nil {
		return nil, err
	}
	var opts []EnumOption
	if err := json.Unmarshal(raw, &opts); err != nil {
		return nil, err
	}
	return opts, nil
}

func (b *Browser) fuzzyMatchOptions(options []EnumOption, target string) []MatchedOption {
	var matched []MatchedOption
	target = strings.ToLower(strings.TrimSpace(target))
	for _, opt := range options {
		text := strings.ToLower(strings.TrimSpace(opt.Text))
		score := 0.0
		if text == target {
			score = 1.0
		} else if strings.HasPrefix(text, target) {
			score = 0.8
		} else if strings.Contains(text, target) {
			score = 0.5
		}
		if score > 0 {
			matched = append(matched, MatchedOption{
				Value: opt.Value,
				Text:  opt.Text,
				Score: score,
			})
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].Score > matched[j].Score
	})
	if len(matched) > 3 {
		matched = matched[:3]
	}
	return matched
}

func (b *Browser) getCoordBySelector(ctx context.Context, selector string) (float64, float64, bool, error) {
	js := fmt.Sprintf(`
(function(){
  function deepQuerySelector(sel) {
    function find(node) {
      if (node.querySelector) {
        const res = node.querySelector(sel);
        if (res) return res;
      }
      const all = node.querySelectorAll ? node.querySelectorAll('*') : [];
      for (const el of all) {
        if (el.shadowRoot) {
          const res = find(el.shadowRoot);
          if (res) return res;
        }
      }
      return null;
    }
    return find(document);
  }

  var el = deepQuerySelector(%q);
  if (!el) return null;
  var r = el.getBoundingClientRect();
  if (r.width === 0 || r.height === 0) {
      el.scrollIntoView({block:'center', behavior:'instant'});
      r = el.getBoundingClientRect();
  }
  return {x: r.left + r.width/2, y: r.top + r.height/2};
})()`, selector)
	raw, err := b.EvalJS(ctx, js)
	if err != nil || string(raw) == "null" {
		return 0, 0, false, err
	}
	var coord struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}
	if err := json.Unmarshal(raw, &coord); err != nil {
		return 0, 0, false, err
	}
	return coord.X, coord.Y, true, nil
}
