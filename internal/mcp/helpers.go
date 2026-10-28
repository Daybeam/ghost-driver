package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Daybeam/ghost-driver/internal/browser"
)

func splitComma(s string) []string {
	var out []string
	cur := ""
	for _, c := range s {
		if c == ',' {
			out = append(out, cur)
			cur = ""
		} else {
			cur += string(c)
		}
	}
	out = append(out, cur)
	return out
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func getCoordBySelector(ctx context.Context, b *browser.Browser, selector string, x, y *float64) (bool, error) {
	// Support Deep Query Selector (Shadow DOM penetration)
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
		return false, err
	}
	var coord struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}
	if err := json.Unmarshal(raw, &coord); err != nil {
		return false, err
	}
	*x, *y = coord.X, coord.Y
	return true, nil
}
