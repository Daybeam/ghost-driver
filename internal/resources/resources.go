package resources

import _ "embed"

//go:embed js/stealth.js
var StealthJS string

//go:embed js/stealth_firefox.js
var StealthJSFirefox string

//go:embed js/extraction.js
var ExtractionJS string

//go:embed js/wait_harness.js
var WaitHarnessJS string

//go:embed js/ax_tree.js
var AXTreeJS string
