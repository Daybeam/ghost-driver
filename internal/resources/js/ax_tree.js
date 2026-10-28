(function(){
  var nodes = [];
  var idMap = new WeakMap();
  var nextId = 0;

  var implicitRoles = {
    a:'link',button:'button',img:'image',table:'table',tr:'row',
    th:'columnheader',td:'cell',ul:'list',ol:'list',li:'listitem',
    nav:'navigation',main:'main',header:'banner',footer:'contentinfo',
    form:'form',section:'region',article:'article',dialog:'dialog',
    label:'label',fieldset:'group',legend:'legend',summary:'button',
    select:'listbox',textarea:'textbox',canvas:'canvas',svg:'svg',
    h1:'heading',h2:'heading',h3:'heading',h4:'heading',h5:'heading',h6:'heading',
    p:'paragraph',div:'generic',span:'generic',details:'group'
  };

  function getRole(el){
    var r = el.getAttribute('role');
    if (r) return r.split(' ')[0];
    var tag = el.tagName.toLowerCase();
    if (tag === 'input'){
      var t = (el.type||'').toLowerCase();
      if (t==='checkbox') return 'checkbox';
      if (t==='radio') return 'radio';
      if (t==='button'||t==='submit'||t==='reset'||t==='image') return 'button';
      if (t==='range') return 'slider';
      if (t==='search') return 'searchbox';
      if (t==='file') return 'textbox';
      return 'textbox';
    }
    return implicitRoles[tag] || null;
  }

  function getName(el){
    var al = el.getAttribute('aria-label');
    if (al) return al;
    var lb = el.getAttribute('aria-labelledby');
    if (lb){
      var e = document.getElementById(lb);
      if (e) return (e.innerText||'').trim().substring(0,300);
    }
    if (el.tagName==='IMG') return el.alt || el.title || '';
    if (el.tagName==='INPUT'||el.tagName==='TEXTAREA'){
      if (el.placeholder) return el.placeholder;
    }
    if (el.title) return el.title;
    var t = (el.innerText||'').trim();
    return t.length>300 ? t.substring(0,300) : t;
  }

  function walk(el, parentId){
    if (el.nodeType !== 1) return;
    if (el.getAttribute('aria-hidden') === 'true') return;
    var style = window.getComputedStyle(el);
    if (style.display === 'none' || style.visibility === 'hidden') return;

    var role = getRole(el);
    var name = getName(el);
    if (!role && !name && el.children.length === 0) return;

    var id = String(nextId++);
    idMap.set(el, id);

    var node = {
      nodeId: id,
      parentId: parentId,
      role: {type:'role', value: role||'generic'},
      name: {value: name},
      properties: [],
      childIds: []
    };

    var focusable = el.tabIndex >= 0 || el.tagName==='BUTTON' || el.tagName==='A' ||
                    el.tagName==='INPUT' || el.tagName==='SELECT' || el.tagName==='TEXTAREA' ||
                    (el.hasAttribute('contenteditable') && el.getAttribute('contenteditable')!=='false');
    node.properties.push({name:'focusable', value:{value:focusable}});
    node.properties.push({name:'disabled', value:{value: el.disabled===true || el.getAttribute('aria-disabled')==='true'}});
    node.properties.push({name:'hidden', value:{value:false}});

    var exp = el.getAttribute('aria-expanded');
    if (exp !== null) node.properties.push({name:'expanded', value:{value: exp==='true'}});
    var chk = el.getAttribute('aria-checked');
    if (chk !== null) node.properties.push({name:'checked', value:{value: chk==='true'}});
    var lvl = el.getAttribute('aria-level') || (el.tagName && el.tagName.match(/^H([1-6])$/));
    if (lvl){
      var lv = typeof lvl === 'string' ? parseInt(lvl) : parseInt(lvl[1]);
      if (lv>0) node.properties.push({name:'level', value:{value:lv}});
    }
    var val = el.getAttribute('aria-valuenow') || el.value;
    if (val !== undefined && val !== '' && val !== null){
      node.properties.push({name:'value', value:{value: typeof val==='string'?val:String(val)}});
    }

    if (parentId !== null && nodes[parseInt(parentId)]){
      nodes[parseInt(parentId)].childIds.push(id);
    }
    nodes.push(node);

    for (var i=0; i<el.children.length; i++){
      walk(el.children[i], id);
    }
  }

  var root = {
    nodeId:'0', parentId:null,
    role:{type:'role',value:'WebArea'},
    name:{value: document.title||''},
    properties:[{name:'focusable',value:{value:false}},{name:'disabled',value:{value:false}}],
    childIds:[]
  };
  nextId = 1;
  nodes.push(root);

  for (var i=0; i<document.body.children.length; i++){
    walk(document.body.children[i], '0');
  }

  return {nodes: nodes};
})()
