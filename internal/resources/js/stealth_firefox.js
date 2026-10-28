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

    // 2. WebGL Fingerprint Masking (Firefox-appropriate vendor)
    const originalGetParameter = WebGLRenderingContext.prototype.getParameter;
    WebGLRenderingContext.prototype.getParameter = function(parameter) {
        if (parameter === 37445) return 'Mozilla';
        if (parameter === 37446) return 'Mozilla';
        return originalGetParameter.apply(this, arguments);
    };

    // 3. AudioContext Masking
    if (window.AudioContext) {
        const originalAudio = window.AudioContext.prototype.getChannelData;
        window.AudioContext.prototype.getChannelData = function() {
            return originalAudio.apply(this, arguments);
        };
    }

    // NOTE: No window.chrome injection — Firefox doesn't have it.
    // NOTE: No Chrome plugins spoofing — Firefox has its own plugin list.

    console.log("[GhostDriver] Stealth Layer Active (Firefox)");
})();
