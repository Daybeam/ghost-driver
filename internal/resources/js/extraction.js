(function() {
    // 1. Semantic Walker with Shadow DOM and Layout support
    let md = "";

    function isVisible(el) {
        if (!el.getBoundingClientRect) return true;
        const rect = el.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0;
    }

    const blockTags = new Set(['div', 'section', 'article', 'aside', 'header', 'footer', 'nav', 'main', 'form', 'blockquote', 'details', 'summary', 'tr', 'table', 'thead', 'tbody', 'tfoot']);
    const listTags = new Set(['ul', 'ol', 'dl']);

    const walk = (node, depth = 0) => {
        if (node.nodeType === Node.TEXT_NODE) {
            const text = node.textContent.trim();
            if (text) {
                // Ensure there's a space if the previous character isn't a newline or space
                if (md.length > 0 && !md.endsWith('\n') && !md.endsWith(' ')) {
                    md += " ";
                }
                md += text;
            }
        } else if (node.nodeType === Node.ELEMENT_NODE) {
            if (!isVisible(node)) return;

            const tag = node.tagName.toLowerCase();

            // Skip noise
            const noise = ['script', 'style', 'noscript', 'canvas', 'video', 'audio', 'embed', 'object'];
            if (noise.includes(tag)) return;

            // Handle block start
            const isBlock = blockTags.has(tag) || listTags.has(tag);
            if (isBlock && md.length > 0 && !md.endsWith('\n')) {
                md += "\n";
            }

            switch(tag) {
                case 'h1': md += "\n\n# "; break;
                case 'h2': md += "\n\n## "; break;
                case 'h3': md += "\n\n### "; break;
                case 'h4': md += "\n\n#### "; break;
                case 'p': md += "\n\n"; break;
                case 'br': md += "\n"; break;
                case 'hr': md += "\n---\n"; break;
                case 'a': md += " ["; break;
                case 'li': md += "\n- "; break;
                case 'dt': md += "\n**"; break;
                case 'dd': md += "**: "; break;
                case 'img':
                    const alt = node.alt || node.title || 'image';
                    md += ` ![${alt}](${node.src}) `;
                    break;
                case 'button': md += "\n[Button: "; break;
                case 'input':
                    if (node.type === 'button' || node.type === 'submit') {
                        md += "\n[Button: " + (node.value || 'Submit') + "]\n";
                    } else if (node.type === 'checkbox' || node.type === 'radio') {
                        md += " [" + (node.checked ? 'x' : ' ') + "] ";
                    } else {
                        md += "\n[Input (" + (node.placeholder || node.name || node.type || 'text') + ")]\n";
                    }
                    return; // Input has no children
                case 'textarea':
                    md += "\n[Textarea (" + (node.placeholder || node.name || 'text') + ")]\n";
                    return;
                case 'strong': case 'b': md += "**"; break;
                case 'em': case 'i': md += "*"; break;
                case 'pre': case 'code': md += "\n```\n"; break;
            }

            // Penetrate Shadow DOM
            if (node.shadowRoot) {
                Array.from(node.shadowRoot.childNodes).forEach(child => walk(child, depth + 1));
            }
            Array.from(node.childNodes).forEach(child => walk(child, depth + 1));

            // Handle block/tag end
            switch(tag) {
                case 'h1': case 'h2': case 'h3': case 'h4': md += "\n"; break;
                case 'p': md += "\n"; break;
                case 'a': md += `](${node.href})`; break;
                case 'strong': case 'b': md += "**"; break;
                case 'em': case 'i': md += "*"; break;
                case 'pre': case 'code': md += "\n```\n"; break;
                case 'button': md += "]\n"; break;
                case 'dt': md += "**"; break;
            }

            if (isBlock && !md.endsWith('\n')) {
                md += "\n";
            }
        }
    };

    if (document.body) {
        walk(document.body);
    }

    return md.trim()
        .replace(/[ \t]+/g, " ")             // Normalize horizontal whitespace
        .replace(/\n\s+\n/g, "\n\n")         // Clean up whitespace-only lines
        .replace(/\n{3,}/g, "\n\n");         // Max 2 consecutive newlines
})();
