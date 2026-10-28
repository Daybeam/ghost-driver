package browser

import (
	"context"
	"encoding/json"
)

type PageStateSummary struct {
	Title    string         `json:"title"`
	URL      string         `json:"url"`
	Heading  string         `json:"heading"`
	Sections []string       `json:"sections"`
	Forms    []FormSummary  `json:"forms"`
	Tables   []TableSummary `json:"tables"`
	Buttons  []string       `json:"buttons"`
}

type FormSummary struct {
	Action string   `json:"action"`
	Inputs []string `json:"inputs"`
}

type TableSummary struct {
	Headers  []string `json:"headers"`
	RowCount int      `json:"row_count"`
}

const pageStateSummaryJS = `(() => {
  const q = (s, el) => Array.from((el || document).querySelectorAll(s));
  const t = el => (el && el.innerText || '').trim().slice(0, 200);
  return {
    title: document.title || '',
    url: location.href,
    heading: t(document.querySelector('h1')),
    sections: q('h2, h3').map(t).filter(Boolean).slice(0, 10),
    forms: q('form').map(f => ({
      action: (f.action || '').slice(0, 200),
      inputs: q('input, select, textarea', f).map(el =>
        el.name || el.id || el.placeholder || el.type || ''
      ).filter(Boolean).slice(0, 20)
    })).slice(0, 5),
    tables: q('table').map(t => ({
      headers: q('th', t).map(el => (el.innerText || '').trim()).filter(Boolean).slice(0, 20),
      row_count: t.querySelectorAll('tr').length
    })).slice(0, 5),
    buttons: q('button, [role=button], input[type=submit]').map(t).filter(Boolean).slice(0, 15)
  };
})()`

func (b *Browser) GetPageStateSummary(ctx context.Context) (*PageStateSummary, error) {
	raw, err := b.EvalJS(ctx, pageStateSummaryJS)
	if err != nil {
		return nil, err
	}
	var summary PageStateSummary
	if err := json.Unmarshal(raw, &summary); err != nil {
		return nil, err
	}
	return &summary, nil
}
