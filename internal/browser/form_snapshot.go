package browser

import (
	"context"
	"encoding/json"
)

// SnapshotForm extracts all form field values from the current page.
// It uses a JS walker to find inputs, selects, and textareas, including
// those in Shadow DOM.
func (b *Browser) SnapshotForm(ctx context.Context) (map[string]string, error) {
	const snapshotJS = `
(function() {
  var results = {};

  function getSelector(el) {
    if (el.id) return '#' + el.id;
    if (el.name) return '[name="' + el.name + '"]';
    // Fallback to a semi-unique path
    var path = el.tagName.toLowerCase();
    if (el.className) path += '.' + Array.from(el.classList).join('.');
    return path;
  }

  function walk(root) {
    var inputs = root.querySelectorAll('input, select, textarea, [role="checkbox"], [role="radio"]');
    inputs.forEach(function(el) {
      var sel = getSelector(el);
      var val = '';
      if (el.type === 'checkbox' || el.type === 'radio') {
        val = el.checked ? 'true' : 'false';
      } else {
        val = el.value || '';
      }
      results[sel] = val;
    });

    var all = root.querySelectorAll('*');
    all.forEach(function(el) {
      if (el.shadowRoot) {
        walk(el.shadowRoot);
      }
    });
  }

  walk(document);
  return results;
})()`

	raw, err := b.EvalJS(ctx, snapshotJS)
	if err != nil {
		return nil, err
	}

	var results map[string]string
	if err := json.Unmarshal(raw, &results); err != nil {
		return nil, err
	}
	return results, nil
}
