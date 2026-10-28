(function() {
    if (window.ghostTeachingInjected) return;
    window.ghostTeachingInjected = true;

    const style = document.createElement('style');
    style.innerHTML = `
        #ghost-teaching-bar {
            position: fixed; top: 0; left: 0; right: 0; height: 40px;
            background: #ff0000; color: white; z-index: 999999;
            display: flex; align-items: center; justify-content: space-between;
            padding: 0 20px; font-family: sans-serif; font-size: 14px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.3); pointer-events: auto;
        }
        #ghost-teaching-bar .status { display: flex; align-items: center; gap: 10px; }
        #ghost-teaching-bar .dot {
            width: 12px; height: 12px; background: white; border-radius: 50%;
            animation: ghost-blink 1s infinite;
        }
        @keyframes ghost-blink { 0% { opacity: 1; } 50% { opacity: 0.3; } 100% { opacity: 1; } }
        #ghost-teaching-bar button {
            background: white; color: #ff0000; border: none; padding: 4px 12px;
            border-radius: 4px; cursor: pointer; font-weight: bold;
        }
        #ghost-teaching-bar button:hover { background: #eee; }
        .ghost-highlight-target { outline: 2px solid #ff0000 !important; outline-offset: 2px; }
    `;
    document.head.appendChild(style);

    const bar = document.createElement('div');
    bar.id = 'ghost-teaching-bar';
    bar.innerHTML = `
        <div class="status">
            <div class="dot"></div>
            <strong>GHOST-DRIVER IS LEARNING...</strong>
            <span id="ghost-last-action" style="margin-left: 20px; opacity: 0.8;">Perform actions on the page</span>
        </div>
        <button id="ghost-finish-btn">FINISH & TEACH LLM</button>
    `;
    document.body.appendChild(bar);

    // Selector Generator
    function getBestSelector(el) {
        if (el.id) return `#${el.id}`;
        if (el.getAttribute('data-testid')) return `[data-testid="${el.getAttribute('data-testid')}"]`;
        if (el.tagName === 'BODY') return 'body';
        let path = el.tagName.toLowerCase();
        if (el.className) {
            const classes = Array.from(el.classList).filter(c => !c.includes('ghost')).join('.');
            if (classes) path += `.${classes}`;
        }
        return path;
    }

    function record(type, el, value = '') {
        const selector = getBestSelector(el);
        const rect = el.getBoundingClientRect();
        const payload = {
            type: type,
            selector: selector,
            value: value,
            x: rect.left + rect.width / 2,
            y: rect.top + rect.height / 2,
            ts: Date.now()
        };

        document.getElementById('ghost-last-action').innerText = `${type.toUpperCase()} on ${selector}`;

        if (window.ghostRecordAction) {
            window.ghostRecordAction(JSON.stringify(payload));
        }
    }

    // Intercept Clicks
    window.addEventListener('mousedown', (e) => {
        if (e.target.closest('#ghost-teaching-bar')) return;
        record('click', e.target);
    }, true);

    // Intercept Inputs
    window.addEventListener('change', (e) => {
        if (e.target.closest('#ghost-teaching-bar')) return;
        if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA' || e.target.tagName === 'SELECT') {
            record('type', e.target, e.target.value);
        }
    }, true);

    document.getElementById('ghost-finish-btn').onclick = () => {
        if (window.ghostRecordAction) {
            window.ghostRecordAction('FINISH');
        }
        bar.style.background = '#00ff88';
        bar.innerText = 'SAVING TRAJECTORY...';
    };
})();
