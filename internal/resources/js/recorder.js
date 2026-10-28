(function() {
    if (window.__ghostRecorderAttached) return;
    window.__ghostRecorderAttached = true;

    console.log("Ghost-Driver: Action Recorder Attached");

    function getSelectorCandidates(el) {
        const candidates = [];

        // 1. data-testid
        const testAttrs = ['data-testid', 'data-qa', 'data-cy', 'data-test'];
        for (const attr of testAttrs) {
            const val = el.getAttribute(attr);
            if (val) candidates.push({ type: 'testid', value: `[${attr}="${val}"]` });
        }

        // 2. Aria Role & Name
        const role = el.getAttribute('role') || el.tagName.toLowerCase();
        const name = (el.innerText || el.textContent || '').trim().substring(0, 30);
        if (name) candidates.push({ type: 'semantic', value: `${role}[name="${name}"]`, text: name });

        // 3. Stable Attributes
        if (el.id && !el.id.match(/\d{5,}/)) {
            candidates.push({ type: 'id', value: `#${el.id}` });
        }

        // 4. Fallback: Path
        let path = '';
        let curr = el;
        while (curr && curr !== document.body) {
            let selector = curr.tagName.toLowerCase();
            if (curr.className) {
                const cls = curr.className.split(' ').filter(c => !c.match(/\d/))[0];
                if (cls) selector += '.' + cls;
            }
            path = selector + (path ? ' > ' + path : '');
            curr = curr.parentElement;
        }
        candidates.push({ type: 'path', value: path });

        return candidates;
    }

    function handleEvent(ev) {
        // Skip events from our own overlay (if any)
        if (ev.target.closest('#ghost-recorder-overlay')) return;

        const el = ev.target;
        const candidates = getSelectorCandidates(el);
        const rect = el.getBoundingClientRect();

        const payload = {
            type: ev.type,
            candidates: candidates,
            x: rect.left + rect.width / 2,
            y: rect.top + rect.height / 2,
            timestamp: Date.now()
        };

        if (ev.type === 'keydown') {
            payload.key = ev.key;
        }

        // Send to Go backend via CDP Binding
        if (window.ghostRecordAction) {
            window.ghostRecordAction(JSON.stringify(payload));
        }
    }

    document.addEventListener('click', handleEvent, { capture: true, passive: true });
    document.addEventListener('keydown', handleEvent, { capture: true, passive: true });

    // ── Overlay UI ──────────────────────────────────────────────────────────
    const overlay = document.createElement('div');
    overlay.id = 'ghost-recorder-overlay';
    const shadow = overlay.attachShadow({ mode: 'closed' });
    shadow.innerHTML = `
        <style>
            .panel {
                position: fixed;
                bottom: 24px;
                right: 24px;
                z-index: 2147483647;
                background: rgba(26, 26, 26, 0.9);
                backdrop-filter: blur(8px);
                color: #fff;
                padding: 16px;
                border-radius: 12px;
                box-shadow: 0 8px 32px rgba(0,0,0,0.4);
                font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
                display: flex;
                flex-direction: column;
                gap: 12px;
                min-width: 200px;
                border: 1px solid rgba(255,255,255,0.1);
            }
            .header { display: flex; align-items: center; gap: 8px; font-weight: 600; font-size: 14px; }
            .dot { width: 8px; height: 8px; background: #ff4b4b; border-radius: 50%; animation: pulse 1.5s infinite; }
            @keyframes pulse { 0% { opacity: 1; } 50% { opacity: 0.4; } 100% { opacity: 1; } }
            .controls { display: flex; gap: 8px; }
            button {
                flex: 1;
                background: #333;
                color: #eee;
                border: none;
                padding: 8px 12px;
                border-radius: 6px;
                cursor: pointer;
                font-size: 13px;
                transition: background 0.2s;
            }
            button:hover { background: #444; }
            .stop-btn { background: #ff4b4b; color: white; }
            .stop-btn:hover { background: #ff3333; }
        </style>
        <div class="panel">
            <div class="header"><div class="dot"></div> Ghost-Driver Recording...</div>
            <div class="controls">
                <button id="pause">Pause</button>
                <button id="stop" class="stop-btn">Finish</button>
            </div>
        </div>
    `;
    document.body.appendChild(overlay);

    shadow.querySelector('#stop').addEventListener('click', () => {
        if (window.ghostRecordAction) {
            window.ghostRecordAction(JSON.stringify({ type: 'CMD_STOP' }));
        }
    });
})();
