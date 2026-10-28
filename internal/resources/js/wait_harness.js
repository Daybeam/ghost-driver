(function(){
  if (window.__ghostWait) return;
  window.__ghostWait = {
    lastMutation: 0,
    inFlight: 0,
    lastNetworkEnd: 0,
    popupLog: []
  };
  var w = window.__ghostWait;

  // DOM mutations
  var obs = new MutationObserver(function(mutations){
    w.lastMutation = Date.now();
    mutations.forEach(m => {
      m.addedNodes.forEach(node => {
        if (node.nodeType === 1 && node.innerText && node.innerText.trim()) {
           w.popupLog.push({t: Date.now(), text: node.innerText.trim()});
           if (w.popupLog.length > 30) w.popupLog.shift();
        }
      });
    });
  });
  obs.observe(document.documentElement, {subtree:true, childList:true, characterData:true});

  // XHR tracking
  var origOpen = XMLHttpRequest.prototype.open;
  var origSend = XMLHttpRequest.prototype.send;
  XMLHttpRequest.prototype.open = function(){ this.__ghost = true; return origOpen.apply(this, arguments); };
  XMLHttpRequest.prototype.send = function(){
    if (this.__ghost) {
      w.inFlight++;
      this.addEventListener('loadend', function(){ w.inFlight--; w.lastNetworkEnd = Date.now(); });
    }
    return origSend.apply(this, arguments);
  };

  // fetch tracking
  var origFetch = window.fetch;
  window.fetch = function(){
    w.inFlight++;
    return origFetch.apply(this, arguments).finally(function(){
      w.inFlight--;
      w.lastNetworkEnd = Date.now();
    });
  };
})()
