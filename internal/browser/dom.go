package browser

import (
	"context"
	"encoding/json"
	"fmt"
)

// OuterHTML returns the raw HTML of the element matched by selector.
func (b *Browser) OuterHTML(ctx context.Context, selector string) (string, error) {
	if selector == "" {
		selector = "html"
	}
	expr := fmt.Sprintf(`document.querySelector(%q)?.outerHTML ?? ""`, selector)
	raw, err := b.EvalJS(ctx, expr)
	if err != nil {
		return "", err
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", err
	}
	return s, nil
}

// InnerText returns the rendered visible text of a selector.
func (b *Browser) InnerText(ctx context.Context, selector string) (string, error) {
	if selector == "" {
		selector = "body"
	}
	expr := fmt.Sprintf(`document.querySelector(%q)?.innerText ?? ""`, selector)
	raw, err := b.EvalJS(ctx, expr)
	if err != nil {
		return "", err
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", err
	}
	return s, nil
}

// HTMLToMarkdown converts the page or a sub-selector to Markdown using an
// in-browser DOM walker — zero multimodal calls, zero CDN fetches.
// Supported modes: "full" (default), "article" (content only), "controls" (interactive elements list).
func (b *Browser) HTMLToMarkdown(ctx context.Context, selector string, mode string) (string, error) {
	if mode == "" {
		mode = "full"
	}

	const controlsTemplate = `
(function extractControls(rootEl) {
  if (!rootEl) return "";
  var out = [];
  var sel = 'input, textarea, select, button, a, [role="button"], [role="link"], summary';
  var els = Array.from(rootEl.querySelectorAll(sel));
  els.forEach(function(el) {
    if (el.offsetParent === null && el.tagName !== 'HTML' && el.tagName !== 'BODY') {
      var style = window.getComputedStyle(el);
      if (style.display === 'none' || style.visibility === 'hidden') return;
    }
    var tag = el.tagName.toLowerCase();
    var role = el.getAttribute('role') || '';
    var text = (el.innerText || el.textContent || '').trim().replace(/\s+/g, ' ');
    var status = '';
    if (el.disabled) status += ' [DISABLED]';
    if (el.readOnly) status += ' [READONLY]';

    if (tag === 'input' || tag === 'textarea') {
      var type = el.getAttribute('type') || 'text';
      var id = el.id ? ' id="' + el.id + '"' : '';
      var name = el.name ? ' name="' + el.name + '"' : '';
      var ph = el.getAttribute('placeholder') ? ' placeholder="' + el.getAttribute('placeholder') + '"' : '';
      var val = el.value ? ' value="' + el.value + '"' : '';
      var label = '';
      if (el.id) {
        var l = document.querySelector('label[for="' + el.id + '"]');
        if (l) label = ' label="' + l.innerText.trim() + '"';
      }
      out.push('[INPUT] type="' + type + '"' + id + name + ph + label + val + status);
    } else if (tag === 'select') {
      var id = el.id ? ' id="' + el.id + '"' : '';
      var opts = Array.from(el.options).slice(0, 5).map(function(o){ return o.value || o.text.trim(); }).join('/');
      out.push('[SELECT]' + id + ' options: ' + opts + status);
    } else if (tag === 'button' || role === 'button') {
      var id = el.id ? ' id="' + el.id + '"' : '';
      out.push('[BUTTON] "' + text + '"' + id + status);
    } else if (tag === 'a' || role === 'link') {
      var href = el.getAttribute('href') || '';
      var id = el.id ? ' id="' + el.id + '"' : '';
      out.push('[A] "' + text + '" href="' + href + '"' + id + status);
    }
  });
  return out.join('\n');
})(%s)`

	const walkerTemplate = `
(function toMd(rootEl) {
  if (!rootEl) return "";
  var out = [];
  var mode = %q;
  var skipTags = {script:1, style:1, noscript:1};
  if (mode === 'article') {
    skipTags.nav = 1; skipTags.aside = 1; skipTags.footer = 1;
    skipTags.header = 1; skipTags.form = 1; skipTags.iframe = 1;
  }
  var block = {h1:1,h2:1,h3:1,h4:1,h5:1,h6:1,p:1,div:1,section:1,
               article:1,li:1,tr:1,blockquote:1,pre:1};
  function walk(n, depth) {
    if (n.nodeType === 3) {
      var t = n.textContent.replace(/\s+/g, ' ');
      if (t.trim()) out.push(t);
      return;
    }
    if (n.nodeType !== 1) return;
    var tag = n.tagName.toLowerCase();
    if (skipTags[tag]) return;
    if (tag === 'br')  { out.push('\n'); return; }
    if (n.offsetParent === null && tag !== 'html' && tag !== 'body') {
      var style = window.getComputedStyle(n);
      if (style.display === 'none' || style.visibility === 'hidden') return;
    }
    if (/^h[1-6]$/.test(tag)) {
      out.push('\n' + '#'.repeat(parseInt(tag[1])) + ' ' + n.innerText.trim() + '\n');
      return;
    }
    if (tag === 'a') {
      out.push('[' + n.innerText.trim() + '](' + (n.getAttribute('href')||'') + ')');
      return;
    }
    if (tag === 'img') {
      out.push('![' + (n.getAttribute('alt')||'') + '](' + (n.getAttribute('src')||'') + ')');
      return;
    }
    if (tag === 'strong' || tag === 'b') { out.push('**' + n.innerText.trim() + '**'); return; }
    if (tag === 'em'     || tag === 'i') { out.push('_'  + n.innerText.trim() + '_');  return; }
    if (tag === 'code') { out.push('\u0060' + n.innerText + '\u0060'); return; }
    if (tag === 'input' || tag === 'textarea' || tag === 'select') {
      var type = n.getAttribute('type') || 'text';
      var placeholder = n.getAttribute('placeholder') || '';
      var value = n.value || '';
      var label = '';
      if (n.id) {
        var l = document.querySelector('label[for="' + n.id + '"]');
        if (l) label = ' label="' + l.innerText.trim() + '"';
      }
      var status = '';
      if (n.disabled) status = '[DISABLED] ';
      if (n.readOnly) status = '[READONLY] ';
      out.push('\n' + status + '<' + tag + ' type="' + type + '"' + label + ' placeholder="' + placeholder + '">' + value + '\n');
      return;
    }
    if (tag === 'pre')  {
      out.push('\n' + '\u0060\u0060\u0060' + '\n' + n.innerText + '\n' + '\u0060\u0060\u0060' + '\n');
      return;
    }
    if (tag === 'li') {
      var prefix = n.closest('ol')
        ? (Array.from(n.parentNode.children).indexOf(n)+1) + '. '
        : '- ';
      out.push('\n' + '  '.repeat(depth) + prefix);
    }
    if (tag === 'tr') {
      var cells = Array.from(n.querySelectorAll('td,th')).map(function(c){ return c.innerText.trim(); });
      out.push('\n| ' + cells.join(' | ') + ' |');
      return;
    }
    if (block[tag]) out.push('\n');
    Array.from(n.childNodes).forEach(function(c){ walk(c, depth+1); });
    if (block[tag]) out.push('\n');
  }
  walk(rootEl, 0);
  return out.join('').replace(/\n{3,}/g, '\n\n').trim();
})(%s)`

	var rootExpr string
	if selector == "" {
		// Readability-style: prefer <main>/<article>, fall back to body.
		rootExpr = `(document.querySelector('main,article,[role="main"],#content,.content') || document.body)`
	} else {
		rootExpr = fmt.Sprintf("document.querySelector(%q)", selector)
	}

	var js string
	if mode == "controls" {
		js = fmt.Sprintf(controlsTemplate, rootExpr)
	} else {
		js = fmt.Sprintf(walkerTemplate, mode, rootExpr)
	}

	raw, err := b.EvalJS(ctx, js)
	if err != nil {
		return b.InnerText(ctx, selector) // graceful fallback
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", err
	}

	b.mu.Lock()
	hasSys := b.HasSystemDialog
	b.mu.Unlock()

	if hasSys {
		s = "> [SYSTEM NOTIFICATION] A system file picker or dialog is currently open and blocking interaction.\n\n" + s
	}

	return s, nil
}

// QueryResult holds a matched element's position and metadata.
type QueryResult struct {
	Found    bool    `json:"found"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Width    float64 `json:"width"`
	Height   float64 `json:"height"`
	Text     string  `json:"text"`
	Selector string  `json:"selector"`
	Disabled bool    `json:"disabled"`
}

// FindElement locates an element by description without any VLM call.
func (b *Browser) FindElement(ctx context.Context, description string) (QueryResult, error) {
	js := fmt.Sprintf(`
(function find(desc) {
  function bbox(el) {
    var r = el.getBoundingClientRect();
    return {found:true, x:r.left+r.width/2, y:r.top+r.height/2,
            width:r.width, height:r.height,
            text:(el.innerText||el.textContent||'').trim().substring(0,120),
            selector:el.tagName.toLowerCase(),
            disabled: !!el.disabled};
  }
  function qs(root, sel) {
    var el = root.querySelector(sel);
    if (el) return el;
    var hosts = root.querySelectorAll('*');
    for (var i=0;i<hosts.length;i++) {
      if (hosts[i].shadowRoot) {
        el = hosts[i].shadowRoot.querySelector(sel);
        if (el) return el;
      }
    }
    return null;
  }

  var esc = desc.replace(/\\/g,'\\\\').replace(/"/g,'\\"');
  var descLower = desc.toLowerCase().trim();

  // 1. data-* attributes (highly stable, including SSO/Auth/Google Accounts attributes)
  var testAttrs = ['data-testid','data-qa','data-cy','data-test','data-automation','data-name','data-email','data-identifier','data-value','data-account-id','data-profile','data-user'];
  for (var a of testAttrs) {
    var el = qs(document, '['+a+'="'+esc+'"]') || qs(document, '['+a+'*="'+esc+'"]');
    if (el) return bbox(el);
  }

  // 2. aria-label and labels
  var el = qs(document, '[aria-label="'+esc+'"]') || qs(document, '[aria-label*="'+esc+'"]');
  if (el) return bbox(el);

  // 3. Exact text match in interactive elements
  var interactives = document.querySelectorAll('button,a,[role="button"],[role="link"],input[type="submit"],summary');
  for (var it of interactives) {
    if ((it.innerText||it.textContent||'').trim() === desc) return bbox(it);
  }

  // 4. Fuzzy text match in interactive elements
  for (var it of interactives) {
    if ((it.innerText||it.textContent||'').toLowerCase().includes(descLower)) return bbox(it);
  }

  // 5. Placeholders and titles
  el = qs(document, 'input[placeholder*="'+esc+'"],textarea[placeholder*="'+esc+'"],[title*="'+esc+'"]');
  if (el) return bbox(el);

  // 6. Deep text search (if not found in common interactive elements)
  var walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT, null, false);
  var node;
  while(node = walker.nextNode()) {
    if (node.textContent.toLowerCase().includes(descLower)) {
       var p = node.parentElement;
       if (p && p.getClientRects().length > 0) return bbox(p);
    }
  }

  return {found:false};
})(%q)
`, description)

	raw, err := b.EvalJS(ctx, js)
	if err != nil {
		return QueryResult{}, err
	}
	var result QueryResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return QueryResult{}, err
	}
	return result, nil
}

// GetAllText returns clean page text with script/style/nav noise stripped.
func (b *Browser) GetAllText(ctx context.Context) (string, error) {
	raw, err := b.EvalJS(ctx, `
(function(){
  var clone = document.body.cloneNode(true);
  ['script','style','nav','footer','header','noscript','svg'].forEach(function(t){
    clone.querySelectorAll(t).forEach(function(e){ e.remove(); });
  });
  return clone.innerText.replace(/\s{3,}/g,'\n\n').trim();
})()`)
	if err != nil {
		return "", err
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", err
	}
	return s, nil
}

// GetMetadata returns page meta tags, og: fields, title, URL without screenshot.
func (b *Browser) GetMetadata(ctx context.Context) (map[string]string, error) {
	raw, err := b.EvalJS(ctx, `
(function(){
  var m = {title: document.title, url: window.location.href};
  document.querySelectorAll('meta').forEach(function(el){
    var k = el.getAttribute('name') || el.getAttribute('property') || '';
    var v = el.getAttribute('content') || '';
    if (k && v) m[k] = v;
  });
  var c = document.querySelector('link[rel="canonical"]');
  if (c) m.canonical = c.href;
  return m;
})()`)
	if err != nil {
		return nil, err
	}
	var result map[string]string
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// DesignTokens holds the extracted design system data.
type DesignTokens struct {
	Colors     map[string]string `json:"colors"`
	Typography map[string]string `json:"typography"`
	Spacing    map[string]string `json:"spacing"`
	Variables  map[string]string `json:"variables"`
}

// GetDesignTokens extracts design tokens (colors, typography, spacing) for a selector.
func (b *Browser) GetDesignTokens(ctx context.Context, selector string) (DesignTokens, error) {
	if selector == "" {
		selector = "body"
	}
	js := fmt.Sprintf(`
(function(sel) {
  var el = document.querySelector(sel);
  if (!el) return null;
  var styles = window.getComputedStyle(el);
  var res = {
    colors: {
      text: styles.color,
      background: styles.backgroundColor,
      border: styles.borderColor
    },
    typography: {
      family: styles.fontFamily,
      size: styles.fontSize,
      weight: styles.fontWeight,
      lineHeight: styles.lineHeight
    },
    spacing: {
      margin: styles.margin,
      padding: styles.padding,
      gap: styles.gap
    },
    variables: {}
  };

  function collectVars(target) {
    var s = window.getComputedStyle(target);
    for (var i = 0; i < s.length; i++) {
      var prop = s[i];
      if (prop.startsWith('--')) {
        res.variables[prop] = s.getPropertyValue(prop).trim();
      }
    }
  }

  collectVars(document.documentElement);
  collectVars(el);
  return res;
})(%q)`, selector)

	raw, err := b.EvalJS(ctx, js)
	if err != nil {
		return DesignTokens{}, err
	}
	if string(raw) == "null" {
		return DesignTokens{}, fmt.Errorf("selector not found: %s", selector)
	}
	var tokens DesignTokens
	if err := json.Unmarshal(raw, &tokens); err != nil {
		return DesignTokens{}, err
	}
	return tokens, nil
}

// DiffDesignTokens returns the keys and values that changed between a and b.
func DiffDesignTokens(a, b DesignTokens) DesignTokens {
	diff := DesignTokens{
		Colors:     make(map[string]string),
		Typography: make(map[string]string),
		Spacing:    make(map[string]string),
		Variables:  make(map[string]string),
	}

	diffMap := func(ma, mb, target map[string]string) {
		for k, v := range mb {
			if ma[k] != v {
				target[k] = v
			}
		}
	}

	diffMap(a.Colors, b.Colors, diff.Colors)
	diffMap(a.Typography, b.Typography, diff.Typography)
	diffMap(a.Spacing, b.Spacing, diff.Spacing)
	diffMap(a.Variables, b.Variables, diff.Variables)

	return diff
}

type ContentAnchor struct {
	Title       string       `json:"title"`        // <h1> text
	Headings    []string     `json:"headings"`     // <h2>/<h3> text list
	FirstPara   string       `json:"first_para"`   // First <p> snippet
	DataTables  int          `json:"data_tables"`  // <table> count
	Links       []LinkAnchor `json:"links"`        // First 10 <a> (text+href)
	ExtractedAt int64        `json:"extracted_at"`
}

type LinkAnchor struct {
	Text string `json:"text"`
	Href string `json:"href"`
}

// ExtractContentAnchors extracts deterministic structural anchors from the page.
func (b *Browser) ExtractContentAnchors(ctx context.Context, selector string) (ContentAnchor, error) {
	js := fmt.Sprintf(`
(function extractAnchors(rootSel) {
  var rootEl = rootSel ? document.querySelector(rootSel) : (document.querySelector('main,article,[role="main"],#content,.content') || document.body);
  if (!rootEl) return {title:'', headings:[], first_para:'', data_tables:0, links:[]};

  var h1 = rootEl.querySelector('h1');
  var title = h1 ? h1.innerText.trim() : '';

  var headings = Array.from(rootEl.querySelectorAll('h2,h3'))
    .map(function(h) { return h.innerText.trim(); })
    .filter(Boolean)
    .slice(0, 20);

  var p = rootEl.querySelector('p');
  var firstPara = p ? (p.innerText || '').trim().substring(0, 200) : '';

  var dataTables = rootEl.querySelectorAll('table').length;

  var links = Array.from(rootEl.querySelectorAll('a'))
    .map(function(a) { return {text: a.innerText.trim(), href: a.href || ''}; })
    .filter(function(l) { return l.text.length > 0; })
    .slice(0, 10);

  return {
    title: title,
    headings: headings,
    first_para: firstPara,
    data_tables: dataTables,
    links: links,
    extracted_at: Date.now()
  };
})(%q)`, selector)

	raw, err := b.EvalJS(ctx, js)
	if err != nil {
		return ContentAnchor{}, err
	}
	var anchors ContentAnchor
	if err := json.Unmarshal(raw, &anchors); err != nil {
		return ContentAnchor{}, err
	}
	return anchors, nil
}
