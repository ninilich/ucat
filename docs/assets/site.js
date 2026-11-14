/* ucat site: panel switching by URL hash. Vanilla JS, no dependencies. */
(function () {
  var panels = [].slice.call(document.querySelectorAll('.panel'));
  var links = [].slice.call(document.querySelectorAll('#menu a'));
  var main = document.getElementById('main');
  var btn = document.querySelector('.menu-btn');
  var bar = document.querySelector('.sidebar');
  var started = false;

  function setMenu(open) {
    bar.classList.toggle('open', open);
    btn.setAttribute('aria-expanded', open ? 'true' : 'false');
  }

  function show() {
    var id = '';
    try { id = decodeURIComponent(location.hash.slice(1)); } catch (e) {}
    if (id === 'compare-table') { id = 'compare'; } // the matrix tab of the comparison
    var panel = panels.filter(function (p) { return p.id === id; })[0] || panels[0];
    panels.forEach(function (p) { p.classList.toggle('active', p === panel); });
    links.forEach(function (a) {
      var here = a.getAttribute('href') === '#' + panel.id ||
        (panels.length === 1 && a.hasAttribute('aria-current'));
      if (here) { a.setAttribute('aria-current', 'page'); }
      else if (!a.hasAttribute('data-keep')) { a.removeAttribute('aria-current'); }
    });
    if (panels.length > 1 || !document.title) {
      document.title = (panel.getAttribute('data-title') || panel.id) + ' - ucat';
    }
    main.scrollTop = 0;
    window.scrollTo(0, 0);
    var h = panel.querySelector('h1, h2');
    if (started && h) { h.focus({ preventScroll: true }); }
    started = true;
    setMenu(false);
  }

  btn.addEventListener('click', function () {
    setMenu(btn.getAttribute('aria-expanded') !== 'true');
  });
  links.forEach(function (a) { a.addEventListener('click', function () { setMenu(false); }); });
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape' && btn.getAttribute('aria-expanded') === 'true') { setMenu(false); btn.focus(); }
  });
  window.addEventListener('hashchange', show);
  show();
})();

/* comparison matrix: tooltips */
(function () {
  if (!document.querySelector('.matrix')) { return; }

  // tooltips live in <body> so no container can clip them
  [].forEach.call(document.querySelectorAll('.tipbox'), function (t) { document.body.appendChild(t); });
  var openBtn = null, box = null, pinned = false, timer = 0;

  function place() {
    if (!openBtn || !box) { return; }
    var r = openBtn.getBoundingClientRect(), m = 8;
    var w = box.offsetWidth, h = box.offsetHeight;
    var x = Math.min(Math.max(m, r.left + r.width / 2 - w / 2), window.innerWidth - w - m);
    var y = r.bottom + 8;
    if (y + h > window.innerHeight - m && r.top - 8 - h >= m) { y = r.top - 8 - h; }
    y = Math.max(m, Math.min(y, window.innerHeight - h - m));
    box.style.left = x + 'px';
    box.style.top = y + 'px';
  }
  function hide() {
    clearTimeout(timer);
    if (openBtn) { openBtn.setAttribute('aria-expanded', 'false'); openBtn.removeAttribute('aria-describedby'); }
    if (box) { box.hidden = true; }
    openBtn = box = null; pinned = false;
  }
  function open(btn, pin) {
    clearTimeout(timer);
    if (openBtn === btn) { pinned = pinned || pin; return; }
    hide();
    box = document.getElementById(btn.getAttribute('aria-controls'));
    if (!box) { return; }
    openBtn = btn; pinned = pin;
    btn.setAttribute('aria-expanded', 'true');
    btn.setAttribute('aria-describedby', box.id);
    box.hidden = false;
    place();
  }
  function later() { clearTimeout(timer); if (!pinned) { timer = setTimeout(hide, 150); } }
  function tipBtn(e) { return e.target.closest ? e.target.closest('[aria-controls^="tip-"]') : null; }

  document.addEventListener('mouseover', function (e) {
    var b = tipBtn(e);
    if (b) { open(b, false); }
    else if (box && box.contains(e.target)) { clearTimeout(timer); }
  });
  document.addEventListener('mouseout', function (e) {
    var b = tipBtn(e);
    if ((b && b === openBtn) || (box && box.contains(e.target))) { later(); }
  });
  document.addEventListener('focusin', function (e) {
    var b = tipBtn(e);
    if (b && b.matches(':focus-visible')) { open(b, false); }
  });
  document.addEventListener('focusout', function (e) {
    var b = tipBtn(e);
    if (b && b === openBtn && !pinned) { hide(); }
  });
  document.addEventListener('click', function (e) {
    var b = tipBtn(e);
    if (b) {
      if (openBtn === b && pinned) { hide(); } else { open(b, true); }
    } else if (!(box && box.contains(e.target))) { hide(); }
  });
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape' && openBtn) {
      var b = openBtn;
      hide();
      if (document.activeElement !== b && b.offsetParent) { b.focus(); }
    }
  });
  window.addEventListener('resize', hide);
  window.addEventListener('hashchange', hide);
  window.addEventListener('scroll', place, true);
})();

/* comparison tabs: the map and the matrix. #compare opens the map, #compare-table the matrix. */
(function () {
  var tabs = [].slice.call(document.querySelectorAll('[role="tab"][data-view]'));
  if (!tabs.length) { return; }

  function select(name, focus, remember) {
    tabs.forEach(function (t) {
      var on = t.getAttribute('data-view') === name;
      t.setAttribute('aria-selected', on ? 'true' : 'false');
      t.tabIndex = on ? 0 : -1;
      document.getElementById(t.getAttribute('aria-controls')).hidden = !on;
      if (on && focus) { t.focus(); }
    });
    if (remember) {
      try { history.replaceState(null, '', name === 'table' ? '#compare-table' : '#compare'); } catch (e) {}
    }
  }
  function fromHash() { select(location.hash === '#compare-table' ? 'table' : 'fit', false, false); }

  tabs.forEach(function (t, i) {
    t.addEventListener('click', function () { select(t.getAttribute('data-view'), false, true); });
    t.addEventListener('keydown', function (e) {
      var to = -1;
      if (e.key === 'ArrowRight') { to = (i + 1) % tabs.length; }
      else if (e.key === 'ArrowLeft') { to = (i + tabs.length - 1) % tabs.length; }
      else if (e.key === 'Home') { to = 0; }
      else if (e.key === 'End') { to = tabs.length - 1; }
      if (to < 0) { return; }
      e.preventDefault();
      select(tabs[to].getAttribute('data-view'), true, true);
    });
  });
  window.addEventListener('hashchange', fromHash);
  fromHash();
})();

/* map: show why a tool sits where it does, on hover, focus and tap */
(function () {
  var out = document.getElementById('mp-why');
  var tools = [].slice.call(document.querySelectorAll('.mp-t[data-why]'));
  if (!out || !tools.length) { return; }
  var rest = out.textContent;

  function say(t) { out.textContent = t ? t.getAttribute('data-why') : rest; }
  tools.forEach(function (t) {
    t.addEventListener('mouseenter', function () { say(t); });
    t.addEventListener('mouseleave', function () { say(document.activeElement && document.activeElement.classList && document.activeElement.classList.contains('mp-t') ? document.activeElement : null); });
    t.addEventListener('focus', function () { say(t); });
    t.addEventListener('blur', function () { say(null); });
    t.addEventListener('click', function () { say(t); });
  });
})();
