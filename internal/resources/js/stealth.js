(function() {
    const overwrite = (obj, prop, value) => {
        Object.defineProperty(obj, prop, {
            get: () => value,
            set: () => {},
            configurable: true,
        });
    };

    // 1. Basic Navigator Overrides
    overwrite(navigator, 'webdriver', false);
    overwrite(navigator, 'languages', ['en-US', 'en']);
    overwrite(navigator, 'plugins', [
        { name: 'PDF Viewer', filename: 'internal-pdf-viewer', description: 'Portable Document Format' },
        { name: 'Chrome PDF Viewer', filename: 'internal-pdf-viewer', description: 'Portable Document Format' }
    ]);

    // 2. WebGL Fingerprint Masking
    const getParameter = HTMLCanvasElement.prototype.getContext;
    const originalGetParameter = WebGLRenderingContext.prototype.getParameter;
    WebGLRenderingContext.prototype.getParameter = function(parameter) {
        if (parameter === 37445) return 'ANGLE (NVIDIA, NVIDIA GeForce RTX 3080, OpenGL ES 2.0)';
        if (parameter === 37446) return 'Google Inc. (NVIDIA)';
        return originalGetParameter.apply(this, arguments);
    };

    // 3. AudioContext Masking
    if (window.AudioContext) {
        const originalAudio = window.AudioContext.prototype.getChannelData;
        window.AudioContext.prototype.getChannelData = function() {
            return originalAudio.apply(this, arguments);
        };
    }

    // 4. Chrome Runtime Mask
    if (window.chrome) {
        overwrite(window.chrome, 'runtime', {
            getEventListeners: () => { throw new Error("Not implemented"); },
            getRequestedPermissions: () => { throw new Error("Not implemented"); },
        });
    }

    // 5. Window Dimension Spoofing (headless has outerWidth/Height === 0)
    try {
        Object.defineProperty(window, 'outerWidth', { get: () => window.innerWidth, configurable: true });
        Object.defineProperty(window, 'outerHeight', { get: () => window.innerHeight, configurable: true });
    } catch(e) {}

    // 6. Chrome CSI / LoadTimes (absent in headless)
    if (window.chrome) {
        try {
            overwrite(window.chrome, 'csi', () => ({ startE: Date.now(), onloadT: Date.now(), pageT: 0, tran: 15 }));
            overwrite(window.chrome, 'loadTimes', () => ({
                requestTime: Date.now() / 1000 - 1,
                startLoadTime: Date.now() / 1000 - 1,
                commitLoadTime: Date.now() / 1000 - 0.5,
                finishDocumentLoadTime: Date.now() / 1000 - 0.3,
                finishLoadTime: Date.now() / 1000 - 0.1,
                firstPaintTime: Date.now() / 1000 - 0.2,
                firstPaintAfterLoadTime: 0,
                navigationType: "Other",
                wasFetchedViaSpdy: true,
                wasNpnNegotiated: true,
            }));
        } catch(e) {}
    }

    // 7. Notification.permission (headless defaults to 'denied')
    try {
        if (window.Notification) {
            Object.defineProperty(Notification, 'permission', { get: () => 'default', configurable: true });
        }
    } catch(e) {}

    // 8. Canvas Rendering Noise (prevents hash-based headless detection)
    try {
        const origToDataURL = HTMLCanvasElement.prototype.toDataURL;
        HTMLCanvasElement.prototype.toDataURL = function(type) {
            const ctx = this.getContext('2d');
            if (ctx && this.width > 0 && this.height > 0) {
                try {
                    const px = ctx.getImageData(0, 0, 1, 1);
                    px.data[0] = (px.data[0] + 1) & 0xFF;
                    ctx.putImageData(px, 0, 0);
                } catch(e) {}
            }
            return origToDataURL.apply(this, arguments);
        };
    } catch(e) {}

    // 9. CDC (Chrome DevTools Connector) Trace Cleanup
    try {
        for (const key of Object.getOwnPropertyNames(window)) {
            if (/^cdc_|^__cdp|^__selenium|^__webdriver/.test(key)) {
                delete window[key];
            }
        }
    } catch(e) {}

    console.log("[GhostDriver] Stealth Layer Active");
})();
