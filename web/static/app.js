/* Kinora web client – vanilla JS, hash routing, no build step. */
'use strict';

// ===================================================================== strings
// Every user-visible text lives in a language file: the built-in ones are i18n/<code>.js, the ones an admin
// imported or edited (Admin → Languages) come from /i18n/custom.js and override a built-in language key by key.
// Language files are pure data: "{0}", "{1}"… are arguments, "{0:00}" pads a number to two digits, and an
// object with "one"/"other"/"=0"… keys picks a plural form by the first argument. Hungarian is the base
// language; any other falls back to English, then to Hungarian.
const isObj = v => v && typeof v === 'object' && !Array.isArray(v);
const PLURAL_KEY = /^(=\d+|zero|one|two|few|many|other)$/;
const isPluralVal = v => isObj(v) && 'other' in v && Object.keys(v).every(k => PLURAL_KEY.test(k));
const isTemplateVal = v => typeof v === 'string' ? /\{\d(:0+)?\}/.test(v) : isPluralVal(v);
function mergeStrings(a, b) {
  const o = Object.assign({}, a);
  for (const [k, v] of Object.entries(b || {})) {
    if (k === 'srvRe' && Array.isArray(v) && Array.isArray(o[k])) {
      // patterns merge by pattern: the more specific language's come first
      const mine = new Set(v.map(p => Array.isArray(p) && p[0]));
      o[k] = [...v, ...o[k].filter(p => !(Array.isArray(p) && mine.has(p[0])))];
    } else o[k] = isObj(v) && isObj(o[k]) && !isPluralVal(v) ? mergeStrings(o[k], v) : v;
  }
  return o;
}
const I18N = (() => {
  const all = {};
  for (const l of Object.values(window.KINORA_I18N || {})) if (l && l.code && isObj(l.strings)) all[l.code] = Object.assign({ builtin: true }, l);
  for (const l of window.KINORA_I18N_CUSTOM || []) {
    if (!l || !l.code || !isObj(l.strings)) continue;
    const b = all[l.code];
    all[l.code] = b ? Object.assign({}, b, { name: l.name || b.name, strings: mergeStrings(b.strings, l.strings), custom: l, original: b })
      : Object.assign({}, l, { custom: l });
  }
  return all;
})();
const LANGS = Object.values(I18N).sort((a, b) => (b.builtin ? 1 : 0) - (a.builtin ? 1 : 0) || (a.builtin && b.builtin ? 0 : a.name.localeCompare(b.name))).map(l => [l.code, l.name]);
const LANG = (() => {
  const ok = l => !!l && !!I18N[l];
  const saved = localStorage.getItem('kinora-lang');
  if (ok(saved)) return saved;
  for (const l of navigator.languages || [navigator.language || '']) { const c = String(l).toLowerCase(); if (ok(c)) return c; if (ok(c.slice(0, 2))) return c.slice(0, 2); }
  return I18N.en ? 'en' : 'hu';
})();
const okLocale = l => { try { return Intl.NumberFormat.supportedLocalesOf([l]).length > 0; } catch { return false; } };
// Raw (uncompiled) strings of a language with its fallbacks, as the language editor and the export see them.
const langChain = code => [...new Set(['hu', code === 'hu' ? null : 'en', code])].filter(c => c && I18N[c]).map(c => I18N[c].strings);
function compileTemplate(s) {
  return (...a) => String(s).replace(/\{(\d)(?::(0+))?\}/g, (_, i, pad) => {
    let v = a[i];
    if (Array.isArray(v)) v = v.join(', ');
    v = v == null ? '' : String(v);
    return pad ? v.padStart(pad.length, '0') : v;
  });
}
function compileStrings(chain) {
  const base = chain[0] || {};
  const raw = chain.reduce(mergeStrings, {});
  const locale = typeof raw.locale === 'string' && okLocale(raw.locale) ? raw.locale : (okLocale(base.locale) ? base.locale : 'hu-HU');
  const pr = new Intl.PluralRules(locale);
  const fn = v => {
    if (!isPluralVal(v)) return compileTemplate(typeof v === 'string' ? v : '');
    const f = {};
    for (const k in v) f[k] = compileTemplate(v[k]);
    return (...a) => (f['=' + a[0]] || f[pr.select(Number(a[0]))] || f.other)(...a);
  };
  const out = {};
  for (const [k, v] of Object.entries(raw)) {
    // a key is a function when it is one in the base language, whatever the translation looks like
    const want = k in base ? isTemplateVal(base[k]) : isTemplateVal(v);
    out[k] = want ? fn(typeof v === 'string' || isPluralVal(v) ? v : base[k]) : v;
  }
  out.locale = locale;
  // the most specific language's patterns win; its fallbacks' patterns still apply after them
  out.srvRe = [];
  for (const s of chain.slice().reverse()) for (const p of Array.isArray(s.srvRe) ? s.srvRe : []) {
    try { if (Array.isArray(p) && typeof p[0] === 'string') out.srvRe.push([new RegExp(p[0], 's'), compileTemplate(p[1] == null ? '' : p[1])]); } catch { /* skip an invalid pattern */ }
  }
  return out;
}
const T = compileStrings(langChain(LANG));
document.documentElement.lang = LANG;
function setLang(l) { if (l === LANG) return; localStorage.setItem('kinora-lang', l); location.reload(); }
function langSwitch() {
  return h('label.lang-switch.lang-select', { onclick: e => e.stopPropagation() }, h('span.small.muted', null, T.language),
    h('select', { 'aria-label': T.language, onchange: e => setLang(e.target.value) }, LANGS.map(([c, name]) => h('option', { value: c, lang: c, selected: c === LANG }, name))));
}
// Server texts (errors, home rows, playback reasons) are Hungarian; translate the known ones:
// exact match, then the language's patterns, then "known prefix: detail".
function srv(msg) {
  if (typeof msg !== 'string' || !msg || LANG === 'hu') return msg;
  const d = T.srv || {};
  if (d[msg]) return d[msg];
  for (const [re, f] of T.srvRe || []) { const m = msg.match(re); if (m) return f(...m.slice(1)); }
  const i = msg.indexOf(': ');
  if (i > 0 && d[msg.slice(0, i)]) return d[msg.slice(0, i)] + ': ' + srv(msg.slice(i + 2));
  return msg;
}
function ago(s) {
  const m = Math.round(s / 60);
  if (m < 1) return T.agoNow;
  if (m < 60) return T.agoMin(m);
  const hr = Math.round(m / 60);
  return hr < 48 ? T.agoHour(hr) : T.agoDay(Math.round(hr / 24));
}
const lbl = (map, key, fallback) => (map && map[key]) || fallback;
const num = (v, digits) => Number(v).toLocaleString(T.locale, { minimumFractionDigits: digits, maximumFractionDigits: digits });

// ===================================================================== helpers
// The packaged apps (Samsung, LG, Titan OS, Android) open this page in app mode: see appshell.js. null in a browser.
const APP = window.KinoraApp || null;
const TV = !!(APP && APP.tv);
const $ = (s, r = document) => r.querySelector(s);
const app = () => document.getElementById('app');

/** h('div.class', {attr|onEvent}, ...children) – tiny DOM builder (text is always escaped). */
const tok = n => parseFloat(getComputedStyle(document.documentElement).getPropertyValue(n)) || 0;
function h(tag, props, ...kids) {
  const [t, ...cls] = tag.split('.');
  const e = document.createElement(t || 'div');
  if (cls.length) e.className = cls.join(' ');
  if (props) {
    for (const [k, v] of Object.entries(props)) {
      if (v == null || v === false) continue;
      if (k.startsWith('on') && typeof v === 'function') e.addEventListener(k.slice(2), v);
      else if (k === 'class') e.className = (e.className ? e.className + ' ' : '') + v;
      else if (k === 'style' && typeof v === 'object') Object.assign(e.style, v);
      else if (['value', 'checked', 'disabled', 'selected', 'textContent', 'htmlFor'].includes(k)) e[k] = v;
      else e.setAttribute(k, v === true ? '' : v);
    }
  }
  add(e, kids);
  return e;
}
/** put(el, ...kids) – replaceChildren that drops false/null/undefined/'' (no raw "false" on screen). */
function put(el, ...kids) { el.textContent = ""; return add(el, kids); }
function add(e, kids) {
  for (const k of kids.flat(Infinity)) {
    if (k == null || k === false || k === '') continue;
    e.append(k instanceof Node ? k : document.createTextNode(String(k)));
  }
  return e;
}
const ICONS = {
  play: 'M8 5v14l11-7z', pause: 'M7 5h4v14H7zM13 5h4v14h-4z', back: 'M19 12H5M11 6l-6 6 6 6', more: 'M12 7a1.5 1.5 0 110-3 1.5 1.5 0 010 3zm0 6.5a1.5 1.5 0 110-3 1.5 1.5 0 010 3zm0 6.5a1.5 1.5 0 110-3 1.5 1.5 0 010 3z',
  check: 'M5 12.5l4.5 4.5L19 7.5', heart: 'M12 20s-7-4.4-7-10a4 4 0 017-2.6A4 4 0 0119 10c0 5.6-7 10-7 10z', star: 'M12 3.5l2.6 5.3 5.9.9-4.3 4.1 1 5.8-5.2-2.7-5.2 2.7 1-5.8-4.3-4.1 5.9-.9z',
  rw: 'M4.5 12a7.5 7.5 0 102.2-5.3L4.5 9M4.5 4.5V9H9', ff: 'M19.5 12a7.5 7.5 0 11-2.2-5.3L19.5 9M19.5 4.5V9H15',
  vol: 'M4 9.5h3.5L12 6v12l-4.5-3.5H4zM15.5 9.5a3.5 3.5 0 010 5M18.5 6.8a7.5 7.5 0 010 10.4', mute: 'M4 9.5h3.5L12 6v12l-4.5-3.5H4zM16 10l4 4M20 10l-4 4',
  fs: 'M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5', cc: 'M3 7a2 2 0 012-2h14a2 2 0 012 2v10a2 2 0 01-2 2H5a2 2 0 01-2-2zM10.5 10a2.4 2.4 0 100 4M17 10a2.4 2.4 0 100 4',
  gear: 'M4 7h9M17 7h3M15 5v4M4 12h3M11 12h9M9 10v4M4 17h11M19 17h1M17 15v4',
  info: 'M12 3a9 9 0 100 18 9 9 0 000-18zM12 11v6M12 7.5v.5', next: 'M6 6l8 6-8 6zM17 6v12', chat: 'M4 5h16v11H9l-5 4z', eye: 'M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12zM12 9a3 3 0 100 6 3 3 0 000-6z',
  eyeOff: 'M3 3l18 18M10.6 6.1A10 10 0 0112 6c6 0 10 6 10 6a17 17 0 01-3 3.5M6.6 7.6C3.8 9.3 2 12 2 12s4 7 10 7a9.5 9.5 0 004.4-1.1', plus: 'M12 5v14M5 12h14', dl: 'M12 4v11M7 10l5 5 5-5M5 20h14',
  lock: 'M7 11V8a5 5 0 0110 0v3M5 11h14v10H5z', people: 'M9 11a3.5 3.5 0 100-7 3.5 3.5 0 000 7zM2 20c0-4 3-6 7-6s7 2 7 6M16 4.5a3.5 3.5 0 010 6.5M18 14c2.5.6 4 2.6 4 6', tag: 'M3 12V4h8l10 10-8 8z M7.5 8.5h.01',
  pip: 'M3 7a2 2 0 012-2h14a2 2 0 012 2v10a2 2 0 01-2 2H5a2 2 0 01-2-2zM12.5 12.5h5.5v3.5h-5.5z', list: 'M4 6h16M4 12h16M4 18h10', pencil: 'M4 20h4L19 9l-4-4L4 16zM13.5 6.5l4 4',
  cast: 'M3 18a3 3 0 013 3M3 14a7 7 0 017 7M3 10a11 11 0 0111 11M3 7V5a2 2 0 012-2h14a2 2 0 012 2v14a2 2 0 01-2 2h-4', share: 'M12 4v11M8 8l4-4 4 4M5 13v6a1 1 0 001 1h12a1 1 0 001-1v-6',
  x: 'M6 6l12 12M18 6L6 18', chevDown: 'M6 9l6 6 6-6', filter: 'M4 5h16l-6 8v6l-4-2v-4z',
  film: 'M4 4h16v16H4zM8 4v16M16 4v16M4 8h4M4 12h4M4 16h4M16 8h4M16 12h4M16 16h4', tv: 'M3 6h18v12H3zM8 21h8M12 18v3M9 3l3 3 3-3',
  search: 'M10.5 4a6.5 6.5 0 100 13 6.5 6.5 0 000-13zM20 20l-4.8-4.8', sortAsc: 'M4 7h5M4 12h8M4 17h11M18 19V5M15 8l3-3 3 3', sortDesc: 'M4 7h11M4 12h8M4 17h5M18 5v14M15 16l3 3 3-3',
};
function icon(name, fill) {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('aria-hidden', 'true');
  const p = document.createElementNS('http://www.w3.org/2000/svg', 'path');
  p.setAttribute('d', ICONS[name] || '');
  const solid = fill ?? ['play', 'pause', 'next', 'star'].includes(name);
  p.setAttribute('fill', solid ? 'currentColor' : 'none');
  p.setAttribute('stroke', 'currentColor');
  p.setAttribute('stroke-width', solid ? '0' : '2');
  p.setAttribute('stroke-linecap', 'round');
  p.setAttribute('stroke-linejoin', 'round');
  svg.append(p);
  return svg;
}
// The one search field: lens, the input, a "/" hint while empty, and a clear button once there's text.
function searchField(input, cls) {
  const clear = h('button.sf-clear', { type: 'button', 'aria-label': T.clearSearch, onclick: () => {
    input.value = ''; input.dispatchEvent(new Event('input', { bubbles: true })); input.focus();
  } }, icon('x'));
  return h('div.search-field' + (cls ? '.' + cls : ''), null, icon('search'), input, h('kbd', { 'aria-hidden': 'true' }, '/'), clear);
}
// "/" jumps to the page's own search (the library filter first, then the header).
document.addEventListener('keydown', e => {
  if (e.key !== '/' || e.ctrlKey || e.metaKey || e.altKey || e.defaultPrevented) return;
  const t = e.target;
  if (t && (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName))) return;
  const f = document.querySelector('.toolbar .search-field input') || document.querySelector('.search-box input');
  if (f && f.offsetParent) { e.preventDefault(); f.focus(); }
});
function toast(msg, err) {
  const box = document.getElementById('toasts');
  for (const old of [...box.children]) if (old.textContent === msg) old.remove();
  while (box.children.length >= 3) box.firstChild.remove();
  const t = h('div.toast' + (err ? '.err' : ''), { role: err ? 'alert' : 'status' }, msg);
  box.append(t);
  setTimeout(() => t.remove(), err ? 6000 : 3200);
}
const fmtTime = s => {
  s = Math.max(0, Math.floor(s || 0));
  const hh = Math.floor(s / 3600), mm = Math.floor(s / 60) % 60, ss = s % 60;
  return (hh ? hh + ':' + String(mm).padStart(2, '0') : mm) + ':' + String(ss).padStart(2, '0');
};
const fmtRuntime = sec => { if (sec < 60) return T.sec(Math.round(sec)); const m = Math.round(sec / 60); return T.hm(Math.floor(m / 60), m % 60); };
const fmtBytes = b => { if (!b) return '0 B'; const u = ['B', 'KB', 'MB', 'GB', 'TB']; const i = Math.min(4, Math.floor(Math.log(b) / Math.log(1024))); return num(b / 1024 ** i, i ? 1 : 0) + ' ' + u[i]; };
const resLabel = (w, hgt) => (hgt >= 1600 || w >= 3200) ? '4K' : (hgt >= 900 || w >= 1800) ? '1080p' : (hgt >= 650 || w >= 1200) ? '720p' : hgt ? 'SD' : '';
const imgURL = (it, kind) => `/api/images/${it.id}/${kind}?v=${it.updated_at || 0}`;
const debounce = (f, ms) => { let t; return (...a) => { clearTimeout(t); t = setTimeout(() => f(...a), ms); }; };
const qrImg = text => {
  try { const q = qrcode(0, 'M'); q.addData(text); q.make(); return h('div.qr', null, h('img', { src: q.createDataURL(5, 2), alt: 'QR: ' + text })); }
  catch { return h('div'); }
};
async function copyText(t) {
  try { await navigator.clipboard.writeText(t); toast(T.copied); }
  catch { prompt(T.copy, t); }
}

// ===================================================================== API
class HTTPError extends Error { constructor(status, msg, body) { super(msg); this.status = status; this.body = body; } }
async function api(method, path, body, opts = {}) {
  let res;
  try {
    res = await fetch(path, {
      method, credentials: 'same-origin', keepalive: !!opts.keepalive,
      headers: body !== undefined ? { 'Content-Type': 'application/json', 'X-Kinora-Lang': LANG } : { 'X-Kinora-Lang': LANG },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
  } catch (e) {
    if (!opts.quiet) toast(T.netErr, true);
    throw e;
  }
  let data = null;
  const ct = res.headers.get('content-type') || '';
  if (ct.includes('json')) data = await res.json().catch(() => null);
  if (!res.ok) {
    const msg = srv(data && data.error) || `HTTP ${res.status}`;
    if (res.status === 401 && !opts.noRedirect) { state.me = null; go('#/login'); }
    else if (res.status === 428 && !opts.noRedirect) { go('#/profiles'); }
    else if (!opts.quiet) toast(msg, true);
    throw new HTTPError(res.status, msg, data);
  }
  return data;
}
const GET = (p, o) => api('GET', p, undefined, o);
const POST = (p, b = {}, o) => api('POST', p, b, o);

// ===================================================================== state & router
const state = { status: null, me: null, facets: null, libs: null, cleanup: [], gen: 0 };
function go(hash) { if (location.hash === hash) route(); else location.hash = hash; }
function onCleanup(f) { state.cleanup.push(f); }
function parseHash() {
  const raw = location.hash.slice(1) || '/';
  const [path, qs] = raw.split('?');
  return { parts: path.split('/').filter(Boolean), q: new URLSearchParams(qs || '') };
}

// a deep link (e.g. #/quickconnect?code=…) survives the login and profile screens
function rememberAfter(hash) { try { sessionStorage.setItem('kinora.after', hash); } catch { /* storage blocked */ } }
function takeAfter() {
  let v = null;
  try { v = sessionStorage.getItem('kinora.after'); sessionStorage.removeItem('kinora.after'); } catch { /* storage blocked */ }
  return v;
}

async function loadMe() {
  try { state.me = await GET('/api/me', { noRedirect: true, quiet: true }); }
  catch { state.me = null; }
  return state.me;
}

// Snowfall: small white flakes drift down over the screens outside the player. A profile setting (off by default);
// the canvas lets every click through, stops while a player is open and is left out under "reduce motion".
const snow = (() => {
  let cv = null, raf = 0, flakes = [], last = 0;
  const still = matchMedia('(prefers-reduced-motion: reduce)');
  const size = () => {
    const r = Math.min(devicePixelRatio || 1, 2);
    cv.width = innerWidth * r; cv.height = innerHeight * r;
    cv.getContext('2d').setTransform(r, 0, 0, r, 0, 0);
    const n = Math.round(Math.min(90, innerWidth * innerHeight / 16000));
    while (flakes.length < n) flakes.push(flake(true));
    flakes.length = n;
  };
  const flake = anywhere => ({ x: Math.random() * innerWidth, y: anywhere ? Math.random() * innerHeight : -6, r: 1 + Math.random() * 2.2,
    v: 25 + Math.random() * 45, sway: Math.random() * 6.3, a: .45 + Math.random() * .5 });
  const frame = t => {
    raf = requestAnimationFrame(frame);
    const dt = Math.min((t - (last || t)) / 1000, .1); last = t;
    const c = cv.getContext('2d');
    c.clearRect(0, 0, innerWidth, innerHeight);
    if (document.querySelector('.player')) return; // films and episodes play without snow
    c.fillStyle = '#fff';
    for (const f of flakes) {
      f.y += f.v * dt; f.sway += dt * .8; f.x += Math.sin(f.sway) * 12 * dt;
      if (f.y > innerHeight + 6) Object.assign(f, flake(false));
      c.globalAlpha = f.a; c.beginPath(); c.arc(f.x, f.y, f.r, 0, 6.2832); c.fill();
    }
  };
  const update = () => {
    const on = !!state.me?.profile && state.me.profile.snow === true && !still.matches;
    if (on && !cv) {
      cv = h('canvas.snow', { 'aria-hidden': 'true' });
      document.body.append(cv); size(); addEventListener('resize', size); last = 0; raf = requestAnimationFrame(frame);
    } else if (!on && cv) {
      cancelAnimationFrame(raf); removeEventListener('resize', size); cv.remove(); cv = null; flakes = [];
    }
  };
  still.addEventListener?.('change', update);
  return update;
})();

async function route() {
  if (APP && APP.leaving) return; // walking back to the app's launcher (appshell.js)
  const gen = ++state.gen;
  for (const f of state.cleanup.splice(0)) { try { f(); } catch { /* ignore */ } }
  document.querySelectorAll('.modal-back').forEach(m => m.remove());
  const { parts, q } = parseHash();
  const [p0, p1, p2] = parts;
  document.title = T.appName; // a view with a title of its own (wizard, title page, player, a page heading) sets it again
  if (!state.status) {
    [state.status, state.branding] = await Promise.all([GET('/api/status', { quiet: true }).catch(() => ({})), GET('/api/branding', { quiet: true }).catch(() => null)]);
    applyBranding();
  }
  if (state.status.setup_needed) { if (p0 !== 'setup') return go('#/setup'); return viewSetup(); }
  if (p0 === 'setup' && !p1) return go('#/login');
  if (p0 === 'join' && p1) return viewJoin(p1);
  if (p0 === 'setpw' && p1) return viewSetPassword(p1);
  if (!state.me) await loadMe();
  snow();
  if (!state.me) {
    if (p0 !== 'login') { if (p0 === 'quickconnect') rememberAfter(location.hash); return go('#/login'); }
    const code = state.status.quickconnect_enabled && (p1 === 'code' || (TV && p1 !== 'password'));
    return code ? viewLoginCode() : viewLogin();
  }
  if (p0 === 'login') return go('#/profiles');
  // the first-run wizard comes right after the admin account, before any profile is picked
  if (p0 === 'setup') return state.me.user.is_admin ? viewWizard(p1) : go('#/home');
  if (p0 === 'quickconnect' && state.me.profile) return viewQuickConnect(q.get('code') || '');
  if (p0 === 'quickconnect') rememberAfter(location.hash);
  if (p0 === 'profiles' || !state.me.profile) return viewProfiles(p1 === 'edit');
  if (DEV) deviceSync();
  try {
    await routeView(p0, p1, p2, q);
    if (gen === state.gen) titleFromHeading();
  } catch (e) {
    if (!(e instanceof HTTPError)) console.error(e);
    else if (e.status === 404 && gen === state.gen) notFoundView(); // a removed title, or one this profile may not see
  }
}
async function routeView(p0, p1, p2, q) {
  switch (p0) {
    case undefined: case 'home': return await viewHome();
    case 'library': return await viewLibrary(+p1, q);
    case 'item': return await viewItem(+p1);
    case 'play': return await viewPlayer(+p1, q);
    case 'search': return await viewSearch(q.get('q') || '');
    case 'collections': return p1 === 'new' ? await viewCollectionEdit(null, q) : p2 === 'edit' ? await viewCollectionEdit(+p1) : p1 ? await viewCollection(+p1) : await viewCollections();
    case 'downloads': return await viewDownloads();
    case 'discover': return await viewDiscover(q);
    case 'requests': return await viewRequests();
    case 'party': return await viewPartyJoin(p1);
    case 'admin': return await viewAdmin(p1 || 'dash');
    case 'account': return await viewAccount();
    case 'journal': return await viewJournal(q);
    case 'resume': return await resumeLast();
    default: return go('#/home');
  }
}
// the app's "Resume" shortcut: the first title of Continue watching, or home when there is none
async function resumeLast() {
  const r = await GET('/api/home', { quiet: true }).catch(() => null);
  const row = r && (r.rows || []).find(x => x.id === 'continue');
  const it = row && (row.items || [])[0];
  location.replace(it ? `#/play/${it.id}` : '#/home');
}
// haptic feedback in the Android app (a no-op elsewhere): light | medium | heavy | tick | success | error
const buzz = kind => APP?.haptic?.(kind);
// a page with a heading of its own names the browser tab and its history entry ("Filmek – Kinora")
function titleFromHeading() {
  if (document.title !== T.appName) return;
  const h1 = document.querySelector('main .page-head h1, main .a-head h1');
  const t = h1 && h1.textContent.trim();
  if (t) document.title = t + ' – ' + T.appName;
}
function notFoundView() {
  const main = shell('');
  main.append(h('div.page-head', null, h('h1', null, T.notFoundTitle)), h('p.empty', null, T.notFoundLede),
    h('p', null, h('a.btn.primary', { href: '#/home' }, T.backHome)));
}
// D-pad / arrow-key navigation: move focus to the nearest focusable element in that direction.
function spatialNav(e) {
  const dir = { ArrowLeft: 'l', ArrowRight: 'r', ArrowUp: 'u', ArrowDown: 'd' }[e.key];
  if (!dir || e.altKey || e.ctrlKey || e.metaKey) return;
  const a = document.activeElement;
  if (a && a.matches('input, textarea, select, [contenteditable], .timeline, video') && !(TV && tvLeavesField(a, dir))) return;
  const player = document.querySelector('.player');
  // the player owns its keys; on a TV the arrows move between its controls while they are on screen
  if (player && !document.querySelector('.modal-back') && !(TV && !player.classList.contains('idle') && a && a !== document.body && a !== player.querySelector('video') && player.contains(a))) return;
  const scope = document.querySelector('.modal-back .modal') || document.querySelector('.menu-pop.floating') ||
    (TV && (document.querySelector('.menu-pop:not(.hidden)') || (player && (player.querySelector('.p-pop:not(.hidden)') || player)))) || document;
  const focusables = [...scope.querySelectorAll('a[href], button, input, select, textarea, [tabindex]:not([tabindex="-1"])')]
    .filter(el => el !== a && !el.disabled && el.offsetParent !== null && !el.closest('.hidden, [inert]'));
  if (!a || a === document.body || !scope.contains(a)) {
    const first = scope.querySelector('.card, .who-item') || focusables[0];
    if (first) { e.preventDefault(); first.focus(); }
    return;
  }
  const r = a.getBoundingClientRect();
  const cx = (r.left + r.right) / 2, cy = (r.top + r.bottom) / 2;
  let best = null, bs = Infinity;
  for (const el of focusables) {
    const b = el.getBoundingClientRect();
    if (!b.width && !b.height) continue;
    const dx = (b.left + b.right) / 2 - cx, dy = (b.top + b.bottom) / 2 - cy;
    let main, cross;
    if (dir === 'l') { if (b.right > r.left + 1) continue; main = -dx; cross = Math.abs(dy); }
    else if (dir === 'r') { if (b.left < r.right - 1) continue; main = dx; cross = Math.abs(dy); }
    else if (dir === 'u') { if (b.bottom > r.top + 1) continue; main = -dy; cross = Math.abs(dx); }
    else { if (b.top < r.bottom - 1) continue; main = dy; cross = Math.abs(dx); }
    const score = main + cross * 2.5;
    if (score < bs) { bs = score; best = el; }
  }
  if (best) { e.preventDefault(); best.focus(); best.scrollIntoView({ block: 'nearest', inline: 'nearest' }); }
}
// TV: a remote has no Tab key, so the arrows also leave a field: up/down always, left/right at the ends of the text.
function tvLeavesField(a, dir) {
  if (a.matches('.timeline, input[type=range]')) return dir === 'u' || dir === 'd';
  if (a.matches('select, video, input[type=checkbox], input[type=radio]')) return true;
  if (dir === 'u' || dir === 'd') return true;
  let at = null, len = 0;
  try { at = a.selectionStart; len = (a.value || '').length; } catch { /* no caret on this input type */ }
  if (at == null) return true;
  return dir === 'l' ? at === 0 : at === len;
}
document.addEventListener('keydown', spatialNav);
window.addEventListener('hashchange', route);
window.addEventListener('DOMContentLoaded', route);

// ===================================================================== layout
function shell(active, ...content) {
  const me = state.me, p = me.profile;
  const root = app();
  put(root);
  const libsNav = h('span');
  const searchInput = h('input', { type: 'search', placeholder: T.searchPh, 'aria-label': T.searchPh, value: active === 'search' ? (parseHash().q.get('q') || '') : '' });
  // the first search adds one history entry; refining it replaces that entry, so Back leaves the search
  const goSearch = v => {
    const hash = '#/search?q=' + encodeURIComponent(v);
    if (location.hash === hash) return;
    if (active === 'search') { history.replaceState(history.state, '', hash); route(); } else location.hash = hash;
  };
  searchInput.addEventListener('input', debounce(() => {
    const v = searchInput.value.trim();
    if (v.length >= 2) goSearch(v);
  }, 300));
  searchInput.addEventListener('keydown', e => { if (e.key === 'Enter' && searchInput.value.trim()) goSearch(searchInput.value.trim()); });
  const menuPop = h('div.menu-pop.hidden', { role: 'menu' },
    h('button', { role: 'menuitem', onclick: () => POST('/api/profiles/leave').then(() => { state.me.profile = null; go('#/profiles'); }) }, T.switchProfile),
    h('a', { role: 'menuitem', href: '#/party' }, T.joinParty),
    h('a', { role: 'menuitem', href: '#/journal' }, T.journal),
    h('a', { role: 'menuitem', href: '#/account' }, T.account),
    me.can_request && h('a', { role: 'menuitem', href: '#/requests' }, T.myRequests),
    !p.is_kids && !me.profile_locked && h('a', { role: 'menuitem', href: '#/quickconnect' }, T.connectDevice),
    h('hr'),
    langSwitch(),
    h('hr'),
    APP && APP.canSwitchServer && h('button', { role: 'menuitem', onclick: () => APP.switchServer(T.leavingApp) }, T.switchServer),
    h('button', { role: 'menuitem', onclick: () => POST('/api/logout').then(() => { state.me = null; go('#/login'); }) }, T.logout));
  const avatarBtn = h('button.avatar', { style: { background: p.color }, 'aria-label': p.name, 'aria-haspopup': 'menu', 'aria-expanded': 'false', onclick: e => {
    e.stopPropagation();
    const open = menuPop.classList.toggle('hidden') === false;
    avatarBtn.setAttribute('aria-expanded', String(open));
  } }, p.name.slice(0, 1).toUpperCase());
  const closeMenu = () => { menuPop.classList.add('hidden'); avatarBtn.setAttribute('aria-expanded', 'false'); };
  // Escape closes the open menu and hands focus back to the button that opened it
  const escMenu = e => { if (e.key === 'Escape' && !menuPop.classList.contains('hidden')) { closeMenu(); avatarBtn.focus(); } };
  document.addEventListener('click', closeMenu);
  document.addEventListener('keydown', escMenu);
  onCleanup(() => { document.removeEventListener('click', closeMenu); document.removeEventListener('keydown', escMenu); });
  const top = h('header.top' + (active === 'home' ? '' : '.solid'), null,
    brandMark('a'),
    h('nav.nav', { 'aria-label': T.mainMenu },
      h('a' + (active === 'home' ? '.on' : ''), { href: '#/home' }, T.home),
      libsNav,
      h('a' + (active === 'collections' ? '.on' : ''), { href: '#/collections' }, T.collections),
      me.can_request && h('a' + (active === 'discover' ? '.on' : ''), { href: '#/discover' }, T.discover),
      h('a' + (active === 'downloads' ? '.on' : ''), { href: '#/downloads' }, T.downloads),
      me.user.is_admin && h('a' + (active === 'admin' ? '.on' : ''), { href: '#/admin' }, T.admin)),
    searchField(searchInput, 'search-box'),
    h('div.menu', null, avatarBtn, menuPop));
  if (active === 'home') { // over the hero the bar is see-through; once scrolled it gets a fill so rows don't show through it
    const onScroll = () => top.classList.toggle('solid', window.scrollY > 24);
    window.addEventListener('scroll', onScroll, { passive: true });
    onCleanup(() => window.removeEventListener('scroll', onScroll));
  }
  const main = h('main', null, content);
  root.append(top, main);
  const fit = () => fitTopNav(top);
  let fitRaf = 0;
  const onResize = () => { cancelAnimationFrame(fitRaf); fitRaf = requestAnimationFrame(fit); };
  addEventListener('resize', onResize);
  onCleanup(() => { removeEventListener('resize', onResize); cancelAnimationFrame(fitRaf); });
  document.fonts?.ready.then(fit);
  (state.libs ? Promise.resolve(state.libs) : GET('/api/libraries').then(l => (state.libs = l))).then(libs => {
    libsNav.replaceWith(...libs.map(l => h('a' + (active === 'lib' + l.id ? '.on' : ''), { href: '#/library/' + l.id }, l.name)));
    fit();
  }).catch(() => {});
  window.scrollTo(0, 0);
  return main;
}

// When the menu's links no longer fit on one line beside the logo and the search (a narrow window, many libraries),
// the menu moves to a row of its own and scrolls sideways, as on a phone, instead of wrapping its last links under the first.
function fitTopNav(top) {
  const nav = top.querySelector('.nav');
  if (!nav || !top.isConnected) return;
  top.classList.remove('stack');
  const links = nav.children;
  top.classList.toggle('stack', links.length > 1 && links[links.length - 1].offsetTop > links[0].offsetTop + 1);
  document.documentElement.style.setProperty('--top-h', top.offsetHeight + 'px');
  revealActiveNav(top);
}
// on a phone the menu scrolls sideways; keep the current page's link in view instead of cut off at the edge
function revealActiveNav(top) {
  const nav = top.querySelector('.nav'), a = nav && nav.querySelector('a.on');
  if (!a || nav.scrollWidth <= nav.clientWidth) return;
  const r = a.getBoundingClientRect(), nr = nav.getBoundingClientRect();
  if (r.left < nr.left || r.right > nr.right) nav.scrollLeft += r.left - nr.left - (nr.width - r.width) / 2;
}

function modal(title, body, actions, onClose) {
  const back = h('div.modal-back', { role: 'dialog', 'aria-modal': 'true', 'aria-label': title });
  let closed = false;
  const close = () => { if (closed) return; closed = true; back.remove(); document.removeEventListener('keydown', esc); onClose?.(); };
  onCleanup(close); // leaving the page closes it too
  const esc = e => { if (e.key === 'Escape') close(); };
  document.addEventListener('keydown', esc);
  back.addEventListener('mousedown', e => { if (e.target === back) close(); });
  const box = h('div.modal', null, h('h2', null, title), body, actions && h('div.foot', null, actions(close)));
  back.append(box);
  document.body.append(back);
  setTimeout(() => (box.querySelector('input,select,button') || box).focus(), 30);
  return { close, box };
}
function confirmBox(text, yesLabel = T.delete, body = null) {
  return new Promise(res => {
    modal(text, body, close => [h('button.btn', { onclick: () => { res(false); close(); } }, T.cancel), h('button.btn.primary', { onclick: () => { res(true); close(); } }, yesLabel)], () => res(false));
  });
}

// ===================================================================== auth views
function brandMark(tag) {
  const b = state.branding;
  const name = (b && b.server_name) || T.appName;
  if (b && b.logo_dark) return h(tag + '.logo.has-img', tag === 'a' ? { href: '#/home', 'aria-label': name } : { 'aria-label': name }, h('img', { src: b.logo_dark, alt: name }));
  return h(tag + '.logo', tag === 'a' ? { href: '#/home' } : null, T.appName);
}

// favicon, apple-touch icon and accent colour from the server branding
function applyBranding() {
  const b = state.branding;
  if (!b) return;
  if (b.favicon) document.querySelector('link[rel="icon"]')?.setAttribute('href', b.favicon);
  if (b.icon_192) document.querySelector('link[rel="apple-touch-icon"]')?.setAttribute('href', b.icon_192);
  if (b.accent && /^#[0-9a-f]{6}$/i.test(b.accent)) document.documentElement.style.setProperty('--amber', b.accent);
  else document.documentElement.style.removeProperty('--amber');
}

function authPage(title, lede, form) {
  put(app(), h('div.center', null, h('div.auth', null, brandMark('span'), h('h1', null, title), h('p.lede', null, lede), form, langSwitch(),
    APP && APP.canSwitchServer && h('button.btn.ghost.wide.app-switch', { type: 'button', onclick: () => APP.switchServer(T.leavingApp) }, T.switchServer))));
  setTimeout(() => form.querySelector('input')?.focus(), 30);
}
function field(label, input, hint) { return h('label.field', null, h('span', null, label), input, hint && h('div.hint', { class: 'mt1' }, hint)); }

function viewSetup() {
  // opened from outside the local network: the server refuses the setup (KINORA_ALLOW_REMOTE_SETUP=1 allows it)
  const blocked = !!(state.status && state.status.setup_blocked);
  const name = h('input', { type: 'text', value: 'Kinora', autocomplete: 'off' });
  const user = h('input', { type: 'text', autocomplete: 'username' });
  const pw = h('input', { type: 'password', autocomplete: 'new-password' });
  const pw2 = h('input', { type: 'password', autocomplete: 'new-password' });
  const logo = h('input', { type: 'file', accept: 'image/png,image/jpeg,image/webp,image/svg+xml' });
  const form = h('form', { onsubmit: async e => {
    e.preventDefault();
    if (pw.value !== pw2.value) return toast(T.pwMismatch, true);
    await POST('/api/setup', { server_name: name.value, username: user.value, password: pw.value });
    if (logo.files[0]) {
      for (const v of ['dark', 'light']) await uploadLogo(v, logo.files[0]).catch(err => toast(err.message, true));
    }
    state.status = null; await loadMe(); go('#/setup/' + WIZ[0]);
  } }, blocked && h('p.bad', { role: 'alert' }, T.setupBlocked),
  field(T.serverName, name), field(T.username, user), field(T.password, pw), field(T.password2, pw2), field(T.setupLogo, logo, T.logoHint),
  h('button.btn.primary.big', { type: 'submit', class: 'wide', disabled: blocked }, T.createAdmin));
  authPage(T.setupTitle, T.setupLede, form);
}

// ===================================================================== first-run wizard
// After the admin account (viewSetup): media folders → metadata → encoder test → downloads → remote access →
// summary. Every step can be skipped; each one saves through the regular admin endpoints, so whatever is left out
// can be done later on the matching Admin page (the summary links there, the dashboard offers to continue).
const WIZ = ['media', 'metadata', 'hardware', 'downloads', 'remote', 'summary'];

async function viewWizard(step) {
  const i = WIZ.indexOf(step);
  if (i < 0) return go('#/setup/' + WIZ[0]);
  document.title = T.wizDocTitle;
  const body = h('div.wiz-body', { 'aria-busy': 'true' });
  const foot = h('div.wiz-foot');
  const names = [T.wizStepAdmin, ...WIZ.map(k => T['wizStep_' + k])];
  put(app(), h('div.center', null, h('div.wiz', null, brandMark('span'),
    h('ol.wiz-steps', { 'aria-label': T.wizProgress(i + 2, names.length) }, names.map((n, j) => h('li' + (j === i + 1 ? '.on' : j <= i ? '.done' : ''),
      { 'aria-current': j === i + 1 ? 'step' : null }, h('span.wiz-dot', { 'aria-hidden': 'true' }, j <= i ? icon('check') : String(j + 1)), h('span.wiz-name', null, n)))),
    h('h1', null, T['wizTitle_' + step]), h('p.lede', null, T['wizLede_' + step]), body, foot, langSwitch())));
  let onNext = null;
  try { onNext = await WIZ_STEPS[step](body); }
  catch (e) { put(body, h('p.bad', { role: 'alert' }, e.message || String(e))); }
  finally { body.removeAttribute('aria-busy'); }
  const nextStep = () => go('#/setup/' + WIZ[i + 1]);
  const last = step === 'summary';
  put(foot,
    i > 0 && h('button.btn.ghost', { onclick: () => go('#/setup/' + WIZ[i - 1]) }, T.wizBack),
    h('span.wiz-gap'),
    !last && h('button.btn.ghost', { onclick: nextStep, title: T.wizSkipHint }, T.wizSkip),
    h('button.btn.primary', { onclick: async e => {
      const b = e.currentTarget;
      b.disabled = true;
      try {
        if (onNext && (await onNext()) === false) return; // the step kept the user here (a rejected value)
        if (!last) return nextStep();
        await POST('/api/admin/setup/finish', {});
        go('#/profiles');
      } catch (err) { toast(err.message, true); }
      finally { b.disabled = false; }
    } }, last ? T.wizFinish : T.wizNext));
  body.querySelector('input, select')?.focus();
}

const WIZ_STEPS = {
  // media folders: the libraries so far and an "add" form with the folder picker
  async media(body) {
    if (!state.installMode) state.installMode = (await GET('/api/admin/browse?path=/').catch(() => ({}))).mode || '';
    const list = h('ul.wiz-list', { 'aria-live': 'polite' });
    const render = async () => {
      const libs = await GET('/api/libraries');
      state.libs = null;
      put(list, libs.length ? libs.map(l => h('li', null, icon('check'), ' ', h('b', null, l.name), h('span.muted', null, ' · ' + T.kinds[l.kind]), h('div.small.muted.mono', null, l.path)))
        : h('li.muted.small', null, T.wizNoLibs));
    };
    const name = h('input', { type: 'text' });
    const path = h('input', { type: 'text', placeholder: state.installMode === 'docker' ? '/media/movies' : '' });
    const kind = h('select', null, Object.entries(T.kinds).map(([k, v]) => h('option', { value: k }, v)));
    const lastPart = p => p.split(/[\\/]/).filter(Boolean).pop() || '';
    const addNow = async () => {
      try {
        await POST('/api/libraries', { name: name.value.trim() || lastPart(path.value) || path.value.trim(), path: path.value.trim(), kind: kind.value }, { quiet: true });
      } catch (e) {
        if (e.body && e.body.perm) permDialog(e.body.perm); else toast(e.message, true);
        return false;
      }
      toast(T.wizLibAdded); name.value = ''; path.value = ''; await render();
      return true;
    };
    const add = h('button.btn', { type: 'button', onclick: async () => {
      if (!path.value.trim()) return toast(T.wizPickFolder, true);
      add.disabled = true;
      try { await addNow(); } finally { add.disabled = false; }
    } }, icon('plus'), T.addLibrary);
    await render();
    put(body, list, h('div.a-form.wiz-form', null,
      h('label.field', null, h('span', null, T.libPath), h('div.row-gap', { class: 'nowrap' }, path,
        h('button.btn', { type: 'button', onclick: () => folderPicker(path.value, p => { path.value = p; if (!name.value) name.value = lastPart(p); }) }, T.choose))),
      h('div.hint', { class: 'mt0' }, state.installMode === 'docker' ? T.mediaHintDocker : T.mediaHintNative),
      h('div.grid2', null, field(T.libName, name), field(T.libKind, kind)), h('div.a-form-foot', null, add)));
    // a folder chosen but not added yet goes in with "Next"; a refused one keeps the user on this step
    return async () => { if (path.value.trim()) return addNow(); };
  },

  // metadata: what TMDB adds, how to get a free key, the language; without a key Kinora uses file and folder
  // names and local NFO files
  async metadata(body) {
    const s = await GET('/api/admin/settings');
    const locked = s.env.tmdb_key;
    const key = h('input', { type: 'password', value: s.values.tmdb_key || '', autocomplete: 'off', disabled: !!locked, placeholder: T.wizOptional });
    const lang = h('select', { disabled: !!s.env.metadata_lang }, [['hu-HU', T.langNames.hun], ['en-US', T.langNames.eng], ['de-DE', T.langNames.ger]]
      .map(([v, l]) => h('option', { value: v, selected: (s.values.metadata_lang || 'hu-HU') === v }, l)));
    const status = h('div.key-status', { role: 'status' });
    const test = h('button.btn.small', { type: 'button', onclick: async () => {
      test.disabled = true; status.className = 'key-status'; status.textContent = T.keyTesting;
      try {
        const r = await POST('/api/admin/keytest', { service: 'tmdb', key: key.value });
        status.className = 'key-status ' + (r.ok ? 'ok' : r.state === 'limit' || r.state === 'network' ? 'warn' : 'bad');
        status.textContent = (T[KEY_STATE[r.state]] || T.keyStateError) + (r.detail && !r.ok ? ' (' + srv(r.detail) + ')' : '');
      } catch (e) { status.className = 'key-status bad'; status.textContent = e.message; }
      finally { test.disabled = false; }
    } }, T.keyTest);
    put(body, h('div.wiz-text', null, h('p', null, T.wizMetaWhat), h('p', null, T.wizMetaWithout)),
      h('div.a-form.wiz-form', null,
        h('label.field', null, h('span', null, T.tmdbKey), h('div.key-row', null, key, test), status,
          h('div.hint', null, T.wizMetaGetKey, ' ', h('a', { href: 'https://www.themoviedb.org/settings/api', target: '_blank', rel: 'noopener noreferrer' }, 'themoviedb.org/settings/api')),
          locked && h('div.env-lock', null, T.envLocked(locked))),
        field(T.metaLang, lang, T.metaLangHint)));
    return async () => {
      const patch = {};
      if (!locked && key.value.trim() && key.value !== s.values.tmdb_key) patch.tmdb_key = key.value.trim();
      if (!s.env.metadata_lang && lang.value !== (s.values.metadata_lang || 'hu-HU')) patch.metadata_lang = lang.value;
      if (Object.keys(patch).length) await api('PATCH', '/api/admin/settings', patch);
    };
  },

  // the encoder test: which hardware encoders work in this install, and what to do when none does
  async hardware(body) {
    if (!state.installMode) state.installMode = (await GET('/api/admin/browse?path=/').catch(() => ({}))).mode || '';
    const out = h('div', { 'aria-live': 'polite' });
    const missing = x => x.error === 'az ffmpeg nem tartalmazza ezt az encodert';
    const show = r => {
      const hw = r.results.filter(x => x.ok && x.hardware);
      put(out,
        h('p', { class: hw.length ? 'ok' : '' }, hw.length ? T.wizHwFound(srv(r.selected.label)) : T.wizHwSoftware),
        !hw.length && h('p.muted.small', null, state.installMode === 'docker' ? T.wizHwDocker : T.wizHwNative),
        h('ul.wiz-list.small', null, r.results.filter(x => !missing(x)).map(x => h('li', null,
          h('span', { class: x.ok ? 'ok' : 'bad' }, x.ok ? T.works : T.encFailed), ' · ', srv(x.label),
          x.name === r.selected.name && [' ', h('span.badge.amber', null, T.inUse)]))));
    };
    const run = h('button.btn', { type: 'button', onclick: async () => {
      run.disabled = true; run.textContent = T.hwTesting;
      try { show(await POST('/api/admin/hwtest')); } catch (e) { put(out, h('p.bad', null, e.message)); }
      finally { run.disabled = false; run.textContent = T.hwTest; }
    } }, T.hwTest);
    put(body, h('div.wiz-text', null, h('p', null, T.wizHwWhat)), h('div.a-form-foot', null, run), out);
    run.click(); // it takes a few seconds: start it right away
    return null;
  },

  // built-in requests and downloads: one download client and one Torznab/Newznab indexer (Jackett, Prowlarr…);
  // the rest (built-in tracker definitions, quality profiles, FlareSolverr, folders) is on Admin → Downloads
  async downloads(body) {
    const s = await GET('/api/admin/acquire/settings');
    const enabled = h('input.switch', { type: 'checkbox', role: 'switch', checked: !!s.enabled });
    const have = h('ul.wiz-list.small', { 'aria-live': 'polite' });
    const listHave = (cls, ixs) => put(have, [...cls.map(c => h('li', null, icon('check'), ' ', T.wizDlClientIs(c.name || c.kind))),
      ...ixs.map(x => h('li', null, icon('check'), ' ', T.wizDlIndexerIs(x.name)))]);
    listHave(s.clients || [], s.indexers || []);
    const refresh = async () => { const n = await GET('/api/admin/acquire/settings'); listHave(n.clients || [], n.indexers || []); };

    const kind = h('select', null, ACQ_CLIENT_KINDS.map(([v, l]) => h('option', { value: v }, l)));
    const cName = h('input', { type: 'text' }), cUrl = h('input', { type: 'url' });
    const cUser = h('input', { type: 'text', autocomplete: 'off' }), cPass = h('input', { type: 'password', autocomplete: 'new-password' });
    const userField = field(T.username, cUser), passLabel = h('span', null, T.password);
    const sync = () => {
      const k = kind.value;
      cUrl.placeholder = ACQ_CLIENT_KINDS.find(x => x[0] === k)[2];
      passLabel.textContent = k === 'sabnzbd' ? T.acqApiKey : T.password;
      userField.classList.toggle('hidden', k === 'deluge' || k === 'sabnzbd');
    };
    kind.addEventListener('change', sync); sync();
    const client = () => ({ id: 0, kind: kind.value, name: cName.value.trim() || ACQ_CLIENT_KINDS.find(x => x[0] === kind.value)[1], url: cUrl.value.trim(),
      username: cUser.value.trim(), password: cPass.value, category: 'kinora-movies', category_tv: 'kinora-tv', enabled: true });
    const cOut = h('p.small', { role: 'status' });
    const cSave = h('button.btn.primary', { type: 'button', onclick: async () => {
      if (!cUrl.value.trim()) return toast(T.wizUrlNeeded, true);
      await POST('/api/admin/acquire/clients', client()); toast(T.saved);
      cUrl.value = cUser.value = cPass.value = cName.value = ''; put(cOut); refresh();
    } }, T.save);

    const iKind = h('select', null, [['torznab', T.acqKindTorznab], ['newznab', T.acqKindNewznab]].map(([v, l]) => h('option', { value: v }, l)));
    const iName = h('input', { type: 'text' });
    const iUrl = h('input', { type: 'url', placeholder: 'http://192.168.1.10:9117/api/v2.0/indexers/all/results/torznab' });
    const iKey = h('input', { type: 'password', autocomplete: 'off' });
    const indexer = () => ({ id: 0, kind: iKind.value, name: iName.value.trim() || iKind.options[iKind.selectedIndex].text, url: iUrl.value.trim(),
      api_key: iKey.value, categories: '', priority: 25, enabled: true });
    const iOut = h('p.small', { role: 'status' });
    const iSave = h('button.btn.primary', { type: 'button', onclick: async () => {
      if (!iUrl.value.trim()) return toast(T.wizUrlNeeded, true);
      await POST('/api/admin/acquire/indexers', indexer()); toast(T.saved);
      iUrl.value = iKey.value = iName.value = ''; put(iOut); refresh();
    } }, T.save);

    put(body, h('div.wiz-text', null, h('p', null, T.wizDlWhat), h('p.muted.small', null, T.wizDlOptional)), have,
      h('details.a-opt', null, h('summary', null, T.acqAddClient), h('div.a-opt-b.a-form', null,
        h('div.grid2', null, field(T.acqClientKind, kind), field(T.acqName, cName)), field(T.acqClientUrl, cUrl),
        h('div.grid2', null, userField, h('label.field', null, passLabel, cPass)), cOut,
        h('div.a-form-foot', null, acqTestButton('/api/admin/acquire/clients/test', client, cOut), cSave))),
      h('details.a-opt', null, h('summary', null, T.acqAddIndexer), h('div.a-opt-b.a-form', null,
        h('div.grid2', null, field(T.acqName, iName), field(T.acqKind, iKind)), field(T.acqUrl, iUrl, T.acqUrlHint), field(T.acqApiKey, iKey), iOut,
        h('div.a-form-foot', null, acqTestButton('/api/admin/acquire/indexers/test', indexer, iOut), iSave))),
      h('div.form-row', null, h('label.switch-row', null, h('span', null, T.acqEnable), enabled), h('div.hint', null, T.wizDlEnableHint)),
      h('p.small', null, h('a', { href: '#/admin/acqsettings' }, T.wizDlMore)));
    return async () => { if (enabled.checked !== !!s.enabled) await api('PUT', '/api/admin/acquire/settings', { enabled: enabled.checked }); };
  },

  // remote access: the addresses on this network, the Tailscale / Cloudflare / proxy guides, the public URL
  async remote(body) {
    const [c, s] = await Promise.all([GET('/api/admin/connect'), GET('/api/admin/settings')]);
    const kindLabel = { lan: T.lan, tailscale: 'Tailscale', other: T.otherNet };
    const pub = h('input', { type: 'url', value: s.values.public_url || '', placeholder: 'https://kinora.example.org', disabled: !!s.env.public_url });
    put(body, h('div.wiz-text', null, h('p', null, T.wizRemoteWhat)),
      c.addresses.length > 0 && h('ul.wiz-list.small', null, c.addresses.map(a => h('li', null, h('b', null, kindLabel[a.kind]), ' · ', h('code.mono', null, a.url),
        ' ', h('button.btn.small.ghost', { type: 'button', onclick: () => copyText(a.url) }, T.copy)))),
      remoteGuides(),
      h('div.a-form.wiz-form', null, field(T.publicUrl, pub, T.wizPublicHint), s.env.public_url && h('div.env-lock', null, T.envLocked(s.env.public_url))));
    return async () => {
      const v = pub.value.trim();
      if (s.env.public_url || v === (s.values.public_url || '')) return;
      try { await api('PATCH', '/api/admin/settings', { public_url: v }, { quiet: true }); }
      catch (e) { toast(e.message, true); pub.focus(); return false; }
    };
  },

  // what is set up, and a link to the Admin page of each part that is not
  async summary(body) {
    const st = await GET('/api/admin/setup/status');
    const row = (ok, text, href) => h('li', null, h('span', { class: ok ? 'ok' : 'muted' }, ok ? icon('check') : '–'), ' ', text,
      !ok && href && [' ', h('a.small', { href }, T.wizSetLater)]);
    const dl = st.downloads;
    put(body, h('ul.wiz-list.wiz-sum', null,
      row(st.libraries > 0, st.libraries > 0 ? T.wizSumLibs(st.libraries, st.movies, st.series, st.episodes) : T.wizSumNoLibs, '#/admin/libraries'),
      row(st.metadata.tmdb, st.metadata.tmdb ? T.wizSumTmdb : T.wizSumNoTmdb, '#/admin/metadata'),
      row(true, st.encoder.hardware ? T.wizSumHw(srv(st.encoder.label)) : T.wizSumSw(srv(st.encoder.label))),
      row(dl.clients > 0 && dl.indexers > 0 && dl.enabled, dl.clients || dl.indexers ? T.wizSumDl(dl.clients, dl.indexers) + (dl.enabled ? '' : ' · ' + T.wizSumDlOff) : T.wizSumNoDl, '#/admin/acqsettings'),
      row(!!st.remote.public_url || st.remote.tailscale, st.remote.public_url ? T.wizSumPublic(st.remote.public_url) : st.remote.tailscale ? T.wizSumTailscale : T.wizSumLocal, '#/admin/connect')),
      h('p.muted.small', null, T.wizSumLater));
    return null;
  },
};

function viewLogin() {
  const user = h('input', { type: 'text', autocomplete: 'username' });
  const pw = h('input', { type: 'password', autocomplete: 'current-password' });
  const form = h('form', { onsubmit: async e => {
    e.preventDefault();
    await POST('/api/login', Object.assign({ username: user.value, password: pw.value }, APP && APP.device()), { noRedirect: true });
    await loadMe(); go('#/profiles');
  } }, field(T.username, user), field(T.password, pw), h('button.btn.primary.big', { type: 'submit', class: 'wide' }, T.login),
  state.status.quickconnect_enabled && h('div.auth-or', null, h('span', null, T.or)),
  state.status.quickconnect_enabled && h('a.btn.big.wide', { href: '#/login/code' }, T.qcLoginBtn));
  authPage(T.loginTitle, (state.status.server_name || T.appName) + ' – ' + T.loginLede, form);
}

// Sign in with a code: this browser shows a 6-digit code, a signed-in device approves it
// (profile menu → Connect a device); the approved poll sets the session cookie.
function viewLoginCode() {
  let alive = true, timer = 0, tick = 0;
  const stop = () => { clearTimeout(timer); clearInterval(tick); };
  onCleanup(() => { alive = false; stop(); });
  const code = h('div.qc-code', { 'aria-live': 'polite' });
  const left = h('p.qc-left');
  const msg = h('p.qc-msg', { role: 'status' });
  const again = h('button.btn.primary.big.wide', { type: 'button', hidden: true, onclick: () => start() }, T.qcNewCode);
  const end = (text, bad) => {
    stop(); code.classList.add('off'); put(left); put(msg, text); msg.classList.toggle('bad', !!bad);
    again.hidden = false; again.focus();
  };
  async function start() {
    stop(); again.hidden = true; code.classList.remove('off'); code.textContent = '––– –––'; put(msg); put(left);
    let ini;
    try { ini = await POST('/api/quickconnect/initiate', APP ? APP.device() : { platform: 'web' }, { noRedirect: true, quiet: true }); }
    catch (e) { if (alive) end(e.message || T.netErr, true); return; }
    if (!alive) return;
    code.textContent = ini.code.slice(0, 3) + ' ' + ini.code.slice(3);
    code.setAttribute('aria-label', ini.code.split('').join(' '));
    const until = Date.now() + ini.expires_in * 1000;
    const count = () => {
      const s = Math.max(0, Math.round((until - Date.now()) / 1000));
      if (!s) return end(T.qcExpired);
      put(left, T.qcExpiresIn(Math.floor(s / 60) + ':' + String(s % 60).padStart(2, '0')));
    };
    count(); tick = setInterval(count, 1000);
    const every = (ini.poll_interval || 2) * 1000;
    const poll = async () => {
      let r = null;
      try { r = await GET('/api/quickconnect/status?secret=' + encodeURIComponent(ini.secret), { noRedirect: true, quiet: true }); }
      catch (e) { if (e.status === 410) { if (alive) end(T.qcExpired); return; } } // a network hiccup: poll again
      if (!alive) return;
      if (r && r.status === 'approved') { stop(); put(msg, T.qcSignedIn); await loadMe(); return go(takeAfter() || '#/home'); }
      if (r && r.status === 'denied') return end(T.qcLoginDenied, true);
      timer = setTimeout(poll, every);
    };
    timer = setTimeout(poll, every);
  }
  const box = h('div.qc-box', null, code, left, msg, again,
    h('ol.qc-steps', null, h('li', null, T.qcStep1), h('li', null, T.qcStep2(T.connectDevice)), h('li', null, T.qcStep3)),
    h('a.btn.ghost.wide', { href: '#/login/password' }, T.qcUsePassword));
  authPage(T.qcLoginTitle, T.qcLoginLede, box);
  start();
}

async function viewJoin(code) {
  let info;
  try { info = await GET('/api/invites/' + encodeURIComponent(code), { quiet: true, noRedirect: true }); }
  catch { return authPage(T.joinTitle, T.inviteInvalid, h('a.btn', { href: '#/login' }, T.loginTitle)); }
  const user = h('input', { type: 'text', autocomplete: 'username' });
  const pw = h('input', { type: 'password', autocomplete: 'new-password' });
  const pw2 = h('input', { type: 'password', autocomplete: 'new-password' });
  const form = h('form', { onsubmit: async e => {
    e.preventDefault();
    if (pw.value !== pw2.value) return toast(T.pwMismatch, true);
    await POST('/api/join', { code, username: user.value, password: pw.value }, { noRedirect: true });
    await loadMe(); go('#/profiles');
  } }, field(T.username, user), field(T.password, pw), field(T.password2, pw2), h('button.btn.primary.big', { type: 'submit', class: 'wide' }, T.join));
  authPage(T.joinTitle, T.joinLede(info.server_name), form);
}

// a one-time link (Jellyfin import, or one an admin made) that sets an account's password and signs it in
async function viewSetPassword(code) {
  let info;
  try { info = await GET('/api/password-links/' + encodeURIComponent(code), { quiet: true, noRedirect: true }); }
  catch { return authPage(T.setPwTitle, T.setPwInvalid, h('a.btn', { href: '#/login' }, T.loginTitle)); }
  const pw = h('input', { type: 'password', autocomplete: 'new-password' });
  const pw2 = h('input', { type: 'password', autocomplete: 'new-password' });
  const user = h('input', { type: 'text', autocomplete: 'username', value: info.username, readonly: true });
  const form = h('form', { onsubmit: async e => {
    e.preventDefault();
    if (pw.value !== pw2.value) return toast(T.pwMismatch, true);
    await POST('/api/password-links/' + encodeURIComponent(code), { password: pw.value }, { noRedirect: true });
    state.me = null; await loadMe(); go('#/profiles');
  } }, field(T.username, user), field(T.password, pw), field(T.password2, pw2), h('button.btn.primary.big', { type: 'submit', class: 'wide' }, T.setPwSave));
  authPage(T.setPwTitle, T.setPwLede(info.username, info.server_name), form);
}

// the address of a password link, as the user will open it
async function passwordLinkURL(code) {
  const base = (await GET('/api/admin/settings')).values.public_url || location.origin;
  return base + '/#/setpw/' + code;
}
function passwordLinkBox(url, expires) {
  return h('div', null, h('p.muted.small', null, T.pwLinkHint, ' ', T.expires + ': ' + new Date(expires * 1000).toLocaleString(T.locale)),
    qrImg(url), h('div.url-row', null, h('code.mono', null, url), h('button.btn', { onclick: () => copyText(url) }, T.copy)));
}

// ===================================================================== profiles
const COLORS = ['#c97c5d', '#b5a27a', '#8fa37a', '#7fa6a0', '#c2b4a3', '#a3826b', '#b07a7a', '#8a9a8f'];

async function viewProfiles(editMode) {
  await loadMe();
  const me = state.me;
  const grid = h('div.who-grid');
  for (const p of me.profiles) {
    grid.append(h('button.who-item', { onclick: () => editMode ? profileEditor(p) : selectProfile(p), 'aria-label': p.name },
      h('div.face', { style: { background: p.color } }, p.name.slice(0, 1).toUpperCase(), p.has_pin && h('span.lock', null, icon('lock'))),
      h('span', null, p.name), p.is_kids && h('small', null, T.kids)));
  }
  const kidsNow = me.profile && me.profile.is_kids;
  if (me.profiles.length < 8 && !kidsNow) grid.append(h('button.who-item', { onclick: () => profileEditor(null) }, h('div.face.add', null, icon('plus')), h('span', null, T.addProfile)));
  put(app(), h('div.center', null, h('div.who', null,
    h('h1', null, editMode ? T.editProfiles : T.whoWatches), grid,
    h('div', { class: 'mt6' }, !kidsNow && h('a.btn.ghost', { href: editMode ? '#/profiles' : '#/profiles/edit' }, editMode ? T.done : T.editProfiles),
      // profiles belong to one account; another family member signs in with their own
      !editMode && !me.profile_locked && h('button.btn.ghost', { onclick: () => POST('/api/logout').then(() => { state.me = null; go('#/login'); }) }, T.switchUser)))));
  grid.querySelector('button')?.focus();
}

async function selectProfile(p) {
  const done = async () => { await loadMe(); state.facets = null; go(takeAfter() || '#/home'); };
  if (!p.has_pin) { await POST(`/api/profiles/${p.id}/select`, {}); return done(); }
  const pin = h('input.pin-input', { type: 'password', inputmode: 'numeric', maxlength: 8, autocomplete: 'off', 'aria-label': T.pinLabel });
  const m = modal(T.pinTitle(p.name), h('form', { onsubmit: async e => {
    e.preventDefault();
    try { await POST(`/api/profiles/${p.id}/select`, { pin: pin.value }); m.close(); done(); }
    catch { pin.value = ''; pin.focus(); }
  } }, pin, h('div.foot', null, h('button.btn.primary', { type: 'submit' }, T.enter))));
}

function profileEditor(p) {
  const isNew = !p;
  p = p || { name: '', color: COLORS[Math.floor(Math.random() * COLORS.length)], is_kids: false, max_rating_level: null, sub_langs: T.defaultLangs, audio_langs: T.defaultLangs, sub_mode: 'auto', autoplay_next: true, max_bitrate: 0, snow: false };
  let color = p.color;
  const name = h('input', { type: 'text', value: p.name, maxlength: 30 });
  const sw = h('div.swatches', null, COLORS.map(c => h('button' + (c === color ? '.on' : ''), { type: 'button', style: { background: c }, 'aria-label': c, onclick: e => { color = c; sw.querySelectorAll('button').forEach(b => b.classList.remove('on')); e.currentTarget.classList.add('on'); } })));
  const pin = h('input', { type: 'password', inputmode: 'numeric', maxlength: 8, placeholder: p.has_pin ? T.pinKeep : '' });
  const removePin = h('input', { type: 'checkbox' });
  const kids = h('input', { type: 'checkbox', checked: p.is_kids });
  const lvl = h('select', null, h('option', { value: '-1' }, T.noLimit), T.ratingLevels.map((l, i) => h('option', { value: i, selected: p.max_rating_level === i }, l)));
  const subs = h('input', { type: 'text', value: p.sub_langs });
  const auds = h('input', { type: 'text', value: p.audio_langs });
  const mode = h('select', null, Object.entries(T.subModes).map(([k, v]) => h('option', { value: k, selected: p.sub_mode === k }, v)));
  const auto = h('input', { type: 'checkbox', checked: p.autoplay_next });
  const snowOn = h('input', { type: 'checkbox', checked: p.snow === true });
  const skipSel = h('select', null, Object.entries(T.skipModes).map(([k, v]) => h('option', { value: k, selected: (p.skip_intro || 'manual') === k }, v)));
  const br = h('select', null, T.bitrateOpts.map(([v, l]) => h('option', { value: v, selected: p.max_bitrate === v }, l)));
  const body = h('div', null, field(T.profileName, name), h('label.field', null, h('span', null, T.color), sw), field(T.pin, pin),
    p.has_pin && h('label.check', null, removePin, T.removePin),
    h('label.check', null, kids, T.isKids), field(T.maxRating, lvl),
    h('div.grid2', null, field(T.subLangs, subs, T.langHint), field(T.audioLangs, auds, T.langHint)),
    h('div.grid2', null, field(T.subMode, mode), field(T.maxBitrate, br)), field(T.skipMode, skipSel), h('label.check', null, auto, T.autoplayNext), h('label.check', null, snowOn, T.snow));
  modal(isNew ? T.addProfile : p.name, body, close => [
    !isNew && state.me.profiles.length > 1 && h('button.btn.danger', { class: 'push', onclick: async () => {
      if (!(await confirmBox(T.confirmDelete(p.name)))) return;
      await api('DELETE', '/api/profiles/' + p.id); close(); viewProfiles(true);
    } }, T.delete),
    h('button.btn', { onclick: close }, T.cancel),
    h('button.btn.primary', { onclick: async () => {
      const body = { name: name.value, color, is_kids: kids.checked, max_rating_level: +lvl.value, sub_langs: subs.value, audio_langs: auds.value, sub_mode: mode.value, autoplay_next: auto.checked, max_bitrate: +br.value, skip_intro: skipSel.value, snow: snowOn.checked };
      if (pin.value) body.pin = pin.value; else if (removePin.checked) body.pin = '';
      if (isNew) await POST('/api/profiles', body); else await api('PATCH', '/api/profiles/' + p.id, body);
      close(); toast(T.saved); viewProfiles(!isNew);
    } }, T.save)]);
}

// ===================================================================== cards & shelves
function itemSub(it) {
  if (it.kind === 'episode') return [it.series_title, it.season_num != null && it.episode_num != null ? T.ep(it.season_num, it.episode_num) : ''].filter(Boolean).join('  ');
  if (it.kind === 'series') return [it.year, it.episode_count ? T.epCount(it.episode_count) : ''].filter(Boolean).join('  ');
  return it.year || '';
}
function artImg(it, kind, alt) {
  const wrap = h('div.art');
  const ph = h('div.ph', null, it.kind === 'episode' && it.series_title ? it.series_title : it.title, it.year && it.kind !== 'episode' ? h('small', null, it.year) : null);
  // a movie or series poster has no fallback on the server, so a known-missing one would only 404
  if (kind === 'poster' && it.has_poster === false && (it.kind === 'movie' || it.kind === 'series')) { wrap.classList.add('noart'); wrap.append(ph); return wrap; }
  const img = h('img', { src: imgURL(it, kind), alt: alt || '', loading: 'lazy', decoding: 'async' });
  img.addEventListener('error', () => { img.replaceWith(ph); wrap.classList.add('noart'); });
  wrap.append(img);
  return wrap;
}
function card(it, style = 'poster', opts = {}) {
  const art = artImg(it, style === 'poster' ? 'poster' : (it.kind === 'episode' ? 'thumb' : 'backdrop'), it.title);
  const ud = it.user_data || {};
  if (style === 'landscape' && ud.position > 0 && it.runtime_sec) art.append(h('div.prog', null, h('i', { style: { width: Math.min(100, ud.position / it.runtime_sec * 100) + '%' } })));
  if (ud.played && it.kind !== 'season') art.append(h('span.tick', { title: T.watched }, icon('check')));
  else if (it.kind === 'series' && it.unplayed_count) art.append(h('span.count', { title: T.unwatchedTitle }, it.unplayed_count));
  if (it.version_count > 1 && style === 'poster') art.append(h('span.vcount', { title: T.versionsN(it.version_count) }, T.versionsShort(it.version_count)));
  const href = '#/item/' + it.id;
  const el = h('div.card.' + style, { tabindex: 0, role: 'link', 'aria-label': it.title, onclick: e => { if (!e.target.closest('.more')) location.hash = opts.playOnClick ? '#/play/' + it.id : href; }, onkeydown: e => { if (e.key === 'Enter') location.hash = opts.playOnClick ? '#/play/' + it.id : href; } },
    art, h('div.cap', null, h('b', null, it.title), h('span', null, itemSub(it)), opts.reason && h('span.why', null, opts.reason)));
  el._item = it; el._reason = opts.reason || '';
  if (opts.menu) {
    const btn = h('button.more', { 'aria-label': T.moreActions, onclick: e => { e.stopPropagation(); opts.menu(it, btn); } }, icon('more', true));
    el.append(btn);
  }
  return el;
}
function popMenu(anchor, entries) {
  document.querySelectorAll('.menu-pop.floating').forEach(m => m.remove());
  const r = anchor.getBoundingClientRect();
  const close = () => { pop.remove(); document.removeEventListener('mousedown', off); };
  const pop = h('div.menu-pop.floating', { role: 'menu', style: { position: 'fixed', top: (r.bottom + tok('--s2')) + 'px', left: Math.min(r.left, innerWidth - tok('--col-side')) + 'px', right: 'auto' } },
    entries.filter(Boolean).map(([label, fn]) => h('button', { role: 'menuitem', onclick: () => { close(); fn(); } }, label)));
  document.body.append(pop);
  const off = e => { if (!pop.contains(e.target)) close(); };
  setTimeout(() => document.addEventListener('mousedown', off), 0);
  pop.querySelector('button')?.focus();
  onCleanup(close);
}
function shelf(title, items, style, opts = {}) {
  const track = h('div.shelf-track.' + style, { role: 'list' }, items.map(it => card(it, style, { menu: opts.menu, reason: opts.reasons && srv(opts.reasons[it.id]), playOnClick: opts.playOnClick })));
  return h('section.shelf', { 'aria-label': title }, h('div.shelf-head', null, h('h2', null, title), opts.more && h('a', { href: opts.more }, T.more)), track);
}

// ===================================================================== home
async function viewHome() {
  const main = shell('home');
  announceArrivals();
  const data = await GET('/api/home');
  const hideMenu = hideAs => (it, btn) => popMenu(btn, [
    [hideAs === 'series' ? T.hideSeries : T.hide, async () => {
      const id = hideAs === 'series' && it.series_id ? it.series_id : it.id;
      await POST(`/api/items/${id}/hide`, { hidden: true }); toast(T.hiddenToast); viewHome();
    }],
    [T.markPlayed, async () => { await POST(`/api/items/${it.id}/played`, { played: true }); viewHome(); }],
    [T.details, () => go('#/item/' + it.id)],
  ]);
  if (!data.rows.length) {
    const admin = state.me.user.is_admin;
    main.append(h('div.empty', { class: 'pt9' }, h('h1', null, T.homeEmptyTitle), h('p', null, admin ? T.homeEmptyAdmin : T.homeEmptyUser),
      admin && h('a.btn.primary', { href: '#/admin/libraries' }, T.addLibrary)));
    return;
  }
  let hero = null;
  if (data.hero) { hero = heroBlock(data.hero, data.rows); main.append(hero); }
  main.append(monthRecapCard() || '');
  for (const row of data.rows) {
    const landscape = row.style === 'landscape';
    main.append(shelf(srv(row.title), row.items, row.style, { more: row.more, reasons: row.reasons, playOnClick: landscape, menu: hideMenu(row.hide_as) }));
  }
  // the beam follows focus: a poster case lit by the remote becomes tonight's feature on the facade
  if (hero) {
    let t;
    main.addEventListener('focusin', e => {
      const c = e.target.closest('.card');
      if (!c || !c._item || !main.contains(c)) return;
      clearTimeout(t);
      t = setTimeout(() => fillHero(hero, c._item, c._reason), tok('--t-out'));
    });
    onCleanup(() => clearTimeout(t));
  }
}
// the first week of a month: last month from the Napló in one line, once per profile and month until dismissed;
// the server keeps the closed month, so the card goes on every device at once (the local key covers an older server)
function monthRecapCard() {
  const now = new Date(), p = state.me.profile;
  if (now.getDate() > 7 || !p) return null;
  const from = new Date(now.getFullYear(), now.getMonth() - 1, 1), to = new Date(now.getFullYear(), now.getMonth(), 1);
  const key = `kinora.monthRecap.${p.id}.${from.getFullYear()}-${from.getMonth() + 1}`;
  const ym = `${from.getFullYear()}-${String(from.getMonth() + 1).padStart(2, '0')}`;
  if ((p.journal_recap_seen || '') >= ym) return null;
  try { if (localStorage.getItem(key)) return null; } catch { /* private mode: show it */ }
  const box = h('section.month-recap', { hidden: true, 'aria-label': T.journal });
  GET(`/api/journal?from=${Math.floor(from / 1000)}&to=${Math.floor(to / 1000)}`, { quiet: true }).then(r => {
    const evs = r.events || [];
    if (!evs.length || !box.isConnected) return;
    const films = evs.filter(e => e.kind === 'movie').length, eps = evs.length - films;
    const g = new Map();
    for (const e of evs) for (const x of e.genres || []) g.set(x, (g.get(x) || 0) + 1);
    const top = [...g].sort((a, b) => b[1] - a[1])[0];
    const month = from.toLocaleDateString(T.locale, { month: 'long' });
    const per = from.getFullYear() === now.getFullYear() ? 'year' : 'last';
    const hide = () => {
      try { localStorage.setItem(key, '1'); } catch { /* ignore */ }
      p.journal_recap_seen = ym;
      api('POST', '/api/journal/recap-seen', { month: ym }, { quiet: true }).catch(() => { /* the local key still hides it here */ });
      box.remove();
    };
    put(box,
      h('div.mr-body', null,
        h('p.mr-kick', null, T.mrTitle(month.charAt(0).toUpperCase() + month.slice(1))),
        h('p.mr-line', null, h('em', null, T.jrFilms(films)), ' ' + T.jrEps(eps), top ? ' ' + T.mrMostly(top[0].toLowerCase()) : '')),
      h('a.btn.primary', { href: `#/journal?p=${per}&b=${from.getMonth()}`, onclick: hide }, T.mrOpen),
      h('button.mr-x', { type: 'button', 'aria-label': T.close, title: T.close, onclick: hide }, '✕'));
    box.hidden = false;
  }).catch(() => { /* no Napló today: the home page goes on without it */ });
  return box;
}

function heroBlock(it, rows) {
  let reason = '';
  for (const r of rows) if (r.reasons && r.reasons[it.id]) { reason = srv(r.reasons[it.id]); break; }
  const sec = h('section.hero', null, h('div.hero-img'), h('div.hero-body'));
  sec._shown = 0;
  fillHero(sec, it, reason);
  return sec;
}
/** fillHero paints one title onto the facade (the initial hero, and whichever case the remote lights up). */
function fillHero(sec, it, reason) {
  if (sec._shown === it.id) return;
  sec._shown = it.id;
  const bg = sec.querySelector('.hero-img');
  const img = new Image();
  bg.classList.add('swap');
  img.onload = () => { if (sec._shown !== it.id) return; bg.style.backgroundImage = 'url("' + img.src + '")'; bg.classList.remove('swap'); };
  img.onerror = () => { if (sec._shown !== it.id) return; bg.style.backgroundImage = 'none'; bg.classList.remove('swap'); };
  img.src = imgURL(it, it.kind === 'episode' ? 'thumb' : 'backdrop');
  const ud = it.user_data || {};
  const title = it.kind === 'episode' && it.series_title ? it.series_title : it.title;
  const sub = it.kind === 'episode' ? T.ep(it.season_num, it.episode_num) + ' · ' + it.title : '';
  reason = reason || '';
  put(sec.querySelector('.hero-body'),
    reason && h('div.reason', null, reason[0].toUpperCase() + reason.slice(1)),
    h('h1', null, title),
    h('div.meta', null, sub && h('span', null, sub), it.year && h('span', null, it.year), it.content_rating && h('span.badge', null, it.content_rating), it.runtime_sec > 0 && it.kind !== 'series' && h('span', null, fmtRuntime(it.runtime_sec)), it.genres && it.genres.length && h('span.muted', null, it.genres.slice(0, 3).join(', '))),
    it.overview && h('p', null, it.overview),
    h('div.actions', null,
      h('button.btn.primary.big', { onclick: () => it.kind === 'series' ? go('#/item/' + it.id) : go('#/play/' + it.id) }, icon('play'), ud.position > 0 ? T.resume(fmtTime(ud.position)) : T.play),
      h('a.btn.big', { href: '#/item/' + it.id }, icon('info'), T.details)));
}

// ===================================================================== search
async function viewSearch(q) {
  const main = shell('search');
  main.append(h('div.page-head', null, h('h1', null, T.searchTitle(q))));
  const s = $('.search-box input'); // focus before the fetch, so keys typed meanwhile land in the field
  if (s) { s.focus(); s.setSelectionRange(s.value.length, s.value.length); }
  const items = await GET('/api/search?group=1&q=' + encodeURIComponent(q)); // one card per series
  main.append(items.length ? h('div.grid', null, items.map(it => card(it, 'poster'))) : h('p.empty', null, T.searchEmpty));
}

// ===================================================================== library browser
async function facets(lib) { return GET('/api/facets' + (lib ? '?library=' + lib : '')); }

async function viewLibrary(libId, q) {
  const main = shell('lib' + libId);
  const libs = state.libs || (state.libs = await GET('/api/libraries'));
  const lib = libs.find(l => l.id === libId);
  const f = await facets(libId);
  state.facets = f;
  const sel = { genre: new Set(q.getAll('genre')), tag: new Set(q.getAll('tag')), year_from: q.get('year_from') || '', year_to: q.get('year_to') || '', min_rating: q.get('min_rating') || '',
    played: q.get('played') || '', favorite: q.get('favorite') === 'true', sort: q.get('sort') || 'title', order: q.get('order') || (q.get('sort') === 'added' ? 'desc' : 'asc'), q: q.get('q') || '' };
  const grid = h('div.grid');
  const countEl = h('span.muted');
  const sentinel = h('div', { class: 'sentinel' });
  let offset = 0, total = 0, loading = false, gen = 0;

  const params = () => {
    const p = new URLSearchParams({ library: libId, sort: sel.sort, order: sel.order });
    sel.genre.forEach(g => p.append('genre', g)); sel.tag.forEach(t => p.append('tag', t));
    for (const k of ['year_from', 'year_to', 'min_rating', 'played', 'q']) if (sel[k]) p.set(k, sel[k]);
    if (sel.favorite) p.set('favorite', 'true');
    return p;
  };
  const load = async reset => {
    if (loading && !reset) return;
    if (reset) { offset = 0; put(grid); gen++; }
    const my = gen;
    if (!reset && offset >= total) return;
    loading = true;
    const p = params(); p.set('offset', offset); p.set('limit', 60);
    try {
      const r = await GET('/api/items?' + p);
      if (my !== gen) return;
      total = r.total; offset += r.items.length;
      countEl.textContent = T.titlesN(total);
      grid.append(...r.items.map(it => card(it, 'poster')));
      if (!total) grid.append(h('p.empty', null, lib && lib.count === 0 ? T.libEmpty : T.noResults));
    } finally { loading = false; }
  };
  const refresh = () => {
    const p = params(); p.delete('library');
    history.replaceState(null, '', '#/library/' + libId + '?' + p);
    paintFiltersBtn();
    load(true);
  };
  const chipList = (list, set) => h('div.chips', null, list.slice(0, 60).map(x => {
    const c = h('button.chip' + (set.has(x.value) ? '.on' : ''), { type: 'button', 'aria-pressed': set.has(x.value) ? 'true' : 'false', onclick: () => {
      set.has(x.value) ? set.delete(x.value) : set.add(x.value);
      c.classList.toggle('on'); c.setAttribute('aria-pressed', set.has(x.value)); refresh();
    } }, x.label || x.value, h('span.n', null, x.count));
    return c;
  }));
  const inp = (key, ph, type = 'number') => { const i = h('input', { type, placeholder: ph, value: sel[key], 'aria-label': ph }); i.addEventListener('input', debounce(() => { sel[key] = i.value.trim(); refresh(); }, 350)); return i; };
  const playedSel = h('select', { 'aria-label': T.watched, onchange: e => { sel.played = e.target.value; refresh(); } },
    [['', T.any], ['false', T.unwatched], ['true', T.watched]].map(([v, l]) => h('option', { value: v, selected: sel.played === v }, l)));
  const fav = h('input', { type: 'checkbox', checked: sel.favorite, onchange: e => { sel.favorite = e.target.checked; refresh(); } });
  const filters = h('aside.filters', { 'aria-label': T.filters },
    f.genres.length > 0 && [h('h2', null, T.genres), chipList(f.genres, sel.genre)],
    f.tags.length > 0 && [h('h2', null, T.tags), chipList(f.tags, sel.tag)],
    h('h2', null, T.year), h('div.grid2', null, inp('year_from', T.yearFrom), inp('year_to', T.yearTo)),
    h('h2', null, T.minRating), inp('min_rating', T.egRating),
    h('h2', null, T.watched), playedSel,
    h('label.check', { class: 'mt3' }, fav, T.onlyFav),
    h('div', { class: 'stack mt4' },
      h('a.btn.ghost', { href: '#/library/' + libId }, T.clear),
      !state.me.profile.is_kids && h('button.btn', { onclick: saveAsSmart }, T.saveSmart)));
  const sortSel = h('select', { 'aria-label': T.sort, onchange: e => { sel.sort = e.target.value; refresh(); } }, f.sorts.map(s => h('option', { value: s.value, selected: s.value === sel.sort }, lbl(T.sortLabels, s.value, s.label))));
  const orderBtn = h('button.btn.icon', { type: 'button', onclick: () => { sel.order = sel.order === 'asc' ? 'desc' : 'asc'; paintOrder(); refresh(); } });
  const paintOrder = () => {
    const l = T.order + ': ' + (sel.order === 'asc' ? T.asc : T.desc);
    orderBtn.setAttribute('aria-label', l); orderBtn.title = l;
    put(orderBtn, icon(sel.order === 'asc' ? 'sortAsc' : 'sortDesc'));
  };
  paintOrder();
  const qInput = inp('q', T.filterTitle, 'search');
  const filtersBtn = h('button.btn.filters-toggle', { type: 'button', 'aria-expanded': 'false', onclick: () => filtersBtn.setAttribute('aria-expanded', String(filters.classList.toggle('open'))) });
  const paintFiltersBtn = () => {
    const n = sel.genre.size + sel.tag.size + ['year_from', 'year_to', 'min_rating', 'played'].filter(k => sel[k]).length + (sel.favorite ? 1 : 0);
    put(filtersBtn, icon('filter'), T.filters, n ? h('span.n', null, '· ' + n) : null);
  };
  paintFiltersBtn();
  main.append(h('div.page-head', null, h('div', null, h('h1', null, lib ? lib.name : ''), countEl)),
    h('div.browse', null, filters, h('div', null,
      h('div.toolbar', null, filtersBtn, searchField(qInput), h('div.sort-ctl', null, sortSel, orderBtn)),
      grid, sentinel)));
  const io = new IntersectionObserver(es => { if (es.some(e => e.isIntersecting)) load(false); }, { rootMargin: '600px' });
  io.observe(sentinel);
  onCleanup(() => io.disconnect());
  await load(true);

  function saveAsSmart() {
    const rules = [{ field: 'library', op: 'is', value: String(libId) }];
    sel.genre.forEach(g => rules.push({ field: 'genre', op: 'is', value: g }));
    sel.tag.forEach(t => rules.push({ field: 'tag', op: 'is', value: t }));
    if (sel.year_from) rules.push({ field: 'year', op: 'gte', value: sel.year_from });
    if (sel.year_to) rules.push({ field: 'year', op: 'lte', value: sel.year_to });
    if (sel.min_rating) rules.push({ field: 'community_rating', op: 'gte', value: sel.min_rating });
    if (sel.played) rules.push({ field: 'played', op: 'is', value: sel.played === 'true' });
    if (sel.favorite) rules.push({ field: 'favorite', op: 'is', value: true });
    if (sel.q) rules.push({ field: 'title', op: 'contains', value: sel.q });
    sessionStorage.setItem('kinora-draft-rules', JSON.stringify({ match: 'all', rules, sort: sel.sort, order: sel.order }));
    go('#/collections/new?smart=1');
  }
}

// ===================================================================== item detail
// the version of a film the profile picked or played last; the detail page opens on it
const versionPick = {};
const chLabel = c => c === 1 ? '1.0' : c === 2 ? '2.0' : c === 6 ? '5.1' : c === 8 ? '7.1' : c > 0 ? c + 'ch' : '';
const editionLabel = e => (T.editions && T.editions[e]) || e;
function versionPicker(it, versions) {
  const group = h('div.versions', { role: 'radiogroup', 'aria-label': T.versionsHead });
  for (const v of versions) {
    const cur = v.id === it.id;
    const label = v.edition ? editionLabel(v.edition) : [v.resolution, v.hdr].filter(Boolean).join(' ') || T.versionPlain;
    const top = v.edition ? [v.resolution, v.hdr].filter(Boolean).join(' ') : '';
    const sub = [v.video_codec && v.video_codec.toUpperCase(), v.audio_codec && [v.audio_codec.toUpperCase(), chLabel(v.channels)].filter(Boolean).join(' '), v.size && fmtBytes(v.size), v.library].filter(Boolean).join(' · ');
    group.append(h('button.version' + (cur ? '.on' : ''), { role: 'radio', 'aria-checked': cur ? 'true' : 'false', onclick: () => {
      if (cur) return;
      versionPick[it.imdb_id] = v.id;
      history.replaceState(null, '', '#/item/' + v.id);
      viewItem(v.id);
    } }, h('b', null, label, top && h('span.vres', null, top)), sub && h('span', null, sub),
      v.position > 0 && v.runtime_sec ? h('i.vprog', null, h('i', { style: { width: Math.min(100, v.position / v.runtime_sec * 100) + '%' } })) : null));
  }
  return group;
}
// the audio and subtitle language this profile starts the film / series with; an episode stands for its series
async function titleLangsDialog(it) {
  const l = await GET(`/api/items/${it.id}/langs`);
  const series = it.kind !== 'movie';
  const opts = (list, cur) => list.map(c => h('option', { value: c, selected: c === cur }, langName(c)));
  const audio = h('select', null, h('option', { value: '' }, T.tlProfile), opts(l.audio.includes(l.audio_lang) || !l.audio_lang ? l.audio : [l.audio_lang, ...l.audio], l.audio_lang));
  const sub = h('select', null, h('option', { value: '' }, T.tlProfile), h('option', { value: 'off', selected: l.sub_lang === 'off' }, T.subOff),
    opts(l.subs.includes(l.sub_lang) || !l.sub_lang || l.sub_lang === 'off' ? l.subs : [l.sub_lang, ...l.subs], l.sub_lang));
  modal(T.tlTitle, h('div.a-form', null,
    h('p.small.muted', null, series ? T.tlSeriesHint : T.tlMovieHint),
    field(T.audioTrack, audio, l.audio.length ? null : T.tlNoAudio), field(T.subtitle, sub, l.subs.length ? null : T.tlNoSubs)),
  close => [h('button.btn', { onclick: close }, T.cancel), h('button.btn.primary', { onclick: async () => {
    await api('PUT', `/api/items/${it.id}/langs`, { audio_lang: audio.value, sub_lang: sub.value }); close(); toast(T.saved);
  } }, T.save)]);
}

// ---- ratings from other sites and the box office (GET …/scores, filled in after the page renders)
const SCORE_SITES = { imdb: 'IMDb', rt: 'Rotten Tomatoes', metacritic: 'Metacritic', tmdb: 'TMDB' };
const compactNum = v => new Intl.NumberFormat(T.locale, { notation: 'compact', maximumFractionDigits: 1 }).format(v);
const usd = (v, compact) => new Intl.NumberFormat(T.locale, { style: 'currency', currency: 'USD', currencyDisplay: 'narrowSymbol',
  notation: compact ? 'compact' : 'standard', maximumFractionDigits: compact ? 1 : 0 }).format(v);

/** A row of site scores: the figure on the site's own scale, a hairline meter, the site and what it counts. */
function scoreStrip(ratings) {
  if (!ratings || !ratings.length) return null;
  return h('ul.scores', { 'aria-label': T.scoresLabel }, ratings.map(r => {
    const site = SCORE_SITES[r.source] || r.source;
    const pct = r.max === 100;
    const fig = h('span.score-fig', null, pct ? num(r.value, 0) : num(r.value, 1), h('small', null, pct ? (r.source === 'rt' ? '%' : '/100') : '/10'));
    const sub = r.votes ? T.scoreVotes(compactNum(r.votes)) : (r.source === 'rt' || r.source === 'metacritic') ? T.scoreCritics : '';
    const kids = [fig, h('span.score-meter', { style: '--v:' + Math.max(0, Math.min(1, r.value / r.max)).toFixed(3), 'aria-hidden': 'true' }),
      h('span.score-site', null, site), sub && h('span.score-sub', null, sub)];
    return h('li', null, r.url
      ? h('a.score', { href: r.url, target: '_blank', rel: 'noopener noreferrer', title: T.scoreOpen(site) }, kids)
      : h('div.score', null, kids));
  }));
}

/** Budget against worldwide gross: two ledger figures and two bars on one scale. */
function boxOfficeBlock(s, compact) {
  if (!s || !(s.budget > 0 || s.revenue > 0)) return null;
  const top = Math.max(s.budget || 0, s.revenue || 0);
  const row = (label, v, cls) => v > 0 && h('div.box-row' + cls, null,
    h('span.box-label', null, label),
    h('span.box-bar', { 'aria-hidden': 'true' }, h('i', { style: '--v:' + (v / top).toFixed(3) })),
    h('b.box-fig', { title: usd(v, false) }, usd(v, true)));
  const ratio = s.budget > 0 && s.revenue > 0 ? s.revenue / s.budget : 0;
  return h('div.box' + (compact ? '.compact' : ''), null,
    row(T.budget, s.budget, '.budget'),
    row(T.revenue + (compact ? '' : ' · ' + T.revenueHint), s.revenue, '.revenue'),
    ratio > 0 && h('p.box-ratio', null, ratio >= 1 ? T.boxRatio(num(ratio, ratio >= 10 ? 0 : 1)) : T.boxShort(num(ratio * 100, 0))),
    !compact && h('p.muted.small', null, T.boxNote));
}

/** Loads the scores into the given slots; slots stay empty (and hidden) when nothing is known. */
async function fillScores(url, ratingsSlot, boxSlot, compact, onRatings) {
  let s;
  try { s = await GET(url); } catch { return; }
  const strip = scoreStrip(s.ratings);
  if (ratingsSlot) {
    const hint = !s.omdb && state.me.user.is_admin && !compact && h('p.muted.small.score-hint', null, T.scoresOmdbHint);
    put(ratingsSlot, strip, hint);
    ratingsSlot.hidden = !strip && !hint;
  }
  if (strip && onRatings) onRatings();
  if (boxSlot) {
    const box = boxOfficeBlock(s, compact);
    if (box) { put(boxSlot, compact ? box : [h('h2', null, T.boxOffice), box]); boxSlot.hidden = false; }
  }
}

async function viewItem(id) {
  const main = shell('item');
  const d = await GET('/api/items/' + id);
  const it = d.item, ud = it.user_data || {};
  if (d.versions && it.imdb_id) {
    // open on the picked / last played version unless the URL already points at it
    const want = versionPick[it.imdb_id] ?? d.preferred_version;
    if (want && want !== it.id && d.versions.some(v => v.id === want)) {
      versionPick[it.imdb_id] = want;
      history.replaceState(null, '', '#/item/' + want);
      return viewItem(want);
    }
    versionPick[it.imdb_id] = it.id;
  }
  document.title = it.title + ' – ' + T.appName;
  const kidsP = state.me.profile.is_kids;
  const bg = h('div.hero-img');
  const bimg = new Image();
  bimg.onload = () => { bg.style.backgroundImage = `url("${bimg.src}")`; };
  bimg.src = imgURL(it, 'backdrop');
  const playTarget = it.kind === 'movie' || it.kind === 'episode' ? it : d.play_next;
  let playLabel = T.play;
  if (playTarget) {
    const pud = playTarget.user_data || {};
    if (playTarget.kind === 'episode' && playTarget !== it) playLabel = pud.position > 0 ? T.resumeEp(playTarget.season_num, playTarget.episode_num) : T.playEp(playTarget.season_num, playTarget.episode_num);
    else if (pud.position > 0) playLabel = T.resume(fmtTime(pud.position));
  }
  const video = (it.streams || []).find(s => s.type === 'video');
  const audios = (it.streams || []).filter(s => s.type === 'audio');
  const subs = (it.streams || []).filter(s => s.type === 'subtitle');
  const facts = h('div.facts', null,
    it.year && h('span.badge', null, it.year), it.content_rating && h('span.badge', null, it.content_rating),
    it.runtime_sec > 0 && it.kind !== 'series' && h('span.badge', null, fmtRuntime(it.runtime_sec)),
    it.height > 0 && h('span.badge', null, resLabel(it.width, it.height)), it.hdr && h('span.badge.hot', null, it.hdr),
    it.video_codec && h('span.badge', null, it.video_codec.toUpperCase()), it.community_rating && h('span.badge.own-rating', null, icon('star'), num(it.community_rating, 1)));
  // films and series get the sites' scores under the facts (the lone star goes once they arrive) and the money below
  const hasScores = it.kind === 'movie' || it.kind === 'series';
  const scoresSlot = hasScores && h('div.scores-slot', { hidden: true });
  const boxSlot = hasScores && it.kind === 'movie' && h('section.box-sec', { hidden: true });
  if (hasScores) fillScores(`/api/items/${it.id}/scores`, scoresSlot, boxSlot, false, () => { const b = facts.querySelector('.own-rating'); b && b.remove(); });

  const playedBtn = h('button.btn.icon' + (ud.played ? '.on' : ''), { 'aria-pressed': ud.played ? 'true' : 'false', title: ud.played ? T.markUnplayed : T.markPlayed, 'aria-label': ud.played ? T.markUnplayed : T.markPlayed, onclick: async () => {
    await POST(`/api/items/${it.id}/played`, { played: !ud.played }); viewItem(id);
  } }, icon('check'));
  const favBtn = h('button.btn.icon' + (ud.favorite ? '.on' : ''), { 'aria-pressed': ud.favorite ? 'true' : 'false', title: T.favorite, 'aria-label': T.favorite, onclick: async () => {
    const v = !ud.favorite; await POST(`/api/items/${it.id}/favorite`, { favorite: v }); ud.favorite = v;
    favBtn.classList.toggle('on', ud.favorite); favBtn.setAttribute('aria-pressed', ud.favorite);
  } }, icon('heart', ud.favorite));
  const stars = h('div.stars', { role: 'radiogroup', 'aria-label': T.myRating });
  const paintStars = v => stars.querySelectorAll('button').forEach((b, i) => b.classList.toggle('on', v != null && i < v));
  for (let i = 1; i <= 5; i++) stars.append(h('button', { 'aria-label': T.starsN(i), onclick: async () => {
    const v = ud.stars === i ? null : i; await POST(`/api/items/${it.id}/stars`, { stars: v }); ud.stars = v; paintStars(v);
  } }, icon('star')));
  paintStars(ud.stars);
  const hideBtn = h('button.btn.icon', { title: ud.hidden ? T.unhide : T.hide, 'aria-label': ud.hidden ? T.unhide : T.hide, onclick: async () => {
    await POST(`/api/items/${it.id}/hide`, { hidden: !ud.hidden }); toast(ud.hidden ? T.saved : T.hiddenToast); viewItem(id);
  } }, icon(ud.hidden ? 'eye' : 'eyeOff'));
  const canParty = it.kind === 'movie' || it.kind === 'episode' || !!playTarget;
  const canDl = it.kind === 'movie' || it.kind === 'episode';
  const moreBtn = h('button.btn', { 'aria-haspopup': 'menu', onclick: e => popMenu(e.currentTarget, [
    !kidsP && [T.addToCollection, () => addToCollectionMenu(it, moreBtn)],
    canParty && [T.startParty, async () => { const r = await POST('/api/party', { item_id: (playTarget || it).id, everyone: false }); go(`#/play/${(playTarget || it).id}?party=${r.code}`); }],
    canDl && [T.subSearch + '…', () => subSearchDialog(it)],
    canDl && [T.download + '…', () => modal(T.download, h('div', null, h('p.muted', null, T.dlKept), downloadWidget(it)), close => [h('button.btn', { onclick: close }, T.close)])],
    state.me.user.is_admin && (it.kind === 'movie' || it.kind === 'series') && [T.fixMatch, () => fixMatchDialog(it)],
    state.me.user.is_admin && (it.kind === 'series' || it.kind === 'season') && [T.segAnalyze, async () => { const r = await POST('/api/admin/segments/analyze', { item_id: it.id }); toast(T.segQueued(r.seasons)); }],
  ]) }, icon('more', true), T.moreBtn);
  const langBtn = (it.kind === 'movie' || it.kind === 'series' || it.kind === 'episode') &&
    h('button.btn', { title: T.tlTitle, onclick: () => titleLangsDialog(it) }, icon('cc'), T.tlBtn);
  const tagBox = !kidsP && tagEditor(it);
  const sideInfo = h('dl.side-info', null,
    it.genres && [h('dt', null, T.genres), h('dd', null, it.genres.join(', '))],
    video && [h('dt', null, T.video), h('dd', null, `${video.codec.toUpperCase()} ${it.width}×${it.height}${it.hdr ? ' ' + it.hdr : ''}${it.bitrate ? ', ' + num(it.bitrate / 1e6, 1) + ' Mbps' : ''}`)],
    audios.length > 0 && [h('dt', null, T.audio), h('dd', null, audios.map(a => `${(a.lang || T.unknownLang)} ${a.codec}${a.channels ? ' ' + a.channels + 'ch' : ''}`).join(', '))],
    subs.length > 0 && [h('dt', null, T.subtitles), h('dd', null, subs.map(s => (s.lang || T.unknownLang) + (s.forced ? ` (${T.forced})` : '') + (s.text_based ? '' : ` [${T.imageSub}]`)).join(', '))],
    it.container && [h('dt', null, T.file), h('dd', null, `${it.container.toUpperCase()}, ${fmtBytes(it.size)}`)],
    d.collections && d.collections.length > 0 && [h('dt', null, T.inCollections), h('dd', null, d.collections.map((c, i) => [i ? ', ' : '', h('a', { href: '#/collections/' + c.id }, c.name)]))]);

  const titleBlock = it.kind === 'episode'
    ? [h('div.sub', null, h('a', { href: '#/item/' + it.series_id }, it.series_title), '  ', T.ep(it.season_num, it.episode_num)), h('h1', null, it.title)]
    : it.kind === 'season' ? [h('div.sub', null, h('a', { href: '#/item/' + it.series_id }, it.series_title)), h('h1', null, it.title)] : [h('h1', null, it.title)];

  main.append(h('section.detail-hero', null, bg, h('div.detail', null,
    h('div.poster', null, artImg(it, it.kind === 'episode' ? 'thumb' : 'poster', it.title)),
    h('div', null, titleBlock, facts, scoresSlot, it.overview && h('p.overview', null, it.overview),
      d.versions && h('div.versions-wrap', null, h('div.vhead', null, T.versionsN(d.versions.length)), versionPicker(it, d.versions)),
      h('div.actions', null,
        playTarget && h('button.btn.primary.big', { onclick: () => go('#/play/' + playTarget.id) }, icon('play'), playLabel),
        playedBtn, favBtn, hideBtn, stars, langBtn, moreBtn)))));
  main.append(h('div.below', null,
    sideInfo.childElementCount > 0 && h('section', null, h('h2', null, T.data), sideInfo), // a series without metadata has nothing to list
    boxSlot,
    tagBox && h('section', null, h('h2', null, T.tags), tagBox)));

  if (it.kind === 'series' && d.seasons) {
    const tabs = h('div.tabs', { role: 'tablist' });
    const list = h('div.episodes');
    let seasonGen = 0;
    const showSeason = async (s, btn) => {
      const my = ++seasonGen;
      tabs.querySelectorAll('.chip').forEach(c => { c.classList.remove('on'); c.setAttribute('aria-selected', 'false'); });
      btn.classList.add('on'); btn.setAttribute('aria-selected', 'true');
      const eps = await GET(`/api/items/${s.id}/children`);
      if (my === seasonGen) put(list, ...eps.map(episodeRow));
    };
    let first = null;
    d.seasons.forEach(s => {
      const b = h('button.chip', { role: 'tab', onclick: () => showSeason(s, b) }, T.season(s.season_num), s.unplayed_count ? h('span.n', null, s.unplayed_count) : null);
      tabs.append(b);
      if (!first || (d.play_next && d.play_next.parent_id === s.id)) first = [s, b];
    });
    main.append(h('h2', { class: 'mt7' }, T.seasons), tabs, list);
    if (first) showSeason(...first);
  }
  if (it.kind === 'season' && d.episodes) main.append(h('h2', { class: 'mt7 mb3' }, T.episodes), h('div.episodes', null, d.episodes.map(episodeRow)));
  if (it.kind === 'episode' && d.next) main.append(shelf(T.nextEp, [d.next], 'landscape', { playOnClick: true }));
  if (d.similar && d.similar.length) main.append(shelf(T.similar, d.similar, 'poster'));
}

// Subtitle search (OpenSubtitles). onPicked gets the new subtitle stream after a download.
// It lives in an element of its own so the player can show it in its popover (a modal would
// stay hidden behind a fullscreen player).
function subSearchPanel(item, onPicked) {
  const profileLangs = (state.me.profile.sub_langs || '').split(',').filter(Boolean);
  const pool = [...new Set([...profileLangs, 'hun', 'eng', 'ger'])];
  let langs = profileLangs.length ? profileLangs.slice() : ['hun', 'eng'];
  const chips = h('div.chips.sub-langs', { role: 'group', 'aria-label': T.subSearchLangs });
  const list = h('div.sub-results', { 'aria-live': 'polite' });
  const root = h('div.sub-search', null, chips, list);
  let gen = 0;
  const paintChips = () => put(chips, ...pool.map(l => h('button.chip' + (langs.includes(l) ? '.on' : ''), { 'aria-pressed': langs.includes(l) ? 'true' : 'false', onclick: () => {
    langs = langs.includes(l) ? langs.filter(x => x !== l) : [...langs, l];
    if (!langs.length) langs = [l];
    paintChips(); load();
  } }, langName(l))));
  const row = r => {
    const flags = [
      r.hash_match && h('span.badge.hot', { title: T.subHashHint }, T.subHash),
      r.trusted && h('span.badge', null, T.subTrusted),
      r.hearing_impaired && h('span.badge', { title: T.subHIHint }, 'HI'),
      r.forced && h('span.badge', null, T.forced),
      r.machine_made && h('span.badge.warn', null, T.subMachine),
    ];
    const btn = h('button.btn' + (r.have ? '' : '.primary'), { disabled: !!r.have, onclick: async () => {
      btn.disabled = true; put(btn, T.subDownloading);
      try {
        const res = await POST(`/api/items/${item.id}/subtitles/download`, { file_id: r.file_id, lang: r.lang, release: r.release || r.file_name || '', hearing_impaired: !!r.hearing_impaired, forced: !!r.forced });
        r.have = true; put(btn, icon('check'), T.subHave); btn.classList.remove('primary');
        toast(res.remaining >= 0 ? T.subDownloadedLeft(res.remaining) : T.subDownloaded);
        onPicked && onPicked(res.stream);
      } catch { btn.disabled = false; put(btn, T.download); }
    } }, r.have ? [icon('check'), T.subHave] : T.download);
    return h('div.sub-row', null,
      h('span.sub-lang', null, (r.lang || '?').toUpperCase()),
      h('div.sub-main', null, h('b', { title: r.file_name || r.release }, r.release || r.file_name || T.unknownLang),
        h('div.sub-meta', null, ...flags.filter(Boolean), h('span.muted.small', null, [T.subDownloads(r.downloads), r.uploader].filter(Boolean).join(' · ')))),
      btn);
  };
  async function load() {
    const my = ++gen;
    put(list, h('p.muted', null, T.subSearching));
    let res;
    try { res = await GET(`/api/items/${item.id}/subtitles/search?langs=${encodeURIComponent(langs.join(','))}`, { quiet: true }); }
    catch (e) { if (my === gen) put(list, h('p.bad', null, e.message || T.netErr)); return; }
    if (my !== gen) return;
    if (!res.enabled) { chips.remove(); put(list, h('p.note-off', null, state.me.user.is_admin ? T.subNotConfiguredAdmin : T.subNotConfigured)); return; }
    if (!res.results.length) { put(list, h('p.muted', null, T.subNone)); return; }
    put(list, ...res.results.map(row), h('p.muted.small.sub-credit', null, T.subCredit));
  }
  paintChips(); load();
  return root;
}
function subSearchDialog(item) {
  modal(T.subSearch, subSearchPanel(item, null), close => [h('button.btn', { onclick: close }, T.close)]).box.classList.add('wide');
}

function episodeRow(e) {
  const ud = e.user_data || {};
  const art = artImg(e, 'thumb', e.title);
  if (ud.position > 0 && e.runtime_sec) art.append(h('div.prog', null, h('i', { style: { width: Math.min(100, ud.position / e.runtime_sec * 100) + '%' } })));
  return h('div.ep.card' + (ud.played ? '.watched' : ''), { tabindex: 0, role: 'link', onclick: ev => { if (!ev.target.closest('button')) go('#/play/' + e.id); }, onkeydown: ev => { if (ev.key === 'Enter' && ev.target === ev.currentTarget) go('#/play/' + e.id); } },
    art,
    h('div', null, h('h3', null, e.episode_num != null ? `${e.episode_num}. ` : '', e.title), h('p', null, e.overview || ''), h('span.muted.small', null, e.runtime_sec ? fmtRuntime(e.runtime_sec) : '')),
    h('div.row-gap', null,
      h('button.btn.icon' + (ud.played ? '.on' : ''), { title: ud.played ? T.markUnplayed : T.markPlayed, 'aria-label': ud.played ? T.markUnplayed : T.markPlayed, onclick: async ev => {
        const btn = ev.currentTarget;
        await POST(`/api/items/${e.id}/played`, { played: !ud.played }); ud.played = !ud.played;
        btn.classList.toggle('on', ud.played); btn.closest('.ep').classList.toggle('watched', ud.played);
      } }, icon('check'))));
}

async function addToCollectionMenu(it, anchor) {
  const cols = (await GET('/api/collections')).filter(c => c.kind === 'manual');
  const target = it.kind === 'season' || it.kind === 'episode' ? it.series_id || it.id : it.id;
  popMenu(anchor, [
    ...cols.map(c => [c.name, async () => { await POST(`/api/collections/${c.id}/items/${target}`); toast(T.addedTo(c.name)); }]),
    [T.newCollection, () => {
      const name = h('input', { type: 'text', maxlength: 80 });
      const shared = h('input', { type: 'checkbox' });
      modal(T.newManual, h('div', null, field(T.collName, name), h('label.check', null, shared, T.shared)), close => [h('button.btn', { onclick: close }, T.cancel),
        h('button.btn.primary', { onclick: async () => {
          const c = await POST('/api/collections', { name: name.value, kind: 'manual', shared: shared.checked });
          await POST(`/api/collections/${c.id}/items/${target}`); close(); toast(T.addedTo(c.name));
        } }, T.create)]);
    }],
  ]);
}

function tagEditor(it) {
  let tags = [...(it.tags || [])];
  const box = h('div.tags', { 'aria-label': T.tags });
  const listId = 'tagopts' + it.id;
  const dl = h('datalist', { id: listId });
  GET('/api/facets').then(f => put(dl, ...f.tags.map(t => h('option', { value: t.value })))).catch(() => {});
  const save = async () => { const r = await api('PUT', `/api/items/${it.id}/tags`, { tags }); tags = r.tags; render(); };
  const input = h('input', { type: 'text', placeholder: T.addTag, list: listId, 'aria-label': T.addTag, onkeydown: e => {
    if (e.key === 'Enter' && input.value.trim()) { e.preventDefault(); tags.push(input.value.trim()); input.value = ''; save(); }
  } });
  const render = () => put(box, icon('tag'), ...tags.map(t => h('span.chip', null, t, h('button', { class: 'chip-x', 'aria-label': T.removeTag(t), onclick: () => { tags = tags.filter(x => x !== t); save(); } }, icon('x')))), input, dl);
  render();
  return h('div', { class: 'mb3' }, box);
}

function downloadWidget(it) {
  const q = h('select', { 'aria-label': T.downloadQ, style: { width: 'auto' } },
    h('option', { value: 'original' }, T.original), ['1080p', '720p', '480p', '360p'].map(v => h('option', { value: v }, v)));
  if (DEV) q.value = '720p'; // to a phone: a smaller file is the usual choice
  const btn = h('button.btn', { onclick: async () => {
    btn.disabled = true;
    try {
      const r = await POST(`/api/items/${it.id}/download`, { quality: q.value });
      if (DEV) { await deviceWant(it, r, q.value); return; }
      if (r.url) { location.href = r.url; return; }
      toast(T.dlStarted);
    } finally { btn.disabled = false; }
  } }, icon('dl'), DEV ? T.dlToDevice : T.download);
  return h('span.row-gap', null, q, btn);
}

// ===================================================================== downloads to the device (Android app)
// window.KinoraOffline (apps/android, KinoraOffline.java): the server prepares the MP4, then the phone's
// DownloadManager fetches it, and the app's launcher plays it without the server. A download belongs to the
// profile that asked for it (offline only the last profile's downloads show, so a kids profile never sees
// another's); what was watched offline comes back here and is sent to the server.
const DEV = (() => { try { return window.KinoraOffline && window.KinoraOffline.available() ? window.KinoraOffline : null; } catch { return null; } })();
function devCall(fn) { try { const r = JSON.parse(fn()); return r && r.error ? null : r; } catch { return null; } }

// asked on the title page: remember it on the device; the hand-over waits until the server has the file
async function deviceWant(it, job, quality) {
  const p = state.me.profile;
  const meta = { series: it.series_title || '', position: (it.user_data && it.user_data.position) || 0,
    episode: it.kind === 'episode' && it.season_num != null ? `S${String(it.season_num).padStart(2, '0')}E${String(it.episode_num || 0).padStart(2, '0')}` : '' };
  const r = devCall(() => DEV.want(JSON.stringify({ id: it.id, key: job.key || '', title: it.title, meta, quality, profile: p.id })));
  if (!r) return toast(T.dlDeviceFailed, true);
  if (r.state === 'done') return toast(T.dlOnDevice);
  if (job.url) await deviceStart(it.id, job.url); // the original file: nothing to prepare
  toast(T.dlDeviceStarted);
  deviceHandOff();
}

// the subtitles go along as text (fetched with this session), the file through a stream-token URL
async function deviceStart(id, url) {
  let subs = [];
  try {
    const d = await GET('/api/items/' + id, { quiet: true });
    const text = (d.item.streams || []).filter(st => st.type === 'subtitle' && st.text_based).slice(0, 10);
    for (const st of text) {
      const r = await fetch(`/api/items/${id}/subtitles/${st.type_index}`, { credentials: 'same-origin' });
      if (r.ok) subs.push({ lang: st.lang, label: st.title || (T.langNames[st.lang] || st.lang), text: await r.text() });
    }
  } catch { subs = []; } // without subtitles rather than not at all
  return devCall(() => DEV.start(id, url, JSON.stringify(subs)));
}

// Hands every title the server has finished to the phone. Runs while the web client is open (every 5 s while
// something is being prepared); a title prepared while the app was closed goes over the next time it opens.
let handOffTimer = 0;
async function deviceHandOff() {
  clearTimeout(handOffTimer);
  if (!DEV || !state.me || !state.me.profile) return;
  const mine = (devCall(() => DEV.list()) || []).filter(x => x.state === 'preparing');
  if (!mine.length) return;
  let jobs = [];
  try { jobs = (await GET('/api/downloads', { quiet: true })).jobs; } catch { /* offline for a moment */ }
  for (const x of mine) {
    const j = jobs.find(j => j.key === x.key);
    if (j && j.status === 'done' && j.file_url) await deviceStart(x.id, j.file_url);
  }
  handOffTimer = setTimeout(deviceHandOff, 5000);
}

// on every start with a profile: tell the phone who is watching, send what was watched offline
let deviceSyncedFor = 0;
async function deviceSync() {
  if (!DEV || !state.me || !state.me.profile) return;
  const p = state.me.profile;
  if (deviceSyncedFor === p.id) return;
  deviceSyncedFor = p.id;
  devCall(() => DEV.setProfile(JSON.stringify({ id: p.id, name: p.name, kids: !!p.is_kids })));
  for (const q of devCall(() => DEV.takeProgress(p.id)) || []) {
    await POST(`/api/items/${q.id}/progress`, { position: q.position, ended: !!q.ended }, { quiet: true }).catch(() => {});
  }
  deviceHandOff();
}

// the Downloads page: what this profile has on the phone
function deviceSection(onChange) {
  if (!DEV) return null;
  const pid = state.me.profile.id;
  const list = (devCall(() => DEV.list()) || []).filter(x => (x.profiles || []).includes(pid));
  const label = x => [x.meta && x.meta.series, x.meta && x.meta.episode, x.title].filter(Boolean).join(' · ');
  const stateText = x => x.state === 'done' ? T.dlOnDevice : x.state === 'downloading' ? T.dlTransferring(Math.round((x.progress || 0) * 100))
    : x.state === 'preparing' ? T.dlPreparing : T.dlDeviceFailed;
  return h('section', { class: 'mt4' }, h('h2', null, T.dlDeviceTitle),
    list.length ? h('div.scroll-x', null, h('table.tbl', null, h('tbody', null, list.map(x => h('tr', null,
      h('td', null, h('a', { href: '#/item/' + x.id }, label(x))), h('td', null, x.quality || ''),
      h('td', { class: x.state === 'failed' ? 'bad' : x.state === 'done' ? 'ok' : '' }, stateText(x)),
      h('td', null, h('div.row-gap', null,
        x.state === 'failed' && h('button.btn', { onclick: async () => {
          const it = (await GET('/api/items/' + x.id)).item;
          const job = await POST(`/api/items/${x.id}/download`, { quality: x.quality || '720p' });
          await deviceWant(it, job, x.quality || '720p'); onChange();
        } }, T.retry),
        h('button.btn.ghost', { onclick: () => { devCall(() => DEV.remove(x.id, pid)); onChange(); } }, T.delete)))))))) : h('p.muted', null, T.dlDeviceEmpty),
    h('p.muted.small', null, T.dlDeviceHint));
}

async function fixMatchDialog(it) {
  const qi = h('input', { type: 'text', value: it.title });
  const yi = h('input', { type: 'number', value: it.year || '' });
  const idi = h('input', { type: 'number', placeholder: T.egTmdbId });
  const results = h('div', { class: 'stack mt3' });
  const kind = it.kind === 'series' ? 'series' : 'movie';
  let m;
  const apply = async tmdbId => { await POST(`/api/items/${it.id}/metadata/match`, { tmdb_id: +tmdbId }); m.close(); toast(T.saved); viewItem(it.id); };
  const search = async () => {
    const r = await GET(`/api/admin/metadata/search?kind=${kind}&q=${encodeURIComponent(qi.value)}&year=${yi.value || 0}`);
    put(results, ...r.slice(0, 10).map(x => h('div.entry', null,
      x.poster ? h('img', { src: x.poster, alt: '', class: 'match-thumb' }) : null,
      h('div', { class: 'grow-min' }, h('b', null, x.title), ' ', h('span.muted', null, x.year || ''), h('div.small.muted', null, (x.overview || '').slice(0, 140))),
      h('button.btn', { onclick: () => apply(x.id) }, T.matchApply))));
    if (!r.length) results.append(h('p.muted', null, T.searchEmpty));
  };
  m = modal(T.fixMatch, h('div', null, h('div.grid2', null, field(T.title, qi), field(T.year, yi)), h('button.btn', { onclick: search }, T.matchSearch),
    results, h('div', { class: 'mt4' }, field(T.tmdbId, idi))), close => [h('button.btn', { onclick: close }, T.cancel), h('button.btn.primary', { onclick: () => idi.value && apply(idi.value) }, T.matchApply)]);
}

// ===================================================================== collections
async function viewCollections() {
  const main = shell('collections');
  const cols = await GET('/api/collections');
  const kids = state.me.profile.is_kids;
  main.append(h('div.page-head', null, h('h1', null, T.collections), !kids && h('div.row-gap', null,
    h('button.btn', { onclick: newManual }, icon('plus'), T.newManual), h('a.btn.primary', { href: '#/collections/new?smart=1' }, icon('plus'), T.newSmart))));
  if (!cols.length) { main.append(h('p.empty', null, T.collEmpty)); return; }
  const shelves = await Promise.all(cols.map(c => GET(`/api/collections/${c.id}?limit=20`, { quiet: true }).catch(() => null)));
  for (const [i, c] of cols.entries()) {
    const r = shelves[i];
    if (!r) continue;
    const head = [c.name, c.kind === 'smart' ? ' · smart' : '', c.shared ? ' · ' + T.sharedShort : ''].join('');
    main.append(shelf(head, r.items, 'poster', { more: '#/collections/' + c.id }));
  }
  function newManual() {
    const name = h('input', { type: 'text', maxlength: 80 });
    const shared = h('input', { type: 'checkbox' });
    const pin = h('input', { type: 'checkbox' });
    modal(T.newManual, h('div', null, field(T.collName, name), h('label.check', null, shared, T.shared), h('label.check', null, pin, T.pinned)), close => [h('button.btn', { onclick: close }, T.cancel),
      h('button.btn.primary', { onclick: async () => { const c = await POST('/api/collections', { name: name.value, kind: 'manual', shared: shared.checked, pinned: pin.checked }); close(); go('#/collections/' + c.id); } }, T.create)]);
  }
}

async function viewCollection(id) {
  const main = shell('collections');
  const r = await GET(`/api/collections/${id}?limit=500`);
  const c = r.collection, kids = state.me.profile.is_kids;
  const pinBtn = !kids && h('button.btn' + (c.pinned ? '.on' : ''), { onclick: async () => { await api('PATCH', '/api/collections/' + id, { pinned: !c.pinned }); viewCollection(id); } }, c.pinned ? T.pinnedOn : T.pinned);
  main.append(h('div.page-head', null, h('div', null, h('h1', null, c.name), h('span.muted', null, `${T.titlesN(r.total)} · ${c.kind === 'smart' ? T.smart : T.manual}${c.shared ? ' · ' + T.sharedShort : ''}`)),
    !kids && h('div.row-gap', null, pinBtn,
      c.kind === 'smart' && h('a.btn', { href: `#/collections/${id}/edit` }, T.edit),
      c.kind === 'manual' && h('button.btn', { onclick: () => renameColl(c) }, T.edit),
      h('button.btn.danger', { onclick: async () => { if (await confirmBox(T.confirmDelete(c.name))) { await api('DELETE', '/api/collections/' + id); go('#/collections'); } } }, T.delete))));
  const grid = h('div.grid');
  const items = r.items;
  const render = () => put(grid, ...items.map((it, i) => card(it, 'poster', { menu: kids || c.kind !== 'manual' ? null : (x, btn) => popMenu(btn, [
    i > 0 && [T.moveUp, () => move(i, -1)], i < items.length - 1 && [T.moveDown, () => move(i, 1)],
    [T.removeFromColl, async () => { await api('DELETE', `/api/collections/${id}/items/${x.id}`); items.splice(i, 1); render(); toast(T.removedFrom(c.name)); }]]) })));
  const move = async (i, d) => { [items[i], items[i + d]] = [items[i + d], items[i]]; render(); await api('PUT', `/api/collections/${id}/order`, { items: items.map(x => x.id) }); };
  render();
  main.append(items.length ? grid : h('p.empty', null, T.noResults));
  function renameColl(c) {
    const name = h('input', { type: 'text', value: c.name });
    const shared = h('input', { type: 'checkbox', checked: c.shared });
    modal(T.edit, h('div', null, field(T.collName, name), h('label.check', null, shared, T.shared)), close => [h('button.btn', { onclick: close }, T.cancel),
      h('button.btn.primary', { onclick: async () => { await api('PATCH', '/api/collections/' + c.id, { name: name.value, shared: shared.checked }); close(); viewCollection(c.id); } }, T.save)]);
  }
}

async function viewCollectionEdit(id, q) {
  const main = shell('collections');
  const f = state.facets || (state.facets = await facets());
  const libs = state.libs || (state.libs = await GET('/api/libraries'));
  const allCols = await GET('/api/collections');
  let c = { name: '', kind: 'smart', shared: false, pinned: false, rules: { match: 'all', rules: [], sort: 'title', order: 'asc', limit: 0 } };
  if (id) c = (await GET(`/api/collections/${id}?limit=1`)).collection;
  else { const draft = sessionStorage.getItem('kinora-draft-rules'); if (draft) { c.rules = JSON.parse(draft); sessionStorage.removeItem('kinora-draft-rules'); } }
  const rs = c.rules || { match: 'all', rules: [] };
  const name = h('input', { type: 'text', value: c.name, maxlength: 80 });
  const shared = h('input', { type: 'checkbox', checked: c.shared });
  const pinned = h('input', { type: 'checkbox', checked: c.pinned });
  const match = h('select', null, h('option', { value: 'all', selected: rs.match !== 'any' }, T.matchAll), h('option', { value: 'any', selected: rs.match === 'any' }, T.matchAny));
  const sort = h('select', null, f.sorts.map(s => h('option', { value: s.value, selected: s.value === (rs.sort || 'title') }, lbl(T.sortLabels, s.value, s.label))));
  const order = h('select', null, h('option', { value: 'asc', selected: rs.order !== 'desc' }, T.asc), h('option', { value: 'desc', selected: rs.order === 'desc' }, T.desc));
  const limit = h('input', { type: 'number', min: 0, value: rs.limit || '' });
  const rows = h('div', { class: 'stack' });
  const previewHead = h('h2', null, T.preview(0));
  const previewGrid = h('div.grid');
  const fieldDesc = name => f.fields.find(x => x.field === name);

  const valueInput = (fd, v) => {
    const opt = (val, label) => h('option', { value: val, selected: String(v) === String(val) }, label);
    switch (fd.type) {
      case 'enum': return h('select', null, fd.enum.map(o => opt(o.value, lbl(T.kindOpts, o.value, o.label))));
      case 'library': return h('select', null, libs.map(l => opt(l.id, l.name)));
      case 'genre': return h('select', null, f.genres.map(g => opt(g.value, g.label || g.value)));
      case 'tag': return h('select', null, f.tags.map(t => opt(t.value, t.value)));
      case 'collection': return h('select', null, allCols.filter(x => x.id !== id).map(x => opt(x.id, x.name)));
      case 'bool': return h('select', null, opt('true', T.yes), opt('false', T.no));
      case 'number': case 'days': return h('input', { type: 'number', value: v ?? '' });
      default: return h('input', { type: 'text', value: v ?? '' });
    }
  };
  const ruleRow = r => {
    const fsel = h('select', { 'aria-label': T.ruleField }, f.fields.map(x => h('option', { value: x.field, selected: x.field === r.field }, lbl(T.ruleFields, x.field, x.label))));
    const osel = h('select', { 'aria-label': T.ruleOp });
    let vin;
    const row = h('div.row-gap.rule-row');
    const rebuild = (keepV) => {
      const fd = fieldDesc(fsel.value);
      put(osel, ...fd.ops.map(o => h('option', { value: o, selected: o === r.op }, T.ops[o] || o)));
      const nv = valueInput(fd, keepV ? r.value : undefined);
      nv.setAttribute('aria-label', T.ruleValue);
      if (vin) vin.replaceWith(nv); vin = nv;
      for (const el of [osel, vin]) { el.onchange = preview; el.oninput = preview; }
    };
    fsel.onchange = () => { r.op = null; rebuild(false); preview(); };
    rebuild(true);
    row.append(fsel, osel, vin, h('button.btn.icon', { 'aria-label': T.delete, onclick: () => { row.remove(); preview(); } }, icon('x')));
    row._get = () => ({ field: fsel.value, op: osel.value, value: row.children[2].value });
    return row;
  };
  for (const r of rs.rules || []) rows.append(ruleRow(r));
  const collect = () => ({ match: match.value, rules: [...rows.children].map(r => r._get()), sort: sort.value, order: order.value, limit: +limit.value || 0 });
  const preview = debounce(async () => {
    try {
      const r = await POST('/api/collections/preview', collect(), { quiet: true });
      previewHead.textContent = T.preview(r.total);
      put(previewGrid, ...r.items.map(it => card(it, 'poster')));
    } catch (e) { previewHead.textContent = e.message; put(previewGrid); }
  }, 300);
  for (const el of [match, sort, order, limit]) { el.onchange = preview; el.oninput = preview; }
  main.append(h('div.page-head', null, h('h1', null, id ? c.name : T.newSmart)),
    h('div.panel', null,
      h('div.grid2', null, field(T.collName, name), h('div.check-col', null, h('label.check', null, shared, T.shared), h('label.check', null, pinned, T.pinned))),
      h('h2', { class: 'mt6' }, T.rules), field('', match), rows,
      h('button.btn', { class: 'mt3', onclick: () => { rows.append(ruleRow({ field: 'genre', op: 'is' })); preview(); } }, icon('plus'), T.addRule),
      h('div.grid2', { class: 'mt4' }, field(T.sort, h('div.row-gap', { class: 'nowrap' }, sort, order)), field(T.limit, limit)),
      h('div.row-gap', { class: 'end' }, h('a.btn', { href: id ? '#/collections/' + id : '#/collections' }, T.cancel),
        h('button.btn.primary', { onclick: async () => {
          const body = { name: name.value, kind: 'smart', rules: collect(), shared: shared.checked, pinned: pinned.checked };
          const r = id ? await api('PATCH', '/api/collections/' + id, body) : await POST('/api/collections', body);
          toast(T.saved); go('#/collections/' + (id || r.id));
        } }, T.save))),
    previewHead, h('div', { class: 'mt3' }, previewGrid));
  preview();
}

// ===================================================================== downloads
async function viewDownloads() {
  const main = shell('downloads');
  main.append(h('div.page-head', null, h('div', null, h('h1', null, T.downloads), h('span.muted', null, T.dlKept))));
  const box = h('div');
  main.append(box);
  let timer, dead = false;
  const render = async () => {
    const r = await GET('/api/downloads');
    if (dead) return; // left while the request was out: start no new timer
    const dev = deviceSection(() => { clearTimeout(timer); render(); });
    if (DEV && (devCall(() => DEV.list()) || []).some(x => x.state === 'preparing' || x.state === 'downloading')) { clearTimeout(timer); timer = setTimeout(render, 2000); }
    if (!r.jobs.length) { put(box, h('p.empty', null, T.dlEmpty), dev); return; }
    put(box, h('div.scroll-x', null, h('table.tbl', null, h('thead', null, h('tr', null, h('th', null, T.title), h('th', null, T.quality), h('th', null, T.status), h('th', null, ''))),
      h('tbody', null, r.jobs.map(j => h('tr', null,
        h('td', null, h('a', { href: '#/item/' + j.item_id }, j.file_name)),
        h('td', null, j.quality),
        h('td', { class: 'td-progress' }, j.status === 'running' ? h('div', null, h('div.bar', null, h('i', { style: { width: Math.round(j.progress * 100) + '%' } })), h('span.small.muted', null, Math.round(j.progress * 100) + '%')) : h('span' + (j.status === 'error' ? '.bad' : ''), { title: srv(j.error) || '' }, T.dlStatus[j.status] + (j.error ? ': ' + srv(j.error) : '')), j.size ? h('div.small.muted', null, fmtBytes(j.size)) : null),
        h('td', null, j.status === 'done' && !DEV && h('a.btn.primary', { href: `/api/downloads/${j.key}/file`, download: j.file_name }, icon('dl'), T.saveFile))))))), dev);
    if (r.jobs.some(j => j.status === 'running' || j.status === 'queued')) { clearTimeout(timer); timer = setTimeout(render, 2000); }
  };
  onCleanup(() => { dead = true; clearTimeout(timer); });
  await render();
}

// ===================================================================== journal (Napló)
// Every finished viewing of the profile, grouped here in the viewer's own time zone.
async function viewJournal(q) {
  const main = shell('journal');
  const now = new Date(), Y = now.getFullYear(), M = now.getMonth();
  const periods = {
    month: () => [new Date(Y, M, 1), new Date(Y, M + 1, 1), 'day'],
    year: () => [new Date(Y, 0, 1), new Date(Y + 1, 0, 1), 'month'],
    last: () => [new Date(Y - 1, 0, 1), new Date(Y, 0, 1), 'month'],
    all: () => [new Date(0), new Date(Y + 1, 0, 1), 'year'],
  };
  const per = periods[q.get('p')] ? q.get('p') : 'year';
  const [from, to, unit] = periods[per]();
  const pick = h('nav.chips.j-period', { 'aria-label': T.jPeriod }, Object.keys(periods).map(k =>
    h('a.chip' + (k === per ? '.on' : ''), { href: '#/journal?p=' + k, 'aria-current': k === per ? 'page' : null }, T.jPeriods[k])));
  const canEdit = jCanEdit();
  // the kicker names the stretch the page shows: the month, the year, or every year
  const kick = per === 'month' ? now.toLocaleDateString(T.locale, { year: 'numeric', month: 'long' })
    : per === 'year' ? String(Y) : per === 'last' ? String(Y - 1) : T.jPeriods.all;
  const headActs = h('div.j-head-btns', null, canEdit && h('button.btn.primary', { type: 'button', onclick: () => jAddViewing() }, icon('plus'), T.jAdd));
  main.append(h('div.page-head.j-head', null,
    h('div.j-title', null, h('p.j-kick', null, kick), h('h1', null, T.journal), h('p.j-lede', null, T.jLede)),
    h('div.j-head-actions', null, pick, headActs)));
  // the same stretch of the period before (month and year so far, last year against the one before it)
  const prevFrom = { month: new Date(Y, M - 1, 1), year: new Date(Y - 1, 0, 1), last: new Date(Y - 2, 0, 1) }[per];
  const prevTo = prevFrom && (per === 'last' ? new Date(Y - 1, 0, 1) : new Date(Math.min(prevFrom.getTime() + (now - from), from.getTime())));
  const span = (a, b) => `/api/journal?from=${Math.floor(a / 1000)}&to=${Math.floor(b / 1000)}`;
  const quiet = p => GET(p, { quiet: true }).catch(() => null);
  const [r, prev, goals, challenges, abandoned] = await Promise.all([GET(span(from, to)), prevFrom ? quiet(span(prevFrom, prevTo)) : null,
    quiet('/api/journal/goals'), quiet('/api/journal/challenges'), quiet('/api/journal/abandoned')]);
  let evs = r.events;
  if (unit === 'year' && evs.length) evs = evs.filter(e => e.at > 0);
  // what does not depend on the period's viewings: challenges and the titles left early
  const extras = () => [jChallenges(challenges || [], canEdit)];
  if (!evs.length) {
    main.append(emptyNote(T.jEmpty, T.jEmptyHint, h('a.btn', { href: '#/home' }, T.home)));
    main.append(h('div.j-pair', null, (per === 'year' || per === 'month') && jGoals(goals || [], [], per, canEdit, headActs), ...extras()));
    return;
  }
  // rewatches: the how-manieth viewing of a film within the period
  const filmKey = e => e.tmdb_id ? 't' + e.tmdb_id : e.title + '|' + (e.year || '');
  const nth = new Map();
  for (const e of evs) if (e.kind === 'movie') { const k = filmKey(e); nth.set(k, (nth.get(k) || 0) + 1); e._nth = nth.get(k); }

  const films = evs.filter(e => e.kind === 'movie'), eps = evs.filter(e => e.kind === 'episode');
  const shows = new Set(eps.map(e => e.series_title || e.title)).size;
  const secs = evs.reduce((a, e) => a + (e.runtime_sec || 0), 0);
  const count = (list, keyOf) => { const m = new Map(); for (const e of list) for (const k of [].concat(keyOf(e))) if (k != null && k !== '') m.set(k, (m.get(k) || 0) + 1); return [...m].sort((a, b) => b[1] - a[1]); };
  // a series counts once per viewing day in the genres, so a binge does not drown out the films
  const genreBase = [...films, ...[...new Map(eps.map(e => [(e.series_title || e.title) + '|' + new Date(e.at * 1000).toDateString(), e])).values()]];
  const genres = count(genreBase, e => e.genres);
  const topGenre = genres[0];
  // the change against the period before, shown only when that period has viewings to compare with
  const pe = prev?.events?.length ? prev.events : null;
  const delta = (d, fmt = num) => pe && h('span.j-delta' + (d > 0 ? '.up' : d < 0 ? '.down' : ''), null,
    h('span', { 'aria-hidden': 'true' }, d > 0 ? '▲ ' : d < 0 ? '▼ ' : ''), (d > 0 ? '+' : d < 0 ? '−' : '±') + fmt(Math.abs(d)));
  const prevSecs = pe ? pe.reduce((a, e) => a + (e.runtime_sec || 0), 0) : 0;
  // under an hour of difference shows in minutes, so a small change does not read as "±0 hours"
  const timeDelta = d => Math.abs(d) < 3600 ? Math.round(d / 60) * 60 : Math.round(d / 3600) * 3600;
  const stat = (label, value, sub, d) => h('div.j-stat', null, h('dt', null, label), h('dd', null, h('span.j-val', null, h('b', null, value), d), sub && h('span.muted.small', null, sub)));
  main.append(h('dl.j-stats', null,
    stat(T.jFilms, num(films.length), '', delta(films.length - (pe || []).filter(e => e.kind === 'movie').length)),
    stat(T.jEpisodes, num(eps.length), shows ? T.jShows(shows) : '', delta(eps.length - (pe || []).filter(e => e.kind === 'episode').length)),
    stat(T.jTime, T.jHours(Math.round(secs / 3600)), secs >= 86400 ? T.jDays(num(secs / 86400, 1)) : '', delta(timeDelta(secs - prevSecs), n => n < 3600 ? T.jMinutes(num(Math.round(n / 60))) : T.jHours(num(Math.round(n / 3600))))),
    topGenre && stat(T.jTopGenre, topGenre[0], T.jShare(Math.round(topGenre[1] / genreBase.length * 100)))));
  if (pe) main.append(h('p.muted.small.j-vs', null, T.jVs[per]));
  else if (prevFrom && prev === null) main.append(h('p.muted.small.j-vs', null, T.jVsFail));
  if (per === 'year') main.append(goalBlock(films.length));
  // the year as a story sits with the page's own actions
  // last year's story only in January 1–15, like the monthly card in the first week
  const today = new Date(), canRecap = per === 'year' || (per === 'last' && today.getMonth() === 0 && today.getDate() <= 15);
  if (canRecap) headActs.prepend(h('button.btn', { type: 'button', onclick: () => openRecap() }, icon('play'), T.jRecap));
  if (canRecap && q.get('recap') === '1' && evs.length) setTimeout(() => openRecap()); // the yearly recap notification opens it

  // timeline: one bar per day, month or year; a click lists that bucket's titles
  const d0 = from.getTime() ? from : new Date(new Date(evs[0].at * 1000).getFullYear(), 0, 1);
  const keyOf = t => { const d = new Date(t * 1000); return unit === 'day' ? d.getDate() : unit === 'month' ? d.getMonth() : d.getFullYear(); };
  const keys = unit === 'day' ? [...Array(new Date(Y, M + 1, 0).getDate())].map((_, i) => i + 1)
    : unit === 'month' ? [...Array(12).keys()]
    : [...Array(Y - d0.getFullYear() + 1)].map((_, i) => d0.getFullYear() + i);
  // on a phone twelve months share one row: three letters each ("márc." and "szept." would run together)
  const narrow = matchMedia('(max-width: 520px)').matches;
  const label = k => {
    if (unit !== 'month') return String(k);
    const s = new Date(2000, k, 1).toLocaleDateString(T.locale, { month: 'short' });
    return narrow ? s.replace(/\.$/, '').slice(0, 3) : s;
  };
  const longLabel = k => unit === 'day' ? new Date(Y, M, k).toLocaleDateString(T.locale, { month: 'long', day: 'numeric' })
    : unit === 'month' ? new Date(d0.getFullYear(), k, 1).toLocaleDateString(T.locale, { year: 'numeric', month: 'long' }) : String(k);
  const buckets = new Map(keys.map(k => [k, []]));
  for (const e of evs) { const b = buckets.get(keyOf(e.at)); if (b) b.push(e); }
  const max = Math.max(1, ...[...buckets.values()].map(b => b.length));
  const detail = h('div.j-detail');
  // ?b= picks the bucket to open with (the home page's monthly recap opens its month)
  const asked = q.get('b') != null && buckets.has(+q.get('b')) ? +q.get('b') : null;
  let sel = asked ?? [...buckets].reverse().find(([, b]) => b.length)[0];
  const bars = h('div.j-bars', { style: { gridTemplateColumns: `repeat(${keys.length}, minmax(${unit === 'day' ? 22 : 24}px, 1fr))` } });
  const showBucket = k => {
    sel = k;
    for (const b of bars.children) b.setAttribute('aria-pressed', String(b._k === k));
    const list = buckets.get(k);
    put(detail, h('h3', null, longLabel(k), h('span.muted', null, ' · ', T.jViewings(list.length))),
      list.length ? h('div.j-posters', null, [...list].reverse().map(posterOf)) : h('p.muted', null, T.jNone));
  };
  const posterOf = e => {
    const art = jArt(e);
    if (e._nth > 1) art.append(h('span.j-again', { title: T.jRewatchN(e._nth) }, T.jRewatchShort(e._nth)));
    if (e.source === 'manual') art.append(h('span.j-manual', { title: T.jManual }, '✎'));
    const when = new Date(e.at * 1000).toLocaleDateString(T.locale, { month: 'short', day: 'numeric' });
    const sub = e.kind === 'episode' ? [e.season != null && e.episode != null ? T.ep(e.season, e.episode) : '', e.title].filter(Boolean).join(' · ') : (e.year || '');
    const body = [art, h('div.cap', null, h('b', null, e.series_title || e.title), sub && h('span', null, sub), h('span.muted', null, when))];
    const name = e.series_title ? e.series_title + ' · ' + e.title : e.title;
    return h('div.j-poster', null, e.item_id ? h('a.j-poster-link', { href: '#/item/' + e.item_id }, body) : h('div.j-poster-link', null, body),
      starPicker(e.rating || 0, async v => {
        try { await api('PUT', `/api/journal/events/${e.id}/rating`, { rating: v }); e.rating = v; return true; } catch { return false; }
      }, T.jRateOf(name)),
      e.source === 'manual' && canEdit && h('button.btn.ghost.small.j-del', { type: 'button', onclick: async () => {
        if (!await confirmBox(T.jDelManual(e.title))) return;
        try { await api('DELETE', '/api/journal/events/' + e.id); toast(T.deleted); route(); } catch { /* shown */ }
      } }, T.delete));
  };
  // days and months still ahead in this month or year: shown faint, not as empty bars to press
  const ahead = k => (per === 'month' && unit === 'day' && k > now.getDate()) || (per === 'year' && unit === 'month' && k > M);
  for (const k of keys) {
    const n = buckets.get(k).length;
    const b = h('button.j-bar' + (ahead(k) ? '.ahead' : ''), { type: 'button', disabled: ahead(k), 'aria-label': longLabel(k) + ': ' + T.jViewings(n), onclick: () => showBucket(k) },
      h('i', { style: { height: (n ? Math.max(4, n / max * 100) : 0) + '%' } }), h('span', null, unit === 'day' && k % 5 !== 1 ? '' : label(k)));
    b._k = k;
    bars.append(b);
  }
  const track = h('div.scroll-x', null, bars);
  main.append(h('section.j-sec.j-big', { 'aria-labelledby': 'j-tl' }, h('h2', { id: 'j-tl' }, T.jTimeline), h('div.j-tl', null, track, detail)));
  showBucket(sel);
  // on a narrow screen the bars scroll sideways: start with the chosen one in view, not with the first day
  requestAnimationFrame(() => {
    const b = [...bars.children].find(x => x._k === sel);
    if (b && track.scrollWidth > track.clientWidth) track.scrollLeft = Math.max(0, b.offsetLeft - bars.offsetLeft + b.offsetWidth / 2 - track.clientWidth / 2);
  });
  if (asked != null) requestAnimationFrame(() => detail.scrollIntoView({ block: 'start', behavior: 'smooth' }));

  // genres, decades, when
  const barList = (rows, total) => h('ul.j-list', null, rows.map(([k, n]) => h('li', null, h('span', null, k),
    h('span.j-meter', { 'aria-hidden': 'true' }, h('i', { style: { width: Math.round(n / rows[0][1] * 100) + '%' } })), h('span.muted', null, num(n)))));
  const decades = count(evs.filter(e => e.year), e => Math.floor(e.year / 10) * 10).map(([k, n]) => [`${k}–${String(k + 9).slice(2)}`, n]);
  const days = [0, 0, 0, 0, 0, 0, 0], hours = Array(24).fill(0);
  for (const e of evs) { const d = new Date(e.at * 1000); days[(d.getDay() + 6) % 7]++; hours[d.getHours()]++; }
  const dMax = Math.max(1, ...days);
  const peak = hours.reduce((bi, n, i) => n > hours[bi] ? i : bi, 0);
  const late = hours.slice(0, 5).reduce((a, n) => a + n, 0);
  const dayName = i => new Date(2024, 0, 1 + i).toLocaleDateString(T.locale, { weekday: 'short' }); // 2024-01-01 was a Monday
  const busiest = days.indexOf(Math.max(...days));
  const whenBox = h('div.j-when-l', null,
      h('ul.j-week', null, days.map((n, i) => h('li', { 'aria-label': new Date(2024, 0, 1 + i).toLocaleDateString(T.locale, { weekday: 'long' }) + ': ' + T.jViewings(n) },
        h('i', { 'aria-hidden': 'true', style: { opacity: n ? 0.25 + 0.75 * n / dMax : 0.08 } }),
        h('b', { 'aria-hidden': 'true' }, num(n)), h('span', { 'aria-hidden': 'true' }, dayName(i))))),
      h('ul.j-facts', null,
        h('li', null, T.jPeak(peak, (peak + 1) % 24)),
        h('li', null, T.jBusiest(new Date(2024, 0, 1 + busiest).toLocaleDateString(T.locale, { weekday: 'long' }))),
        late > 0 && h('li', null, T.jLate(late))));

  // who and where: directors (a series' creators), cast and countries, from TMDB; a series counts once a day
  const directors = count(genreBase, e => e.directors || []);
  const cast = count(genreBase, e => e.cast || []);
  const countries = count(genreBase, e => e.countries || []);
  const regions = (() => { try { return new Intl.DisplayNames([T.locale], { type: 'region' }); } catch { return null; } })();
  const country = c => { let n = c; try { n = regions?.of(c) || c; } catch { /* an old code */ } return jFlag(c) + ' ' + n; };
  const rewatched = count(films, filmKey).filter(([, n]) => n > 1).map(([k, n]) => [films.find(e => filmKey(e) === k).title, n]);
  const rated = films.filter(e => e.rating > 0);
  const avg = rated.length ? rated.reduce((a, e) => a + e.rating, 0) / rated.length : 0;
  // one grid for every breakdown, so short lists sit side by side instead of leaving holes
  const breakdowns = h('div.j-grid', null,
    genres.length > 0 && h('section.j-sec', null, h('h2', null, T.genres), barList(genres.slice(0, 6))),
    decades.length > 0 && h('section.j-sec', null, h('h2', null, T.jDecades), barList(decades.slice(0, 6))),
    directors.length > 0 && h('section.j-sec', null, h('h2', null, T.jDirectors), barList(directors.slice(0, 6))),
    cast.length > 0 && h('section.j-sec', null, h('h2', null, T.jCast), barList(cast.slice(0, 6))),
    countries.length > 0 && h('section.j-sec', null, h('h2', null, T.jCountries), barList(countries.slice(0, 6).map(([c, n]) => [country(c), n]))),
    (rewatched.length > 0 || rated.length > 0) && h('div.j-stack', null,
    rewatched.length > 0 && h('section.j-sec', null, h('h2', null, T.jRewatched), barList(rewatched.slice(0, 6))),
    rated.length > 0 && h('section.j-sec', null, h('h2', null, T.jRated),
      h('p', null, h('b.j-avg', null, '★ ' + num(avg, 1)), ' ', h('span.muted', null, T.jRatedOf(rated.length, films.length))),
      h('ul.j-list.j-toprated', null, [...rated].sort((a, b) => b.rating - a.rating || b.at - a.at).slice(0, 5).map(e =>
        h('li', null, h('span', null, e.title), h('span.j-starline', { 'aria-label': T.jStars(e.rating) }, '★'.repeat(e.rating)), h('span.muted', null, e.year || '')))))));

  // a week of hours: when the viewings start, hour by hour
  const grid = [...Array(7)].map(() => Array(24).fill(0));
  for (const e of evs) { const d = new Date(e.at * 1000); grid[(d.getDay() + 6) % 7][d.getHours()]++; }
  const gMax = Math.max(1, ...grid.flat());
  main.append(breakdowns, h('section.j-sec.j-big.j-when', { 'aria-labelledby': 'j-hm' }, h('h2', { id: 'j-hm' }, T.jWhen),
    whenBox,
    h('div.scroll-x.j-when-r', { 'aria-label': T.jHeatmap }, h('table.j-heat', null,
      h('thead', null, h('tr', null, h('th'), [...Array(24).keys()].map(hr => h('th', { scope: 'col' }, hr % 3 ? '' : String(hr))))),
      h('tbody', null, grid.map((row, d) => h('tr', null, h('th', { scope: 'row' }, dayName(d)),
        row.map((n, hr) => h('td', { title: `${dayName(d)} ${hr}:00 · ${T.jViewings(n)}`, 'aria-label': `${dayName(d)} ${hr}:00: ${T.jViewings(n)}` },
          h('i', { style: { opacity: n ? 0.2 + 0.8 * n / gMax : 0 } }))))))),
      h('p.j-heat-key', { 'aria-hidden': 'true' }, h('span', null, '0'), [0.2, 0.5, 0.75, 1].map(o => h('i', { style: { opacity: o } })), h('span', null, num(gMax))))));

  // records: the longest run of days, the biggest day and week, binge weekends
  main.append(jRecords(evs));

  main.append(h('div.j-pair', null, (per === 'year' || per === 'month') && jGoals(goals || [], evs, per, canEdit, headActs), ...extras()));
  main.append(jAbandoned(abandoned || []));

  // the year as a story: one big statement per page; click (or →, ←) to step, Esc to leave
  function openRecap() {
    const year = String(from.getFullYear());
    const big = (t, cls = '') => h('p.jr-big' + cls, null, t);
    const kick = t => h('p.jr-kick', null, t);
    const bucketList = [...buckets];
    const [bk, bEvs] = bucketList.reduce((best, b) => b[1].length > best[1].length ? b : best, bucketList[0]);
    const tops = new Map();
    for (const e of evs) {
      const k = e.series_title || e.title, t = tops.get(k) || { title: k, n: 0, last: 0, e, series: e.kind === 'episode' };
      t.n++; t.last = Math.max(t.last, e.at); if (!t.e.image_id && e.image_id) t.e = e; tops.set(k, t);
    }
    const top = [...tops.values()].sort((a, b) => b.n - a.n || b.last - a.last).slice(0, 5);
    const perDay = count(evs, e => new Date(e.at * 1000).toDateString());
    const marathon = perDay.length && perDay.reduce((b, x) => x[1] > b[1] || (x[1] === b[1] && new Date(x[0]) < new Date(b[0])) ? x : b);
    const owl = late * 100 >= evs.length * 15;
    const hoursText = secs < 3600 ? T.jMinutes(Math.round(secs / 60)) : T.jHours(Math.round(secs / 3600));
    const pages = [
      () => [h('p.jr-year', null, year), big(T.jrIntro), h('p.jr-small', null, T.jrTap)],
      () => [kick(T.jrTotal), h('p.jr-big', null, h('em', null, T.jrFilms(films.length)), ' ' + T.jrEps(eps.length)),
        h('p.jr-small', null, T.jrScreen(hoursText)), secs >= 86400 && h('p.jr-small', null, T.jDays(num(secs / 86400, 1))),
        per === 'year' && state.me.profile?.journal_goal > 0 && h('p.jr-small', null, T.jGoalOf(num(films.length), num(state.me.profile.journal_goal)))],
      topGenre && (() => [kick(T.jTopGenre), h('p.jr-huge', null, topGenre[0]), big(T.jShare(Math.round(topGenre[1] / genreBase.length * 100))),
        genres.length > 1 && h('ol.jr-rest', { start: 2 }, genres.slice(1, 4).map(([g]) => h('li', null, g)))]),
      bEvs.length && (() => {
        const mx = Math.max(1, ...bucketList.map(([, b]) => b.length));
        return [kick(T.jrMonth), h('p.jr-huge', null, new Date(from.getFullYear(), bk, 1).toLocaleDateString(T.locale, { month: 'long' })),
          big(T.jViewings(bEvs.length)),
          h('div.jr-bars', { 'aria-hidden': 'true' }, bucketList.map(([k, b]) => h('div', null,
            h('i' + (k === bk ? '.on' : ''), { style: { height: (b.length ? Math.max(4, b.length / mx * 100) : 2) + '%' } }),
            h('span', null, new Date(2024, k, 1).toLocaleDateString(T.locale, { month: 'narrow' })))))];
      }),
      top.length && (() => [kick(T.jrTitles), h('ol.jr-top', null, top.map((t, i) => h('li' + (i ? '' : '.first'), null,
        h('span.jr-n', null, i + 1),
        t.e.image_id ? artImg({ id: t.e.image_id, kind: t.series ? 'series' : 'movie', title: t.title, updated_at: t.e.updated_at }, 'poster', '') : h('div.art.noart', null, h('div.ph', null, t.title)),
        h('div', null, h('b', null, t.title), h('span.muted', null, t.series ? T.jrEpCount(t.n) : T.jrTimes(t.n))))))]),
      marathon && marathon[1] >= 2 && (() => [kick(T.jrMarathon),
        h('p.jr-huge', null, new Date(marathon[0]).toLocaleDateString(T.locale, { month: 'long', day: 'numeric' })), big(T.jrOneDay(marathon[1]))]),
      () => [kick(owl ? T.jrOwl : T.jWhen), h('p.jr-huge', null, T.jrAround(peak)),
        big(T.jrFavDay(new Date(2024, 0, 1 + busiest).toLocaleDateString(T.locale, { weekday: 'long' }))), late > 0 && h('p.jr-small', null, T.jLate(late))],
      decades.length && (() => [kick(T.jrDecade), h('p.jr-huge', null, decades[0][0]), big(T.jViewings(decades[0][1]))]),
      directors.length && (() => [kick(T.jrDirector), h('p.jr-huge', null, directors[0][0]), big(T.jViewings(directors[0][1]))]),
      rated.some(e => e.rating >= 4) && (() => [kick(T.jrBest), h('ol.jr-top', null, [...rated].sort((a, b) => b.rating - a.rating || b.at - a.at).slice(0, 5).map((e, i) =>
        h('li' + (i ? '' : '.first'), null, h('span.jr-n', null, i + 1), jArt(e), h('div', null, h('b', null, e.title), h('span.muted', null, '★'.repeat(e.rating))))))]),
      () => [big(T.jrEnd), h('p.jr-actions', null, h('button.btn.primary', { onclick: () => show(0) }, '↺ ' + T.jrAgain), h('button.btn', { onclick: () => close() }, T.close))],
    ].filter(Boolean);

    let at = 0;
    const bars = h('div.jr-progress', { 'aria-hidden': 'true' }, pages.map(() => h('i')));
    const page = h('div.jr-page', { 'aria-live': 'polite' });
    const box = h('div.jr', { role: 'dialog', 'aria-modal': 'true', 'aria-label': T.jRecap + ' ' + year },
      bars, h('div.jr-head', null, h('span', null, 'Kinora · ' + year), h('button.jr-x', { 'aria-label': T.close, onclick: () => close() }, '✕')),
      page, h('div.jr-nav', null, h('button.jr-prev', { 'aria-label': T.jrPrev, onclick: () => show(at - 1) }), h('button.jr-next', { 'aria-label': T.jrNext, onclick: () => show(at + 1) })));
    const show = i => {
      if (i < 0 || i >= pages.length) return;
      at = i;
      page.replaceChildren(...pages[i]().filter(Boolean));
      page.classList.remove('in'); void page.offsetWidth; page.classList.add('in');
      [...bars.children].forEach((b, j) => b.classList.toggle('on', j <= i));
      box.classList.toggle('last', i === pages.length - 1);
    };
    // keys while the story is open: Tab stays inside it, Space steps on unless it presses a button of the page itself
    const key = e => {
      if (e.key === 'Escape') { e.preventDefault(); close(); return; }
      if (e.key === 'Tab') {
        const f = [...box.querySelectorAll('button, a[href]')].filter(el => el.offsetParent !== null);
        if (!f.length) return;
        const i = f.indexOf(document.activeElement);
        if (e.shiftKey && i <= 0) { e.preventDefault(); f[f.length - 1].focus(); }
        else if (!e.shiftKey && (i === -1 || i === f.length - 1)) { e.preventDefault(); f[0].focus(); }
        return;
      }
      const own = e.target instanceof Element && e.target.closest('.jr-page a, .jr-page button');
      if (e.key === 'ArrowRight' || (e.key === ' ' && !own)) { e.preventDefault(); show(at + 1); }
      else if (e.key === 'ArrowLeft') { e.preventDefault(); show(at - 1); }
    };
    const opener = document.activeElement;
    let closed = false;
    const close = () => {
      if (closed) return; closed = true; box.remove(); document.documentElement.style.overflow = ""; document.removeEventListener('keydown', key);
      if (opener instanceof HTMLElement && opener.isConnected) opener.focus();
    };
    onCleanup(close);
    document.addEventListener('keydown', key);
    document.body.append(box); document.documentElement.style.overflow = "hidden";
    show(0);
    box.querySelector('.jr-x').focus();
  }
}

// ---------------------------------------------------------------- Napló 2: stars, viewings by hand, goals, records, challenges
const jCanEdit = () => { const p = state.me.profile; return !!p && !p.is_kids && !state.me.profile_locked; };

// a viewing's poster: the library's artwork, the TMDB poster of one added by hand, or the title
function jArt(e) {
  if (e.image_id) return artImg({ id: e.image_id, kind: e.kind === 'episode' ? 'series' : 'movie', title: e.series_title || e.title, year: e.year, updated_at: e.updated_at }, 'poster', '');
  if (e.poster) return h('div.art', null, h('img', { src: e.poster, alt: '', loading: 'lazy' }));
  return h('div.art.noart', null, h('div.ph', null, e.series_title || e.title));
}

// a country's flag from its ISO 3166-1 code
const jFlag = c => /^[A-Z]{2}$/.test(c) ? String.fromCodePoint(...[...c].map(ch => 0x1F1A5 + ch.charCodeAt(0))) : '';

// five stars; pressing the lit last one clears them. onSet resolves false when saving failed.
function starPicker(value, onSet, label) {
  const box = h('div.j-stars', { role: 'radiogroup', 'aria-label': label || T.jRate });
  const draw = () => [...box.children].forEach((b, i) => { b.classList.toggle('on', i < value); b.setAttribute('aria-checked', String(i + 1 === value)); });
  for (let i = 1; i <= 5; i++) box.append(h('button', { type: 'button', role: 'radio', 'aria-label': T.jStars(i), title: T.jStars(i),
    onclick: async ev => { ev.preventDefault(); ev.stopPropagation(); const v = value === i ? 0 : i; if (await onSet(v) !== false) { value = v; draw(); } } }, '★'));
  draw();
  return box;
}

// a TMDB search field with a result list; pick(result) is called with the chosen one
function jSearch(type, placeholder, row, pick) {
  const input = h('input', { type: 'search', placeholder, 'aria-label': placeholder, autocomplete: 'off' });
  const list = h('ul.j-results', { role: 'listbox' });
  let timer = 0, seq = 0;
  input.addEventListener('input', () => {
    clearTimeout(timer);
    timer = setTimeout(async () => {
      const q = input.value.trim(), my = ++seq;
      if (q.length < 2) { list.replaceChildren(); return; }
      let res = [];
      try { res = await GET(`/api/journal/search?type=${type}&q=${encodeURIComponent(q)}`); } catch { return; }
      if (my !== seq) return;
      put(list, res.length ? res.slice(0, 12).map(x => h('li', null, h('button.j-result', { type: 'button', onclick: () => pick(x) }, row(x))))
        : h('li.muted', null, T.jNoMatch));
    }, 300);
  });
  return { input, list, el: h('div.j-search', null, input, list) };
}

const jResultRow = (img, title, sub) => [img ? h('img', { src: img, alt: '', loading: 'lazy' }) : h('span.j-noimg'), h('span', null, h('b', null, title), sub && h('span.muted', null, sub))];

// a film seen outside Kinora: from TMDB, with the day and the stars
function jAddViewing() {
  let chosen = null, stars = 0;
  const today = new Date(), iso = d => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
  const date = h('input', { type: 'date', value: iso(today), max: iso(today), 'aria-label': T.jAddWhen });
  const picked = h('div.j-picked');
  const search = jSearch('movie', T.jAddSearch, x => jResultRow(x.poster, x.title, x.year || ''), x => {
    chosen = x;
    put(picked, h('div.j-result.on', null, ...jResultRow(x.poster, x.title, x.year || '')));
    search.list.replaceChildren();
  });
  const body = h('div.j-add', null, search.el, picked, field(T.jAddWhen, date), h('div.field', null, h('label', null, T.jRate), starPicker(0, v => { stars = v; }, T.jRate)),
    h('p.muted.small', null, T.jAddHint));
  modal(T.jAdd, body, close => [h('button.btn', { onclick: close }, T.cancel), h('button.btn.primary', { onclick: async () => {
    if (!chosen) { toast(T.jAddPick, true); search.input.focus(); return; }
    const [y, m, d] = date.value.split('-').map(Number);
    const day = new Date(y, m - 1, d), isToday = iso(day) === iso(new Date());
    const at = Math.floor((isToday ? Date.now() : new Date(y, m - 1, d, 20).getTime()) / 1000);
    try { await api('POST', '/api/journal/events', { tmdb_id: chosen.id, at, rating: stars }); close(); toast(T.saved); route(); } catch { /* shown */ }
  } }, T.save)]);
}

// more goals: films, hours, episodes or films of a genre, by month or year, with the pace they need
function jGoals(goals, evs, per, canEdit, headActs) {
  const box = h('section.j-sec.j-big', { 'aria-labelledby': 'j-gl' });
  const list = h('div.j-goals');
  const now = new Date();
  const span = p => p === 'month' ? [new Date(now.getFullYear(), now.getMonth(), 1), new Date(now.getFullYear(), now.getMonth() + 1, 1)]
    : [new Date(now.getFullYear(), 0, 1), new Date(now.getFullYear() + 1, 0, 1)];
  const shown = goals.filter(g => per === 'year' || g.period === 'month');
  const value = g => {
    const [a, b] = span(g.period), list = evs.filter(e => e.at * 1000 >= a && e.at * 1000 < b);
    if (g.metric === 'hours') return Math.floor(list.reduce((x, e) => x + (e.runtime_sec || 0), 0) / 3600);
    if (g.metric === 'episodes') return list.filter(e => e.kind === 'episode').length;
    const films = list.filter(e => e.kind === 'movie');
    return g.metric === 'genre' ? films.filter(e => (e.genres || []).some(x => x.toLowerCase() === g.genre.toLowerCase())).length : films.length;
  };
  const rows = shown.map(g => {
    const v = value(g), [a, b] = span(g.period), due = Math.floor(g.target * (now - a) / (b - a)), ahead = v - due;
    const name = (g.period === 'month' ? T.jGoalNameMonth : T.jGoalNameYear)(g.metric === 'genre' ? T.jGoalGenreOf(g.genre) : T.jGoalMetric[g.metric]);
    const pace = v >= g.target ? T.jGoalDone : ahead >= 0 ? T.jGoalPaceAhead(ahead) : T.jGoalPaceBehind(-ahead);
    return h('div.j-goal', null, h('div.j-goal-head', null, h('span', null, h('b', null, name), ' · ', h('span.muted', null, pace)),
        canEdit && h('button.btn.ghost.small', { type: 'button', 'aria-label': T.delete + ': ' + name, onclick: async () => {
          if (!await confirmBox(T.jGoalDel(name))) return;
          try { await api('DELETE', '/api/journal/goals/' + g.id); route(); } catch { /* shown */ }
        } }, T.delete)),
      h('div.j-goal-bar', { role: 'progressbar', 'aria-valuemin': 0, 'aria-valuemax': g.target, 'aria-valuenow': Math.min(v, g.target), 'aria-label': name },
        h('i', { style: { width: Math.min(100, v / g.target * 100) + '%' } }), h('u', { style: { left: Math.min(100, due / g.target * 100) + '%' } })),
      h('p.small', null, `${num(v)} / ${num(g.target)} ${g.metric === 'hours' ? T.jUnitHours : g.metric === 'episodes' ? T.jUnitEps : T.jUnitFilms}`));
  });
  const add = canEdit && h('button.btn.small', { type: 'button', onclick: () => jGoalForm(evs) }, icon('plus'), T.jGoalAdd);
  if (!rows.length && !add) return h('span');
  // with no goal yet the lone button would take a whole section: it joins the page's own actions
  if (!rows.length && headActs) { add.classList.remove('small'); headActs.append(add); return h('span'); }
  list.append(...rows);
  box.append(h('div.j-sec-head', null, h('h2', { id: 'j-gl' }, T.jGoals), add), list);
  return box;
}

function jGoalForm(evs) {
  const period = h('select', null, h('option', { value: 'month' }, T.jGoalPeriods.month), h('option', { value: 'year' }, T.jGoalPeriods.year));
  const metric = h('select', null, Object.keys(T.jGoalMetric).map(k => h('option', { value: k }, T.jGoalMetric[k])), h('option', { value: 'genre' }, T.jGoalGenre));
  const genres = [...new Set(evs.flatMap(e => e.genres || []))].sort((a, b) => a.localeCompare(b, T.locale));
  const genre = h('input', { type: 'text', list: 'j-genres', maxlength: 80 });
  const target = h('input', { type: 'number', min: 1, max: 10000, step: 1, value: 4 });
  const gField = field(T.jGoalGenre, genre); gField.hidden = true;
  metric.onchange = () => { gField.hidden = metric.value !== 'genre'; };
  const body = h('div', null, field(T.jGoalPeriod, period), field(T.jGoalWhat, metric), gField, h('datalist', { id: 'j-genres' }, genres.map(g => h('option', { value: g }))), field(T.jGoalTarget, target));
  modal(T.jGoalAdd, body, close => [h('button.btn', { onclick: close }, T.cancel), h('button.btn.primary', { onclick: async () => {
    const t = Math.round(+target.value);
    if (!(t >= 1 && t <= 10000)) { target.reportValidity(); return; }
    if (metric.value === 'genre' && !genre.value.trim()) { genre.focus(); return; }
    try {
      // the yearly film goal is the profile's own (every device and the widgets show it), not a second one
      if (period.value === 'year' && metric.value === 'films') {
        if (t > 1000) { target.max = 1000; target.reportValidity(); return; }
        const p = state.me.profile;
        await api('PATCH', '/api/profiles/' + p.id, { journal_goal: t }); p.journal_goal = t;
      } else await api('POST', '/api/journal/goals', { period: period.value, metric: metric.value, genre: genre.value.trim(), target: t });
      close(); toast(T.saved); route();
    } catch { /* shown */ }
  } }, T.save)]);
}

// records of the period, in the viewer's own days
function jRecords(evs) {
  const dayKey = d => `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
  const perDay = new Map();
  for (const e of evs) { const k = dayKey(new Date(e.at * 1000)); perDay.set(k, (perDay.get(k) || 0) + 1); }
  // the longest run of days with at least one viewing
  let best = 0, bestEnd = null, run = 0, prev = null;
  const days = [...new Set(evs.map(e => { const d = new Date(e.at * 1000); return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime(); }))].sort((a, b) => a - b);
  for (const t of days) {
    const d = new Date(t);
    run = prev && Math.round((d - prev) / 86400000) === 1 ? run + 1 : 1;
    if (run > best) { best = run; bestEnd = d; }
    prev = d;
  }
  // the biggest day
  let top = null;
  for (const e of evs) { const d = new Date(e.at * 1000), n = perDay.get(dayKey(d)); if (!top || n > top.n) top = { d, n }; }
  // the biggest week (Monday to Sunday)
  const weeks = new Map();
  for (const e of evs) { const d = new Date(e.at * 1000); const m = new Date(d.getFullYear(), d.getMonth(), d.getDate() - (d.getDay() + 6) % 7); const k = m.getTime(); weeks.set(k, (weeks.get(k) || 0) + 1); }
  const [wk, wn] = [...weeks].reduce((b, x) => x[1] > b[1] ? x : b, [0, 0]);
  // binge weekends: 4 or more episodes of one series over a Saturday and Sunday
  const binges = new Map();
  for (const e of evs) {
    if (e.kind !== 'episode') continue;
    const d = new Date(e.at * 1000), wd = d.getDay();
    if (wd !== 0 && wd !== 6) continue;
    const sat = new Date(d.getFullYear(), d.getMonth(), d.getDate() - (wd === 0 ? 1 : 0)).getTime(), k = sat + '|' + (e.series_title || e.title);
    binges.set(k, (binges.get(k) || 0) + 1);
  }
  const bingeList = [...binges].filter(([, n]) => n >= 4).sort((a, b) => b[1] - a[1]);
  const fmt = d => d.toLocaleDateString(T.locale, { year: 'numeric', month: 'short', day: 'numeric' });
  const stat = (label, value, sub) => h('div.j-stat', null, h('dt', null, label), h('dd', null, h('b', null, value), sub && h('span.muted.small', null, sub)));
  const startOf = bestEnd && new Date(bestEnd.getFullYear(), bestEnd.getMonth(), bestEnd.getDate() - best + 1);
  return h('section.j-sec.j-big', { 'aria-labelledby': 'j-rec' }, h('h2', { id: 'j-rec' }, T.jRecords), h('dl.j-stats.j-rec', null,
    stat(T.jStreak, T.jDaysN(best), best > 1 ? `${fmt(startOf)} – ${fmt(bestEnd)}` : fmt(bestEnd)),
    top && stat(T.jBigDay, T.jViewings(top.n), fmt(top.d)),
    wn > 0 && stat(T.jBigWeek, T.jViewings(wn), T.jWeekOf(fmt(new Date(wk)))),
    stat(T.jBinges, num(bingeList.length), bingeList.length ? T.jBingeTop(bingeList[0][0].split('|').slice(1).join('|'), bingeList[0][1]) : T.jBingeNone)));
}

// challenges: lists of titles to tick off, built from a director, an actor, a film series or by hand
function jChallenges(list, canEdit) {
  const box = h('section.j-sec.j-big', { 'aria-labelledby': 'j-ch' }, h('div.j-sec-head', null, h('h2', { id: 'j-ch' }, T.jChallenges),
    canEdit && h('button.btn.small', { type: 'button', onclick: () => jChallengeForm() }, icon('plus'), T.jChNew)));
  if (!list.length) { box.append(h('p.muted', null, T.jChEmpty)); return canEdit ? box : h('span'); }
  for (const c of list) {
    const pct = c.total ? Math.round(c.done / c.total * 100) : 0;
    const posters = h('div.j-ch-grid', null, c.items.map(it => {
      const art = it.poster ? h('div.art', null, h('img', { src: it.poster, alt: '', loading: 'lazy' })) : h('div.art.noart', null, h('div.ph', null, it.title));
      if (it.watched) art.append(h('span.tick', { title: T.watched }, icon('check')));
      const body = [art, h('div.cap', null, h('b', null, it.title), h('span', null, it.year || ''))];
      return h('div.j-poster' + (it.watched ? '.seen' : ''), { 'aria-label': `${it.title}${it.year ? ' (' + it.year + ')' : ''}: ${it.watched ? T.watched : T.jChTodo}` },
        it.item_id ? h('a.j-poster-link', { href: '#/item/' + it.item_id }, body) : h('div.j-poster-link', null, body),
        canEdit && h('button.btn.ghost.small.j-del', { type: 'button', 'aria-label': T.jChRemove + ': ' + it.title, onclick: async () => {
          try { await api('DELETE', `/api/journal/challenges/${c.id}/items/${it.kind}/${it.tmdb_id}`); route(); } catch { /* shown */ }
        } }, '✕'));
    }));
    const tools = canEdit && h('div.j-ch-tools', null,
      h('button.btn.ghost.small', { type: 'button', onclick: () => jChallengeAddTitles(c) }, '＋ ' + T.jChAddTitles),
      h('button.btn.ghost.small', { type: 'button', onclick: () => {
        const name = h('input', { type: 'text', maxlength: 120, value: c.title, required: true });
        modal(T.edit, h('div.j-add', null, field(T.jChName, name)), close => [h('button.btn', { onclick: close }, T.cancel), h('button.btn.primary', { onclick: async () => {
          const t = name.value.trim();
          if (!t) { name.reportValidity(); return; }
          try { await api('PATCH', '/api/journal/challenges/' + c.id, { title: t }); close(); route(); } catch { /* shown */ }
        } }, T.save)]);
        name.focus();
      } }, T.edit),
      h('button.btn.ghost.small', { type: 'button', onclick: async () => {
        if (!await confirmBox(T.jChDel(c.title))) return;
        try { await api('DELETE', '/api/journal/challenges/' + c.id); route(); } catch { /* shown */ }
      } }, T.delete));
    box.append(h('details.j-ch', { open: (c.total === 0 || c.done < c.total) && list.length <= 3 },
      h('summary', null, h('b', null, c.title), h('span.muted', null, ` · ${T.jChProgress(c.done, c.total)}`), c.total > 0 && c.done >= c.total && h('span.j-ch-done', null, ' ✓'),
        h('span.j-goal-bar', { role: 'progressbar', 'aria-valuemin': 0, 'aria-valuemax': c.total, 'aria-valuenow': c.done, 'aria-label': c.title }, h('i', { style: { width: pct + '%' } }))),
      c.items.length ? posters : h('p.muted', null, T.jChNoTitles), tools));
  }
  return box;
}

function jChallengeForm() {
  const name = h('input', { type: 'text', maxlength: 120, placeholder: T.jChNameHint });
  let src = 'directing', pickedSrc = null;
  const kinds = { directing: T.jChFromDirector, acting: T.jChFromActor, collection: T.jChFromCollection, own: T.jChFromOwn };
  const slot = h('div');
  const picked = h('div.j-picked');
  const draw = () => {
    pickedSrc = null; picked.replaceChildren();
    if (src === 'own') { put(slot, h('p.muted.small', null, T.jChOwnHint)); return; }
    const type = src === 'collection' ? 'collection' : 'person';
    const s = jSearch(type, src === 'collection' ? T.jChSearchCollection : T.jChSearchPerson,
      x => type === 'person' ? jResultRow(x.photo, x.name, x.known_for || '') : jResultRow(x.poster, x.name, ''),
      x => { pickedSrc = x; put(picked, h('div.j-result.on', null, ...(type === 'person' ? jResultRow(x.photo, x.name, x.known_for || '') : jResultRow(x.poster, x.name, '')))); s.list.replaceChildren(); if (!name.value.trim()) name.placeholder = x.name; });
    put(slot, s.el);
  };
  const tabs = h('nav.chips', { 'aria-label': T.jChFrom }, Object.entries(kinds).map(([k, l]) => h('button.chip' + (k === src ? '.on' : ''), { type: 'button', onclick: e => {
    src = k; for (const b of tabs.children) b.classList.toggle('on', b === e.currentTarget); draw();
  } }, l)));
  draw();
  modal(T.jChNew, h('div.j-add', null, h('p.small', null, T.jChFrom), tabs, slot, picked, field(T.jChName, name)), close => [h('button.btn', { onclick: close }, T.cancel), h('button.btn.primary', { onclick: async () => {
    const body = { title: name.value.trim() };
    if (src !== 'own') {
      if (!pickedSrc) { toast(T.jChPick, true); return; }
      if (src === 'collection') body.collection_id = pickedSrc.id; else { body.person_id = pickedSrc.id; body.role = src; }
    } else if (!body.title) { name.focus(); return; }
    try { await api('POST', '/api/journal/challenges', body); close(); toast(T.saved); route(); } catch { /* shown */ }
  } }, T.save)]);
}

function jChallengeAddTitles(c) {
  let kind = 'movie';
  const slot = h('div');
  const added = h('p.muted.small');
  let n = 0;
  const draw = () => {
    const s = jSearch(kind, T.jChSearchTitle, x => jResultRow(x.poster, x.title, x.year || ''), async x => {
      try {
        await api('POST', `/api/journal/challenges/${c.id}/items`, { items: [{ kind, tmdb_id: x.id, title: x.title, year: x.year || 0, poster: x.poster || '' }] });
        n++; added.textContent = T.jChAdded(n, x.title);
      } catch { /* shown */ }
    });
    put(slot, s.el);
  };
  const tabs = h('nav.chips', null, [['movie', T.jMovies], ['series', T.jSeries]].map(([k, l]) => h('button.chip' + (k === kind ? '.on' : ''), { type: 'button', onclick: e => {
    kind = k; for (const b of tabs.children) b.classList.toggle('on', b === e.currentTarget); draw();
  } }, l)));
  draw();
  modal(T.jChAddTitles + ': ' + c.title, h('div.j-add', null, tabs, slot, added), close => [h('button.btn.primary', { onclick: close }, T.done)], () => { if (n) route(); });
}

// started and left before a fifth of the runtime, untouched for two weeks
function jAbandoned(items) {
  if (!items.length) return h('span');
  return h('section.j-sec', { 'aria-labelledby': 'j-ab' }, h('div.j-sec-head.j-ab-head', null, h('h2', { id: 'j-ab' }, T.jAbandoned), h('p.muted.small', null, T.jAbandonedHint)),
    h('div.j-posters', null, items.map(it => {
      const wrap = h('div.j-ab', null, card(it, 'poster'), h('button.btn.ghost.small', { type: 'button', onclick: async () => {
        try { await api('POST', `/api/journal/abandoned/${it.id}/hide`); wrap.remove(); } catch { /* shown */ }
      } }, T.jAbandonedHide));
      return wrap;
    })));
}

// ===================================================================== account
async function viewAccount() {
  const main = shell('account');
  const cur = h('input', { type: 'password', autocomplete: 'current-password' });
  const nw = h('input', { type: 'password', autocomplete: 'new-password' });
  main.append(h('div.page-head', null, h('h1', null, T.account)),
    h('div.panel', { class: 'narrow' }, h('h2', null, T.changePw), field(T.currentPw, cur), field(T.newPw, nw),
      h('button.btn.primary', { onclick: async () => { await POST('/api/me/password', { current: cur.value, new: nw.value }); cur.value = nw.value = ''; toast(T.saved); } }, T.save)),
    h('div.panel', { class: 'narrow' }, h('h2', null, T.language), langSwitch()),
    pushPanel(),
    snowPanel(),
    devicesPanel(),
    h('p.muted', null, `${state.me.user.username} · ${state.me.server_name} · ${T.version} ${state.me.version}`));
}

// the Napló's yearly goal: films watched this year against the profile's goal, with the pace it needs;
// saved on the profile, so every device shows the same goal
function goalBlock(films) {
  const p = state.me.profile, goal = p?.journal_goal || 0;
  if (!goal) return h('span'); // a new one is set from the goals' own "New goal" button
  const canEdit = p && !p.is_kids && !state.me.profile_locked;
  const box = h('section.j-goal.j-band', { 'aria-labelledby': 'j-yg' });
  const edit = () => {
    const input = h('input', { type: 'number', min: 1, max: 1000, step: 1, value: goal || 52, 'aria-label': T.jGoalFilms, style: { width: '7em' } });
    const save = async v => {
      try { await api('PATCH', '/api/profiles/' + p.id, { journal_goal: v }); p.journal_goal = v; toast(T.saved); route(); }
      catch { /* api() has shown the error */ }
    };
    put(box, h('form.j-goal-form', { onsubmit: e => { e.preventDefault(); const v = Math.round(+input.value); if (v >= 1 && v <= 1000) save(v); else input.reportValidity(); } },
      h('label', null, T.jGoalFilms, ' ', input),
      h('button.btn.primary', { type: 'submit' }, T.save),
      goal > 0 && h('button.btn.ghost', { type: 'button', onclick: () => save(0) }, T.jGoalRemove),
      h('button.btn.ghost', { type: 'button', onclick: () => put(box, ...show()) }, T.cancel)));
    input.focus();
  };
  const show = () => {
    const now = new Date(), y0 = new Date(now.getFullYear(), 0, 1), y1 = new Date(now.getFullYear() + 1, 0, 1);
    const due = Math.floor(goal * (now - y0) / (y1 - y0)), ahead = films - due;
    const pace = films >= goal ? T.jGoalDone : ahead >= 0 ? T.jGoalAhead(ahead) : T.jGoalBehind(-ahead);
    return [h('div.j-band-txt', null,
        h('div.j-goal-head', null, h('h2', { id: 'j-yg' }, T.jGoal), canEdit && h('button.btn.ghost.small', { type: 'button', onclick: edit }, T.edit)),
        h('p.j-band-of', null, h('b', null, T.jGoalOf(num(films), num(goal))), ' ', h('span.j-pace' + (films >= goal || ahead >= 0 ? '.up' : ''), null, pace))),
      h('div.j-band-bar', null,
        h('div.j-goal-bar', { role: 'progressbar', 'aria-valuemin': 0, 'aria-valuemax': goal, 'aria-valuenow': Math.min(films, goal), 'aria-label': T.jGoal },
          h('i', { style: { width: Math.min(100, films / goal * 100) + '%' } }), h('u', { style: { left: Math.min(100, due / goal * 100) + '%' }, title: T.jGoalDue(due) })),
        h('p.muted.small', null, T.jGoalDue(due)))];
  };
  put(box, ...show());
  return box;
}

// the Android app's notifications on this device: the server keeps them, the app fetches them in the
// background (no Google push service); which kinds come is chosen here, as in the iOS app
function pushPanel() {
  if (!APP?.pushAvailable) return h('span');
  const box = h('div.panel', { class: 'narrow' });
  const load = async () => {
    const r = await GET('/api/push/android', { quiet: true }).catch(() => null);
    if (!r || !r.enabled) { box.remove(); return; }
    const muted = new Set(r.muted || []);
    const on = h('input', { type: 'checkbox', checked: r.paired, onchange: async e => {
      const want = e.target.checked;
      try {
        if (want) await POST('/api/push/android', {}); else await api('DELETE', '/api/push/android');
        APP.push(want); buzz('success'); toast(T.saved); load();
      } catch { e.target.checked = !want; }
    } });
    const cats = (r.categories || []).filter(c => c !== 'admin_requests' || state.me.user.is_admin);
    put(box, h('h2', null, T.notifications), h('label.check', null, on, T.pushThisDevice), h('p.hint', null, T.pushAndroidHint),
      r.paired && h('div', { class: 'mt3' }, cats.map(c => h('label.check', null, h('input', { type: 'checkbox', checked: !muted.has(c), onchange: async e => {
        const was = !e.target.checked;
        if (e.target.checked) muted.delete(c); else muted.add(c);
        try { await api('PATCH', '/api/push/android', { muted: [...muted] }); toast(T.saved); }
        catch { e.target.checked = was; if (was) muted.delete(c); else muted.add(c); }
      } }), T.pushCats[c] || c))));
  };
  load();
  return box;
}

// the current profile's snowfall switch; saved on the server, so it follows the profile to every device
function snowPanel() {
  const p = state.me.profile;
  const box = h('input', { type: 'checkbox', checked: p.snow === true, disabled: p.is_kids || state.me.profile_locked, onchange: async e => {
    const on = e.target.checked;
    try { await api('PATCH', '/api/profiles/' + p.id, { snow: on }); p.snow = on; snow(); toast(T.saved); }
    catch { e.target.checked = !on; }
  } });
  return h('div.panel', { class: 'narrow' }, h('h2', null, T.appearance), h('label.check', null, box, T.snow), h('p.hint', null, T.snowHint));
}

function deviceRow(d, onChange, showUser) {
  return h('div.entry.device', null, h('div', null,
    h('div', null, h('strong', null, d.name || T.platforms[d.platform] || d.platform), d.current && ['\u00a0', h('span.badge.amber', null, T.thisDevice)]),
    h('p.muted.small', null, [showUser && d.username, T.platforms[d.platform] || d.platform, d.app_version && 'v' + d.app_version, d.ip,
      T.lastActive + ': ' + ago(Date.now() / 1000 - d.last_seen)].filter(Boolean).join(' · '))),
    !d.current && h('button.btn.danger', { onclick: async () => { await api('DELETE', '/api/devices/' + d.id); toast(T.deleted); onChange(); } }, T.signOutDevice));
}

function devicesPanel() {
  const box = h('div.panel', { class: 'narrow' });
  const load = async () => {
    const r = await GET('/api/devices');
    put(box, h('h2', null, T.myDevices), r.devices.map(d => deviceRow(d, load)),
      r.devices.length > 1 && h('button.btn', { onclick: async () => { const x = await api('DELETE', '/api/devices'); toast(T.signedOutN(x.removed)); load(); } }, T.signOutOthers));
  };
  load();
  return box;
}

// ===================================================================== quick connect (approve a TV / phone)
async function viewQuickConnect(code) {
  const main = shell('account');
  const me = state.me;
  if (me.profile.is_kids || me.profile_locked) { main.append(h('div.center', { class: 'tall' }, h('p.lede', null, T.qcKids))); return; }
  const box = h('div.auth');
  main.append(h('div.center', { class: 'tall' }, box));
  const inp = h('input.pin-input', { type: 'text', inputmode: 'numeric', maxlength: 7, autocomplete: 'off', 'aria-label': T.qcCode, value: code });
  const ask = () => { put(box, h('form', { onsubmit: async e => { e.preventDefault(); await lookup(inp.value); } },
    h('h1', null, T.connectDevice), h('p.lede', null, T.qcLede), inp, h('button.btn.primary.big', { type: 'submit', class: 'wide mt3' }, T.next))); inp.focus(); };
  async function lookup(c) {
    let info;
    try { info = await POST('/api/quickconnect/lookup', { code: c }); } catch { return ask(); }
    const jf = info.platform === 'jellyfin';
    const pin = h('input.pin-input', { type: 'password', inputmode: 'numeric', maxlength: 8, autocomplete: 'off', 'aria-label': T.qcPinLabel });
    const pinRow = h('label.field', { hidden: true }, h('span', null, T.qcPinLabel), pin);
    const choices = jf ? me.profiles.map(p => [p.id, p.name, p]) : [[0, T.qcAnyProfile, null], ...me.profiles.map(p => [p.id, p.name, p])];
    const radios = choices.map(([id, label, p]) =>
      h('label.check', null, h('input', { type: 'radio', name: 'qcp', value: id, checked: p ? p.id === me.profile.id : false,
        onchange: () => { pinRow.hidden = !(jf && p && p.has_pin); if (!pinRow.hidden) pin.focus(); } }), ' ', label,
        p && p.is_kids ? ' (' + T.kids + ')' : '', p && p.has_pin ? ' – PIN' : ''));
    pinRow.hidden = !(jf && me.profile.has_pin);
    put(box, h('h1', null, T.connectDevice), h('p.lede', null, T.qcWants(info.device_name || T.unknownDevice, T.platforms[info.platform] || info.platform)),
      h('div.code', null, info.code),
      h('fieldset', { class: 'mt3' }, h('legend', null, T.qcProfile), radios),
      jf && pinRow,
      h('p.muted.small', null, jf ? T.qcJellyfinNote : [T.qcPinNote, ' ', T.qcKidsNote]),
      h('div.row-gap', { class: 'mt3' },
        h('button.btn.primary.big', { onclick: async () => {
          const pid = +(box.querySelector('input[name=qcp]:checked')?.value || 0);
          await POST('/api/quickconnect/approve', jf ? { code: info.code, profile_id: pid, pin: pin.value } : { code: info.code, profile_id: pid });
          put(box, h('h1', null, T.connectDevice), h('p.lede', null, T.qcApproved), h('a.btn', { href: '#/home' }, T.home));
        } }, T.approve),
        h('button.btn', { onclick: async () => { await POST('/api/quickconnect/approve', { code: info.code, action: 'deny' }); toast(T.qcDenied); ask(); } }, T.deny)));
    box.querySelector('button.primary')?.focus();
  }
  if (code && code.replace(/[^0-9]/g, '').length === 6) await lookup(code); else ask();
}

// ===================================================================== party join
async function viewPartyJoin(code) {
  if (code) {
    try { const info = await GET('/api/party/' + encodeURIComponent(code), { quiet: true }); location.replace(`#/play/${info.item_id}?party=${info.code}`); return; }
    catch (e) { if (e.status !== 401 && e.status !== 428) toast(e.message || T.partyNotFound, true); }
  }
  const main = shell('party');
  const inp = h('input.pin-input', { type: 'text', maxlength: 6, autocomplete: 'off', 'aria-label': T.partyCodeLabel, class: 'upper' });
  main.append(h('div.center', { class: 'tall' }, h('form.auth', { onsubmit: e => { e.preventDefault(); go('#/party/' + inp.value.trim().toUpperCase()); } },
    h('h1', null, T.partyJoinTitle), h('p.lede', null, T.partyCodeLabel), inp, h('button.btn.primary.big', { type: 'submit', class: 'wide mt3' }, T.enter))));
  inp.focus();
}

// ===================================================================== player
function detectCaps() {
  const v = document.createElement('video');
  const ms = window.MediaSource || window.ManagedMediaSource;
  const can = t => (ms && ms.isTypeSupported && ms.isTypeSupported(t)) || v.canPlayType(t) === 'probably';
  const caps = { video: [], audio: [], containers: [], hdr: false };
  if (can('video/mp4; codecs="avc1.640028"')) caps.video.push('h264');
  if (can('video/mp4; codecs="hvc1.1.6.L120.90"') || can('video/mp4; codecs="hev1.1.6.L120.90"')) caps.video.push('hevc');
  if (can('video/webm; codecs="vp9"') || can('video/mp4; codecs="vp09.00.10.08"')) caps.video.push('vp9');
  if (can('video/mp4; codecs="av01.0.05M.08"')) caps.video.push('av1');
  if (can('audio/mp4; codecs="mp4a.40.2"')) caps.audio.push('aac');
  if (can('audio/mpeg')) caps.audio.push('mp3');
  if (can('audio/webm; codecs="opus"') || can('audio/mp4; codecs="opus"')) caps.audio.push('opus');
  if (can('audio/flac') || can('audio/mp4; codecs="flac"')) caps.audio.push('flac');
  if (can('audio/mp4; codecs="ac-3"')) caps.audio.push('ac3');
  if (can('audio/mp4; codecs="ec-3"')) caps.audio.push('eac3');
  if (v.canPlayType('video/mp4')) caps.containers.push('mp4');
  if (v.canPlayType('video/webm')) caps.containers.push('webm');
  if (v.canPlayType('video/quicktime')) caps.containers.push('mov');
  if (v.canPlayType('video/x-matroska') === 'probably') caps.containers.push('mkv');
  caps.hdr = !!(window.matchMedia && matchMedia('(dynamic-range: high)').matches && caps.video.includes('hevc'));
  return APP ? APP.extraCaps(caps) : caps;
}
const langName = l => T.langNames[l] || l || T.unknownLang;

async function viewPlayer(id, q) {
  const profile = state.me.profile;
  const root = app();
  put(root);
  const video = h('video', { playsinline: true, preload: 'auto', 'aria-label': T.video });
  const spinner = h('div.spin', { role: 'progressbar', 'aria-label': T.loading });
  const mid = h('div.p-mid', null, spinner);
  const titleEl = h('h2'), subEl = h('div.s');
  const timeEl = h('span.p-time', null, '0:00'), remEl = h('button.p-time.rem', { type: 'button', title: T.timeEndToggle, 'aria-label': T.timeEndToggle }, '');
  // the right end of the time line shows the time left, or, after a click, the clock time the film ends (at the current speed)
  let showEnd = localStorage.getItem('kinora-endtime') === '1';
  remEl.onclick = e => { e.stopPropagation(); showEnd = !showEnd; try { localStorage.setItem('kinora-endtime', showEnd ? '1' : ''); } catch { /* private mode */ } paintTimeline(); };
  const rail = h('div.rail'), ready = h('div.ready'), done = h('div.done'), knob = h('div.knob'), hoverT = h('div.hover-t.hidden');
  const timeline = h('div.timeline', { role: 'slider', tabindex: 0, 'aria-label': T.timeline, 'aria-valuemin': 0 }, rail, ready, done, knob, hoverT);
  const playBtn = h('button.p-btn.p-main', { 'aria-label': T.play }, icon('play'));
  const bigPlay = h('button.p-big', { 'aria-label': T.play, onclick: () => togglePlay() }, icon('play')); // paused: a large target in the middle of the picture
  const volBtn = h('button.p-btn', { 'aria-label': T.mute }, icon('vol'));
  const vol = h('input.vol', { type: 'range', min: 0, max: 1, step: 0.05, value: localStorage.getItem('kinora-vol') || 1, 'aria-label': T.volume });
  const nextBtn = h('button.p-btn.hidden', { 'aria-label': T.nextEp, title: T.nextEp + ' (N)' }, icon('next'));
  const ccBtn = h('button.p-btn', { 'aria-label': T.subtitle + ' / ' + T.audioTrack, title: T.subtitle + ' (C)' }, icon('cc'));
  const gearBtn = h('button.p-btn', { 'aria-label': T.quality }, icon('gear'));
  const whyBtn = h('button.p-btn', { 'aria-label': T.why, title: T.why }, icon('info'));
  const favBtn = h('button.p-btn', { 'aria-label': T.favorite, title: T.favorite }, icon('heart'));
  const moreBtn = h('button.p-btn', { 'aria-label': T.moreActions, title: T.moreActions }, icon('more', true));
  const fsBtn = h('button.p-btn' + (TV ? '.hidden' : ''), { 'aria-label': T.fullscreen, title: T.fullscreen + ' (F)' }, icon('fs'));
  const chapBtn = h('button.p-btn.hidden', { 'aria-label': T.chapters, title: T.chapters }, icon('list'));
  const appPip = !TV && !!APP?.canPip?.(); // the Android app's own picture-in-picture (its WebView has none)
  const pipBtn = (document.pictureInPictureEnabled || appPip) && !TV ? h('button.p-btn', { 'aria-label': T.pip, title: T.pip + ' (P)' }, icon('pip')) : null;
  // Chromecast from the Android app; not in a watch party (everyone plays on their own screen there)
  const castApi = !TV && !q.get('party') ? APP?.cast || null : null;
  const castBtn = castApi ? h('button.p-btn', { 'aria-label': T.cast, title: T.cast }, icon('cast')) : null;
  const castNote = h('div.cast-note');
  const pop = h('div.p-pop.hidden');
  const upnext = h('div.upnext.hidden');
  const skipBtn = h('button.btn.primary.big.skip-seg.hidden', { onclick: () => doSkip() });
  const ui = h('div.ui', null,
    h('div.p-top', null, h('button.p-btn', { 'aria-label': T.back, title: T.back, onclick: () => exit() }, icon('back')), h('div.p-acts', null, favBtn, moreBtn, whyBtn)),
    mid, bigPlay,
    // bottom: title, the time line with elapsed and remaining at its ends, then three groups: sound | transport | the rest
    h('div.p-bottom', null,
      h('div.p-head', null, titleEl, subEl),
      h('div.p-line', null, timeEl, timeline, remEl),
      h('div.p-bar', null,
        h('div.p-l', null, !TV && h('div.p-vol', null, volBtn, vol)), // a TV's volume is the remote's
        h('div.p-c', null,
          h('button.p-btn', { 'aria-label': T.rewind, title: T.rewind, onclick: () => seekBy(-10) }, icon('rw')),
          playBtn,
          h('button.p-btn', { 'aria-label': T.forward, title: T.forward, onclick: () => seekBy(10) }, icon('ff')),
          nextBtn),
        h('div.p-r', null, chapBtn, ccBtn, gearBtn, castBtn, pipBtn, fsBtn))));
  const stage = h('div.stage', null, video, ui, pop, upnext, skipBtn);
  const player = h('div.player.paused', null, stage);
  root.append(player);

  // ---------------------------------------------------------------- state
  let info = null, item = null, hls = null, offset = 0, mode = 'direct', session = null;
  let audioIdx = null, burnIdx = null, maxBitrate = 0, force = '', subIdx = -1, showAllLangs = false;
  let duration = 0, credits = null, nextItem = null, nextShown = false, nextTimer = null, lastReport = 0, exiting = false;
  let dragging = false, dead = false;
  let segments = [], shownSeg = null, markStart = null;
  let subDelay = 0, subCache = null; // seconds added to every cue (+ = later); parsed cues of the current track
  const SPEEDS = [0.5, 0.75, 1, 1.25, 1.5, 2];
  const skipped = new Set();
  const caps = detectCaps();
  const partyCode = q.get('party');
  let party = null;
  video.volume = +vol.value;

  // casting: the Chromecast plays the stream, the page follows its state (castSt, polled every second)
  let casting = false, castSt = {}, castPoll = null, castEnded = false;
  const CAST_CAPS = { video: ['h264'], audio: ['aac'], containers: ['mp4'] }; // what the Default Media Receiver plays everywhere
  const pos = () => offset + ((casting ? castSt.position : video.currentTime) || 0);
  const isPaused = () => casting ? !castSt.playing : video.paused;
  const isHLS = () => mode !== 'direct';
  const transcodedEnd = () => {
    if (!isHLS()) return duration;
    if (casting) return offset + (castSt.duration || 0);
    let end = 0;
    if (hls && hls.levels && hls.levels[hls.currentLevel >= 0 ? hls.currentLevel : 0]?.details) end = hls.levels[hls.currentLevel >= 0 ? hls.currentLevel : 0].details.totalduration;
    else if (video.seekable && video.seekable.length) end = video.seekable.end(video.seekable.length - 1);
    return offset + end;
  };

  async function start(startAt, opts = {}) {
    if (dead) return;
    clearUpNext();
    put(mid, spinner);
    const body = { caps: casting ? CAST_CAPS : caps, start: startAt, max_bitrate: maxBitrate, force: casting && force === 'remux' ? '' : force, prev_session: session || '' };
    if (audioIdx != null) body.audio_idx = audioIdx;
    if (burnIdx != null) body.burn_sub_idx = burnIdx;
    let r;
    try { r = await POST(`/api/items/${id}/playback`, body, { quiet: true }); }
    catch (e) { return showError(e.message, e.body && e.body.reasons); }
    if (dead) { if (r.session) fetch('/api/hls/' + r.session, { method: 'DELETE', credentials: 'same-origin' }); return; }
    // the server sends chapters with Go field names (Start, Title)
    r.chapters = (r.chapters || []).map(c => ({ start: c.start ?? c.Start ?? 0, title: c.title ?? c.Title ?? '' }));
    info = r; mode = r.mode; session = r.session || null; offset = r.start_offset || 0;
    duration = r.duration || 0; credits = r.credits_start ?? null;
    segments = r.segments || [];
    const cr = segments.find(g => g.type === 'credits');
    if (cr) credits = cr.start;
    if (audioIdx == null) audioIdx = r.audio_idx;
    if (hls) { hls.destroy(); hls = null; }
    video.removeAttribute('src');
    const autoplay = opts.paused ? false : true;
    if (casting) {
      castEnded = false;
      castSt = { connected: true, position: mode === 'direct' ? startAt : 0, playing: false };
      const art = item && imgURL(item, item.kind === 'episode' ? 'thumb' : 'backdrop');
      castApi.load({
        url: new URL(r.url, location.href).href, mime: mode === 'direct' ? 'video/mp4' : 'application/x-mpegURL',
        title: item ? (item.kind === 'episode' ? item.series_title : item.title) : '',
        subtitle: item && item.kind === 'episode' ? `${T.ep(item.season_num, item.episode_num)} · ${item.title}` : '',
        image: art ? new URL(art, location.href).href : '', start: mode === 'direct' ? startAt : 0,
      });
      put(mid, castNote);
      paintTimeline();
      return;
    }
    if (mode === 'direct') {
      video.src = r.url;
      video.addEventListener('loadedmetadata', () => { if (startAt > 0) video.currentTime = startAt; }, { once: true });
    } else if (window.Hls && Hls.isSupported() && (window.MediaSource || !video.canPlayType('application/vnd.apple.mpegurl'))) {
      // Safari's ManagedMediaSource lets the browser pause loading ("endstreaming") and it does not always resume it,
      // so playback stopped at the end of the first ~80 s; classic MediaSource keeps hls.js in charge. Where only
      // ManagedMediaSource exists (iPhone) the native HLS player below is used instead.
      hls = new Hls({ maxBufferLength: 30, backBufferLength: 60, startPosition: 0, manifestLoadingMaxRetry: 6, levelLoadingMaxRetry: 6, fragLoadingMaxRetry: 6, preferManagedMediaSource: !window.MediaSource });
      hls.on(Hls.Events.ERROR, (_, data) => {
        if (!data.fatal) return;
        if (data.type === Hls.ErrorTypes.MEDIA_ERROR) { hls.recoverMediaError(); return; }
        showError(T.playErr + ': ' + (data.details || data.type));
      });
      hls.on(Hls.Events.LEVEL_UPDATED, paintTimeline);
      hls.loadSource(r.url);
      hls.attachMedia(video);
    } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
      video.src = r.url;
    } else {
      return showError(T.noHls);
    }
    if (autoplay) video.play().catch(() => { playBtn.focus(); });
    applySubtitle();
    paintTimeline();
  }

  async function seekTo(t) {
    t = Math.max(0, Math.min(duration ? duration - 1 : t, t));
    if (casting) {
      if (!isHLS()) { castApi.cmd('seek', t); castSt.position = t; paintTimeline(); return; }
      if (t >= offset && t <= transcodedEnd() - 1) { castApi.cmd('seek', t - offset); castSt.position = t - offset; paintTimeline(); return; }
      await start(t);
      return;
    }
    if (!isHLS()) { video.currentTime = t; return; }
    if (t >= offset && t <= transcodedEnd() - 1) { video.currentTime = t - offset; return; }
    await start(t, { paused: video.paused && !party });
  }
  function seekBy(d) { buzz('light'); userSeek(pos() + d); }
  function userSeek(t) { if (party) party.send({ t: 'seek', pos: t }); seekTo(t); }
  function togglePlay() {
    buzz('light');
    if (casting) { castApi.cmd(castSt.playing ? 'pause' : 'play'); castSt.playing = !castSt.playing; paintCastState(); return; }
    if (video.paused) { if (party) party.send({ t: 'play' }); video.play().catch(() => {}); }
    else { if (party) party.send({ t: 'pause', pos: pos() }); video.pause(); }
  }

  function showError(msg, reasons) {
    put(mid, h('div.p-error', { role: 'alert' }, h('h2', null, T.playErr), h('p', null, msg),
      reasons && reasons.length && h('ul.why-list.muted', null, reasons.map(r => h('li', null, srv(r)))),
      h('div.row-gap', null, h('button.btn.primary', { onclick: () => start(pos()) }, T.retry),
        force !== 'transcode' && h('button.btn', { onclick: () => { force = 'transcode'; start(pos()); } }, T.forceTranscode),
        h('button.btn', { onclick: () => exit() }, T.back))));
  }

  // ---------------------------------------------------------------- subtitles (fetched VTT, shifted by the HLS start offset)
  let subTrack = null, subGen = 0, cueLine = -3, cueProbe = null;
  // cues are only readable while the track is not 'disabled', so empty it first
  const clearCues = () => { if (subTrack) { subTrack.mode = 'hidden'; for (const c of [...(subTrack.cues || [])]) subTrack.removeCue(c); subTrack.mode = 'disabled'; } };
  async function applySubtitle() {
    const my = ++subGen; // a newer call wins; stale fetches are dropped
    clearCues();
    if (subIdx < 0 || !info) return;
    const st = info.streams.find(s => s.type === 'subtitle' && s.type_index === subIdx);
    if (!st || !st.text_based) return;
    try {
      let cues = subCache && subCache.idx === subIdx ? subCache.cues : null;
      if (!cues) {
        const res = await fetch(`/api/items/${id}/subtitles/${subIdx}`, { credentials: 'same-origin' });
        if (!res.ok) throw new Error(srv((await res.json().catch(() => ({}))).error) || res.status);
        cues = parseVTT(await res.text());
        if (my !== subGen) return;
        subCache = { idx: subIdx, cues };
      }
      clearCues();
      if (!subTrack) subTrack = video.addTextTrack('subtitles', 'Kinora', st.lang || 'und');
      for (const c of cues) {
        const s = c.start + subDelay - offset, e = c.end + subDelay - offset;
        if (e <= 0) continue;
        try { const cue = new VTTCue(Math.max(0, s), e, c.text); cue.line = cueLine; subTrack.addCue(cue); } catch { /* ignore */ }
      }
      subTrack.mode = 'showing';
      placeCues();
    } catch (e) { toast(T.subtitle + ': ' + e.message, true); }
  }
  // A cue sits three lines above the picture's bottom edge. While the controls show, it moves up above them:
  // on a phone held upright the picture fills the screen and three lines are not enough to clear the bar.
  function placeCues() {
    if (!subTrack || !subTrack.cues) return;
    let n = -3;
    if (!player.classList.contains('idle')) {
      if (!cueProbe) cueProbe = player.appendChild(h('span', { 'aria-hidden': 'true', style: { position: 'absolute', visibility: 'hidden', fontSize: 'var(--cue)', lineHeight: 'var(--lh-cue)' } }, 'X'));
      // the bar's own top padding is only its fade; the title above the timeline is the first thing a cue must clear
      const step = cueProbe.getBoundingClientRect().height, bar = ui.querySelector('.p-bottom > .p-head') || ui.querySelector('.p-bottom');
      const covered = bar ? video.getBoundingClientRect().bottom - bar.getBoundingClientRect().top : 0;
      if (step > 0 && covered > 0) n = -Math.max(3, Math.ceil(covered / step) + 1);
    }
    if (n === cueLine) return;
    cueLine = n;
    for (const c of [...subTrack.cues]) c.line = n;
    if (subTrack.mode === 'showing') { subTrack.mode = 'hidden'; subTrack.mode = 'showing'; } // a cue on screen keeps its old place until redrawn
  }
  const cueWatch = new MutationObserver(placeCues);
  cueWatch.observe(player, { attributes: true, attributeFilter: ['class'] });
  addEventListener('resize', placeCues);
  onCleanup(() => { cueWatch.disconnect(); removeEventListener('resize', placeCues); });
  function parseVTT(text) {
    const out = [];
    const ts = s => { const p = s.trim().split(':').map(Number); return p.length === 3 ? p[0] * 3600 + p[1] * 60 + p[2] : p[0] * 60 + p[1]; };
    for (const block of text.replace(/\r/g, '').split(/\n\n+/)) {
      const lines = block.split('\n');
      const i = lines.findIndex(l => l.includes('-->'));
      if (i < 0) continue;
      const [a, b] = lines[i].split('-->');
      out.push({ start: ts(a), end: ts(b.trim().split(' ')[0]), text: lines.slice(i + 1).join('\n').replace(/<[^>]+>/g, '') });
    }
    return out;
  }
  function chooseInitialSubtitle() {
    if (typeof info.sub_idx === 'number') { // the server picked it (the title's choice, then the profile's)
      subIdx = info.streams.some(s => s.type === 'subtitle' && s.text_based && s.type_index === info.sub_idx) ? info.sub_idx : -1;
      return;
    }
    const langs = (profile.sub_langs || '').split(',').filter(Boolean);
    const subs = info.streams.filter(s => s.type === 'subtitle' && s.text_based);
    if (profile.sub_mode === 'off' || !subs.length) return;
    const audio = info.streams.find(s => s.type === 'audio' && s.type_index === audioIdx);
    const forced = subs.find(s => s.forced && langs.includes(s.lang));
    const audioForeign = !audio || !langs.length || audio.lang !== langs[0];
    if (profile.sub_mode === 'always' || audioForeign) {
      for (const l of langs) { const s = subs.find(x => x.lang === l && !x.forced); if (s) { subIdx = s.type_index; return; } }
    }
    if (forced) subIdx = forced.type_index;
  }

  // ---------------------------------------------------------------- popovers
  function openPop(build) {
    if (!pop.classList.contains('hidden') && pop._b === build) { pop.classList.add('hidden'); return; }
    pop._b = build; put(pop, build()); pop.classList.remove('hidden');
    pop.classList.toggle('at-top', build === buildWhy || build === buildMore); // opened from the top bar
    pop.querySelector('button')?.focus();
  }
  const opt = (label, on, fn) => h('button.opt' + (on ? '.on' : ''), { 'aria-pressed': on ? 'true' : 'false', onclick: () => { pop.classList.add('hidden'); fn(); } }, on ? icon('check') : h('span', { class: 'opt-gap' }), label);
  // a language picked in the player is kept for the film / series: the next episode starts with it too
  function keepLangs(v) {
    if (v.audio_lang === '' || v.sub_lang === '') return; // a track without a language says nothing to keep
    api('PUT', `/api/items/${id}/langs`, v, { quiet: true }).catch(() => {});
  }
  function buildTracks() {
    const langs = (profile.sub_langs || '').split(',').filter(Boolean), alangs = (profile.audio_langs || '').split(',').filter(Boolean);
    const audios = info.streams.filter(s => s.type === 'audio');
    const subs = info.streams.filter(s => s.type === 'subtitle');
    const visA = showAllLangs || !alangs.length ? audios : audios.filter(a => alangs.includes(a.lang) || a.type_index === audioIdx);
    const visS = showAllLangs || !langs.length ? subs : subs.filter(s => langs.includes(s.lang) || s.type_index === subIdx || s.type_index === burnIdx);
    const hiddenCount = (audios.length - visA.length) + (subs.length - visS.length);
    return [
      h('h3', null, T.audioTrack),
      ...visA.map(a => opt(`${langName(a.lang)} – ${a.codec}${a.channels ? ' ' + a.channels + 'ch' : ''}${a.title ? ' (' + a.title + ')' : ''}`, a.type_index === audioIdx, () => { audioIdx = a.type_index; keepLangs({ audio_lang: a.lang }); start(pos()); })),
      h('h3', null, T.subtitle),
      opt(T.subOff, subIdx < 0 && burnIdx == null, () => { subIdx = -1; keepLangs({ sub_lang: 'off' }); if (burnIdx != null) { burnIdx = null; start(pos()); } else applySubtitle(); }),
      ...visS.map(s => s.text_based
        ? opt(`${langName(s.lang)}${s.forced ? ` (${T.forced})` : ''}${s.external ? ' · ' + T.external : ''}`, s.type_index === subIdx, () => { subIdx = s.type_index; keepLangs({ sub_lang: s.lang }); if (burnIdx != null) { burnIdx = null; start(pos()); } else applySubtitle(); })
        : opt(`${langName(s.lang)} · ${T.burnIn} (${s.codec})`, s.type_index === burnIdx, () => { subIdx = -1; burnIdx = s.type_index; keepLangs({ sub_lang: s.lang }); applySubtitle(); start(pos()); })),
      h('button.opt', { onclick: () => { pop._b = null; put(pop, h('h3', null, T.subSearch), subSearchPanel(item, st => {
        info.streams.push(st); subIdx = st.type_index; subCache = null; applySubtitle(); pop.classList.add('hidden');
      })); pop.classList.remove('hidden'); } }, h('span', { class: 'opt-gap' }), T.subSearch + '…'),
      (hiddenCount > 0 || showAllLangs) && h('label.check', { class: 'mt3' }, h('input', { type: 'checkbox', checked: showAllLangs, onchange: e => { showAllLangs = e.target.checked; pop._b = null; openPop(buildTracks); } }), T.showAll),
      subIdx >= 0 && delayControl(),
    ];
  }
  const fmtDelay = () => (subDelay > 0 ? '+' : subDelay < 0 ? '−' : '') + num(Math.abs(subDelay), 1) + ' s';
  function shiftSub(d) {
    subDelay = d === 0 ? 0 : Math.round((subDelay + d) * 10) / 10;
    applySubtitle();
    pop.querySelector('.sub-delay')?.replaceChildren(fmtDelay());
  }
  function delayControl() {
    return h('div', null, h('h3', null, T.subDelay + ' (G / H)'),
      h('div.row-gap', null, h('button.btn', { onclick: () => shiftSub(-0.5) }, '−' + num(0.5, 1)), h('b.sub-delay', { 'aria-live': 'polite' }, fmtDelay()),
        h('button.btn', { onclick: () => shiftSub(0.5) }, '+' + num(0.5, 1)), h('button.btn.ghost', { onclick: () => shiftSub(0) }, '0')));
  }
  function buildQuality() {
    const q = [[0, T.qAuto], [8000, '8 Mbps'], [4000, '4 Mbps'], [2000, '2 Mbps'], [1000, '1 Mbps']];
    return [h('h3', null, T.quality), ...q.map(([v, l]) => opt(l, maxBitrate === v, () => { maxBitrate = v; force = v ? 'transcode' : ''; start(pos()); })),
      ...(party ? [] : [h('h3', null, T.speed + ' (< / >)'), ...SPEEDS.map(r => opt(num(r, r % 0.5 ? 2 : 1).replace(/[.,]0+$/, '') + '×', video.playbackRate === r, () => setSpeed(r)))])];
  }
  // the party keeps everyone at 1× (its sync nudges the rate), so speed is a solo feature
  function setSpeed(r) {
    if (party) return;
    video.defaultPlaybackRate = video.playbackRate = r; // the default survives a restarted stream
    toast(T.speed + ': ' + num(r, 2).replace(/[.,]?0+$/, '') + '×');
  }
  function stepSpeed(d) { const i = SPEEDS.indexOf(video.playbackRate); setSpeed(SPEEDS[Math.max(0, Math.min(SPEEDS.length - 1, (i < 0 ? 2 : i) + d))]); }
  function buildChapters() {
    const p = pos(), chs = info.chapters || [];
    const cur = chs.filter(c => c.start <= p).pop();
    return [h('h3', null, T.chapters), ...chs.map((c, i) => opt(`${fmtTime(c.start)}  ${c.title || T.chapters + ' ' + (i + 1)}`, c === cur, () => userSeek(c.start)))];
  }
  function togglePiP() {
    if (!pipBtn || casting) return;
    if (appPip) { if (!video.paused || video.currentTime > 0) APP.pip(); return; }
    if (document.pictureInPictureElement) document.exitPictureInPicture().catch(() => {});
    else video.requestPictureInPicture().catch(() => {});
  }
  function buildWhy() {
    const src = info.source || {};
    return [h('h3', null, T.whyTitle[mode] || mode),
      h('ul.why-list', null, (info.reasons || []).map(r => h('li', null, srv(r)))),
      info.encoder && h('p', { class: 'mt3' }, h('span.muted', null, T.encoder + ': '), srv(info.encoder_label) || info.encoder),
      info.bitrate && h('p.muted.small', null, T.target(num(info.bitrate / 1e6, 1), info.width)),
      h('p.muted.small', null, `${T.source}: ${(src.container || '').toUpperCase()} · ${src.video_codec || '?'} ${src.width}×${src.height} · ${src.audio_codec || '–'}${src.hdr ? ' · ' + src.hdr : ''}`),
      h('p.muted.small', null, `${T.browser}: ${caps.video.join(', ')} | ${caps.audio.join(', ')}`)];
  }
  function buildMore() {
    return [h('h3', null, item ? item.title : ''),
      opt(T.addToCollection, false, () => addToCollectionMenu(item, moreBtn)),
      opt(T.tags + '…', false, () => modal(T.tags, tagEditor(item), close => [h('button.btn', { onclick: close }, T.close)])),
      !partyCode && opt(T.startParty, false, async () => { const r = await POST('/api/party', { item_id: id }); exiting = true; await report(); go(`#/play/${id}?party=${r.code}&t=${Math.floor(pos())}`); }),
      state.me.user.is_admin && opt(T.markIntroStart + ` (${fmtTime(pos())})`, false, () => { markStart = pos(); toast(T.markIntroStart + ': ' + fmtTime(markStart)); }),
      state.me.user.is_admin && opt(T.markIntroEnd + ` (${fmtTime(pos())})`, false, () => {
        if (markStart == null || pos() <= markStart) return toast(T.markStartFirst, true);
        saveMark('intro', markStart, pos()); markStart = null;
      }),
      state.me.user.is_admin && opt(T.markCredits + ` (${fmtTime(pos())})`, false, () => saveMark('credits', pos(), duration || pos() + 60)),
      state.me.user.is_admin && segments.some(g => g.source === 'manual') && opt(T.clearIntro, false, async () => {
        await api('DELETE', `/api/items/${id}/segments/intro`); const f = await GET(`/api/items/${id}/segments`); segments = f.segments; toast(T.deleted);
      }),
    ];
  }
  ccBtn.onclick = () => info && openPop(buildTracks);
  gearBtn.onclick = () => info && openPop(buildQuality);
  whyBtn.onclick = () => info && openPop(buildWhy);
  moreBtn.onclick = () => item && openPop(buildMore);
  chapBtn.onclick = () => info && openPop(buildChapters);
  if (pipBtn) pipBtn.onclick = togglePiP;

  // ---------------------------------------------------------------- Chromecast
  // Connecting moves playback there from where it is (a fresh stream the receiver plays: H.264/AAC);
  // disconnecting brings it back here, paused, at the receiver's position.
  function paintCastState() {
    const paused = !castSt.playing;
    player.classList.toggle('paused', paused);
    put(playBtn, icon(paused ? 'play' : 'pause')); playBtn.setAttribute('aria-label', paused ? T.play : T.pause);
  }
  function castTick() {
    if (!casting || dead) return;
    const s = castApi.state();
    if (!s.connected) { castOff(); return; }
    castSt = s;
    put(castNote, icon('cast'), h('span', null, T.castingTo(s.device || 'Chromecast')));
    if (mid.firstChild !== castNote) put(mid, castNote);
    paintCastState();
    if (!dragging) paintTimeline();
    const now = Date.now();
    if (now - lastReport > 10000 && s.playing) { lastReport = now; report(); }
    checkUpNext();
    checkSkip();
    if (s.failed && !castEnded) { castEnded = true; toast(T.castFailed, true); }
    if (s.ended && !castEnded) {
      castEnded = true;
      report(true);
      if (nextItem && profile.autoplay_next) playNext(); else if (!nextItem) exit();
    }
  }
  function castOn() {
    if (casting || dead || !castApi.connected()) return;
    const at = info ? pos() : resumeAt0;
    casting = true;
    castBtn.classList.add('on');
    video.pause(); APP?.keepAwake(false);
    if (ms) ms.playbackState = 'none'; // the Cast SDK shows its own notification meanwhile
    clearInterval(castPoll); castPoll = setInterval(castTick, 1000);
    if (info) start(at);
  }
  function castOff() {
    if (!casting) return;
    const at = pos();
    casting = false;
    clearInterval(castPoll); castPoll = null;
    castBtn.classList.remove('on');
    if (mid.firstChild === castNote) put(mid);
    if (dead) return;
    setupMediaSession(); if (ms) ms.playbackState = 'paused';
    start(at, { paused: true });
  }
  let resumeAt0 = 0;
  const onCast = e => { const ev = e.detail || {}; if (ev.event === 'connected') castOn(); else if (ev.event === 'disconnected') castOff(); };
  if (castBtn) {
    castBtn.onclick = () => castApi.pick();
    addEventListener('kinora-cast', onCast);
  }
  favBtn.onclick = async () => {
    if (!item) return;
    buzz('medium');
    const v = !item.user_data.favorite;
    await POST(`/api/items/${id}/favorite`, { favorite: v }); // on failure nothing changes
    item.user_data.favorite = v; favBtn.classList.toggle('on', v); put(favBtn, icon('heart', v));
  };

  // ---------------------------------------------------------------- timeline & controls
  function paintTimeline() {
    const d = duration || 1, p = pos();
    done.style.width = (p / d * 100) + '%';
    knob.style.left = (p / d * 100) + '%';
    const te = transcodedEnd();
    ready.style.left = (isHLS() ? offset / d * 100 : 0) + '%';
    ready.style.width = (Math.max(0, Math.min(d, te) - (isHLS() ? offset : 0)) / d * 100) + '%';
    timeEl.textContent = fmtTime(p);
    const left = Math.max(0, duration - p);
    remEl.textContent = !duration ? '' : showEnd
      ? new Date(Date.now() + left / (video.playbackRate || 1) * 1000).toLocaleTimeString(T.locale, { hour: '2-digit', minute: '2-digit' })
      : '−' + fmtTime(left);
    timeline.setAttribute('aria-valuemax', Math.round(duration));
    timeline.setAttribute('aria-valuenow', Math.round(p));
    timeline.setAttribute('aria-valuetext', fmtTime(p));
  }
  function chapterMarks() {
    timeline.querySelectorAll('.chap').forEach(c => c.remove());
    if (!duration) return;
    for (const c of info.chapters || []) if (c.start > 1) timeline.append(h('div.chap', { title: c.title, style: { left: (c.start / duration * 100) + '%' } }));
  }
  const tAt = e => { const r = timeline.getBoundingClientRect(); return Math.max(0, Math.min(1, ((e.clientX ?? e.touches?.[0]?.clientX) - r.left) / r.width)) * duration; };
  timeline.addEventListener('pointerdown', e => { dragging = true; timeline.setPointerCapture(e.pointerId); });
  timeline.addEventListener('pointermove', e => {
    const t = tAt(e);
    hoverT.classList.remove('hidden'); hoverT.style.left = (t / (duration || 1) * 100) + '%';
    const ch = (info?.chapters || []).filter(c => c.start <= t).pop();
    hoverT.textContent = fmtTime(t) + (ch && ch.title ? ' · ' + ch.title : '');
    if (dragging) { done.style.width = knob.style.left = (t / (duration || 1) * 100) + '%'; }
  });
  timeline.addEventListener('pointerleave', () => hoverT.classList.add('hidden'));
  timeline.addEventListener('pointerup', e => { if (dragging) { dragging = false; userSeek(tAt(e)); } });
  timeline.addEventListener('keydown', e => { if (e.key === 'ArrowLeft') { e.preventDefault(); e.stopPropagation(); seekBy(-10); } if (e.key === 'ArrowRight') { e.preventDefault(); e.stopPropagation(); seekBy(10); } });
  playBtn.onclick = togglePlay;
  video.addEventListener('click', togglePlay);
  video.addEventListener('dblclick', toggleFS);
  vol.oninput = () => { video.volume = +vol.value; video.muted = false; localStorage.setItem('kinora-vol', vol.value); paintVol(); };
  volBtn.onclick = () => { video.muted = !video.muted; paintVol(); };
  const paintVol = () => {
    put(volBtn, icon(video.muted || video.volume === 0 ? 'mute' : 'vol'));
    vol.style.setProperty('--v', (video.muted ? 0 : video.volume) * 100 + '%'); // the filled part of the slider track
  };
  const setVol = v => { // keys behave like the slider: unmute, remember
    video.volume = Math.round(Math.max(0, Math.min(1, v)) * 100) / 100; video.muted = false;
    vol.value = video.volume; localStorage.setItem('kinora-vol', video.volume); paintVol();
  };
  fsBtn.onclick = toggleFS;
  function toggleFS() { if (document.fullscreenElement) document.exitFullscreen(); else player.requestFullscreen?.().catch(() => {}); }
  video.addEventListener('play', () => { APP?.keepAwake(true); player.classList.remove('paused'); put(playBtn, icon('pause')); playBtn.setAttribute('aria-label', T.pause); if (ms && !casting) ms.playbackState = 'playing'; });
  video.addEventListener('pause', () => { APP?.keepAwake(false); player.classList.add('paused'); wake(); /* paused: show the controls again */ put(playBtn, icon('play')); playBtn.setAttribute('aria-label', T.play); if (ms && !casting) ms.playbackState = 'paused'; report(); });
  const ms = navigator.mediaSession;
  const msAction = (a, f) => { try { ms.setActionHandler(a, f); } catch { /* action not supported here */ } };
  let lastPosState = 0;
  function setupMediaSession() {
    if (!ms || !window.MediaMetadata) return;
    const art = imgURL(item, item.kind === 'episode' ? 'thumb' : item.has_poster === false ? 'backdrop' : 'poster');
    ms.metadata = new MediaMetadata({ title: item.title, album: (state.status && state.status.server_name) || T.appName,
      artist: item.kind === 'episode' ? `${item.series_title} · ${T.ep(item.season_num, item.episode_num)}` : String(item.year || ''),
      artwork: [{ src: new URL(art, location.href).href, type: 'image/jpeg' }] });
    msAction('play', () => { if (video.paused) togglePlay(); });
    msAction('pause', () => { if (!video.paused) togglePlay(); });
    msAction('seekbackward', d => seekBy(-((d && d.seekOffset) || 10)));
    msAction('seekforward', d => seekBy((d && d.seekOffset) || 10));
    msAction('seekto', d => { if (d && d.seekTime != null) userSeek(d.seekTime); });
    msAction('nexttrack', nextItem ? () => playNext() : null);
  }
  function paintPositionState() {
    if (!ms || !ms.setPositionState || !duration || Date.now() - lastPosState < 1000) return;
    lastPosState = Date.now();
    try { ms.setPositionState({ duration, position: Math.min(duration, Math.max(0, pos())), playbackRate: video.playbackRate || 1 }); } catch { /* ignore */ }
  }
  // a stall that lasts while the playlist still has more to load: kick hls.js (buffering resumed, loading restarted here)
  let stallT = null;
  const unstall = () => { clearTimeout(stallT); stallT = null; };
  video.addEventListener('waiting', () => {
    put(mid, spinner); party?.buffering(true);
    unstall();
    if (!hls) return;
    const h0 = hls;
    stallT = setTimeout(() => {
      if (h0 !== hls || video.paused || video.readyState >= 3 || dead) return;
      console.warn('kinora: playback stalled, restarting loading at', video.currentTime);
      if (hls.resumeBuffering) hls.resumeBuffering();
      hls.startLoad(video.currentTime);
    }, 4000);
  });
  video.addEventListener('playing', () => { unstall(); if (mid.firstChild === spinner) put(mid); party?.buffering(false); });
  video.addEventListener('canplay', () => { if (mid.firstChild === spinner && !video.paused) put(mid); party?.buffering(false); });
  video.addEventListener('loadeddata', () => { if (video.paused && mid.firstChild === spinner) put(mid); });
  video.addEventListener('timeupdate', () => {
    if (!dragging) paintTimeline();
    paintPositionState();
    const now = Date.now();
    if (now - lastReport > 10000 && !video.paused) { lastReport = now; report(); }
    checkUpNext();
    checkSkip();
  });
  video.addEventListener('ended', () => {
    if (dead) return;
    if (isHLS() && transcodedEnd() < duration - 2) return; // HLS still producing
    report(true);
    if (nextItem && profile.autoplay_next && !party) playNext(); else if (!nextItem && !party) exit();
  });
  video.addEventListener('error', () => { if (!dead && mode === 'direct' && video.error && video.getAttribute('src')) { force = 'remux'; start(pos()); } });

  // idle hide
  let idleT;
  const wake = () => { player.classList.remove('idle'); clearTimeout(idleT); idleT = setTimeout(() => { if (!video.paused && pop.classList.contains('hidden')) player.classList.add('idle'); }, 3000); };
  stage.addEventListener('mousemove', wake); stage.addEventListener('touchstart', wake, { passive: true });
  wake();

  // keyboard
  const keys = e => {
    if (e.target.matches('input, textarea, select')) return;
    if (document.querySelector('.modal-back')) return;
    const wasIdle = player.classList.contains('idle');
    wake();
    if (TV && tvKeys(e, wasIdle)) return;
    switch (e.key) {
      case ' ': case 'k': e.preventDefault(); togglePlay(); break;
      case 'ArrowLeft': if (e.target !== timeline) { e.preventDefault(); seekBy(-10); } break;
      case 'ArrowRight': if (e.target !== timeline) { e.preventDefault(); seekBy(10); } break;
      case 'ArrowUp': e.preventDefault(); setVol(video.volume + 0.05); break;
      case 'ArrowDown': e.preventDefault(); setVol(video.volume - 0.05); break;
      case 'p': case 'P': togglePiP(); break;
      case 'g': case 'G': if (subIdx >= 0) { shiftSub(-0.1); toast(T.subDelay + ': ' + fmtDelay()); } break;
      case 'h': case 'H': if (subIdx >= 0) { shiftSub(0.1); toast(T.subDelay + ': ' + fmtDelay()); } break;
      case '<': stepSpeed(-1); break;
      case '>': stepSpeed(1); break;
      case 'f': case 'F': toggleFS(); break;
      case 'm': case 'M': video.muted = !video.muted; paintVol(); break;
      case 'c': case 'C': if (info) openPop(buildTracks); break;
      case 'n': case 'N': if (nextItem) playNext(); break;
      case 'Escape': if (!pop.classList.contains('hidden')) pop.classList.add('hidden'); else if (!document.fullscreenElement) exit(); break;
    }
  };
  document.addEventListener('keydown', keys);
  // TV remote: with the controls hidden left/right seek, OK plays/pauses and up/down bring the controls up;
  // once they are on screen the arrows move between them (spatialNav) and OK presses the lit one.
  function tvKeys(e, wasIdle) {
    if (e.defaultPrevented) return true; // spatialNav already moved the focus
    const a = document.activeElement;
    const onCtl = !wasIdle && a && a !== video && a !== timeline && a !== document.body && player.contains(a) && a.offsetParent !== null;
    switch (e.key) {
      case 'ArrowUp': case 'ArrowDown':
        e.preventDefault();
        if (!onCtl && !(a === timeline && !wasIdle)) (pop.classList.contains('hidden') ? playBtn : pop.querySelector('button') || playBtn).focus();
        return true;
      case 'ArrowLeft': case 'ArrowRight':
        return onCtl;
      case 'Enter':
        if (onCtl) return true;
        e.preventDefault(); togglePlay();
        return true;
    }
    return false;
  }

  // ---------------------------------------------------------------- progress & next episode
  async function report(ended) {
    if (!item || (!ended && pos() < 1)) return;
    try { await POST(`/api/items/${id}/progress`, { position: pos(), ended: !!ended }, { keepalive: true, quiet: true }); } catch { /* offline */ }
  }
  // ---------------------------------------------------------------- skip intro / recap / credits
  function currentSeg() {
    const p = pos();
    return segments.find(g => g.type !== 'preview' && p >= g.start && p < g.end - 1) || null;
  }
  function checkSkip() {
    const mode = profile.skip_intro || 'manual';
    let g = mode === 'off' ? null : currentSeg();
    if (g && g.type === 'credits' && nextItem) g = null; // the next-episode card takes over
    if (g && mode === 'auto' && !skipped.has(g.type + g.start) && !party) {
      skipped.add(g.type + g.start);
      toast(T.skippedIntro);
      userSeek(g.end);
      return;
    }
    if (g === shownSeg) return;
    shownSeg = g;
    skipBtn.classList.toggle('hidden', !g);
    if (g) skipBtn.textContent = g.type === 'credits' ? T.skipCredits : g.type === 'recap' ? T.skipRecap : T.skipIntro;
  }
  function doSkip() {
    const g = currentSeg();
    if (!g) return;
    buzz('heavy');
    skipped.add(g.type + g.start);
    skipBtn.classList.add('hidden'); shownSeg = null;
    if (party) party.send({ t: 'skip', pos: g.end, kind: g.type }); // everyone in the party jumps
    seekTo(g.end);
  }
  async function saveMark(type, start, end) {
    const r = await api('PUT', `/api/items/${id}/segments/${type}`, { start, end });
    toast(T.markSaved);
    const fresh = await GET(`/api/items/${id}/segments`).catch(() => null);
    if (fresh) segments = fresh.segments;
    if (r.can_propagate && await confirmBox(T.propagateAsk, T.apply)) {
      const p = await POST(`/api/items/${id}/segments/${type}/propagate`, {});
      toast(T.propagated(p.updated));
    }
  }

  function checkUpNext() {
    if (!nextItem || nextShown || !duration) return;
    const at = credits ?? Math.max(0, duration - 30);
    if (pos() >= at) showUpNext();
  }
  function showUpNext() {
    nextShown = true;
    let n = 10;
    const count = h('span.count', null, T.nextIn(n));
    put(upnext, artImg(nextItem, 'thumb', nextItem.title), h('div.b', null,
      h('div.muted.small', null, T.nextEp + ' · ' + T.ep(nextItem.season_num, nextItem.episode_num)), h('b', null, nextItem.title),
      h('div', { class: 'mt2 mb3' }, profile.autoplay_next && !party ? count : null),
      h('div.row-gap', null, h('button.btn.primary', { onclick: playNext }, icon('play'), T.playNow), h('button.btn', { onclick: clearUpNext }, T.dismiss))));
    upnext.classList.remove('hidden');
    if (profile.autoplay_next && !party) nextTimer = setInterval(() => { n--; count.textContent = T.nextIn(n); if (n <= 0) playNext(); }, 1000);
  }
  function clearUpNext() { clearInterval(nextTimer); upnext.classList.add('hidden'); }
  async function playNext() {
    clearUpNext();
    if (!nextItem) return;
    buzz('heavy');
    await report(true);
    if (party) party.send({ t: 'item', item_id: nextItem.id, pos: 0 });
    exiting = true;
    go(`#/play/${nextItem.id}` + (partyCode ? `?party=${partyCode}` : ''));
  }

  async function exit() {
    if (exiting) return;
    exiting = true;
    await report();
    if (partyCode) { location.replace('#/home'); return; } // leaving a party lands on home, and back does not rejoin
    if (history.length > 1) history.back(); else go('#/item/' + id);
  }

  onCleanup(() => {
    dead = true;
    APP?.keepAwake(false);
    document.removeEventListener('keydown', keys);
    clearUpNext(); clearTimeout(idleT);
    if (!exiting) report();
    if (castBtn) removeEventListener('kinora-cast', onCast);
    if (casting) { castApi.cmd('stop'); clearInterval(castPoll); casting = false; }
    APP?.refresh?.(); // the app's widgets and Android TV's Play next row show where this was left
    if (hls) hls.destroy();
    video.pause(); video.removeAttribute('src'); video.load();
    if (session) fetch('/api/hls/' + session, { method: 'DELETE', credentials: 'same-origin', keepalive: true }).catch(() => {});
    if (party) party.close();
    if (document.fullscreenElement) document.exitFullscreen().catch(() => {});
    if (document.pictureInPictureElement === video) document.exitPictureInPicture().catch(() => {});
    if (ms) { ms.metadata = null; ms.playbackState = 'none'; for (const a of ['play', 'pause', 'seekbackward', 'seekforward', 'seekto', 'nexttrack']) msAction(a, null); }
  });

  // ---------------------------------------------------------------- go
  let detail;
  try { detail = await GET('/api/items/' + id, { quiet: true }); }
  catch (e) {
    if (!dead) put(mid, h('div.p-error', { role: 'alert' }, h('h2', null, e.status === 404 ? T.notFoundTitle : T.playErr), h('p', null, e.status === 404 ? T.notFoundLede : e.message),
      h('div.row-gap', null, h('button.btn.primary', { onclick: () => go('#/home') }, T.backHome))));
    return;
  }
  item = detail.item;
  if (item.imdb_id) versionPick[item.imdb_id] = item.id; // back on the detail page, show the version just played
  nextItem = detail.next || null;
  nextBtn.classList.toggle('hidden', !nextItem);
  nextBtn.onclick = playNext;
  put(favBtn, icon('heart', item.user_data.favorite)); favBtn.classList.toggle('on', item.user_data.favorite);
  titleEl.textContent = item.kind === 'episode' ? item.series_title : item.title;
  subEl.textContent = item.kind === 'episode' ? `${T.ep(item.season_num, item.episode_num)} · ${item.title}` : [item.year, item.content_rating].filter(Boolean).join(' · ');
  document.title = item.title + ' – ' + T.appName;
  const resumeAt = q.has('t') ? +q.get('t') : (item.user_data.position || 0);
  if (partyCode) party = partyClient(partyCode, player, stage, { video, pos, seekTo, getItemId: () => id, leave: () => exit(),
    onItem: nid => { exiting = true; location.replace(`#/play/${nid}?party=${partyCode}`); } });
  setupMediaSession();
  resumeAt0 = partyCode ? 0 : resumeAt;
  if (castApi && castApi.connected()) castOn(); // already casting: the next title goes there too
  await start(partyCode ? 0 : resumeAt, { paused: !!partyCode });
  if (info) {
    chooseInitialSubtitle(); applySubtitle(); chapterMarks();
    chapBtn.classList.toggle('hidden', (info.chapters || []).length < 2);
    if (mode === 'transcode') toast(`${T.whyTitle[mode]}: ${srv((info.reasons || [])[0]) || ''} – ${T.whyDetails}`);
  }
}

// ===================================================================== watch party client
function partyClient(code, player, stage, p) {
  const panel = h('aside.party', { 'aria-label': T.party });
  const membersEl = h('div.members');
  const chat = h('div.chat', { 'aria-live': 'polite' });
  const status = h('div.status');
  const everyone = h('input', { type: 'checkbox' });
  const msg = h('input', { type: 'text', maxlength: 500, placeholder: T.partyChatPh, 'aria-label': T.partyChatPh });
  const link = location.origin + '/#/party/' + code;
  const reacts = h('div.reacts');
  panel.append(
    h('button.btn.danger.party-leave', { onclick: () => p.leave() }, icon('back'), T.partyLeave),
    h('header', null, h('div.row-gap', { class: 'spread' }, h('div', null, h('div.code', { 'aria-label': T.party + ' ' + code }, code)),
      h('button.btn', { onclick: () => modal(T.partyLink, h('div', null, qrImg(link), h('p.mono', { class: 'mt3 break-all' }, link)), close => [APP?.share && h('button.btn', { onclick: () => APP.share(T.partyLink, link) }, icon('share'), T.share), h('button.btn', { onclick: () => copyText(link) }, T.copy), h('button.btn.primary', { onclick: close }, T.close)]) }, T.partyLink)),
      h('label.check.hidden', { class: 'mt3 mb0' }, everyone, T.partyEveryone), status),
    membersEl, chat, reacts,
    h('form', { onsubmit: e => { e.preventDefault(); if (msg.value.trim()) { send({ t: 'chat', text: msg.value }); msg.value = ''; } } }, msg, h('button.btn', { type: 'submit' }, T.partySend)));
  player.append(panel);

  let ws, myId = 0, offset = 0, bestRtt = Infinity, last = null, closed = false, retry = 0, pingT, syncT, applying = 0, bufState = false, lastSeq = 0;
  const serverNow = () => Date.now() + offset;
  const send = m => { if (ws && ws.readyState === 1) ws.send(JSON.stringify(m)); };
  everyone.onchange = () => send({ t: 'everyone', v: everyone.checked });

  function line(el) { chat.append(el); chat.scrollTop = chat.scrollHeight; }
  function connect() {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    ws = new WebSocket(`${proto}://${location.host}/api/party/${code}/ws`);
    ws.onopen = () => { retry = 0; status.textContent = ''; ping(); };
    ws.onmessage = ev => {
      const m = JSON.parse(ev.data);
      switch (m.t) {
        case 'hello': myId = m.id; put(reacts, ...m.reactions.map(e => h('button', { 'aria-label': e, onclick: () => send({ t: 'react', emoji: e }) }, e))); break;
        case 'pong': {
          const now = Date.now(), rtt = now - m.c;
          if (rtt <= bestRtt * 1.5 || rtt < 60) { bestRtt = Math.min(bestRtt, rtt); offset = m.s - (m.c + rtt / 2); }
          break;
        }
        case 'state': onState(m); break;
        case 'chat': line(h('div', null, h('b', { style: { color: m.color } }, m.name + ': '), m.text)); break;
        case 'sys': line(h('div.sys', null, srv(m.text))); break;
        case 'react': {
          const f = h('div.float-react', { style: { left: (10 + Math.random() * 70) + '%' } }, m.emoji);
          stage.append(f); setTimeout(() => f.remove(), 2700); break;
        }
      }
    };
    ws.onclose = ev => {
      if (closed) return;
      if (ev.code === 1006 && retry > 4) { status.textContent = T.partyNotFound; return; }
      status.textContent = T.partyLost;
      setTimeout(connect, Math.min(8000, 500 * 2 ** retry++));
    };
  }
  function ping() { send({ t: 'ping', c: Date.now() }); }
  pingT = setInterval(ping, 5000);

  function onState(st) {
    last = st; lastSeq = st.seq;
    if (st.item_id !== p.getItemId()) { p.onItem(st.item_id); return; }
    const me = st.members.find(x => x.id === myId);
    everyone.closest('label').classList.toggle('hidden', !(me && me.host));
    everyone.checked = st.everyone;
    put(membersEl, h('div.muted.small', null, T.partyMembers), ...st.members.map(x => h('div.member', null, h('span.dot', { style: { background: x.color } }), x.name, x.host && h('span.muted.small', null, '· ' + T.host), x.buffering && h('span.small', { style: { color: 'var(--amber)' } }, '· ' + T.buffering))));
    status.textContent = st.waiting.length && st.intended ? T.partyWaiting(st.waiting) : '';
    sync(true);
  }
  // drift > 1.5 s → hard seek; 0.15–1.5 s → nudge playbackRate; otherwise 1.0
  function sync(fromState) {
    if (!last) return;
    const v = p.video;
    const target = last.pos + (last.playing ? (serverNow() - last.at) / 1000 : 0);
    if (last.playing && v.paused) { applying++; v.play().catch(() => {}).finally(() => setTimeout(() => applying--, 50)); }
    if (!last.playing && !v.paused) { applying++; v.pause(); setTimeout(() => applying--, 50); }
    const drift = target - p.pos();
    if (Math.abs(drift) > 1.5 && (fromState || last.playing)) { applying++; p.seekTo(target).finally(() => setTimeout(() => applying--, 50)); v.playbackRate = 1; }
    else if (last.playing && Math.abs(drift) > 0.15) v.playbackRate = drift > 0 ? 1.05 : 0.95;
    else v.playbackRate = 1;
  }
  syncT = setInterval(() => sync(false), 1000);
  connect();
  return {
    send: m => { if (applying) return; send(m); },
    buffering: v => { if (v !== bufState) { bufState = v; send({ t: 'buffering', v }); } },
    close: () => { closed = true; clearInterval(pingT); clearInterval(syncT); try { ws.close(); } catch { /* ignore */ } panel.remove(); },
  };
}

// ===================================================================== admin
async function viewAdmin(tab) {
  if (!state.me.user.is_admin) return go('#/home');
  const main = shell('admin');
  const tabs = [['dash', T.aDash], ['libraries', T.aLibs], ['users', T.aUsers], ['invites', T.aInvites], ['transcode', T.aTranscode], ['metadata', T.aMeta], ['connect', T.aConnect], ['devices', T.aDevices], ['reports', T.aReports], ['languages', T.aLangs], ['settings', T.aSettings], ['import', T.aImport], ['acqrequests', T.aAcqRequests], ['acqqueue', T.aAcqQueue], ['acqsettings', T.aAcqSettings]];
  const label = Object.fromEntries(tabs);
  const groups = [['library', T.aGroupLibrary, ['dash', 'libraries', 'transcode', 'metadata']], ['access', T.aGroupAccess, ['users', 'invites', 'devices', 'reports', 'connect']], ['acquire', T.aGroupAcquire, ['acqrequests', 'acqqueue', 'acqsettings']], ['system', T.aGroupSystem, ['languages', 'settings', 'import']]];
  const fn = { dash: adminDash, libraries: adminLibs, users: adminUsers, invites: adminInvites, transcode: adminTranscode, metadata: adminMeta, connect: adminConnect, devices: adminDevices, reports: adminReports, languages: adminLangs, settings: adminSettings, import: adminImport, acqrequests: adminAcqRequests, acqqueue: adminAcqQueue, acqsettings: adminAcqSettings }[tab];
  if (!fn) return go('#/admin/dash');
  const body = h('div.a-body', { 'aria-busy': 'true' }, aSkeleton());
  const cur = (groups.find(g => g[2].includes(tab)) || groups[0])[0];
  const link = k => h('a' + (k === tab ? '.on' : ''), { href: '#/admin/' + k, 'aria-current': k === tab && 'page' }, label[k]);
  main.append(h('div.admin', null, h('div.admin-side', null, adminSideNav(groups, cur, link), adminPhoneNav(groups, cur, link)), body));
  // phones: the second row scrolls; bring the current page into view
  const strip = main.querySelector('.admin-mitems'), on = strip.querySelector('a.on');
  if (on && strip.scrollWidth > strip.clientWidth) strip.scrollLeft += on.getBoundingClientRect().left - strip.getBoundingClientRect().left - (strip.clientWidth - on.offsetWidth) / 2;
  try { await fn(body); }
  catch (e) {
    if (e.status === 401 || e.status === 428) return;
    put(body, aHead(label[tab]), h('div.a-error', { role: 'alert' }, h('p', null, T.aLoadFailed), e.message && h('p.muted.small', null, e.message), h('button.btn', { onclick: () => route() }, T.retry)));
  } finally { body.removeAttribute('aria-busy'); }
}

// Wide screens: a ruled list of collapsible groups. The current page's group always stays open;
// which of the others are folded is remembered in the browser.
const ADMIN_FOLDED = 'kinora-admin-folded';
function adminSideNav(groups, cur, link) {
  let folded;
  try { folded = new Set(JSON.parse(localStorage.getItem(ADMIN_FOLDED) || '[]')); } catch { folded = new Set(); }
  const save = () => localStorage.setItem(ADMIN_FOLDED, JSON.stringify([...folded]));
  return h('nav.admin-nav', { 'aria-label': T.admin }, h('div.admin-nav-title', null, T.admin), groups.map(([id, title, ks]) => {
    const here = id === cur, open = here || !folded.has(id);
    const items = h('div.admin-nav-items', { id: 'anav-' + id, inert: !open }, h('div', null, ks.map(link)));
    const grp = h('div.admin-grp' + (open ? '.open' : '') + (here ? '.cur' : ''), null,
      h('button.admin-nav-h', { type: 'button', 'aria-expanded': String(open), 'aria-controls': 'anav-' + id, 'aria-disabled': here && 'true',
        onclick: () => {
          if (here) return;
          const now = !grp.classList.toggle('open');
          if (now) folded.add(id); else folded.delete(id);
          items.inert = now; grp.querySelector('.admin-nav-h').setAttribute('aria-expanded', String(!now)); save();
        } }, h('span', null, title), h('span.admin-nav-n', null, ks.length), !here && icon('chevDown')),
      items);
    return grp;
  }));
}

// Phones: two rows instead of one fifteen-item strip: the groups above, the chosen group's pages below.
function adminPhoneNav(groups, cur, link) {
  const items = h('div.admin-mitems');
  const btns = groups.map(([id, title, ks]) => h('button.admin-mgrp-b', { type: 'button', 'aria-pressed': String(id === cur), onclick: () => pick(id) }, title));
  function pick(id) {
    groups.forEach(([g], i) => btns[i].setAttribute('aria-pressed', String(g === id)));
    put(items, groups.find(g => g[0] === id)[2].map(link));
    items.scrollLeft = 0;
  }
  put(items, groups.find(g => g[0] === cur)[2].map(link));
  return h('nav.admin-mnav', { 'aria-label': T.admin }, h('div.admin-mgrp', null, btns), items);
}

// shared admin building blocks: page head, sections, tables, states
const aHead = (title, lede, ...acts) => h('header.a-head', null, h('div.a-head-t', null, h('h1', null, title), lede && h('p.a-lede', null, lede)),
  acts.filter(Boolean).length > 0 && h('div.a-acts', null, acts));
// a stacked section: heading (with an optional hint and actions) above its content
const aSec = (title, hint, acts, ...kids) => h('section.a-sec', null,
  h('div.a-sec-h', null, h('div', null, h('h2', null, title), hint && h('p.a-hint', null, hint)), acts && h('div.a-acts', null, acts)), ...kids);
// a settings section: title and explanation on the left, the controls on the right
const aSplit = (title, hint, ...kids) => h('section.a-split', null, h('div.a-split-h', null, h('h2', null, title), hint && h('p.a-hint', null, hint)), h('div.a-split-b', null, ...kids));
// a table with a header row; ".a-stack" turns rows into labelled blocks on phones
const aTable = (cols, rows, cls = '') => h('div.scroll-x', null, h('table.tbl.a-tbl' + cls, null,
  h('thead', null, h('tr', null, cols.map(c => h('th', null, c)))), h('tbody', null, rows)));
const tdl = (cls, label, ...kids) => h('td' + cls, { 'data-label': label || null }, label ? h('div', null, ...kids) : kids);
const aBar = (done, total) => h('div.bar', null, h('i', { style: { width: (total ? done / total * 100 : 5) + '%' } }));
const aSkeleton = () => h('div.a-skel', { role: 'status', 'aria-label': T.loading }, h('i.w30'), h('i.w60'), h('i'), h('i'), h('i.w80'));
const emptyNote = (title, hint, action) => h('div.empty-note', null, h('p', null, title), hint && h('p.muted.small', null, hint), action);

async function adminDash(body) {
  let t, dead = false;
  // a new install whose first-run wizard was not finished: offer to continue it (or to put it away)
  const wiz = await GET('/api/admin/setup/status', { quiet: true }).catch(() => null);
  let wizNote = wiz && wiz.pending && h('div.wiz-note', null,
    h('div', null, h('b', null, T.wizDashTitle), h('p.muted.small', null, T.wizDashHint)),
    h('div.row-gap', null, h('a.btn.primary', { href: '#/setup/' + WIZ[0] }, T.wizDashContinue),
      h('button.btn.ghost', { onclick: async () => { await POST('/api/admin/setup/finish', {}); wizNote.remove(); wizNote = null; } }, T.wizDashDismiss)));
  const render = async first => {
    let d;
    try { d = await GET('/api/admin/dashboard', { quiet: !first }); }
    catch (e) { if (first) throw e; if (!dead) t = setTimeout(render, 5000); return; } // a blip (restart, Wi-Fi) only delays the refresh
    if (!state.libs) state.libs = await GET('/api/libraries', { quiet: true }).catch(() => null); // the scan table names the libraries
    if (dead) return;
    if (updWait) { // an update started here: the new version answering ends the wait
      const a = parseVer(d.version), b = parseVer(updWait.target);
      if (a && b && a.every((x, i) => x === b[i])) { updWait = null; toast(T.updDone(d.version)); setTimeout(() => location.reload(), 1500); }
      else if (Date.now() - updWait.since > 5 * 60e3) { updWait = null; toast(T.updSlow, true); }
    }
    const up = d.uptime_sec, uptime = up < 60 ? T.uptimeUnder1 : up < 3600 ? T.min(Math.floor(up / 60)) : up > 86400 ? T.uptimeDH(Math.floor(up / 86400), Math.floor(up / 3600) % 24) : T.uptimeHM(Math.floor(up / 3600), Math.floor(up / 60) % 60);
    const stat = (v, l) => h('div.stat', null, h('b', { title: String(v) }, v), h('span', null, l));
    const group = (title, stats, foot) => h('div.a-ledger-g', null, h('h2.a-eyebrow', null, title), h('div.a-stats', null, stats), foot);
    put(body, aHead(T.aDash, T.aLedeDash), wizNote || null,
      h('div.a-ledger', null,
        group(T.aGroupLibrary, [stat(d.library.movies, T.statMovies), stat(d.library.series, T.statSeries), stat(d.library.episodes, T.statEpisodes), stat(fmtBytes(d.library.bytes), T.size)]),
        group(T.aServer, [stat(d.transcodes.length, T.activeTranscodes), stat(d.parties.rooms, T.parties), stat(uptime, T.uptime)],
          verLine(d.version, d.update, async btn => {
            btn.disabled = true;
            try { await POST('/api/admin/update-check'); } catch {}
            if (!dead) { clearTimeout(t); render(); }
          }, async btn => {
            if (!await confirmBox(T.updConfirm(d.update.latest), T.updNow, h('p.muted', null, T.updConfirmBody))) return;
            btn.disabled = true;
            try { await POST('/api/admin/update-apply'); updWait = { target: d.update.latest, since: Date.now() }; }
            catch { /* api() already showed the error */ }
            if (!dead) { clearTimeout(t); render(); }
          }))),
      d.library.missing > 0 && h('p.a-warn', null, T.missingFiles(d.library.missing)),
      d.party_reports > 0 && h('p.a-warn', null, T.aReportsOpen(d.party_reports), ' ', h('a', { href: '#/admin/reports' }, T.aReports + ' →')),
      aSec(T.activeTranscodes, null, null, d.transcodes.length
        ? aTable([T.title, T.aColMode, T.encoder, T.time], d.transcodes.map(s => h('tr', null, tdl('', T.title, h('b', null, s.title)), tdl('', T.aColMode, s.mode), tdl('.mono.small', T.encoder, s.encoder), tdl('', T.time, fmtTime(s.seconds)))), '.a-stack')
        : h('p.a-quiet', null, T.noActiveTranscodes)),
      aSec(T.scans, null, h('a.btn.ghost', { href: '#/admin/libraries' }, T.aLibs + ' →'), scanTable(d.scans)),
      aSec(T.selectedEnc, null, h('a.btn.ghost', { href: '#/admin/transcode' }, T.aTranscode + ' →'),
        h('p', null, h('b', null, srv(d.encoder_selected.label))), h('p.muted.small', null, T.tonemap + ': ' + (d.tonemap ? T.yes : T.no))));
    t = setTimeout(render, 5000);
  };
  onCleanup(() => { dead = true; clearTimeout(t); });
  await render(true);
}
// The version footer of the dashboard: the running version, and next to it whether GitHub has a newer release.
// the update the dashboard started ({ target, since }); kept across re-renders and admin page changes
let updWait = null;
const parseVer = v => { const m = /(\d+)\.(\d+)\.(\d+)/.exec(v || ''); return m && m.slice(1).map(Number); };
function verLine(version, u, onCheck, onApply) {
  const day = s => new Date(s * 1000).toLocaleDateString(T.locale);
  const line = h('p.a-ver', null, T.version + ' ', h('span', null, version));
  if (!u || u.disabled) return line;
  const checked = u.checked_at ? T.updChecked(new Date(u.checked_at * 1000).toLocaleString(T.locale)) : '';
  const again = h('button.a-upd-again', { type: 'button', title: checked ? checked + ' · ' + T.updCheckNow : T.updCheckNow, 'aria-label': T.updCheckNow,
    onclick: e => onCheck && onCheck(e.currentTarget) }, '↻');
  if (u.available) {
    const how = { docker: T.updHowDocker, windows: u.can_apply ? T.updHowWindows : T.updHowManual, systemd: T.updHowSystemd }[u.mode] || T.updHowManual;
    const running = updWait || u.applying;
    const failed = !running && u.last_run && !u.last_run.ok;
    line.append(' ', again);
    return h('div.a-upd', null,
      line,
      h('a.a-upd-link', { href: u.url, target: '_blank', rel: 'noopener noreferrer' },
        h('b', null, T.updAvailable(u.latest)), u.published_at ? h('span', null, day(u.published_at)) : null, ' →'),
      running ? h('p.a-upd-run', { role: 'status' }, T.updRunning(u.latest)) : h('p.a-upd-how', null, how),
      failed && h('p.a-upd-fail', null, T.updLastFailed(u.last_run.at, u.last_run.line)),
      u.can_apply && !running && onApply && h('button.btn.primary.small.a-upd-now', { type: 'button', onclick: e => onApply(e.currentTarget) }, T.updNow));
  }
  if (u.checked_at) line.append(h('span.a-upd-ok', { title: checked }, ' · ' + T.updLatest));
  else if (u.error) line.append(h('span.a-upd-ok', null, ' · ' + T.updFailed));
  line.append(' ', again);
  return line;
}
function scanTable(scans) {
  if (!scans || !scans.length) return h('p.a-quiet', null, T.noScans);
  const libName = id => (state.libs || []).find(l => l.id === id)?.name || '#' + id;
  return aTable([T.aColLibrary, T.status, T.aColProblems], scans.map(s => h('tr', null, tdl('', T.aColLibrary, h('b', null, libName(s.library_id))),
    tdl('.td-progress', T.status, s.running ? h('div', null, `${T.phase[s.phase]} – ${s.done}/${s.total}`, aBar(s.done, s.total)) : s.error ? h('span.bad', null, T.scanErr + ': ' + srv(s.error)) : T.scanSummary(s.changed, s.removed, s.moved, num(s.took_ms / 1000, 1))),
    tdl('', T.aColProblems, s.errors ? h('span.bad', { title: srv(s.last_error) }, T.badFiles(s.errors)) : h('span.muted', null, '–')))), '.a-stack');
}

async function adminLibs(body) {
  let t, dead = false;
  if (!state.installMode) state.installMode = (await GET('/api/admin/browse?path=/').catch(() => ({}))).mode || '';
  const render = async () => {
    const [libs, scans] = await Promise.all([GET('/api/libraries'), GET('/api/admin/scan')]);
    if (dead) return;
    state.libs = libs;
    const scanOf = id => scans.find(s => s.library_id === id);
    put(body, aHead(T.aLibs, T.aLedeLibs, libs.length > 0 && h('button.btn.primary', { onclick: addLib }, icon('plus'), T.addLibrary)),
      libs.length ? aTable([T.libName, T.libPath, T.libKind, T.lastScan, ''], libs.map(l => {
        const s = scanOf(l.id);
        return h('tr', null, tdl('', T.libName, h('b', null, l.name), h('div.small.muted', null, T.titlesN(l.count))), tdl('.mono.path', T.libPath, (l.paths || [l.path]).map(p => h('div', null, p))), tdl('.nowrap', T.libKind, T.kinds[l.kind]),
          tdl('.td-progress', T.lastScan, s && s.running ? h('div', null, `${T.phase[s.phase]} – ${s.done}/${s.total}`, aBar(s.done, s.total))
            : s && s.error ? h('span.bad', null, srv(s.error)) : l.last_scan_at ? ago(Date.now() / 1000 - l.last_scan_at) : T.never,
            s && s.errors ? h('div.small.bad', { title: srv(s.last_error) }, `${T.badFiles(s.errors)}: ${srv(s.last_error)}`) : null),
          h('td.acts', null, h('div.row-gap', null, h('button.btn', { disabled: !!(s && s.running), onclick: async () => { await POST(`/api/libraries/${l.id}/scan`); render(); } }, T.scan),
            h('button.btn', { onclick: () => editLib(l) }, T.edit),
            h('button.btn.ghost.icon-only', { 'aria-label': T.moreActions, title: T.moreActions, 'aria-haspopup': 'menu', onclick: e => popMenu(e.currentTarget, [
              [T.permCheck, async () => permDialog(await GET('/api/admin/permcheck?library=' + l.id))],
              [T.delete, async () => { if (await confirmBox(T.confirmDelete(l.name))) { await api('DELETE', '/api/libraries/' + l.id); state.libs = null; render(); } }]]) }, icon('more', true)))));
      }), '.a-stack') : emptyNote(T.homeEmptyAdmin, null, h('button.btn.primary', { onclick: addLib }, icon('plus'), T.addLibrary)));
    clearTimeout(t);
    if (scans.some(s => s.running)) t = setTimeout(render, 1500);
  };
  onCleanup(() => { dead = true; clearTimeout(t); });
  await render();

  // the folders of a library: one row per folder (browse, remove) and "add folder"
  function folderRows(initial, onPick) {
    const rows = h('div');
    const add = (v = '') => {
      const input = h('input', { type: 'text', value: v, placeholder: rows.children.length ? '' : '/media/movies' });
      const row = h('div.row-gap', { class: 'nowrap mt1' }, input,
        h('button.btn', { type: 'button', onclick: () => folderPicker(input.value, p => { input.value = p; if (onPick) onPick(p); }) }, T.choose),
        h('button.btn.ghost.icon-only', { type: 'button', 'aria-label': T.removeFolder, title: T.removeFolder, onclick: () => { if (rows.children.length > 1) row.remove(); else input.value = ''; } }, icon('x')));
      rows.append(row);
      return input;
    };
    (initial.length ? initial : ['']).forEach(v => add(v));
    const el = h('div.field', null, h('span', null, T.libPath), rows,
      h('button.btn.ghost', { type: 'button', class: 'mt1', onclick: () => add().focus() }, icon('plus'), T.addFolder),
      h('div.hint', null, T.libFoldersHint));
    return { el, values: () => [...rows.querySelectorAll('input')].map(i => i.value.trim()).filter(Boolean) };
  }

  // rename, or point at other folders; items and watch state stay (moved files are matched by size)
  function editLib(l) {
    const name = h('input', { type: 'text', value: l.name });
    const folders = folderRows(l.paths || [l.path]);
    modal(T.editLibrary, h('div', null, field(T.libName, name), folders.el,
      h('div.hint', { class: 'mt0' }, T.libEditHint)), close => [h('button.btn', { onclick: close }, T.cancel),
      h('button.btn.primary', { onclick: async () => {
        try { await api('PATCH', '/api/libraries/' + l.id, { name: name.value, paths: folders.values() }, { quiet: true }); }
        catch (e) { if (e.body && e.body.perm) permDialog(e.body.perm); else toast(e.message, true); return; }
        close(); state.libs = null; toast(T.saved); render();
      } }, T.save)]);
  }

  function addLib() {
    const name = h('input', { type: 'text' });
    const folders = folderRows([], p => { if (!name.value) name.value = p.split(/[\\/]/).filter(Boolean).pop() || ''; });
    const kind = h('select', null, Object.entries(T.kinds).map(([k, v]) => h('option', { value: k }, v)));
    modal(T.addLibrary, h('div', null, field(T.libName, name), folders.el,
      h('div.hint', { class: 'mt0' }, state.installMode === 'docker' ? T.mediaHintDocker : T.mediaHintNative), field(T.libKind, kind)), close => [h('button.btn', { onclick: close }, T.cancel),
      h('button.btn.primary', { onclick: async () => {
        try { await POST('/api/libraries', { name: name.value, paths: folders.values(), kind: kind.value }, { quiet: true }); }
        catch (e) { if (e.body && e.body.perm) permDialog(e.body.perm); else toast(e.message, true); return; }
        close(); state.libs = null; render();
      } }, T.addLibrary)]);
  }
}

function permDialog(rep) {
  const copyBlock = lines => h('div', null, h('pre.code', null, lines.join('\n')), h('button.btn', { onclick: () => copyText(lines.join('\n')) }, T.copy));
  modal(T.permCheck, h('div', null,
    h('p', { class: rep.ok ? 'ok' : 'bad' }, rep.ok ? T.permOk : srv(rep.message)),
    !rep.ok && rep.problems.length > 0 && h('ul.small', null, rep.problems.map(pr => h('li.mono', null, `${pr.path}: ${pr.need === 'x' ? T.permEnter : T.permRead}`))),
    !rep.ok && rep.commands.length > 0 && [h('p', { class: 'mt3' }, T.permFix), copyBlock(rep.commands)],
    !rep.ok && rep.group_commands && [h('p', { class: 'mt3' }, T.permGroup), copyBlock(rep.group_commands)],
    rep.hint && h('p.muted.small', { class: 'mt3' }, srv(rep.hint))), close => [
    !rep.ok && rep.can_fix && h('button.btn.primary', { onclick: async e => {
      e.currentTarget.disabled = true;
      try { const fixed = await api('POST', '/api/admin/permfix?path=' + encodeURIComponent(rep.path)); close(); permDialog(fixed); }
      catch (err) { toast(err.message, true); close(); }
    } }, T.permAutoFix),
    h('button.btn' + (!rep.ok && rep.can_fix ? '' : '.primary'), { onclick: close }, T.close)]);
}

async function folderPicker(start, onPick) {
  const list = h('div', { class: 'scroll-list' });
  const cur = h('div.mono');
  const shortcuts = h('div.chips', { 'aria-label': T.shortcuts });
  let current = '';
  const load = async p => {
    const r = await GET('/api/admin/browse' + (p ? '?path=' + encodeURIComponent(p) : ''));
    current = r.path; cur.textContent = r.path + (r.host_path && r.host_path !== r.path ? '  ·  ' + T.hostPath(r.host_path) : '');
    put(shortcuts, (r.shortcuts || []).map(sc => h('button.chip' + (sc === r.path ? '.on' : ''), { onclick: () => load(sc) }, sc)));
    const locked = new Set(r.locked || []);
    put(list, r.parent && h('button.opt', { onclick: () => load(r.parent) }, '↑ ' + T.up), ...r.dirs.map(d => h('button.opt', { onclick: () => load(r.path.replace(/[\\/]$/, '') + r.sep + d) },
      locked.has(d) && [icon('lock'), ' '], d, locked.has(d) && h('span.muted.small', null, ' · ' + T.locked))));
    if (!r.dirs.length) list.append(h('p.muted.small', null, T.noSubdirs));
  };
  modal(T.pickFolder, h('div', null, shortcuts, cur, list), close => [h('button.btn', { onclick: close }, T.cancel),
    h('button.btn', { onclick: async () => permDialog(await GET('/api/admin/permcheck?path=' + encodeURIComponent(current))) }, T.permCheck),
    h('button.btn.primary', { onclick: () => { onPick(current); close(); } }, T.useFolder)]);
  await load(start || '').catch(() => load(''));
}

async function adminUsers(body) {
  const [users, libs] = await Promise.all([GET('/api/admin/users'), GET('/api/libraries')]);
  state.libs = libs;
  const me = state.me.user.id;
  const libText = u => u.is_admin || !u.libraries ? h('span.muted', null, T.aLibsAll)
    : u.libraries.length ? libs.filter(l => u.libraries.includes(l.id)).map(l => l.name).join(', ') || T.aLibsNone
      : h('span.badge.amber', null, T.aLibsNone);
  put(body, aHead(T.aUsers, T.aLedeUsers, h('button.btn.primary', { onclick: newUser }, icon('plus'), T.newUser)),
    aTable([T.aColUser, T.aColProfiles, T.aColRole, T.aColLibs, ''], users.map(u => h('tr', null,
      tdl('', null, h('div.u-cell', null, h('span.u-ini', { 'aria-hidden': 'true' }, (u.username[0] || '?').toUpperCase()), h('b', null, u.username), u.id === me && h('span.muted', null, ' ' + T.you), u.needs_password && h('span.badge.amber', { title: T.needsPasswordHint }, T.needsPassword))),
      tdl('.muted', T.aColProfiles, T.profilesN(u.profiles)),
      tdl('', T.aColRole, u.is_admin ? h('span.badge.amber', null, T.isAdmin) : h('span.muted', null, T.aRoleUser)),
      tdl('', T.aColLibs, libText(u)),
      h('td.acts', null, h('div.row-gap', null,
        h('button.btn', { onclick: () => resetPw(u) }, T.resetPw),
        u.id !== me && h('button.btn', { onclick: () => pwLink(u) }, T.pwLink),
        !u.is_admin && h('button.btn', { onclick: () => editLibs(u) }, T.aColLibs),
        u.id !== me && h('button.btn', { onclick: async () => { await api('PATCH', '/api/admin/users/' + u.id, { is_admin: !u.is_admin }); adminUsers(body); } }, u.is_admin ? T.revokeAdmin : T.grantAdmin),
        u.id !== me && h('button.btn.danger', { onclick: async () => { if (await confirmBox(T.confirmDelete(u.username))) { await api('DELETE', '/api/admin/users/' + u.id); adminUsers(body); } } }, T.delete))))), '.a-stack'));
  function newUser() {
    const un = h('input', { type: 'text' }), pw = h('input', { type: 'password' }), adm = h('input', { type: 'checkbox' });
    modal(T.newUser, h('div', null, field(T.username, un), field(T.password, pw), h('label.check', null, adm, T.isAdmin)), close => [h('button.btn', { onclick: close }, T.cancel),
      h('button.btn.primary', { onclick: async () => { await POST('/api/admin/users', { username: un.value, password: pw.value, is_admin: adm.checked }); close(); adminUsers(body); } }, T.create)]);
  }
  function editLibs(u) {
    const all = h('input', { type: 'checkbox', checked: !u.libraries });
    const boxes = libs.map(l => [l, h('input', { type: 'checkbox', checked: !u.libraries || u.libraries.includes(l.id) })]);
    const sync = () => boxes.forEach(([, b]) => { b.disabled = all.checked; });
    all.onchange = sync; sync();
    modal(T.aColLibs + ': ' + u.username, h('div', null, h('p.muted.small', null, T.aLibsLede),
      h('label.check', null, all, T.aLibsAll), ...boxes.map(([l, b]) => h('label.check', null, b, l.name))),
    close => [h('button.btn', { onclick: close }, T.cancel),
      h('button.btn.primary', { onclick: async () => {
        await api('PATCH', '/api/admin/users/' + u.id, { libraries: all.checked ? null : boxes.filter(([, b]) => b.checked).map(([l]) => l.id) });
        close(); adminUsers(body);
      } }, T.save)]);
  }
  async function pwLink(u) {
    const r = await POST('/api/admin/users/' + u.id + '/password-link');
    const url = await passwordLinkURL(r.code);
    modal(T.pwLink + ': ' + u.username, passwordLinkBox(url, r.expires_at), close => [h('button.btn.primary', { onclick: close }, T.close)]);
  }
  function resetPw(u) {
    const pw = h('input', { type: 'password' });
    modal(T.resetPw + ': ' + u.username, field(T.password, pw), close => [h('button.btn', { onclick: close }, T.cancel),
      h('button.btn.primary', { onclick: async () => { await api('PATCH', '/api/admin/users/' + u.id, { password: pw.value }); close(); toast(T.saved); } }, T.save)]);
  }
}

async function adminInvites(body) {
  const invs = await GET('/api/admin/invites');
  const base = (await GET('/api/admin/settings')).values.public_url || location.origin;
  const linkOf = c => base + '/#/join/' + c;
  put(body, aHead(T.aInvites, T.aLedeInvites, invs.length > 0 && h('button.btn.primary', { onclick: newInv }, icon('plus'), T.newInvite)),
    invs.length ? h('div.a-list', null, invs.map(i => h('div.entry.a-invite' + (i.uses_left > 0 ? '' : '.spent'), null,
      qrImg(linkOf(i.code)),
      h('div', null, h('div.code', null, i.code), i.note && h('p.a-invite-note', null, i.note),
        h('p.muted.small', null, T.usesLeft(i.uses_left), i.expires_at ? ` · ${T.expires}: ${new Date(i.expires_at * 1000).toLocaleString(T.locale)}` : ''),
        h('div.url-row', null, h('code.mono', null, linkOf(i.code)), h('button.btn', { onclick: () => copyText(linkOf(i.code)) }, T.copy),
          h('button.btn.danger', { onclick: async () => { await api('DELETE', '/api/admin/invites/' + i.code); adminInvites(body); } }, T.delete))))))
      : emptyNote(T.noInvites, T.invitesEmptyHint, h('button.btn.primary', { onclick: newInv }, icon('plus'), T.newInvite)));
  function newInv() {
    const note = h('input', { type: 'text', placeholder: T.egNote }), uses = h('input', { type: 'number', value: 1, min: 1, max: 100 }), hours = h('input', { type: 'number', value: 72, min: 0 });
    modal(T.newInvite, h('div', null, field(T.note, note), h('div.grid2', null, field(T.uses, uses), field(T.validHours, hours))), close => [h('button.btn', { onclick: close }, T.cancel),
      h('button.btn.primary', { onclick: async () => { await POST('/api/admin/invites', { note: note.value, uses: +uses.value, hours: +hours.value }); close(); adminInvites(body); } }, T.create)]);
  }
}

// Admin → Import: Jellyfin users and watch state. Preview first (reads Jellyfin, writes nothing), then import.
const IMPORT_FORM = 'kinora-jf-import'; // address and path maps are remembered in this browser; the key never is
async function adminImport(body) {
  let saved = {};
  try { saved = JSON.parse(localStorage.getItem(IMPORT_FORM) || '{}'); } catch { /* private mode */ }
  const url = h('input', { type: 'url', placeholder: 'http://192.168.1.10:8096', value: saved.url || '', autocomplete: 'off' });
  const key = h('input', { type: 'password', autocomplete: 'off', spellcheck: false });
  const maps = h('textarea', { rows: 3, placeholder: '/data/movies => /media/movies', spellcheck: false }, saved.maps || '');
  const out = h('div', { 'aria-live': 'polite' });
  const go1 = h('button.btn.primary', { type: 'submit' }, T.importPreview);
  const parseMaps = () => maps.value.split('\n').map(l => l.split('=>')).filter(p => p.length === 2 && p[0].trim() && p[1].trim())
    .map(([from, to]) => ({ from: from.trim(), to: to.trim() }));
  const form = h('form', { onsubmit: async e => {
    e.preventDefault();
    try { localStorage.setItem(IMPORT_FORM, JSON.stringify({ url: url.value, maps: maps.value })); } catch { /* ignore */ }
    go1.disabled = true; put(out, h('p.muted', { role: 'status' }, T.importReading));
    try {
      const plan = await POST('/api/admin/import/jellyfin/preview', { url: url.value, api_key: key.value, path_maps: parseMaps() }, { quiet: true });
      showPlan(plan);
    } catch (err) {
      const code = err.body && err.body.code;
      put(out, h('div.a-error', { role: 'alert' }, h('p', null, (code && T.importErr[code]) || err.message), err.body && err.body.detail && h('p.muted.small', null, err.body.detail)));
    } finally { go1.disabled = false; }
  } },
    field(T.importURL, url), field(T.importKey, key, T.importKeyHint), field(T.importMaps, maps, T.importMapsHint), h('div.row-gap', null, go1));
  put(body, aHead(T.aImport, T.aLedeImport), aSplit(T.importJF, T.importJFHint, form), out, ...arrImportSection());

  const method = m => T.importMethod[m] || m;
  const reason = r => T.importReason[r] || r;
  const entryTitle = e => e.kind === 'episode'
    ? `${e.series || ''} ${e.season != null && e.episode != null ? T.ep(e.season, e.episode) : ''} – ${e.title}`
    : e.title + (e.year ? ` (${e.year})` : '');
  const stateText = e => [e.played && T.importPlayed, e.position > 0 && !e.played && T.importResume(fmtTime(e.position)), e.favorite && T.importFav].filter(Boolean).join(', ');

  function showPlan(plan) {
    const picks = {};
    const rows = plan.users.map(u => {
      const box = h('input', { type: 'checkbox', checked: u.action !== 'skip', disabled: u.action === 'skip', 'aria-label': u.name });
      picks[u.jellyfin_id] = box;
      return h('tr', null,
        h('td', null, box),
        tdl('', T.importColJF, h('b', null, u.name), u.is_admin && h('span.muted.small', null, ' (' + T.isAdmin + ')')),
        tdl('', T.importColKinora, T.importAction[u.action] ? T.importAction[u.action].replace('{0}', u.kinora_name) : u.action),
        tdl('', T.importColMatched, String(u.matched)),
        tdl('', T.importColUnmatched, u.unmatched ? h('span.badge.amber', null, String(u.unmatched)) : '0'));
    });
    const details = plan.users.filter(u => u.entries.length).map(u => h('details.a-sec', null,
      h('summary', null, h('b', null, u.name), ' – ', T.importDetails(u.matched, u.unmatched)),
      aTable([T.importColItem, T.importColState, T.importColResult], u.entries.slice(0, 500).map(e => h('tr', null,
        tdl('', T.importColItem, entryTitle(e), e.path && h('div.muted.small.mono', null, e.path)),
        tdl('.muted', T.importColState, stateText(e)),
        tdl('', T.importColResult, e.match
          ? h('span', null, '→ ', e.match.title, e.match.year ? ` (${e.match.year})` : '', h('span.muted.small', null, ' · ' + method(e.method)))
          : h('span.badge.warn', null, reason(e.reason))))), '.a-stack'),
      u.entries.length > 500 && h('p.muted.small', null, T.importMore(u.entries.length - 500))));
    const apply = h('button.btn.primary', { onclick: async () => {
      const users = Object.entries(picks).filter(([, b]) => b.checked && !b.disabled).map(([id]) => id);
      if (!users.length) return toast(T.importNoneSelected, true);
      if (!await confirmBox(T.importConfirm(users.length), T.importApply)) return;
      apply.disabled = true;
      try { await showReport(plan, await POST('/api/admin/import/jellyfin/apply', { plan_id: plan.id, users })); }
      finally { apply.disabled = false; }
    } }, T.importApply);
    const csv = h('a.btn', { href: '/api/admin/import/jellyfin/' + plan.id + '/export.csv', download: '' }, T.importCSV);
    const methods = Object.entries(plan.by_method || {}).map(([m, n]) => `${method(m)}: ${n}`).join(' · ');
    put(out,
      aSec(T.importPreviewTitle(plan.server_name, plan.version), T.importPreviewHint, h('div.row-gap', null, csv, apply),
        h('p', null, T.importTotals(plan.matched, plan.unmatched)), methods && h('p.muted.small', null, methods),
        aTable(['', T.importColJF, T.importColKinora, T.importColMatched, T.importColUnmatched], rows, '.a-stack')),
      ...details);
  }

  async function showReport(plan, rep) {
    const rows = await Promise.all(rep.users.map(async u => h('tr', null,
      tdl('', T.importColJF, h('b', null, u.name)),
      tdl('', T.importColKinora, u.kinora_name || '—', u.created && h('span.badge.amber', null, T.importNew)),
      tdl('', T.importColChanged, String(u.changed)),
      tdl('.muted', T.importColUnchanged, String(u.unchanged)),
      tdl('', T.importColUnmatched, String(u.unmatched)),
      tdl('', T.importColLink, u.error ? h('span.badge.warn', null, srv(u.error))
        : u.password_code ? passwordLinkBox(await passwordLinkURL(u.password_code), u.password_expires) : h('span.muted', null, T.importHasPassword)))));
    put(out, aSec(T.importDone(rep.changed), T.importDoneHint, h('div.row-gap', null,
      h('a.btn', { href: '/api/admin/import/jellyfin/' + plan.id + '/export.csv', download: '' }, T.importCSV),
      h('button.btn', { onclick: () => showPlan(plan) }, T.importBack)),
    aTable([T.importColJF, T.importColKinora, T.importColChanged, T.importColUnchanged, T.importColUnmatched, T.importColLink], rows, '.a-stack')));
  }
}

// Sonarr / Radarr / Prowlarr: followed series and wanted films, quality profiles, indexers, clients, root folders
const ARR_FORM = 'kinora-arr-import';
function arrImportSection() {
  let saved = {};
  try { saved = JSON.parse(localStorage.getItem(ARR_FORM) || '{}'); } catch { /* private mode */ }
  const app = h('select', null, [['sonarr', 'Sonarr'], ['radarr', 'Radarr'], ['prowlarr', 'Prowlarr']].map(([v, l]) => h('option', { value: v, selected: saved.app === v }, l)));
  const url = h('input', { type: 'url', placeholder: 'http://192.168.1.10:8989', value: saved.url || '', autocomplete: 'off' });
  const key = h('input', { type: 'password', autocomplete: 'off', spellcheck: false });
  const maps = h('textarea', { rows: 3, placeholder: '/tv => /media/shows', spellcheck: false }, saved.maps || '');
  const out = h('div', { 'aria-live': 'polite' });
  const go1 = h('button.btn.primary', { type: 'submit' }, T.importPreview);
  const parseMaps = () => maps.value.split('\n').map(l => l.split('=>')).filter(p => p.length === 2 && p[0].trim() && p[1].trim())
    .map(([from, to]) => ({ from: from.trim(), to: to.trim() }));
  const form = h('form', { onsubmit: async e => {
    e.preventDefault();
    try { localStorage.setItem(ARR_FORM, JSON.stringify({ app: app.value, url: url.value, maps: maps.value })); } catch { /* ignore */ }
    go1.disabled = true; put(out, h('p.muted', { role: 'status' }, T.importReading));
    try { showPlan(await POST('/api/admin/import/arr/preview', { app: app.value, url: url.value, api_key: key.value, path_maps: parseMaps() }, { quiet: true })); }
    catch (err) {
      const code = err.body && err.body.code;
      put(out, h('div.a-error', { role: 'alert' }, h('p', null, (code && T.arrErr[code]) || err.message), err.body && err.body.detail && h('p.muted.small', null, err.body.detail)));
    } finally { go1.disabled = false; }
  } }, field(T.arrApp, app), field(T.arrURL, url), field(T.importKey, key, T.importKeyHint), field(T.importMaps, maps, T.arrMapsHint), h('div.row-gap', null, go1));

  const act = (a, reason) => h('span' + (a === 'skip' ? '.badge.warn' : a === 'create' || a === 'request' || a === 'follow' ? '.badge.amber' : '.muted'), null,
    (T.arrAction[a] || a) + (reason ? ' – ' + (T.arrReason[reason] || reason) : ''));
  const notes = n => (n || []).map(x => h('div.muted.small', null, T.arrNote[x] || x));
  const res = r => r === 0 ? T.arrResUnknown : r + 'p';

  function showPlan(plan) {
    const titles = plan.app === 'sonarr' ? plan.series : plan.movies;
    const secs = [
      ['profiles', T.arrSecProfiles, plan.profiles, [T.arrColName, T.arrColResult, T.arrColDetails], p => [
        tdl('', T.arrColName, h('b', null, p.name)), tdl('', T.arrColResult, act(p.action)),
        tdl('', T.arrColDetails, h('div', null, p.profile.resolutions.map(res).join(' > '),
          p.profile.upgrade ? ' · ' + T.arrUpgrade(res(p.profile.cutoff_resolution) + ' ' + (p.profile.cutoff_source || '')) : ''), notes(p.notes))]],
      ['folders', T.arrSecFolders, plan.folders, [T.arrColPath, T.arrColResult], f => [
        tdl('', T.arrColPath, h('code.mono', null, f.path), f.mapped !== f.path && h('div.muted.small.mono', null, '→ ' + f.mapped)), tdl('', T.arrColResult, act(f.action, f.reason))]],
      ['clients', T.arrSecClients, plan.clients, [T.arrColName, T.arrColURL, T.arrColResult], x => [
        tdl('', T.arrColName, h('b', null, x.name), x.kind && h('span.muted.small', null, ' ' + x.kind)),
        tdl('', T.arrColURL, x.url && h('code.mono', null, x.url), x.category && h('div.muted.small', null, T.arrCategory(x.category))),
        tdl('', T.arrColResult, act(x.action, x.reason), notes(x.notes))]],
      ['indexers', T.arrSecIndexers, plan.indexers, [T.arrColName, T.arrColURL, T.arrColResult], x => [
        tdl('', T.arrColName, h('b', null, x.name), x.kind && h('span.muted.small', null, ' ' + x.kind)),
        tdl('', T.arrColURL, x.url && h('code.mono', null, x.url)), tdl('', T.arrColResult, act(x.action, x.reason), notes(x.notes))]],
      ['titles', plan.app === 'sonarr' ? T.arrSecSeries : T.arrSecMovies, titles, [T.arrColTitle, T.arrColResult], x => [
        tdl('', T.arrColTitle, x.title + (x.year ? ` (${x.year})` : ''),
          x.monitor && h('div.muted.small', null, T.arrMonitor[x.monitor] + (x.seasons && x.seasons.length ? ': ' + x.seasons.join(', ') : ''))),
        tdl('', T.arrColResult, act(x.action, x.reason))]],
    ].filter(([, , list]) => list && list.length);
    const picks = {};
    const count = list => list.filter(x => x.action !== 'skip' && x.action !== 'existing' && x.action !== 'link').length;
    const blocks = secs.map(([id, title, list, cols, row]) => {
      const box = picks[id] = h('input', { type: 'checkbox', checked: count(list) > 0, 'aria-label': title, onclick: e => e.stopPropagation() });
      return h('details.a-sec', { open: list.length <= 12 },
        h('summary.arr-sum', null, box, h('b', null, title), h('span.muted', null, T.arrSecCount(count(list), list.length))),
        aTable(cols, list.slice(0, 400).map(x => h('tr', null, row(x))), '.a-stack'),
        list.length > 400 && h('p.muted.small', null, T.importMore(list.length - 400)));
    });
    const apply = h('button.btn.primary', { onclick: async () => {
      const sections = Object.entries(picks).filter(([, b]) => b.checked).map(([id]) => id);
      if (!sections.length) return toast(T.importNoneSelected, true);
      if (!await confirmBox(T.arrConfirm, T.importApply)) return;
      apply.disabled = true;
      try { await POST('/api/admin/import/arr/apply', { plan_id: plan.id, sections }); follow(plan); }
      catch { apply.disabled = false; }
    } }, T.importApply);
    put(out, aSec(T.arrPreviewTitle(plan.app[0].toUpperCase() + plan.app.slice(1), plan.version), T.arrPreviewHint, h('div.row-gap', null, apply),
      (plan.blockers || []).map(b => h('div.a-error', { role: 'alert' }, h('p', null, T.arrReason[b] || b)))), ...blocks);
  }

  // the import runs in the background; this shows its progress until it ends
  async function follow(plan) {
    const bar = h('div'), info = h('div');
    put(out, aSec(T.arrRunning, null, null, bar, info));
    for (;;) {
      let r;
      try { r = await GET('/api/admin/import/arr/' + plan.id, { quiet: true }); } catch { return; }
      const p = r.progress || { done: 0, total: 0, counts: {}, errors: [] };
      put(bar, aBar(p.done, p.total), h('p.muted.small', null, `${p.done} / ${p.total}`));
      const counts = Object.entries(p.counts).filter(([k]) => !k.endsWith(':skip')).map(([k, n]) => {
        const [sec, a] = k.split(':');
        return h('li', null, `${T.arrSecName[sec] || sec}: ${T.arrAction[a] || a} × ${n}`);
      });
      put(info, counts.length > 0 && h('ul', null, counts),
        p.errors.length > 0 && h('div', null, h('p', null, h('b', null, T.arrErrors(p.errors.length))),
          h('ul', null, p.errors.slice(0, 100).map(e => h('li', null, `${e.name}: ${T.arrReason[e.error] || srv(e.error)}`)))));
      if (!p.running && r.started) { out.querySelector('h2').textContent = T.arrDone; return; }
      await new Promise(res => setTimeout(res, 1000));
      if (!out.isConnected) return;
    }
  }
  return [aSplit(T.arrTitle, T.arrHint, form), out];
}

// keys: [key, label, kind, options, hint] rows; a string or { h, hint } starts a titled section (two-column layout)
// settings whose value is an API key the admin page can test (service name for /api/admin/keytest)
const KEY_TEST = { tmdb_key: 'tmdb', omdb_key: 'omdb', opensubtitles_key: 'opensubtitles' };
// the last result per field, so it survives the page redrawing itself after a save
const keyChecks = {};
const KEY_STATE = { ok: 'keyStateOk', invalid: 'keyStateInvalid', login: 'keyStateLogin', limit: 'keyStateLimit', network: 'keyStateNetwork', missing: 'keyStateMissing', error: 'keyStateError' };

function settingsForm(values, env, keys, onSaved) {
  const inputs = {};
  const keyTests = []; // API-key fields re-check themselves after a save
  const isRow = Array.isArray;
  const fields = keys.filter(isRow);
  const row = ([k, label, kind, opts, hint]) => {
    const locked = env[k];
    let el;
    if (kind === 'toggle') {
      el = inputs[k] = h('input.switch', { type: 'checkbox', role: 'switch', checked: values[k] !== '0', disabled: !!locked });
      return h('div.form-row', null, h('label.switch-row', null, h('span', null, label), el), hint && h('div.hint', null, hint), locked && h('div.env-lock', null, T.envLocked(locked)));
    }
    if (kind === 'select') el = h('select', { disabled: !!locked }, opts.map(([v, l]) => h('option', { value: v, selected: values[k] === v }, l)));
    else el = h('input', { type: kind || 'text', value: values[k] ?? '', disabled: !!locked });
    inputs[k] = el;
    const svc = KEY_TEST[k];
    if (svc) {
      // a whole example URL pasted (…&apikey=abcd1234): keep only the key
      const status = h('div.key-status');
      const show = c => { status.className = 'key-status' + (c ? ' ' + c.cls : ''); status.textContent = c ? c.text : ''; };
      show(keyChecks[k]);
      el.addEventListener('input', () => {
        const m = /[?&](?:apikey|api_key)=([^&#\s]+)/i.exec(el.value);
        if (m) el.value = decodeURIComponent(m[1]);
        delete keyChecks[k]; show(null);
      });
      const btn = h('button.btn.small', { type: 'button', onclick: () => runTest() }, T.keyTest);
      const runTest = async () => {
        btn.disabled = true; show({ cls: '', text: T.keyTesting });
        try {
          const r = await POST('/api/admin/keytest', { service: svc, key: el.value,
            user: inputs.opensubtitles_user ? inputs.opensubtitles_user.value : '', pass: inputs.opensubtitles_pass ? inputs.opensubtitles_pass.value : '' });
          keyChecks[k] = { cls: r.ok ? 'ok' : r.state === 'limit' || r.state === 'network' ? 'warn' : 'bad',
            text: (T[KEY_STATE[r.state]] || T.keyStateError) + (r.detail && !r.ok ? ' (' + srv(r.detail) + ')' : '') };
        } catch (e) { keyChecks[k] = { cls: 'bad', text: e.message || String(e) }; }
        finally { btn.disabled = false; show(keyChecks[k]); }
      };
      keyTests.push(() => el.value.trim() ? runTest() : (delete keyChecks[k], show(null)));
      return h('label.field', null, h('span', null, label), h('div.key-row', null, el, btn), status, hint && h('div.hint', null, hint), locked && h('div.env-lock', null, T.envLocked(locked)));
    }
    return h('label.field', null, h('span', null, label), el, hint && h('div.hint', null, hint), locked && h('div.env-lock', null, T.envLocked(locked)));
  };
  const save = h('button.btn.primary', { disabled: fields.every(([k]) => env[k]), onclick: async () => {
    const body = {};
    for (const [k, , kind] of fields) if (!env[k]) body[k] = kind === 'toggle' ? (inputs[k].checked ? '1' : '0') : inputs[k].value;
    save.disabled = true;
    try { await api('PATCH', '/api/admin/settings', body); toast(T.saved); await Promise.all(keyTests.map(t => t())); onSaved && onSaved(); } finally { save.disabled = false; }
  } }, T.save);
  if (keys.every(isRow)) return h('div.a-form', null, fields.map(row), h('div.a-form-foot', null, save));
  const groups = [];
  for (const key of keys) {
    if (!isRow(key)) groups.push({ ...(typeof key === 'string' ? { h: key } : key), rows: [] });
    else groups[groups.length - 1].rows.push(row(key));
  }
  return h('div.a-form', null, groups.map(g => aSplit(g.h, g.hint, g.rows)), h('div.a-form-foot.split', null, save));
}

async function adminTranscode(body) {
  const [d, s] = await Promise.all([GET('/api/admin/dashboard'), GET('/api/admin/settings')]);
  const missing = r => r.error === 'az ffmpeg nem tartalmazza ezt az encodert';
  const table = (res, sel) => aTable([T.encoder, T.result, T.time, T.details], res.map(r => h('tr', { class: r.name === sel && 'sel' },
    tdl('', null, h('b', null, srv(r.label)), r.name === sel && [' ', h('span.badge.amber', null, T.inUse)], h('div.small.muted.mono', null, r.name)),
    tdl('', T.result, h('span', { class: r.ok ? 'ok' : missing(r) ? 'off' : 'bad' }, r.ok ? T.works : missing(r) ? T.unavailable : T.encFailed)), tdl('', T.time, r.ok ? r.ms + ' ms' : ''), tdl('.small.muted', T.details, srv(r.error) || ''))), '.a-stack');
  const tbl = h('div', null, table(d.encoders || [], d.encoder_selected.name));
  const btn = h('button.btn', { onclick: async () => { btn.disabled = true; btn.textContent = T.hwTesting; try { const r = await POST('/api/admin/hwtest'); put(tbl, table(r.results, r.selected.name)); toast(T.selectedEnc + ': ' + r.selected.label); } finally { btn.disabled = false; btn.textContent = T.hwTest; } } }, T.hwTest);
  const encOpts = [['auto', T.encAuto], ...(d.encoders || []).filter(e => e.ok && (e.codec || 'h264') === 'h264').map(e => [e.name, srv(e.label)])];
  const codecOpts = [['auto', T.codecAuto], ['h264', T.codecH264]];
  put(body, aHead(T.aTranscode, T.aLedeTranscode),
    aSplit(T.encSetting, T.encSettingHint + ' ' + T.codecSettingHint,
      h('dl.side-info.a-facts', null, h('dt', null, T.selectedEnc), h('dd', null, srv(d.encoder_selected.label)), h('dt', null, T.tonemap), h('dd', null, d.tonemap ? T.yes : T.no)),
      settingsForm(s.values, s.env, [['encoder', T.encSetting, 'select', encOpts], ['video_codec', T.codecSetting, 'select', codecOpts]], () => adminTranscode(body))),
    await segmentsPanel(s),
    aSec(T.encTest, T.encTestHint, btn, tbl));
}

async function segmentsPanel(s) {
  const st = await GET('/api/admin/segments').catch(() => null);
  return aSplit(T.aSegments, T.segHint,
    st && h('dl.side-info.a-facts', null,
      h('dt', null, T.segBackend), h('dd', null, h('span', { class: st.available ? 'ok' : 'bad' }, srv(st.backend))),
      st.stats && [h('dt', null, T.status), h('dd', null, T.segStats(st.stats.episodes, st.stats.analysed, st.stats.with_intro, st.stats.with_credits, st.stats.manual))],
      h('dt', null, T.segQueue), h('dd', null, String(st.queue || 0)),
      st.running && [h('dt', null, T.segRunning), h('dd', null, st.running)],
      st.last_error && [h('dt', null, T.lastError), h('dd.bad.small', null, srv(st.last_error))]),
    settingsForm(s.values, s.env, [['intro_detection', T.segDetection, 'toggle'], ['intro_window_min', T.segWindow, 'number']]));
}

async function adminMeta(body) {
  const [s, m] = await Promise.all([GET('/api/admin/settings'), GET('/api/admin/metadata')]);
  const retry = h('input', { type: 'checkbox' });
  const st = m.status;
  let t;
  put(body, aHead(T.aMeta, T.aLedeMeta),
    aSplit('TMDB', T.metaHint,
      settingsForm(s.values, s.env, [['tmdb_key', T.tmdbKey, 'password'], ['omdb_key', T.omdbKey, 'password', null, T.omdbKeyHint], ['metadata_lang', T.metaLang, 'select', [['hu-HU', T.langNames.hun], ['en-US', T.langNames.eng], ['de-DE', T.langNames.ger]], T.metaLangHint], ['watch_region', T.watchRegion, 'text', null, T.watchRegionHint]], () => setTimeout(() => adminMeta(body), 400))),
    aSplit('OpenSubtitles', T.osHint,
      settingsForm(s.values, s.env, [['opensubtitles_key', T.osKey, 'password', null, T.osKeyHint], ['opensubtitles_user', T.osUser, 'text', null, T.osUserHint], ['opensubtitles_pass', T.osPass, 'password']])),
    aSplit(T.refreshMeta, T.refreshMetaHint,
      h('label.check', null, retry, T.retryNoMatch),
      h('div.a-form-foot', null, h('button.btn.primary', { disabled: !m.enabled, onclick: async () => { await POST('/api/admin/metadata/refresh', { retry_no_match: retry.checked }); adminMeta(body); } }, T.refreshMeta)),
      !m.enabled && h('p.note-off', null, T.metaNeedsKey),
      h('div.a-status', null, h('span.muted', null, T.metaStatus + ': '), st.running ? [T.metaRunning(st.done, st.total), st.relang ? ' · ' + T.metaRelang(st.relang) : '', st.translate ? ' · ' + T.metaTranslate(st.translate) : '', aBar(st.done, st.total)] : st.finished_at ? T.metaDone(st.matched, st.no_match) : '–',
        st.last_error ? h('div.bad.small', null, srv(st.last_error)) : null)));
  if (st.running) { t = setTimeout(() => adminMeta(body), 2000); onCleanup(() => clearTimeout(t)); }
}

async function adminConnect(body) {
  const c = await GET('/api/admin/connect');
  const card = (title, url, sub) => h('div.conn-card', null, qrImg(url),
    h('div', null, h('b', null, title), sub && h('p.muted.small', null, sub), h('div.url-row', null, h('code.mono', { title: url }, url), h('button.btn', { onclick: () => copyText(url) }, T.copy))));
  const kindLabel = { lan: T.lan, tailscale: 'Tailscale', other: T.otherNet };
  put(body, aHead(T.aConnect, T.connectLede),
    aSec(T.aAddresses, !c.public_url && T.noPublic, !c.public_url && h('a.btn.ghost', { href: '#/admin/settings' }, T.aSettings + ' →'),
      h('div.conn-grid', null, c.public_url && card(T.publicUrl, c.public_url), c.addresses.map(a => card(kindLabel[a.kind] + ' (' + a.iface + ')', a.url))),
      c.in_container && h('p.muted.small', { class: 'mt3' }, T.inDocker)),
    aSec(T.aRemote, T.aRemoteHint, null, remoteGuides()));
}

// how to reach the server from outside: Tailscale, Cloudflare Tunnel, a reverse proxy (Admin → Connect and the wizard)
function remoteGuides() {
  const opt = (title, ...kids) => h('details.a-opt', null, h('summary', null, title), h('div.a-opt-b', null, ...kids));
  return h('div.a-opts', null,
    opt(T.tsTitle, h('p', null, T.tsHint),
      h('pre.code', null, 'TS_AUTHKEY=tskey-... docker compose -f docker-compose.yml -f docker-compose.tailscale.yml up -d')),
    opt(T.cfTitle, h('p', null, T.cfHint),
      h('pre.code', null, 'CF_TUNNEL_TOKEN=... docker compose -f docker-compose.yml -f docker-compose.cloudflared.yml up -d'), h('p.muted.small', null, T.cfPublic)),
    opt(T.proxyTitle, h('p', null, T.proxyHint),
      h('pre.code', null, T.exampleHost + ' {\n  reverse_proxy 127.0.0.1:8096\n}'),
      h('pre.code', null, 'location / {\n  proxy_pass http://127.0.0.1:8096;\n  proxy_http_version 1.1;\n  proxy_set_header Upgrade $http_upgrade;\n  proxy_set_header Connection "upgrade";\n  proxy_set_header Host $host;\n  proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n  proxy_set_header X-Forwarded-Proto $scheme;\n  proxy_buffering off;\n  client_max_body_size 0;\n}')));
}

async function adminDevices(body) {
  const r = await GET('/api/admin/devices');
  put(body, aHead(T.aDevices, T.aLedeDevices),
    r.devices.length ? h('div.a-list', null, h('p.a-count', null, T.aDevicesN(r.devices.length)), r.devices.map(d => deviceRow(d, () => adminDevices(body), true))) : emptyNote(T.aNoDevices));
}

// Watch party chat lines members reported. Deleting the sender's account (Users) removes them;
// closing a report only takes it off this list.
async function adminReports(body) {
  const r = await GET('/api/admin/party-reports');
  const row = p => h('div.entry', null, h('div', null,
    h('p', null, h('strong', null, p.sender_name), p.sender_login && h('span.muted', null, ' (' + p.sender_login + ')'), ': ', p.text),
    h('p.muted.small', null, [T.aReportBy(p.reporter_name), p.reason, T.aReportRoom(p.room), ago(Date.now() / 1000 - Date.parse(p.created_at) / 1000)].filter(Boolean).join(' · '))),
    h('span.row-gap', null,
      p.sender_user > 0 && h('a.btn.ghost', { href: '#/admin/users' }, T.aUsers + ' →'),
      h('button.btn', { onclick: async () => { await api('DELETE', '/api/admin/party-reports/' + p.id); toast(T.aReportClosed); adminReports(body); } }, T.aReportClose)));
  put(body, aHead(T.aReports, T.aLedeReports),
    r.reports.length ? h('div.a-list', null, h('p.a-count', null, T.aReportsN(r.reports.length)), r.reports.map(row)) : emptyNote(T.aNoReports));
}

// ------------------------------------------------------------------ languages (export, import, edit, add)
const clone = v => JSON.parse(JSON.stringify(v));
const getPath = (o, p) => p.reduce((a, k) => (a == null ? undefined : a[k]), o);
// Editable leaves of a strings object: [path, value]. Plural forms follow the language's own plural rules.
function langLeaves(o, locale) {
  const out = [];
  let cats = ['one', 'other'];
  try { cats = new Intl.PluralRules(locale).resolvedOptions().pluralCategories; } catch { /* keep default */ }
  const walk = (v, p) => {
    if (typeof v === 'string') { out.push(p); return; }
    if ((p.length === 1 || (p.length === 2 && p[0] === 'app')) && isPluralVal(v)) {
      const forms = [...Object.keys(v).filter(k => k.startsWith('=')), ...cats.filter(c => c !== 'other'), 'other'];
      for (const f of new Set(forms)) out.push([...p, f]);
      return;
    }
    if (p[0] === 'srvRe' && p.length === 1 && Array.isArray(v)) { v.forEach((e, i) => Array.isArray(e) && out.push(['srvRe', i, 1])); return; }
    if (Array.isArray(v)) v.forEach((e, i) => walk(e, [...p, i]));
    else if (isObj(v)) for (const [k, e] of Object.entries(v)) walk(e, [...p, k]);
  };
  for (const [k, v] of Object.entries(o)) if (!['kinora_language', 'code', 'name'].includes(k)) walk(v, [k]);
  return out;
}
const langFull = code => langChain(code).reduce(mergeStrings, {});
const langOwn = code => (I18N[code] && I18N[code].strings) || {};
// A row counts as translated when it has text; an extra plural form may stay empty while "other" is set.
const langDone = (own, p) => {
  const v = getPath(own, p);
  if (typeof v === 'string' && v !== '') return true;
  const parent = getPath(own, p.slice(0, -1)), f = String(p[p.length - 1]);
  return p.length > 1 && isPluralVal(parent) && !!parent.other && f !== 'other' && !f.startsWith('=');
};
function langStats(code) {
  const own = langOwn(code), rows = langLeaves(langFull(code), own.locale || code);
  const done = rows.filter(p => langDone(own, p)).length;
  return { done, total: rows.length, pct: rows.length ? Math.floor(done / rows.length * 100) : 100 };
}
function downloadJSON(name, data) {
  const a = h('a', { href: URL.createObjectURL(new Blob([JSON.stringify(data, null, 1) + '\n'], { type: 'application/json' })), download: name });
  document.body.append(a); a.click(); a.remove();
  setTimeout(() => URL.revokeObjectURL(a.href), 5000);
}
const langExport = code => ({ kinora_language: 1, code, name: I18N[code].name, strings: code === 'hu' ? clone(langOwn('hu')) : langFull(code) });
async function saveLang(code, name, strings) {
  await api('PUT', '/api/admin/languages/' + encodeURIComponent(code), { kinora_language: 1, code, name, strings });
  toast(T.langSavedReload);
  setTimeout(() => location.reload(), 600);
}

async function adminLangs(body) {
  const file = h('input.hidden', { type: 'file', accept: 'application/json,.json', onchange: async e => {
    const f = e.target.files[0];
    e.target.value = '';
    if (!f) return;
    let d;
    try { d = JSON.parse(await f.text()); } catch { d = null; }
    if (!d || !isObj(d.strings) || typeof d.code !== 'string' || typeof d.name !== 'string' || (d.kinora_language && d.kinora_language !== 1)) { toast(T.langBadFile, true); return; }
    const code = d.code.trim().toLowerCase();
    if (I18N[code] && !(await askYes(T.langOverwrite(I18N[code].name), T.langImport))) return;
    try { await saveLang(code, d.name, d.strings); } catch (err) { toast(err.message, true); }
  } });
  const row = code => {
    const l = I18N[code], st = langStats(code);
    const kind = l.builtin ? (l.custom ? T.langModified : T.langBuiltin) : T.langAdded;
    return h('tr', null,
      tdl('', null, h('b', { lang: code }, l.name), h('div.small.muted.mono', null, code)),
      tdl('.small', T.langType, kind),
      tdl('', T.langProgress, h('div.pct' + (st.pct === 100 ? '.ok' : ''), null, h('div.bar', null, h('i', { style: { width: st.pct + '%' } })), T.langComplete(st.pct))),
      h('td.acts', null, h('div.row-gap', null,
        h('button.btn', { onclick: () => langEditor(body, code) }, T.edit),
        h('button.btn', { onclick: () => downloadJSON('kinora-' + code + '.json', langExport(code)) }, T.langExport),
        l.custom && h('button.btn.danger', { onclick: async () => {
          if (!(await askYes(l.builtin ? T.langResetAsk(l.name) : T.confirmDelete(l.name), l.builtin ? T.langReset : T.delete))) return;
          await api('DELETE', '/api/admin/languages/' + encodeURIComponent(code));
          toast(T.langSavedReload);
          setTimeout(() => location.reload(), 600);
        } }, l.builtin ? T.langReset : T.delete))));
  };
  put(body, aHead(T.aLangs, T.langsLede, h('button.btn', { onclick: () => file.click() }, T.langImport), h('button.btn.primary', { onclick: () => newLangDialog(body) }, icon('plus'), T.langNew)), file,
    aTable([T.language, T.langType, T.langProgress, ''], LANGS.map(([c]) => row(c)), '.a-stack'),
    h('p.a-foot-note', null, T.langFormatHint('{0}', '{1}')));
}

function askYes(text, yes) {
  return new Promise(res => {
    modal(text, null, close => [h('button.btn', { onclick: () => { close(); res(false); } }, T.cancel), h('button.btn.primary', { onclick: () => { close(); res(true); } }, yes)]);
  });
}

function newLangDialog(body) {
  const code = h('input', { type: 'text', required: true, maxlength: 12, placeholder: 'fr, pt-br', autocapitalize: 'off', spellcheck: 'false' });
  const name = h('input', { type: 'text', required: true, maxlength: 40, placeholder: 'Français' });
  const locale = h('input', { type: 'text', maxlength: 20, placeholder: 'fr-FR', autocapitalize: 'off', spellcheck: 'false' });
  const from = h('select', null, h('option', { value: '' }, T.langFromEmpty), LANGS.map(([c, n]) => h('option', { value: c }, n)));
  const err = h('p.bad.small');
  modal(T.langNew, h('div', null,
    h('label.field', null, h('span', null, T.langCode), code), h('label.field', null, h('span', null, T.langName), name),
    h('label.field', null, h('span', null, T.langLocale), locale), h('label.field', null, h('span', null, T.langCopyFrom), from), err),
  close => [h('button.btn', { onclick: close }, T.cancel), h('button.btn.primary', { onclick: async () => {
    const c = code.value.trim().toLowerCase(), n = name.value.trim();
    if (!/^[a-z]{2,3}(-[a-z0-9]{2,8})?$/.test(c)) { err.textContent = srv('érvénytelen nyelvkód (pl. fr vagy pt-br)'); return; }
    if (!n) { err.textContent = srv('a nyelv neve 1–40 karakter lehet'); return; }
    if (I18N[c]) { err.textContent = T.langExists; return; }
    const strings = from.value ? clone(from.value === 'hu' ? langOwn('hu') : langFull(from.value)) : {};
    const loc = locale.value.trim() || c;
    strings.locale = okLocale(loc) ? loc : c;
    try { await saveLang(c, n, strings); close(); } catch (e) { err.textContent = e.message; }
  } }, T.langCreate)]);
}

function langEditor(body, code) {
  const l = I18N[code];
  const own = clone(langOwn(code));
  const full = langFull(code);
  // what an empty field falls back to, and the text a translator works from
  const fallback = langChain(code).slice(0, -1).reduce(mergeStrings, {});
  const refCode = code === 'en' ? 'hu' : 'en';
  const ref = langFull(refCode);
  const paths = langLeaves(full, own.locale || code);
  const name = h('input', { type: 'text', value: l.name, maxlength: 40, 'aria-label': T.langName });
  const q = h('input', { type: 'search', placeholder: T.langSearch, 'aria-label': T.langSearch });
  const onlyMissing = h('input', { type: 'checkbox' });
  const count = h('span.small.muted');
  const fields = paths.map(p => {
    const v = getPath(own, p);
    // "srv" and "app" are keyed by their Hungarian text; for "app", show English to other languages where there is one
    const refText = p[0] === 'srvRe' ? getPath(full, ['srvRe', p[1], 0]) : p[0] === 'srv' ? p[1]
      : p[0] === 'app' ? (code === 'en' ? p[1] : (typeof getPath(ref, p) === 'string' ? getPath(ref, p) : p[1])) : getPath(ref, p);
    const fb = getPath(fallback, p);
    const long = String(refText || fb || '').length > 70;
    const input = h(long ? 'textarea' : 'input', { type: long ? null : 'text', value: typeof v === 'string' ? v : '', placeholder: typeof fb === 'string' ? fb : '', rows: long ? 2 : null, spellcheck: 'true', lang: code, 'aria-label': p.join('.') });
    const label = p[0] === 'srv' ? 'srv' : p[0] === 'srvRe' ? 'srvRe ' + p[1] : p[0] === 'app' ? (p.length > 2 ? 'app.' + p[2] : 'app') : p.join('.');
    const tr = h('tr', null, h('td.mono.small', null, label), h('td.small.muted', { lang: refCode }, refText == null ? '' : String(refText)), h('td', null, input));
    const form = String(p[p.length - 1]);
    return { p, input, tr, optional: p.length > 1 && isPluralVal(getPath(full, p.slice(0, -1))) && form !== 'other' && !form.startsWith('='), hay: (label + ' ' + (p[0] === 'app' ? p[1] + ' ' : '') + (refText || '') + ' ' + (fb || '')).toLowerCase() };
  });
  const filter = () => {
    const s = q.value.trim().toLowerCase();
    let n = 0;
    for (const f of fields) { const show = (!s || f.hay.includes(s) || f.input.value.toLowerCase().includes(s)) && (!onlyMissing.checked || !(f.input.value || (f.optional && fields.some(o => o.p.length === f.p.length && o.p.slice(0, -1).join('\x00') === f.p.slice(0, -1).join('\x00') && o.p[o.p.length - 1] === 'other' && o.input.value)))); f.tr.classList.toggle('hidden', !show); if (show) n++; }
    count.textContent = T.langRows(n);
  };
  q.addEventListener('input', filter);
  onlyMissing.addEventListener('change', filter);
  const collect = () => {
    const out = clone(own);
    // server-message patterns: keep the ones that have a translation here
    out.srvRe = fields.filter(f => f.p[0] === 'srvRe' && f.input.value).map(f => [getPath(full, ['srvRe', f.p[1], 0]), f.input.value]);
    if (!out.srvRe.length) delete out.srvRe;
    for (const { p, input } of fields) {
      if (p[0] === 'srvRe') continue;
      const v = input.value;
      let o = out;
      for (let i = 0; i < p.length - 1; i++) {
        if (o[p[i]] == null || typeof o[p[i]] !== 'object') {
          if (!v) { o = null; break; }
          const r = getPath(full, p.slice(0, i + 1));
          o[p[i]] = Array.isArray(r) ? clone(r) : {};
        }
        o = o[p[i]];
      }
      if (!o) continue;
      const k = p[p.length - 1];
      if (v) o[k] = v;
      else if (!Array.isArray(o)) delete o[k];
    }
    // a plural form set needs "other"; borrow it from another form rather than drop the translation
    for (const v of [...Object.values(out), ...(isObj(out.app) ? Object.values(out.app) : [])]) {
      if (!isObj(v) || !Object.keys(v).length || !Object.keys(v).every(x => PLURAL_KEY.test(x))) continue;
      if (!v.other) v.other = Object.values(v).find(Boolean);
    }
    if (isObj(out.app)) for (const [k, v] of Object.entries(out.app)) if (isObj(v) && !Object.keys(v).length) delete out.app[k];
    for (const k of ['srv', 'app', 'sortLabels', 'ruleFields', 'kindOpts', 'langNames', 'platforms', 'editions']) if (isObj(out[k]) && !Object.keys(out[k]).length) delete out[k];
    return out;
  };
  put(body, h('div.a-crumb', null, h('button.btn.ghost', { onclick: () => adminLangs(body) }, '← ' + T.aLangs)),
    aHead([l.name, ' ', h('span.badge.a-code', null, code)], T.langEditorHint('{0}', '{1}')),
    h('div.a-toolbar', null, h('label.field', null, h('span', null, T.langName), name), h('label.field.grow', null, h('span', null, T.langSearch), q),
      h('label.check', null, onlyMissing, T.langOnlyMissing), count),
    h('table.tbl.lang-tbl', null, h('thead', null, h('tr', null, h('th', null, T.langKey), h('th', null, I18N[refCode] ? I18N[refCode].name : refCode), h('th', null, l.name))),
      h('tbody', null, fields.map(f => f.tr))),
    h('div.row-gap.lang-save', null,
      h('button.btn.primary', { onclick: async e => { e.target.disabled = true; try { await saveLang(code, name.value.trim() || l.name, collect()); } catch (err) { toast(err.message, true); e.target.disabled = false; } } }, T.save),
      h('button.btn', { onclick: () => downloadJSON('kinora-' + code + '.json', { kinora_language: 1, code, name: name.value.trim() || l.name, strings: collect() }) }, T.langExport)));
  filter();
}

async function uploadLogo(variant, file) {
  const res = await fetch('/api/admin/branding/logo?variant=' + variant, { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': file.type || 'application/octet-stream' }, body: file });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(srv(data.error) || 'HTTP ' + res.status);
  state.branding = data;
  applyBranding();
  return data;
}

function lookPanel(values, env, rerender) {
  const b = state.branding || {};
  const slot = (variant, label) => {
    const url = variant === 'dark' ? b.logo_dark : b.logo_light;
    const file = h('input.hidden', { type: 'file', accept: 'image/png,image/jpeg,image/webp,image/svg+xml', onchange: async e => {
      try { await uploadLogo(variant, e.target.files[0]); toast(T.saved); rerender(); } catch (err) { toast(err.message, true); }
    } });
    return h('div.logo-slot', null, h('span.small.muted', null, label),
      h('div.logo-preview.' + variant, null, url ? h('img', { src: url + '?v=' + Date.now(), alt: label }) : h('span.muted.small', null, T.noLogo)),
      h('div.row-gap', null, h('button.btn', { onclick: () => file.click() }, T.uploadLogo), file,
        url && h('button.btn.danger', { onclick: async () => { state.branding = await api('DELETE', '/api/admin/branding/logo?variant=' + variant); applyBranding(); rerender(); } }, T.removeLogo)));
  };
  const accent = h('input', { type: 'color', value: values.brand_accent || '#f0b44c', disabled: !!env.brand_accent, 'aria-label': T.accent });
  return aSplit(T.aLook, T.logoHint,
    h('div.logo-slots', null, slot('dark', T.logoDark), slot('light', T.logoLight)),
    h('label.field.a-accent', null, h('span', null, T.accent), h('div.row-gap', null, accent,
      h('button.btn', { disabled: !!env.brand_accent, onclick: async () => { await api('PATCH', '/api/admin/settings', { brand_accent: accent.value }); state.branding = await GET('/api/branding'); applyBranding(); toast(T.saved); } }, T.save),
      h('button.btn.ghost', { disabled: !!env.brand_accent, onclick: async () => { await api('PATCH', '/api/admin/settings', { brand_accent: '' }); state.branding = await GET('/api/branding'); applyBranding(); rerender(); } }, T.defaultAccent)),
      env.brand_accent && h('div.env-lock', null, T.envLocked(env.brand_accent))));
}

async function adminSettings(body) {
  const s = await GET('/api/admin/settings');
  state.branding = await GET('/api/branding').catch(() => state.branding);
  put(body, aHead(T.aSettings, T.aLedeSettings),
    settingsForm(s.values, s.env, [{ h: T.setGeneral, hint: T.setGeneralHint }, ['server_name', T.serverName], ['public_url', T.publicUrl, 'url'],
      { h: T.setScanning, hint: T.setScanningHint }, ['scan_interval_min', T.scanInterval, 'number'],
      { h: T.setNetwork, hint: T.setNetworkHint }, ['quickconnect_enabled', T.qcEnabled, 'toggle'], ['discovery_enabled', T.discEnabled, 'toggle'],
      ['push_enabled', T.pushEnabled, 'toggle'], ['push_relay_url', T.pushRelayUrl, 'url', null, T.pushRelayHint],
      { h: T.setJellyfin, hint: T.setJellyfinHint },
      ['jellyfin_compat', T.jfCompat, 'toggle', null, s.jellyfin_compat_addr ? T.jfCompatPort(s.jellyfin_compat_addr) : T.jfCompatNoEnv]], () => { state.status = null; }),
    lookPanel(s.values, s.env, () => adminSettings(body)),
    aSplit(T.wizReopenTitle, T.wizReopenHint, h('a.btn', { href: '#/setup/' + WIZ[0] }, T.wizReopen)),
    aSplit(T.backupTitle, T.backupHint, h('a.btn', { href: '/api/admin/backup', download: '' }, icon('dl'), T.backup)));
}

// ===================================================================== admin: acquisition (Beszerzés)
// The built-in acquisition module (internal/acquire): find a film on TMDB, pick a release from the
// indexers, the download client fetches it, Kinora imports it into a library. Admin only (M1).
const acqStateLabel = st => ({ downloading: T.acqDownloading, importing: T.acqImporting, done: T.acqDone, failed: T.acqFailed, removed: T.acqRemoved })[st] || st;
const acqReject = code => ({ resolution: T.acqRejRes, source: T.acqRejSource, size: T.acqRejSize, seeders: T.acqRejSeeders, language: T.acqRejLang, title: T.acqRejTitle, '3d': T.acqRej3d, blocklisted: T.acqRejBlock })[code] || code;
const fmtSpeed = b => fmtBytes(b) + '/s';
// a test button for an unsaved configuration: the result stays under the form
// download clients: torrents (qBittorrent, Transmission, Deluge) and Usenet (SABnzbd, NZBGet); Kinora picks by the release
const ACQ_CLIENT_KINDS = [['qbittorrent', 'qBittorrent', 'http://192.168.1.10:8080'], ['transmission', 'Transmission', 'http://192.168.1.10:9091'],
  ['deluge', 'Deluge', 'http://192.168.1.10:8112'], ['sabnzbd', 'SABnzbd', 'http://192.168.1.10:8080'], ['nzbget', 'NZBGet', 'http://192.168.1.10:6789']];

function acqTestButton(url, body, out) {
  return h('button.btn', { type: 'button', onclick: async e => {
    const b = e.currentTarget;
    b.disabled = true; put(out, T.acqTesting); out.className = 'small muted';
    try { const r = await POST(url, body(), { quiet: true }); put(out, T.acqTestOk(r.server || '')); out.className = 'small ok'; }
    catch (err) { put(out, err.message); out.className = 'small bad'; }
    finally { b.disabled = false; }
  } }, T.acqTest);
}

// one row per film or series: its downloads are grouped, the row shows the one that matters most
// (running > failed > finished), the older attempts and the other episodes open below it
const epCode = (s, e) => `S${String(s).padStart(2, '0')}E${String(e).padStart(2, '0')}`;
const grabWhat = g => g.season < 0 ? '' : g.episodes ? g.episodes.split(',').map(e => epCode(g.season, e)).join(', ') : T.seasonN(g.season);
const QRANK = { downloading: 0, importing: 1, failed: 2, done: 3, removed: 4 };
async function adminAcqQueue(body) {
  let t, dead = false, filter = 'active', picked = false;
  const open = new Set();
  onCleanup(() => { dead = true; clearTimeout(t); });
  const render = async () => {
    const [list, st] = await Promise.all([GET('/api/admin/acquire/queue', { quiet: true }).catch(() => null), GET('/api/admin/acquire/stats', { quiet: true }).catch(() => null)]);
    if (dead || !list) return;
    last = [list, st];
    paint();
    clearTimeout(t);
    t = setTimeout(render, list.some(g => g.status === 'downloading' || g.status === 'importing') ? 3000 : 15000);
  };
  let last;
  const paint = () => {
    const [list, st] = last;
    const groups = new Map();
    for (const g of list) {
      const k = g.series_id ? 's' + g.series_id : g.kind + g.tmdb_id;
      if (!groups.has(k)) groups.set(k, { key: k, title: g.title, year: g.year, series: !!g.series_id || g.season >= 0, grabs: [] });
      groups.get(k).grabs.push(g);
    }
    const all = [...groups.values()].map(x => {
      x.grabs.sort((a, b) => QRANK[a.status] - QRANK[b.status] || b.created_at - a.created_at);
      x.top = x.grabs[0];
      x.state = x.top.status === 'importing' ? 'downloading' : x.top.status;
      x.last = Math.max(...x.grabs.map(g => g.updated_at || g.created_at));
      return x;
    }).sort((a, b) => QRANK[a.top.status] - QRANK[b.top.status] || b.last - a.last);
    const count = { active: 0, failed: 0, done: 0, all: 0 };
    for (const x of all) {
      if (x.state === 'downloading') count.active++;
      else if (x.state === 'failed') count.failed++;
      else if (x.state === 'done') count.done++;
      if (x.state !== 'removed') count.all++;
    }
    if (!picked && filter === 'active' && !count.active) filter = count.failed ? 'failed' : 'all';
    const shown = all.filter(x => filter === 'all' ? x.state !== 'removed' : x.state === (filter === 'active' ? 'downloading' : filter));

    const acts = g => h('div.row-gap.q-acts', null,
      g.status === 'failed' && h('button.btn.small', { onclick: async () => { await POST(`/api/admin/acquire/grabs/${g.id}/retry`); render(); } }, T.retry),
      g.status !== 'removed' && h('button.btn.small.danger', { onclick: async () => {
        const done = g.status === 'done';
        // a finished download: the box can also delete the torrent's data and the film from the library
        const wipe = done && h('input', { type: 'checkbox', checked: true });
        if (!await confirmBox(done ? T.acqForgetAsk(g.title) : T.acqRemoveAsk(g.title), done ? T.acqForget : T.acqRemove,
          wipe && h('div', null, h('label.check', null, wipe, T.acqDeleteFiles), h('div.hint.mt1', null, T.acqDeleteFilesHint)))) return;
        const all = done && wipe.checked;
        try { await api('DELETE', `/api/admin/acquire/grabs/${g.id}?files=${!done || all ? 1 : 0}&library=${all ? 1 : 0}`); }
        catch { /* api() already showed the error */ }
        render();
      } }, g.status === 'done' ? T.acqForget : T.acqRemove));
    const badge = g => h('span.badge' + (g.status === 'failed' ? '.warn' : g.status === 'done' ? '.acq-have' : ''), null, acqStateLabel(g.status));
    const prog = g => (g.status === 'downloading' || g.status === 'importing') && h('div.q-prog', null, aBar(g.progress, 1),
      h('span.small.muted', null, num(g.progress * 100, 0) + '%' + (g.speed > 0 ? ' · ' + fmtSpeed(g.speed) : '') + (g.eta > 0 ? ' · ' + fmtTime(g.eta) : '')));
    const sub = g => h('div.q-detail', null,
      h('div.row-gap', null, badge(g), grabWhat(g) && h('b.small', null, grabWhat(g)), h('span.small.muted', null, [g.size ? fmtBytes(g.size) : '', ago(Date.now() / 1000 - g.created_at)].filter(Boolean).join(' · '))),
      h('div.acq-reltitle.muted', { title: g.release_title }, g.release_title),
      g.error && h('div.bad.small', null, srv(g.error)),
      g.status === 'done' && g.target_path && h('div.muted.small.mono.acq-url', null, g.target_path),
      prog(g), acts(g));
    const row = x => {
      const g = x.top, more = x.grabs.length - 1, isOpen = open.has(x.key);
      const what = x.series ? (x.grabs.length > 1 ? T.qEpisodes(x.grabs.filter(y => y.status !== 'removed').length) : grabWhat(g)) : '';
      return h('li.q-row.q-' + x.state, null,
        h('div.q-main', null,
          h('div.q-title', null, h('b', null, x.title, x.year ? h('span.muted', null, ' (' + x.year + ')') : null),
            h('div.small.muted', null, [what, g.size ? fmtBytes(g.size) : '', ago(Date.now() / 1000 - x.last)].filter(Boolean).join(' · ')),
            g.error && g.status === 'failed' && h('div.bad.small.q-err', { title: srv(g.error) }, srv(g.error))),
          h('div.q-state', null, badge(g), prog(g)),
          h('div.q-side', null, more > 0 || g.release_title ? h('button.btn.small.ghost', { 'aria-expanded': String(isOpen), onclick: () => { isOpen ? open.delete(x.key) : open.add(x.key); paint(); } },
            more > 0 ? T.qHistory(x.grabs.length) : T.qDetails) : null, acts(g))),
        isOpen && h('div.q-subs', null, x.grabs.map(sub)));
    };
    const tabs = [['active', T.qActive], ['failed', T.qFailed], ['done', T.qDone], ['all', T.qAll]];
    put(body, aHead(T.aAcqQueue, T.aLedeAcqQueue, h('a.btn', { href: '#/admin/acqrequests' }, T.aAcqRequests)),
      st && acqStatsPanel(st),
      all.length ? [
        h('div.chips.q-tabs', { role: 'tablist' }, tabs.map(([k, l]) => h('button.chip' + (filter === k ? '.on' : ''), { type: 'button', role: 'tab', 'aria-selected': String(filter === k), onclick: () => { filter = k; picked = true; paint(); } },
          l, h('span.n', null, ' ' + count[k])))),
        shown.length ? h('ul.q-list', null, shown.map(row)) : h('p.empty', null, T.qNone)]
        : emptyNote(T.acqQueueEmpty, null, h('a.btn', { href: '#/discover' }, T.discover)));
  };
  await render();
}

async function adminAcqSettings(body, then) {
  const [s, defs] = await Promise.all([GET('/api/admin/acquire/settings'), GET('/api/admin/acquire/definitions').catch(() => [])]);
  const rerender = next => adminAcqSettings(body, next);
  const enabled = h('input.switch', { type: 'checkbox', role: 'switch', checked: s.enabled, 'aria-label': T.acqEnable, onchange: async e => {
    await api('PUT', '/api/admin/acquire/settings', { enabled: e.target.checked }); toast(T.saved); rerender();
  } });

  // indexers (Torznab: a tracker's own API, or Jackett / Prowlarr standing in for several)
  const indexerDialog = (x, defID) => {
    const isNew = !x;
    const pre = defID && defs.find(d => d.id === defID);
    x = x || { name: pre ? pre.name : '', kind: defs.length ? 'cardigann' : 'torznab', url: '', api_key: '', categories: '', enabled: true, priority: 25, definition_id: pre ? pre.id : defs.length ? defs[0].id : '', config: '{}' };
    const name = h('input', { type: 'text', value: x.name });
    const kind = h('select', null, [['torznab', T.acqKindTorznab], ['newznab', T.acqKindNewznab], ['cardigann', T.acqKindDef]].map(([v, l]) => h('option', { value: v, selected: (x.kind || 'torznab') === v }, l)));
    // the built-in engine: a definition and a form made from its settings
    let cfg = {};
    try { cfg = JSON.parse(x.config || '{}') || {}; } catch (_) { cfg = {}; }
    const defSel = h('select', null, defs.map(d => h('option', { value: d.id, selected: d.id === x.definition_id }, d.name)));
    const defBox = h('div.a-form');
    let inputs = {};
    const drawDef = () => {
      const d = defs.find(d => d.id === defSel.value);
      inputs = {};
      if (!d) { put(defBox, h('p.small.muted', null, T.acqDefNone)); return; }
      put(defBox, h('p.small.muted', null, [d.type, d.language, (d.links || [])[0]].filter(Boolean).join(' · ')),
        ...(d.settings || []).map(st => {
          const v = st.name in cfg && d.id === x.definition_id ? cfg[st.name] : st.default;
          if (st.type === 'info') return h('p.small.muted.acq-def-info', null, (st.label || '').replace(/<[^>]+>/g, ' '));
          if (st.type === 'checkbox') {
            const el = inputs[st.name] = h('input.switch', { type: 'checkbox', role: 'switch', checked: v === true || v === 'true' });
            return h('div.form-row', null, h('label.switch-row', null, h('span', null, st.label || st.name), el));
          }
          if (st.type === 'select') {
            const el = inputs[st.name] = h('select', null, Object.entries(st.options || {}).map(([k, l]) => h('option', { value: k, selected: String(v) === k }, l)));
            return field(st.label || st.name, el);
          }
          const secret = st.type === 'password' || /^(cookie|apikey|passkey)$|token/i.test(st.name);
          const el = inputs[st.name] = h('input', { type: secret ? 'password' : 'text', value: v == null ? '' : String(v), autocomplete: 'off' });
          return field(st.label || st.name, el);
        }));
    };
    defSel.onchange = drawDef;
    drawDef();
    const config = () => { const o = {}; for (const [k, el] of Object.entries(inputs)) o[k] = el.type === 'checkbox' ? el.checked : el.value; return JSON.stringify(o); };
    const url = h('input', { type: 'url', value: x.url, placeholder: 'http://192.168.1.10:9117/api/v2.0/indexers/all/results/torznab' });
    const key = h('input', { type: 'password', value: x.api_key, autocomplete: 'off' });
    const cats = h('input', { type: 'text', value: x.categories, placeholder: '2000' });
    const prio = h('input', { type: 'number', min: 1, max: 99, value: x.priority });
    const en = h('input.switch', { type: 'checkbox', role: 'switch', checked: x.enabled });
    const out = h('p.small', { role: 'status' });
    const data = () => kind.value === 'cardigann'
      ? { id: x.id || 0, kind: 'cardigann', name: name.value, definition_id: defSel.value, config: config(), priority: +prio.value || 25, enabled: en.checked }
      : { id: x.id || 0, kind: kind.value, name: name.value, url: url.value, api_key: key.value, categories: cats.value, priority: +prio.value || 25, enabled: en.checked };
    const apiPart = h('div.a-form', null, field(T.acqUrl, url, T.acqUrlHint), field(T.acqApiKey, key), field(T.acqCats, cats, T.acqCatsHint));
    const defPart = h('div.a-form', null, defs.length ? field(T.acqDef, defSel, T.acqDefHint) : null, defBox);
    const sync = () => { const c = kind.value === 'cardigann'; apiPart.hidden = c; defPart.hidden = !c; };
    kind.addEventListener('change', sync); sync();
    modal(isNew ? T.acqAddIndexer : x.name, h('div.a-form', null, h('div.grid2', null, field(T.acqName, name), field(T.acqKind, kind)),
      apiPart, defPart, field(T.acqPriority, prio, T.acqPriorityHint),
      h('div.form-row', null, h('label.switch-row', null, h('span', null, T.acqEnabledRow), en)), out),
      close => [acqTestButton('/api/admin/acquire/indexers/test', data, out),
        h('button.btn', { onclick: close }, T.cancel),
        h('button.btn.primary', { onclick: async () => { await api(isNew ? 'POST' : 'PATCH', '/api/admin/acquire/indexers' + (isNew ? '' : '/' + x.id), data()); close(); toast(T.saved); rerender(); } }, T.save)]);
  };
  const ixRows = s.indexers.map(x => h('tr', null,
    tdl('', T.acqName, h('b', null, x.name), h('div.muted.small.mono.acq-url', null, x.kind === 'cardigann' ? T.acqDefRow((defs.find(d => d.id === x.definition_id) || {}).name || x.definition_id) : x.url)),
    tdl('', T.acqStatus, !x.enabled ? h('span.muted.small', null, T.acqIdxOff) : x.last_error ? h('span.bad.small', { title: x.last_error }, T.acqIdxError) : x.last_ok_at ? h('span.ok.small', null, T.acqIdxOk) : h('span.muted.small', null, '–')),
    tdl('', T.acqPriority, x.priority),
    tdl('.a-tbl-acts', '', h('div.row-gap', null, h('button.btn', { onclick: () => indexerDialog(x) }, T.acqEdit),
      h('button.btn.danger', { onclick: async () => { if (await confirmBox(T.confirmDelete(x.name))) { await api('DELETE', '/api/admin/acquire/indexers/' + x.id); rerender(); } } }, T.delete)))));

  // uploaded indexer definitions (Cardigann YAML, the format Jackett and Prowlarr use)
  const upload = h('input', { type: 'file', accept: '.yml,.yaml', multiple: true, hidden: true, onchange: async e => {
    const files = [...e.target.files]; e.target.value = '';
    let n = 0;
    for (const f of files) {
      try { await POST('/api/admin/acquire/definitions', { yaml: await f.text() }, { quiet: true }); n++; }
      catch (err) { toast(f.name + ': ' + err.message, true); }
    }
    if (n) { toast(T.acqDefUploaded(n)); rerender(); }
  } });
  upload.style.display = 'none';
  const defRows = defs.map(d => h('tr', null,
    tdl('', T.acqName, h('b', null, d.name), h('div.muted.small.mono', null, d.id + ((d.links || [])[0] ? ' · ' + d.links[0] : ''))),
    tdl('', T.acqKind, h('span.small', null, (d.builtin ? T.acqDefBuiltin + ' · ' : '') + (d.type || '–'))),
    tdl('', T.acqDefUsed, d.in_use),
    tdl('.a-tbl-acts', '', d.builtin ? null : h('button.btn.danger', { disabled: d.in_use > 0, title: d.in_use > 0 ? T.acqDefInUse : '', onclick: async () => {
      if (await confirmBox(T.confirmDelete(d.name))) { await api('DELETE', '/api/admin/acquire/definitions/' + encodeURIComponent(d.id)); rerender(); }
    } }, T.delete))));
  const defList = h('div', null,
    defs.length ? aTable([T.acqName, T.acqKind, T.acqDefUsed, ''], defRows, '.a-stack') : emptyNote(T.acqDefEmpty, null),
    h('div.a-form-foot', null, upload,
      h('button.btn.primary', { onclick: () => catalogDialog(id => rerender({ openDef: id })) }, icon('search'), T.catBrowse),
      defs.some(d => !d.builtin) && h('button.btn', { onclick: async e => {
        const b = e.currentTarget; b.disabled = true;
        try { const r = await POST('/api/admin/acquire/catalog/update', {}); toast(r.updated ? T.catUpdated(r.updated) : T.catUpToDate); if (r.updated) rerender(); }
        finally { b.disabled = false; }
      } }, T.catUpdate),
      h('button.btn', { onclick: () => upload.click() }, icon('plus'), T.acqDefUpload)));

  // sites behind a Cloudflare / DDoS-Guard browser check go through FlareSolverr
  const fsUrl = h('input', { type: 'url', value: s.flaresolverr || '', placeholder: 'http://flaresolverr:8191' });
  const fsOut = h('p.small', { role: 'status' });
  const fsBox = aSplit(T.fsTitle, T.fsHint, h('div.a-form', null, field(T.fsUrl, fsUrl, T.fsUrlHint), fsOut,
    h('div.a-form-foot', null, acqTestButton('/api/admin/acquire/flaresolverr/test', () => ({ url: fsUrl.value }), fsOut),
      h('button.btn.primary', { onclick: async () => { await api('PUT', '/api/admin/acquire/settings', { flaresolverr: fsUrl.value }); toast(T.saved); rerender(); } }, T.save))));

  const KINDS = ACQ_CLIENT_KINDS;
  // what a client sorts by: qBittorrent category and folder, Transmission / Deluge a folder, Usenet a category
  const isUsenet = k => k === 'sabnzbd' || k === 'nzbget';
  const hasCat = k => k !== 'transmission' && k !== 'deluge';
  const clientDialog = c => {
    const isNew = !c;
    c = c || { kind: 'qbittorrent', name: '', url: '', username: '', password: '', category: 'kinora-movies', save_path: '', category_tv: 'kinora-tv', save_path_tv: '', path_remote: '', path_local: '', enabled: true };
    const kind = h('select', null, KINDS.map(([v, l]) => h('option', { value: v, selected: c.kind === v }, l)));
    const inp = (k, more) => h('input', Object.assign({ type: 'text', value: c[k] || '' }, more));
    const ci = {
      name: inp('name'), url: inp('url', { type: 'url' }),
      username: inp('username', { autocomplete: 'off' }), password: inp('password', { type: 'password', autocomplete: 'new-password' }),
      category: inp('category'), save_path: inp('save_path'), category_tv: inp('category_tv'), save_path_tv: inp('save_path_tv'),
      path_remote: inp('path_remote', { placeholder: '/downloads' }), path_local: inp('path_local', { placeholder: '/data/downloads' }),
    };
    const en = h('input.switch', { type: 'checkbox', role: 'switch', checked: c.enabled });
    const userField = field(T.username, ci.username), passLabel = h('span', null, T.password);
    const passField = h('label.field', null, passLabel, ci.password);
    const cat = [field(T.acqCategory, ci.category), field(T.acqCategory, ci.category_tv)];
    const dir = [field(T.acqFolder, ci.save_path), field(T.acqFolder, ci.save_path_tv)];
    const where = h('p.hint.acq-where');
    // an empty field shows what applies instead: the client's default for films, the films' value for series
    const follow = () => {
      ci.category.placeholder = ci.save_path.placeholder = T.acqClientDefault;
      ci.category_tv.placeholder = ci.category.value.trim() || T.acqClientDefault;
      ci.save_path_tv.placeholder = ci.save_path.value.trim() || T.acqClientDefault;
    };
    ci.category.addEventListener('input', follow); ci.save_path.addEventListener('input', follow);
    const sync = () => {
      const k = kind.value;
      ci.url.placeholder = KINDS.find(x => x[0] === k)[2];
      passLabel.textContent = k === 'sabnzbd' ? T.acqApiKey : T.password;
      userField.classList.toggle('hidden', k === 'deluge' || k === 'sabnzbd');
      dir.forEach(f => f.classList.toggle('hidden', isUsenet(k)));
      cat.forEach(f => f.classList.toggle('hidden', !hasCat(k)));
      where.textContent = isUsenet(k) ? T.acqWhereUsenet : hasCat(k) ? T.acqWhereBoth : T.acqWhereFolder;
      follow();
    };
    kind.addEventListener('change', sync); sync();
    const out = h('p.small', { role: 'status' });
    const data = () => Object.assign({ id: c.id || 0, kind: kind.value, enabled: en.checked }, Object.fromEntries(Object.entries(ci).map(([k, el]) => [k, el.value.trim()])));
    const col = (ic, title, ...kids) => h('div.acq-route-col', null, h('h4', null, icon(ic), title), ...kids);
    modal(isNew ? T.acqAddClient : c.name, h('div.a-form.acq-cdlg', null,
      h('h3.acq-sub', null, T.acqConn),
      h('div.grid2', null, field(T.acqClientKind, kind), field(T.acqName, ci.name)),
      field(T.acqClientUrl, ci.url), h('div.grid2', null, userField, passField),
      h('h3.acq-sub', null, T.acqWhere), where,
      h('div.acq-route-grid', null, col('film', T.kinds.movies, cat[0], dir[0]), col('tv', T.kinds.shows, cat[1], dir[1], h('div.hint', null, T.acqTvHint))),
      h('details.a-opt.acq-adv', { open: !!(c.path_remote || c.path_local) }, h('summary', null, T.acqPathMap),
        h('div.a-opt-b', null, h('div.grid2', null, field(T.acqPathRemote, ci.path_remote), field(T.acqPathLocal, ci.path_local)), h('div.hint', null, T.acqPathHint))),
      h('div.form-row', null, h('label.switch-row', null, h('span', null, T.acqEnabledRow), en)), out),
    close => [acqTestButton('/api/admin/acquire/clients/test', data, out),
      h('button.btn', { onclick: close }, T.cancel),
      h('button.btn.primary', { onclick: async () => { await api(isNew ? 'POST' : 'PATCH', '/api/admin/acquire/clients' + (isNew ? '' : '/' + c.id), data()); close(); toast(T.saved); rerender(); } }, T.save)]);
  };
  // each client as a ruled entry: who it is, and where films and series end up
  const route = (k, category, path) => {
    const parts = [hasCat(k) && category && h('span.acq-cat', null, icon('tag'), category), !isUsenet(k) && path && h('span.mono', null, path)].filter(Boolean);
    return parts.length ? parts : h('span.muted', null, T.acqClientDefault);
  };
  const clientRows = s.clients.map(c => {
    const own = (hasCat(c.kind) && c.category_tv) || (!isUsenet(c.kind) && c.save_path_tv);
    return h('article.acq-client' + (c.enabled ? '' : '.is-off'), null,
      h('div.acq-client-h', null,
        h('div.acq-client-t', null, h('h3', null, c.name),
          h('p.small.muted', null, (KINDS.find(x => x[0] === c.kind) || [, c.kind])[1] + ' · ' + (isUsenet(c.kind) ? 'Usenet' : 'Torrent') + ' · ', h('span.mono', null, c.url))),
        h('span.acq-dot.small.' + (c.enabled ? 'ok' : 'off'), null, c.enabled ? T.acqOn : T.acqOff),
        h('div.row-gap', null, h('button.btn', { onclick: () => clientDialog(c) }, icon('pencil'), T.acqEdit),
          h('button.btn.danger', { onclick: async () => { if (await confirmBox(T.confirmDelete(c.name))) { await api('DELETE', '/api/admin/acquire/clients/' + c.id); rerender(); } } }, T.delete))),
      h('dl.acq-route', null,
        h('dt', null, icon('film'), T.kinds.movies), h('dd', null, route(c.kind, c.category, c.save_path)),
        h('dt', null, icon('tv'), T.kinds.shows), h('dd', null, own ? route(c.kind, c.category_tv || c.category, c.save_path_tv || c.save_path) : h('span.muted', null, T.acqSameAsFilms)),
        c.path_remote && [h('dt', null, T.acqPathMap), h('dd', null, h('span.mono', null, c.path_remote), ' → ', h('span.mono', null, c.path_local))]));
  });
  const clientList = h('div', null,
    s.clients.length ? h('div.acq-clients', null, clientRows) : emptyNote(T.acqNoClients, null),
    h('div.a-form-foot', null, h('button.btn', { onclick: () => clientDialog(null) }, icon('plus'), T.acqAddClient)));

  // quality profile (the built-in one; named profiles below) and import
  const builtin = qualityEditor(s.profile);
  const mode = h('select', null, [['hardlink', T.acqModeLink], ['copy', T.acqModeCopy]].map(([v, l]) => h('option', { value: v, selected: s.import_mode === v }, l)));
  const saveProfile = h('button.btn.primary', { onclick: async () => {
    await api('PUT', '/api/admin/acquire/settings', { profile: builtin.value(), import_mode: mode.value }); toast(T.saved); rerender();
  } }, T.save);
  const profileDialog = pr => {
    const isNew = !pr;
    const ed = qualityEditor(pr ? JSON.parse(pr.json) : s.profile);
    const name = h('input', { type: 'text', value: pr ? pr.name : '' });
    const def = h('input.switch', { type: 'checkbox', role: 'switch', checked: !!(pr && pr.is_default) });
    modal(isNew ? T.profAdd : pr.name, h('div.a-form', null, field(T.acqName, name),
      h('div.form-row', null, h('label.switch-row', null, h('span', null, T.profDefault), def)), ed.el),
    close => [h('button.btn', { onclick: close }, T.cancel),
      h('button.btn.primary', { onclick: async () => {
        await api(isNew ? 'POST' : 'PATCH', '/api/admin/acquire/profiles' + (isNew ? '' : '/' + pr.id), { name: name.value, is_default: def.checked, profile: ed.value() });
        close(); toast(T.saved); rerender();
      } }, T.save)]);
  };
  const profRows = (s.profiles || []).map(pr => h('tr', null,
    tdl('', T.acqName, h('b', null, pr.name), pr.is_default && h('span.badge.acq-have', { class: 'ml2' }, T.profDefaultShort)),
    tdl('', T.profSummary, profileSummary(JSON.parse(pr.json))),
    tdl('.a-tbl-acts', '', h('div.row-gap', null, h('button.btn', { onclick: () => profileDialog(pr) }, T.acqEdit),
      h('button.btn.danger', { onclick: async () => { if (await confirmBox(T.confirmDelete(pr.name))) { await api('DELETE', '/api/admin/acquire/profiles/' + pr.id); rerender(); } } }, T.delete)))));
  const sw = (label, el) => h('div.form-row', null, h('label.switch-row', null, h('span', null, label), el));

  // requests (Felfedezés): who is approved automatically, weekly quota, where requested films go
  const auto = h('select', null, [['admins', T.reqAutoAdmins], ['all', T.reqAutoAll]].map(([v, l]) => h('option', { value: v, selected: s.auto_approve === v }, l)));
  const quota = h('input', { type: 'number', min: 0, max: 1000, value: s.quota_week });
  const retryH = h('input', { type: 'number', min: 1, max: 168, value: s.retry_hours });
  const reqSettings = aSplit(T.reqSettings, T.reqSettingsHint, h('div.a-form', null,
    field(T.reqAuto, auto), h('div.grid2', null, field(T.reqQuota, quota, T.reqQuotaHint), field(T.reqRetry, retryH, T.reqRetryHint)),
    h('div.a-form-foot', null, h('button.btn.primary', { onclick: async () => {
      await api('PUT', '/api/admin/acquire/settings', { auto_approve: auto.value, quota_week: +quota.value || 0, retry_hours: +retryH.value || 6 });
      toast(T.saved); rerender();
    } }, T.save))));

  // the page: on/off in the head, an overview ledger, and the four areas as tabs
  const ixOn = s.indexers.filter(x => x.enabled), ixBad = ixOn.filter(x => x.last_error).length,
    ixNew = ixOn.filter(x => !x.last_error && !x.last_ok_at).length;
  const clOn = s.clients.filter(c => c.enabled);
  const protos = [...new Set(clOn.map(c => isUsenet(c.kind) ? 'Usenet' : 'Torrent'))];
  const TABS = [['sources', T.acqTabSources], ['clients', T.acqClients], ['quality', T.acqTabQuality], ['requests', T.reqSettings]];
  const panels = {
    sources: [aSplit(T.acqIndexers, T.acqIndexersHint,
      s.indexers.length ? aTable([T.acqName, T.acqStatus, T.acqPriority, ''], ixRows, '.a-stack') : emptyNote(T.acqNoIndexers, null),
      h('div.a-form-foot', null, h('button.btn', { onclick: () => indexerDialog(null) }, icon('plus'), T.acqAddIndexer))),
    aSplit(T.acqDefs, T.acqDefsHint, defList), fsBox],
    clients: [aSplit(T.acqClients, T.acqClientsHint, clientList)],
    quality: [aSplit(T.acqProfile, T.acqProfileHint, h('div.a-form', null, builtin.el, field(T.acqImportMode, mode, T.acqImportModeHint), h('div.a-form-foot', null, saveProfile))),
      aSplit(T.profiles, T.profilesHint,
        profRows.length ? aTable([T.acqName, T.profSummary, ''], profRows, '.a-stack') : emptyNote(T.profNone, null),
        h('div.a-form-foot', null, h('button.btn', { onclick: () => profileDialog(null) }, icon('plus'), T.profAdd)))],
    requests: [reqSettings],
  };
  // the overview ledger is the tab strip: each area with its headline figure and how it stands
  const pane = {}, tab = {};
  for (const [k, kids] of Object.entries(panels)) pane[k] = h('div.acq-pane', { role: 'tabpanel', id: 'acq-' + k, 'aria-labelledby': 'acq-t-' + k }, kids);
  const show = k => {
    state.acqTab = k;
    for (const [id] of TABS) {
      const on = id === k;
      tab[id].classList.toggle('on', on); tab[id].setAttribute('aria-selected', String(on)); pane[id].hidden = !on;
    }
  };
  const cell = (k, n, label, note, warn) => tab[k] = h('button.acq-ov-i', { type: 'button', role: 'tab', id: 'acq-t-' + k, 'aria-controls': 'acq-' + k, onclick: () => show(k) },
    h('span.acq-ov-h', null, TABS.find(t => t[0] === k)[1]), h('span.acq-ov-n', null, h('b', null, n), h('span', null, label)),
    h('small' + (warn ? '.warn' : ''), null, note));
  const tabs = h('div.acq-ov', { role: 'tablist', 'aria-label': T.aAcqSettings },
    cell('sources', ixOn.length, T.acqOvSources, ixBad ? T.acqOvBad(ixBad) : !ixOn.length ? T.acqOvNone : ixNew ? T.acqOvUnchecked : T.acqOvOk, ixBad || !ixOn.length),
    cell('clients', clOn.length, T.acqOvClients, protos.length ? protos.join(' · ') : T.acqOvNone, !clOn.length),
    cell('quality', 1 + (s.profiles || []).length, T.acqOvProfiles, s.import_mode === 'copy' ? T.acqModeCopy : T.acqModeLink),
    cell('requests', s.quota_week || '∞', T.acqOvQuota, s.auto_approve === 'all' ? T.acqOvAutoAll : T.acqOvAutoAdmins));
  put(body, aHead(T.aAcqSettings, T.acqModuleHint,
    h('label.acq-power', null, h('span.acq-dot.' + (s.enabled ? 'ok' : 'off'), null, s.enabled ? T.acqOn : T.acqOff), enabled)),
    !s.enabled && h('p.a-warn', null, T.acqOffNote),
    tabs, Object.values(pane));
  show(TABS.some(t => t[0] === state.acqTab) ? state.acqTab : 'sources');
  if (then && then.openDef) { show('sources'); indexerDialog(null, then.openDef); }
}

// the definition catalogue: the community collection Prowlarr keeps, several hundred sites; a click saves
// one and opens the new indexer's dialog with it
async function catalogDialog(added) {
  const list = h('div.cat-list', { 'aria-live': 'polite' }, aSkeleton());
  const q = h('input', { type: 'search', placeholder: T.catSearchPh, 'aria-label': T.catSearchPh });
  const f = { type: 'all', lang: '' };
  const typeChips = h('div.chips.cat-types', { role: 'group', 'aria-label': T.acqKind });
  const langSel = h('select', { 'aria-label': T.catLang });
  const count = h('p.small.muted.cat-count');
  const m = modal(T.catTitle, h('div.cat', null, h('p.small.muted', null, T.catLede),
    h('div.cat-bar', null, searchField(q, 'cat-search'), langSel), typeChips, count, list), close => [h('button.btn', { onclick: close }, T.close)]);
  m.box.classList.add('wide');
  let all;
  try { all = await GET('/api/admin/acquire/catalog', { quiet: true }); }
  catch (e) { put(list, h('p.bad', null, e.message)); return; }
  const langOf = l => (l || '').split('-')[0].toLowerCase();
  const langs = [...new Set(all.map(e => langOf(e.language)).filter(Boolean))]
    .sort((a, b) => (a === 'hu' ? -1 : b === 'hu' ? 1 : isoName(a, 'language').localeCompare(isoName(b, 'language'))));
  put(langSel, h('option', { value: '' }, T.catAnyLang), langs.map(l => h('option', { value: l }, isoName(l, 'language'))));
  const TYPES = [['all', T.catAll], ['public', T.catPublic], ['private', T.catPrivate]];
  const install = async (e, btn) => {
    btn.disabled = true;
    try { await POST('/api/admin/acquire/catalog/' + encodeURIComponent(e.id), {}); m.close(); toast(T.catAdded(e.name)); added(e.id); }
    finally { btn.disabled = false; }
  };
  const hostOf = u => { try { return new URL(u).host; } catch (_) { return ''; } };
  const row = e => {
    const btn = h('button.btn.small' + (e.installed && !e.outdated ? '' : '.primary'), { onclick: ev => install(e, ev.currentTarget) },
      e.installed ? (e.outdated ? T.catRefresh : T.catAddIndexer) : T.catAdd);
    return h('li.cat-row', null,
      h('div.cat-main', null, h('b', null, e.name),
        h('div.small.muted', null, [e.type === 'public' ? T.catPublicOne : e.type === 'semi-private' ? T.catSemiOne : T.catPrivateOne,
          e.language && isoName(langOf(e.language), 'language'), hostOf((e.links || [])[0])].filter(Boolean).join(' · ')),
        e.description && h('div.small.cat-desc', null, e.description)),
      h('div.cat-side', null, e.installed && h('span.badge.acq-have', null, T.catInstalled), btn));
  };
  const paint = () => {
    put(typeChips, TYPES.map(([k, l]) => h('button.chip' + (f.type === k ? '.on' : ''), { type: 'button', 'aria-pressed': String(f.type === k), onclick: () => { f.type = k; paint(); } }, l)));
    const words = q.value.trim().toLowerCase().split(/\s+/).filter(Boolean);
    const hits = all.filter(e => (f.type === 'all' || (f.type === 'public' ? e.type === 'public' : e.type !== 'public'))
      && (!f.lang || langOf(e.language) === f.lang)
      && words.every(w => (e.name + ' ' + e.id + ' ' + (e.description || '') + ' ' + (e.links || []).join(' ')).toLowerCase().includes(w)));
    const shown = hits.slice(0, 80);
    put(count, T.catCount(hits.length) + (hits.length > shown.length ? ' · ' + T.catNarrow : ''));
    put(list, shown.length ? h('ul.cat-ul', null, shown.map(row)) : h('p.empty', null, T.noResults));
  };
  q.addEventListener('input', debounce(paint, 150));
  langSel.addEventListener('change', () => { f.lang = langSel.value; paint(); });
  paint();
  q.focus();
}

// the quality profile controls, shared by the built-in profile and the named ones
const RES_OPTS = () => [[2160, '2160p (4K)'], [1080, '1080p'], [720, '720p'], [576, '576p'], [480, '480p'], [0, T.acqResUnknown]];
const SRC_OPTS = () => [['remux', 'Remux'], ['bluray', 'BluRay'], ['webdl', 'WEB-DL'], ['webrip', 'WEBRip'], ['hdtv', 'HDTV'], ['', T.profAnySource]];
function qualityEditor(p) {
  const RES = RES_OPTS();
  const primary = h('select', null, RES.filter(([v]) => v).map(([v, l]) => h('option', { value: v, selected: p.resolutions[0] === v }, l)));
  const resBoxes = RES.map(([v, l]) => [v, h('input', { type: 'checkbox', checked: p.resolutions.includes(v) }), l]);
  const minGB = h('input', { type: 'number', min: 0, step: 0.1, value: p.min_size_gb || 0 });
  const maxGB = h('input', { type: 'number', min: 0, step: 1, value: p.max_size_gb || 0 });
  const seeders = h('input', { type: 'number', min: 0, value: p.min_seeders || 0 });
  const swi = on => h('input.switch', { type: 'checkbox', role: 'switch', checked: !!on });
  const hun = swi(p.prefer_hun), hunOnly = swi(p.require_hun), hevc = swi(p.prefer_hevc), upgrade = swi(p.upgrade);
  const cutRes = h('select', null, RES.filter(([v]) => v).map(([v, l]) => h('option', { value: v, selected: (p.cutoff_resolution || 1080) === v }, l)));
  const cutSrc = h('select', null, SRC_OPTS().map(([v, l]) => h('option', { value: v, selected: (p.cutoff_source ?? 'bluray') === v }, l)));
  const sw = (label, el) => h('div.form-row', null, h('label.switch-row', null, h('span', null, label), el));
  const cutoff = h('div.grid2', null, field(T.profCutoffRes, cutRes), field(T.profCutoffSrc, cutSrc));
  const syncUp = () => cutoff.classList.toggle('hidden', !upgrade.checked);
  upgrade.addEventListener('change', syncUp); syncUp();
  const el = h('div', null,
    field(T.acqPrimaryRes, primary),
    h('div.form-row', null, h('span.small.muted', null, T.acqAllowedRes), h('div.acq-resboxes', null, resBoxes.map(([, b, l]) => h('label.check', null, b, l)))),
    h('div.grid2', null, field(T.acqMinGB, minGB), field(T.acqMaxGB, maxGB, T.acqMaxGBHint)),
    field(T.acqMinSeeders, seeders),
    sw(T.acqPreferHun, hun), sw(T.acqRequireHun, hunOnly), sw(T.acqPreferHevc, hevc),
    sw(T.profUpgrade, upgrade), h('div.hint', null, T.profUpgradeHint), cutoff);
  const value = () => {
    const first = +primary.value;
    // the preferred resolution first, then the other ticked ones
    const others = resBoxes.filter(([v, b]) => b.checked && v !== first).map(([v]) => v);
    return Object.assign({}, p, { resolutions: [first, ...others], min_size_gb: +minGB.value || 0, max_size_gb: +maxGB.value || 0,
      min_seeders: +seeders.value || 0, prefer_hun: hun.checked, require_hun: hunOnly.checked, prefer_hevc: hevc.checked,
      upgrade: upgrade.checked, cutoff_resolution: +cutRes.value, cutoff_source: cutSrc.value });
  };
  return { el, value };
}
const profileSummary = p => [p.resolutions.filter(Boolean).map(r => r + 'p').join(', '), p.prefer_hun && 'HUN', p.require_hun && T.profHunOnly,
  p.upgrade && T.profUpTo((p.cutoff_resolution || '') + 'p ' + ((SRC_OPTS().find(x => x[0] === p.cutoff_source) || [, ''])[1]))].filter(Boolean).join(' · ');

// ===================================================================== Felfedezés and requests (acquisition M2)
// Anyone (not from a kids profile) can ask for a film; the server downloads it once approved.
const reqLabel = st => ({ pending: T.reqPending, approved: T.reqApproved, waiting: T.reqWaiting, downloading: T.reqDownloading,
  available: T.reqAvailable, denied: T.reqDenied, failed: T.reqFailed, canceled: T.reqCanceled })[st] || st;
const reqOpen = st => ['pending', 'approved', 'waiting', 'downloading'].includes(st);
// the requester's own button: withdraw one not yet downloading, or clear away one that ended without the film
const reqDrop = (r, done) => ['pending', 'approved', 'waiting', 'failed', 'denied'].includes(r.status) &&
  h('button.btn.danger', { onclick: async () => {
    const closed = r.status === 'failed' || r.status === 'denied';
    await api('DELETE', '/api/requests/' + r.id); toast(closed ? T.reqRemovedToast : T.reqCanceledToast); done && done();
  } }, r.status === 'failed' || r.status === 'denied' ? T.reqRemove : T.reqCancel);

function discoverTile(it, onChange) {
  const open = () => it.kind === 'series' ? seriesDialog(it.id, onChange) : discoverDialog(it.id, onChange);
  return h('button.acq-card' + (it.item_id && it.kind !== 'series' ? '.have' : ''), { type: 'button', 'aria-label': it.title + (it.year ? ' (' + it.year + ')' : ''), onclick: open },
    h('div.acq-poster', null, it.poster ? h('img', { src: it.poster, alt: '', loading: 'lazy' }) : h('span', null, it.title), seenMark(it)),
    h('b', null, it.title), h('span.muted.small', null, it.year || '–', it.rating > 0 ? ' · ★ ' + num(it.rating, 1) : ''),
    it.request && it.request !== 'canceled' ? h('span.badge' + (it.request === 'denied' || it.request === 'failed' ? '.warn' : ''), null, reqLabel(it.request))
      : it.item_id ? h('span.badge.acq-have', null, it.kind === 'series' ? T.discHaveSeries : T.discHave)
      : it.followed ? h('span.badge', null, T.seriesFollowedShort) : null);
}

// the profile has seen this title (kept by TMDB id, so it shows even after the file is gone)
function seenMark(it) {
  const w = it.watched;
  if (!w) return null;
  const series = it.kind === 'series';
  return h('span.disc-seen' + (series ? '.eps' : ''), { title: series ? T.seenEpisodes(w.episodes) : T.seenMovie },
    icon('check'), series ? h('span', null, w.episodes) : null);
}

// the watched panel of a discover dialog: when, and for a series how far in each season
function seenBlock(w, seasons) {
  if (!w) return null;
  const when = w.at > 0 ? T.seenOn(new Date(w.at * 1000).toLocaleDateString(T.locale, { year: 'numeric', month: 'short', day: 'numeric' })) : '';
  let title = T.seenMovie, rows = null;
  if (seasons) {
    const per = w.seasons || {};
    const full = seasons.length > 0 && seasons.every(s => (per[s.number] || 0) >= s.episode_count && s.episode_count > 0);
    title = full ? T.seenAll : T.seenEpisodes(w.episodes);
    const shown = seasons.filter(s => per[s.number] > 0);
    rows = shown.length > 0 && h('div.seen-seasons', null, shown.map(s => {
      const n = Math.min(per[s.number], s.episode_count || per[s.number]), of = s.episode_count || n;
      return h('div.seen-season', null, h('span', null, T.seasonN(s.number)),
        h('span.seen-bar', { role: 'img', 'aria-label': n + ' / ' + of }, h('i', { style: { width: Math.round(n / of * 100) + '%' } })),
        h('span.muted.small', null, n + ' / ' + of));
    }));
  }
  return h('div.seen-box', null,
    h('div.seen-head', null, h('span.seen-ico', null, icon('check')), h('div', null, h('b', null, title), when && h('div.muted.small', null, when))),
    rows);
}

// where a request goes: the requester picks among the films libraries (film) or the series libraries (series)
function libChoice(list, noneText) {
  if (!list.length) return { el: h('p.bad', null, noneText), value: () => 0, ok: false };
  const sel = h('select', null, list.map(l => h('option', { value: l.id }, l.name)));
  return { el: list.length > 1 ? field(T.discLibrary, sel) : null, value: () => +sel.value, ok: true };
}

// where a title streams in the server's region (TMDB, data by JustWatch); a logo opens the Felfedezés filtered to that service
function watchBlock(w, kind, close) {
  if (!w) return null;
  const prov = p => h('a.disc-prov', { href: '#/discover?kind=' + kind + '&p=' + p.id, title: p.name, onclick: () => close() },
    p.logo ? h('img', { src: p.logo, alt: '' }) : null, h('span', null, p.name));
  const other = [...new Set([...w.rent, ...w.buy].map(p => p.name))];
  return h('div.disc-watch', null, h('h3', null, T.discWhere(isoName(w.region, 'region'))),
    w.stream.length ? h('div.disc-provs', null, w.stream.map(prov)) : h('p.muted.small', null, T.discWhereNone),
    other.length > 0 && h('p.muted.small', null, T.discRentBuy + ': ' + other.slice(0, 6).join(', ')),
    h('p.muted.small.disc-jw', null, w.link ? h('a', { href: w.link, target: '_blank', rel: 'noopener noreferrer' }, T.discJustWatch) : T.discJustWatch));
}

async function discoverDialog(id, onChange) {
  const d = await GET('/api/discover/movies/' + id);
  const r = d.request;
  const lc = !r && !d.item_id ? libChoice((await GET('/api/discover/libraries')).movies, T.discNoMovieLib) : null;
  const mine = r && r.user_id === state.me.user.id;
  const facts = [d.year, d.runtime > 0 && fmtRuntime(d.runtime * 60), (d.genres || []).slice(0, 3).join(', ')].filter(Boolean).join(' · ');
  const state_ = d.item_id ? h('p.ok', null, T.discHaveLong)
    : r ? h('p', null, h('span.badge', null, reqLabel(r.status)), ' ', r.user_name && !mine ? h('span.muted.small', null, T.reqBy(r.user_name)) : null,
      r.status === 'downloading' && r.progress > 0 ? h('div.acq-prog', null, aBar(r.progress, 1), h('span.small.muted', null, num(r.progress * 100, 0) + '%')) : null,
      r.note ? h('div.muted.small', null, srv(r.note)) : null)
    : null;
  let shut = () => {};
  const drop = mine && reqDrop(r, () => { shut(); onChange && onChange(); });
  const scores = h('div.scores-slot.compact', { hidden: true }), box = h('div.box-slot', { hidden: true });
  fillScores(`/api/discover/movies/${d.id}/scores`, scores, box, true);
  modal(d.title + (d.year ? ' (' + d.year + ')' : ''), h('div.disc-detail', null,
    d.poster && h('img.acq-mposter', { src: d.poster, alt: '' }),
    h('div.acq-mbody', null, d.original_title && d.original_title !== d.title && h('p.muted.small', null, d.original_title),
      facts && h('p.muted', null, facts), scores, seenBlock(d.watched), d.overview && h('p.acq-overview', null, d.overview), box, state_, lc && lc.el,
      watchBlock(d.watch, 'movie', () => shut()))),
  close => (shut = close, [
    h('button.btn', { onclick: close }, T.close),
    d.item_id ? h('a.btn.primary', { href: '#/item/' + d.item_id, onclick: close }, icon('play'), T.discWatch)
      : drop ? drop
      : !r ? h('button.btn.primary', { disabled: !lc.ok, onclick: async e => {
        const b = e.currentTarget; // null once the handler awaits
        b.disabled = true;
        try {
          const q = await POST('/api/requests', { tmdb_id: d.id, library_id: lc.value() });
          close(); toast(q.status === 'pending' ? T.reqSentPending(d.title) : T.reqSentApproved(d.title)); onChange && onChange();
        } finally { b.disabled = false; }
      } }, icon('plus'), T.discAsk) : null]));
}

// Felfedezés: search, or browse TMDB by streaming service, genre, year, rating and language; with no
// filter it shows curated shelves and one shelf per leading streaming service of the region.
const DISC_YEARS = () => {
  const y = new Date().getFullYear();
  return [['', T.discAnyTime, 0, 0], ['this', T.discThisYear, y, y], ['recent', T.discRecent, y - 2, 0], ...[2020, 2010, 2000, 1990, 1980].map(d => [d + 's', d + '–' + (d + 9), d, d + 9]), ['old', T.discOlder, 0, 1979]];
};
// the services the Felfedezés offers as filters, by their TMDB names (ads tiers and channels left out)
const BIG_PROVIDERS = [[/^netflix$/i, 'Netflix'], [/^(hbo )?max$/i, 'HBO Max'], [/^disney\s*(\+|plus)$/i, 'Disney+'], [/^amazon prime video$|^prime video$/i, 'Prime Video']];
const DISC_LANGS = ['hu', 'en', 'fr', 'de', 'es', 'it', 'da', 'sv', 'ko', 'ja'];
const isoName = (code, type) => { try { return new Intl.DisplayNames([T.locale], { type }).of(code); } catch { return code.toUpperCase(); } };

async function viewDiscover(q) {
  if (!state.me.can_request) return go('#/home');
  const main = shell('discover');
  const f = { q: q.get('q') || '', kind: q.get('kind') === 'series' ? 'series' : 'movie', p: new Set((q.get('p') || '').split(',').filter(Boolean)),
    g: q.get('g') || '', sort: q.get('sort') || '', y: q.get('y') || '', r: q.get('r') || '', lang: q.get('lang') || '', fresh: q.get('new') === '1' };
  const filtered = () => f.p.size > 0 || f.g || f.sort || f.y || f.r || f.lang;
  const nFilters = () => f.p.size + ['g', 'sort', 'y', 'r', 'lang'].filter(k => f[k]).length;
  const url = () => {
    const p = new URLSearchParams();
    if (f.q) p.set('q', f.q);
    if (f.kind === 'series') p.set('kind', 'series');
    if (f.p.size) p.set('p', [...f.p].join(','));
    ['g', 'sort', 'y', 'r', 'lang'].forEach(k => f[k] && p.set(k, f[k]));
    if (f.fresh) p.set('new', '1');
    const s = String(p);
    return '#/discover' + (s ? '?' + s : '');
  };
  const opts = await GET('/api/discover/options', { quiet: true }).catch(() => ({ providers: [], movie_genres: [], series_genres: [], region: '' }));
  // only the big four; a service picked earlier (a link from a title's "where to watch") stays on the bar
  const bigProvs = BIG_PROVIDERS.map(([re]) => opts.providers.find(p => re.test(p.name))).filter(Boolean);
  for (const id of f.p) { const p = opts.providers.find(x => String(x.id) === id); if (p && !bigProvs.includes(p)) bigProvs.push(p); }

  // ---- the bar: search, films / series, sort; then the streaming services; then the rest
  const input = h('input', { type: 'search', placeholder: T.discSearchPh, 'aria-label': T.discSearchPh, value: f.q });
  const kindTabs = h('div.seg.disc-kind', { role: 'tablist', 'aria-label': T.discKind });
  const sortSel = h('select', { 'aria-label': T.sort });
  const sortCtl = h('label.disc-sort', null, h('span', null, T.discSortBy), sortSel);
  const genreSel = h('select', { 'aria-label': T.genres });
  const yearSel = h('select', { 'aria-label': T.year }, DISC_YEARS().map(([v, l]) => h('option', { value: v, selected: f.y === v }, l)));
  const rateSel = h('select', { 'aria-label': T.minRating }, [['', T.discAnyRating], ['6', '★ 6+'], ['7', '★ 7+'], ['8', '★ 8+']].map(([v, l]) => h('option', { value: v, selected: f.r === v }, l)));
  const langSel = h('select', { 'aria-label': T.discLang }, h('option', { value: '' }, T.discAnyLang), DISC_LANGS.map(c => h('option', { value: c, selected: f.lang === c }, isoName(c, 'language'))));
  const fresh = h('button.fchip', { type: 'button', 'aria-pressed': String(f.fresh) });
  const clearBtn = h('button.disc-clear', { type: 'button' }, icon('x'), T.clear);
  const provBox = h('div.menu-pop.disc-spop', { role: 'group', 'aria-label': T.discServices, hidden: true });
  const provBtn = h('button.fchip.disc-sbtn', { type: 'button', 'aria-haspopup': 'true', 'aria-expanded': 'false' });
  const provClose = e => { if (!provMenu.contains(e.target)) setProvOpen(false); };
  const setProvOpen = open => {
    provBox.hidden = !open; provBtn.setAttribute('aria-expanded', String(open));
    document[open ? 'addEventListener' : 'removeEventListener']('mousedown', provClose);
  };
  provBtn.addEventListener('click', () => setProvOpen(provBox.hidden));
  provBox.addEventListener('keydown', e => { if (e.key === 'Escape') { setProvOpen(false); provBtn.focus(); } });
  onCleanup(() => document.removeEventListener('mousedown', provClose));
  const provMenu = h('div.menu.disc-stream', null, provBtn, provBox);
  const moreFilters = h('div.disc-filters');
  const filtersBtn = h('button.btn.filters-toggle', { type: 'button', 'aria-expanded': 'false', onclick: () => filtersBtn.setAttribute('aria-expanded', String(moreFilters.classList.toggle('open'))) });
  const body = h('div', { 'aria-live': 'polite' });
  const sentinel = h('div.disc-more');

  const paintKind = () => put(kindTabs, [['movie', T.discMovies], ['series', T.discSeries]].map(([k, l]) =>
    h('button' + (f.kind === k ? '.on' : ''), { type: 'button', role: 'tab', 'aria-selected': String(f.kind === k), onclick: () => { if (f.kind !== k) { f.kind = k; f.g = ''; changed(); } } }, l)));
  const paintSelects = () => {
    put(sortSel, [['', T.discSortPopular], ['new', T.discSortNew], ['top', T.discSortTop]].map(([v, l]) => h('option', { value: v, selected: f.sort === v }, l)));
    const gs = f.kind === 'series' ? opts.series_genres : opts.movie_genres;
    put(genreSel, h('option', { value: '' }, T.discAnyGenre), gs.map(g => h('option', { value: g.id, selected: f.g === String(g.id) }, g.name)));
    genreSel.style.display = gs.length ? '' : 'none';
  };
  const provName = p => BIG_PROVIDERS.find(([re]) => re.test(p.name))?.[1] || p.name;
  const paintProv = () => {
    provMenu.style.display = bigProvs.length ? '' : 'none';
    const sel = bigProvs.filter(p => f.p.has(String(p.id)));
    provBtn.classList.toggle('on', sel.length > 0);
    put(provBtn, sel.length === 1 && sel[0].logo ? h('img', { src: sel[0].logo, alt: '' }) : null,
      sel.length === 0 ? T.discServices : sel.length === 1 ? provName(sel[0]) : T.discServices + ' · ' + sel.length, icon('chevDown'));
    put(provBox, bigProvs.map(p => {
      const on = f.p.has(String(p.id));
      return h('label.disc-sitem', null, h('input', { type: 'checkbox', checked: on, onchange: () => {
        on ? f.p.delete(String(p.id)) : f.p.add(String(p.id)); changed();
      } }), p.logo ? h('img', { src: p.logo, alt: '', loading: 'lazy' }) : h('span.disc-snologo'), h('span', null, provName(p)));
    }), opts.region && h('p.disc-region', null, T.discRegion(isoName(opts.region, 'region'))));
  };
  const paintBar = () => {
    paintKind(); paintSelects(); paintProv(); clearBtn.style.display = filtered() || f.fresh ? '' : 'none';
    [genreSel, yearSel, rateSel, langSel].forEach(s => s.classList.toggle('on', !!s.value));
    fresh.setAttribute('aria-pressed', String(f.fresh)); fresh.classList.toggle('on', f.fresh);
    put(fresh, f.fresh ? icon('check') : null, T.discOnlyNew);
    const n = nFilters() + (f.fresh ? 1 : 0);
    put(filtersBtn, icon('filter'), T.filters, n ? h('span.n', null, '· ' + n) : null);
  };

  sortSel.addEventListener('change', () => { f.sort = sortSel.value; changed(); });
  genreSel.addEventListener('change', () => { f.g = genreSel.value; changed(); });
  yearSel.addEventListener('change', () => { f.y = yearSel.value; changed(); });
  rateSel.addEventListener('change', () => { f.r = rateSel.value; changed(); });
  langSel.addEventListener('change', () => { f.lang = langSel.value; changed(); });
  fresh.addEventListener('click', () => { f.fresh = !f.fresh; changed(); });
  clearBtn.addEventListener('click', () => {
    f.p.clear(); f.g = f.sort = f.y = f.r = f.lang = ''; f.fresh = false;
    yearSel.value = rateSel.value = langSel.value = '';
    changed();
  });
  input.addEventListener('input', debounce(() => { f.q = input.value.trim(); changed(); }, 350));

  main.append(h('div.page-head', null, h('h1', null, T.discover), h('a.btn', { href: '#/requests' }, T.myRequests)),
    h('p.muted.disc-lede', null, T.discLede),
    h('div.disc-bar', null,
      searchField(input, 'disc-search'),
      h('div.disc-view', null, kindTabs, h('div.disc-view-r', null, filtersBtn, sortCtl)),
      add(moreFilters, [provMenu, genreSel, yearSel, rateSel, langSel, fresh, clearBtn])),
    body, sentinel);

  // ---- results
  let gen = 0, page = 0, pages = 0, loading = false;
  const keep = it => !f.fresh || (!it.item_id && !(it.request && it.request !== 'canceled' && it.request !== 'denied'));
  const reload = () => changed(true);
  const tiles = items => items.filter(keep).map(it => discoverTile(it, reload));
  const grid = items => h('div.acq-results', null, tiles(items));
  const browseURL = (extra = {}) => {
    const p = new URLSearchParams({ kind: f.kind });
    const prov = extra.p != null ? String(extra.p) : [...f.p].join(',');
    if (prov) p.set('providers', prov);
    const sort = extra.sort != null ? extra.sort : f.sort;
    if (sort) p.set('sort', sort);
    if (f.g) p.set('genres', f.g);
    const yr = DISC_YEARS().find(([v]) => v === f.y);
    if (yr && yr[2]) p.set('from', yr[2]);
    if (yr && yr[3]) p.set('to', yr[3]);
    if (f.r) p.set('rating', f.r);
    if (f.lang) p.set('lang', f.lang);
    p.set('page', extra.page || 1);
    return '/api/discover/browse?' + p;
  };
  const shelfOf = (title, items, more, logo) => items.filter(keep).length > 0 && h('section.shelf.disc-shelf', { 'aria-label': title },
    h('div.shelf-head', null, h('h2', null, logo && h('img.disc-shelflogo', { src: logo, alt: '' }), title), more && h('a', { href: more }, T.more)),
    h('div.shelf-track.poster', null, tiles(items)));

  async function showcase(my) {
    const lists = f.kind === 'series'
      ? [['tv_trending', T.discTvTrending], ['tv_airing', T.discTvAiring], ['tv_popular', T.discTvPopular]]
      : [['trending', T.discTrending], ['upcoming', T.discUpcoming], ['popular', T.discPopular], ['top', T.discTop]];
    const provs = bigProvs.slice(0, 4);
    const [res, byProv] = await Promise.all([
      Promise.all(lists.map(([l]) => GET('/api/discover?list=' + l, { quiet: true }).catch(() => []))),
      Promise.all(provs.map(p => GET(browseURL({ p: p.id }), { quiet: true }).then(r => r.results).catch(() => [])))]);
    if (my !== gen) return;
    const kindQ = f.kind === 'series' ? 'kind=series&' : '';
    const shelves = [];
    lists.forEach(([, t], i) => shelves.push(shelfOf(t, res[i])));
    // the services go between the first curated shelf and the rest
    const provShelves = provs.map((p, i) => shelfOf(T.discOnService(p.name), byProv[i], '#/discover?' + kindQ + 'p=' + p.id, p.logo));
    put(body, shelves[0], provShelves, shelves.slice(1));
    if (!body.querySelector('.shelf')) put(body, h('p.empty', null, T.discNothing));
  }

  async function more() {
    if (loading || page >= pages) return;
    loading = true;
    const my = gen;
    try {
      const r = await GET(browseURL({ page: page + 1 }), { quiet: true });
      if (my !== gen) return;
      page = r.page || page + 1; pages = r.total_pages;
      body.querySelector('.acq-results').append(...tiles(r.results));
    } catch { pages = 0; } finally { loading = false; rearm(); }
  }

  async function load() {
    const my = ++gen;
    page = pages = 0;
    put(body, aSkeleton());
    try {
      if (f.q.length >= 2) {
        const [mv, tv] = await Promise.all(['movie', 'series'].map(k => GET('/api/discover/search?kind=' + k + '&q=' + encodeURIComponent(f.q), { quiet: true }).catch(() => [])));
        if (my !== gen) return;
        const secs = (f.kind === 'series' ? [[T.discSeries, tv], [T.discMovies, mv]] : [[T.discMovies, mv], [T.discSeries, tv]])
          .map(([t, l]) => [t, l.filter(keep)]).filter(([, l]) => l.length);
        put(body, secs.map(([t, l]) => h('section.disc-sec', null, h('h2', null, t), grid(l.slice(0, 24)))));
        if (!body.firstChild) put(body, h('p.empty', null, T.searchEmpty));
        return;
      }
      if (!filtered()) return await showcase(my);
      const r = await GET(browseURL(), { quiet: true });
      if (my !== gen) return;
      page = r.page || 1; pages = r.total_pages;
      put(body, h('p.muted.disc-count', null, T.titlesN(r.total)), grid(r.results));
      if (!r.total) put(body, emptyNote(T.noResults, f.p.size ? T.discNoResultsHint : null, h('button.btn', { onclick: () => clearBtn.click() }, T.clear)));
      rearm();
    } catch (e) { if (my === gen) put(body, h('p.bad', null, e.message)); }
  }

  function changed(keepURL) {
    if (!keepURL) history.replaceState(history.state, '', url());
    paintBar();
    load();
  }

  const io = new IntersectionObserver(es => { if (es.some(e => e.isIntersecting) && page > 0) more(); }, { rootMargin: '800px' });
  // observing again reports the current state, so a sentinel still in view (a short page, or one emptied by "only new") loads on
  const rearm = () => { io.unobserve(sentinel); io.observe(sentinel); };
  io.observe(sentinel);
  onCleanup(() => io.disconnect());
  paintBar();
  await load();
}

async function viewRequests() {
  if (!state.me.can_request) return go('#/home');
  const main = shell('requests');
  main.append(h('div.page-head', null, h('h1', null, T.myRequests), h('a.btn', { href: '#/discover' }, T.discover)), h('p.muted.disc-lede', null, T.reqLede));
  const box = h('div');
  main.append(box);
  let t;
  onCleanup(() => clearTimeout(t));
  const render = async () => {
    const list = (await GET('/api/requests')).filter(r => r.status !== 'canceled');
    put(box, list.length ? h('div.req-list', null, list.map(r => h('div.entry.req', null,
      r.poster ? h('img.match-thumb', { src: r.poster, alt: '' }) : null,
      h('div', null, h('b', null, r.title, r.year ? ' (' + r.year + ')' : ''),
        h('div', null, h('span.badge' + (r.status === 'denied' || r.status === 'failed' ? '.warn' : r.status === 'available' ? '.acq-have' : ''), null, reqLabel(r.status)),
          r.kind === 'series' && h('span.muted.small', null, ' · ' + T.discSeriesOne + ': ' + seasonsLabel(r.seasons)),
          h('span.muted.small', null, ' · ' + ago(Date.now() / 1000 - r.created_at))),
        r.status === 'downloading' && h('div.acq-prog', null, aBar(r.progress, 1), h('span.small.muted', null, num(r.progress * 100, 0) + '%')),
        r.note && h('div.muted.small', null, srv(r.note))),
      h('div.row-gap', null,
        r.status === 'available' && h('button.btn', { onclick: () => (r.kind === 'series' ? seriesDialog : discoverDialog)(r.tmdb_id, render) }, T.discWatch),
        reqDrop(r, render)))))
      : emptyNote(T.reqNone, null, h('a.btn.primary', { href: '#/discover' }, T.discover)));
    clearTimeout(t);
    if (list.some(r => reqOpen(r.status))) t = setTimeout(render, 10000);
  };
  await render();
}

// told once: requested films that arrived since the last visit
async function announceArrivals() {
  if (!state.me || !state.me.can_request) return;
  const list = await GET('/api/requests/unseen', { quiet: true, noRedirect: true }).catch(() => []);
  for (const r of list.slice(0, 3)) toast(T.reqArrived(r.title));
}

// which tab of the admin's request list a state belongs to (canceled ones show only under "all")
const REQ_TAB = { pending: 'pending', approved: 'active', waiting: 'active', downloading: 'active', available: 'done', failed: 'problem', denied: 'problem' };

async function adminAcqRequests(body) {
  let t, dead = false, tab = 'pending', picked = false, list;
  onCleanup(() => { dead = true; clearTimeout(t); });
  const set = await GET('/api/admin/acquire/settings');
  const load = async () => {
    const got = await GET('/api/admin/acquire/requests', { quiet: !!list }).catch(e => { if (!list) throw e; return null; });
    if (dead) return;
    if (got) { list = got; paint(); }
    clearTimeout(t);
    t = setTimeout(load, list.some(r => r.status === 'approved' || r.status === 'downloading') ? 5000 : 30000);
  };
  const decide = async (r, approve) => {
    if (approve) { await POST(`/api/admin/acquire/requests/${r.id}/approve`, {}); toast(T.reqApprovedToast(r.title)); return load(); }
    const note = h('input', { type: 'text', placeholder: T.reqDenyNotePh });
    modal(T.reqDenyAsk(r.title), field(T.reqDenyNote, note), close => [h('button.btn', { onclick: close }, T.cancel),
      h('button.btn.danger', { onclick: async () => { await POST(`/api/admin/acquire/requests/${r.id}/deny`, { note: note.value }); close(); load(); } }, T.reqDeny)]);
  };
  const remove = async r => {
    if (!await confirmBox(T.reqRemoveAsk(r.title), T.reqRemove)) return;
    await api('DELETE', '/api/requests/' + r.id); load();
  };
  const canSearch = r => r.kind === 'movie' && ['pending', 'approved', 'waiting', 'failed'].includes(r.status) && set.libraries.length > 0;
  const fullTitle = r => r.title + (r.year ? ' (' + r.year + ')' : '');
  // an arrived film can be searched again for another version: that download is not tied to the request,
  // so the request stays „Megérkezett” and the new file joins the film as a second version
  const canSearchMore = r => r.kind === 'movie' && r.status === 'available' && set.libraries.length > 0;
  const acts = r => {
    const more = [
      canSearch(r) && [T.isInteractive, () => movieInteractive(r.tmdb_id, fullTitle(r), r.library_id || 0, r.id)],
      ['waiting', 'failed', 'denied', 'canceled'].includes(r.status) && [T.reqRemove, () => remove(r)],
    ].filter(Boolean);
    return h('div.q-side', null,
      r.status === 'pending' && h('button.btn.small.primary', { onclick: () => decide(r, true) }, T.reqApprove),
      r.status === 'pending' && h('button.btn.small', { onclick: () => decide(r, false) }, T.reqDeny),
      ['waiting', 'failed', 'denied'].includes(r.status) && h('button.btn.small', { onclick: () => decide(r, true) }, T.retry),
      r.status === 'downloading' && h('a.btn.small.ghost', { href: '#/admin/acqqueue' }, T.aAcqQueue),
      canSearchMore(r) && h('button.btn.small.ghost', { title: T.isInteractive, onclick: () => movieInteractive(r.tmdb_id, fullTitle(r), r.library_id || 0, 0) }, icon('search'), T.isInteractiveShort),
      more.length > 0 && h('button.btn.small.ghost.icon-only', { 'aria-label': T.moreActions, title: T.moreActions, 'aria-haspopup': 'menu', onclick: e => popMenu(e.currentTarget, more) }, icon('more', true)));
  };
  const row = r => {
    const bad = r.status === 'failed' || r.status === 'denied';
    const meta = [r.kind === 'series' ? T.discSeriesOne + ': ' + seasonsLabel(r.seasons) : '', r.library_name, T.reqBy(r.user_name || '–'), ago(Date.now() / 1000 - r.created_at)].filter(Boolean);
    return h('li.q-row.rq-row', null, h('div.rq-main', null,
      h('div.rq-thumb', null, r.poster ? h('img', { src: r.poster, alt: '', loading: 'lazy' }) : icon(r.kind === 'series' ? 'tv' : 'film')),
      h('div.q-title', null, h('b', { title: fullTitle(r) }, r.title, r.year ? h('span.muted', null, ' (' + r.year + ')') : null),
        h('div.small.muted.rq-meta', null, meta.join(' · ')),
        r.note && h('div.small.rq-note' + (bad ? '.bad' : '.muted'), { title: srv(r.note) }, srv(r.note))),
      h('div.q-state', null, h('span.badge' + (bad ? '.warn' : r.status === 'available' ? '.acq-have' : r.status === 'pending' ? '.amber' : ''), null, reqLabel(r.status)),
        r.status === 'downloading' && h('div.q-prog', null, aBar(r.progress, 1), h('span.small.muted', null, num(r.progress * 100, 0) + '%'))),
      acts(r)));
  };
  const paint = () => {
    const count = { pending: 0, active: 0, done: 0, problem: 0, all: list.length };
    for (const r of list) if (REQ_TAB[r.status]) count[REQ_TAB[r.status]]++;
    if (!picked) tab = count.pending ? 'pending' : count.active ? 'active' : count.problem ? 'problem' : 'all';
    const shown = list.filter(r => tab === 'all' || REQ_TAB[r.status] === tab);
    const tabs = [['pending', T.reqTabPending], ['active', T.qActive], ['problem', T.reqTabProblem], ['done', T.reqTabDone], ['all', T.qAll]];
    put(body, aHead(T.aAcqRequests, T.aLedeAcqRequests, h('a.btn', { href: '#/admin/acqqueue' }, T.aAcqQueue)),
      list.length ? [
        h('div.chips.q-tabs', { role: 'tablist' }, tabs.map(([k, l]) => h('button.chip' + (tab === k ? '.on' : ''), { type: 'button', role: 'tab', 'aria-selected': String(tab === k), onclick: () => { tab = k; picked = true; paint(); } },
          l, h('span.n', null, ' ' + count[k])))),
        shown.length ? h('ul.q-list', null, shown.map(row)) : h('p.empty', null, T.qNone)]
        : emptyNote(T.reqNoneAll, T.reqNoneHint, h('a.btn', { href: '#/discover' }, T.discover)));
  };
  await load();
}

// ===================================================================== series (acquisition M3)
const seasonsLabel = s => s === 'all' || !s ? T.monAll : s === 'future' ? T.monFuture : T.seasonsList(s.split(',').join(', '));

// the "which seasons" control shared by the request dialog and the admin's follow dialog
function seasonPicker(seasons, initialMode = 'all', initialSet = []) {
  const mode = h('select', { 'aria-label': T.monWhat }, [['all', T.monAll], ['future', T.monFuture], ['seasons', T.monSeasons]].map(([v, l]) => h('option', { value: v, selected: v === initialMode }, l)));
  const boxes = seasons.map(s => [s.number, h('input', { type: 'checkbox', checked: initialSet.includes(s.number) })]);
  const list = h('div.season-picks', null, seasons.map((s, i) => h('label.check', null, boxes[i][1],
    h('span', null, T.seasonN(s.number), h('span.muted.small', null, ' · ' + T.epCount(s.episode_count) + (s.have ? ' · ' + T.seasonHave(s.have) : ''))))));
  const sync = () => list.classList.toggle('hidden', mode.value !== 'seasons');
  mode.addEventListener('change', sync); sync();
  return { el: h('div', null, field(T.monWhat, mode), list), mode: () => mode.value, chosen: () => boxes.filter(([, b]) => b.checked).map(([n]) => n) };
}

async function seriesDialog(id, onChange) {
  const d = await GET('/api/discover/series/' + id);
  const r = d.request;
  const lc = !r ? libChoice((await GET('/api/discover/libraries')).shows, T.discNoShowLib) : null;
  const mine = r && r.user_id === state.me.user.id;
  const facts = [d.year, T.seasonsCount(d.seasons.length), (d.genres || []).slice(0, 3).join(', '), d.ended ? T.seriesEnded : null].filter(Boolean).join(' · ');
  const picker = seasonPicker(d.seasons);
  const status = r ? h('p', null, h('span.badge', null, reqLabel(r.status)), ' ', h('span.muted.small', null, seasonsLabel(r.seasons)),
      r.user_name && !mine ? h('span.muted.small', null, ' · ' + T.reqBy(r.user_name)) : null, r.note ? h('div.muted.small', null, srv(r.note)) : null)
    : d.followed ? h('p.ok', null, T.seriesFollowed) : null;
  let shut = () => {};
  const scores = h('div.scores-slot.compact', { hidden: true });
  fillScores(`/api/discover/series/${d.id}/scores`, scores, null, true);
  modal(d.title + (d.year ? ' (' + d.year + ')' : ''), h('div.disc-detail', null,
    d.poster && h('img.acq-mposter', { src: d.poster, alt: '' }),
    h('div.acq-mbody', null, d.original_title && d.original_title !== d.title && h('p.muted.small', null, d.original_title),
      h('p.muted', null, facts), scores, seenBlock(d.watched, d.seasons), d.overview && h('p.acq-overview', null, d.overview), status,
      watchBlock(d.watch, 'series', () => shut()),
      !r && d.seasons.length > 0 && picker.el, lc && lc.el)),
  close => (shut = close, [
    h('button.btn', { onclick: close }, T.close),
    d.item_id ? h('a.btn', { href: '#/item/' + d.item_id, onclick: close }, icon('play'), T.discWatch) : null,
    r && mine ? reqDrop(r, () => { close(); onChange && onChange(); }) || null : null,
    !r && h('button.btn.primary', { disabled: !lc.ok, onclick: async e => {
      const b = e.currentTarget;
      const m = picker.mode(), chosen = picker.chosen();
      if (m === 'seasons' && !chosen.length) return toast(T.pickSeason, true);
      b.disabled = true;
      try {
        const q = await POST('/api/requests', { kind: 'series', tmdb_id: d.id, seasons: m === 'seasons' ? chosen.join(',') : m, library_id: lc.value() });
        close(); toast(q.status === 'pending' ? T.reqSentPending(d.title) : T.reqSentApproved(d.title)); onChange && onChange();
      } finally { b.disabled = false; }
    } }, icon('plus'), T.discAsk)]));
}


// ===================================================================== interactive search (like Radarr / Sonarr)
// Every release from every indexer in one sortable table; any of them can be downloaded – a rejected one after a
// warning that says why it was rejected. load() → { results, errors }, grab(release) starts the download.
function interactiveSearch({ title, load, grab }) {
  let sortKey = 'score', dir = -1, hideRej = false, data = null;
  const body = h('div.isearch', null, h('div.acq-loading', null, h('div.spin'), h('p.muted', null, T.acqSearching)));
  const m = modal(T.isTitle(title), body, close => [h('button.btn', { onclick: close }, T.close)]);
  m.box.classList.add('wide', 'is-modal');
  const age = r => { const t = Date.parse(r.published); return t > 0 && t < Date.now() + 864e5 ? (Date.now() - t) / 1000 : null; };
  const qrank = r => (r.release.resolution || 0) * 100 + ({ remux: 60, bluray: 50, webdl: 45, webrip: 35, hdtv: 15, dvd: 5 }[r.release.source] || 0);
  const val = {
    age: r => { const a = age(r); return a == null ? Infinity : a; },
    title: r => r.title.toLowerCase(),
    indexer: r => (r.indexer || '').toLowerCase(),
    size: r => r.size || 0,
    peers: r => r.seeders,
    quality: qrank,
    score: r => (r.rejected ? -1e6 : 0) + r.score,
  };
  const cols = [['age', T.isAge], ['title', T.acqRelease], ['indexer', T.acqIndexer], ['size', T.acqSize], ['peers', T.isPeers], ['quality', T.isQuality], ['score', T.acqScore]];
  const sent = new Set();
  function render() {
    const rows = data.results.filter(r => !hideRej || !r.rejected).slice().sort((a, b) => {
      const x = val[sortKey](a), y = val[sortKey](b);
      return (x < y ? -1 : x > y ? 1 : 0) * dir;
    });
    const nRej = data.results.filter(r => r.rejected).length;
    const head = h('tr', null, cols.map(([k, l]) => h('th', null, h('button.is-sort' + (k === sortKey ? '.on' : ''), { type: 'button', 'aria-sort': k === sortKey ? (dir > 0 ? 'ascending' : 'descending') : null,
      onclick: () => { if (sortKey === k) dir = -dir; else { sortKey = k; dir = k === 'title' || k === 'indexer' || k === 'age' ? 1 : -1; } render(); } },
      l, k === sortKey ? (dir > 0 ? ' ▲' : ' ▼') : ''))), h('th'));
    const row = r => {
      const key = r.info_hash || r.guid || r.title;
      const btn = h('button.btn' + (r.rejected ? '' : '.primary'), { disabled: sent.has(key), 'aria-label': T.acqGrab + ': ' + r.title, onclick: async e => {
        const b = e.currentTarget;
        if (r.rejected && !await confirmBox(T.isGrabRejected(acqReject(r.rejected)), T.acqGrab)) return;
        b.disabled = true;
        try { await grab(r); sent.add(key); toast(T.isSent(r.title)); render(); } catch { b.disabled = false; }
      } }, sent.has(key) ? icon('check') : icon('dl'));
      const a = age(r);
      return h('tr' + (r.rejected ? '.acq-rej' : ''), null,
        h('td.is-age', null, a == null ? '–' : ago(a)),
        h('td.acq-rel', null, h('div.acq-reltitle', { title: r.title }, r.title),
          h('div.acq-tags', null, r.release.season >= 0 && h('span.badge', null, r.release.episodes && r.release.episodes.length ? r.release.episodes.map(e => `S${String(r.release.season).padStart(2, '0')}E${String(e).padStart(2, '0')}`).join(' ') : T.isPack(r.release.season)),
            r.release.codec && h('span.badge', null, r.release.codec.toUpperCase()), r.release.hdr && h('span.badge', null, r.release.hdr.toUpperCase()),
            (r.release.languages || []).includes('hun') && h('span.badge.acq-hun', null, 'HUN'),
            r.info_url && /^https?:/.test(r.info_url) && h('a.small', { href: r.info_url, target: '_blank', rel: 'noopener noreferrer' }, T.acqInfo)),
          r.rejected && h('div.bad.small', null, acqReject(r.rejected))),
        h('td', null, r.indexer),
        h('td.is-num', null, r.size ? fmtBytes(r.size) : '–'),
        h('td.is-num', null, r.seeders >= 0 ? num(r.seeders, 0) + ' / ' + num(Math.max(r.peers, 0), 0) : '–'),
        h('td', null, r.quality || '–'),
        h('td.is-num', null, r.rejected ? '–' : num(r.score, 0)),
        h('td.a-tbl-acts', null, btn));
    };
    put(body,
      h('div.is-bar', null, h('span.muted.small', null, T.acqFound(data.results.length, data.results.length - nRej)),
        nRej > 0 && h('label.check.small', null, h('input', { type: 'checkbox', checked: hideRej, onchange: e => { hideRej = e.target.checked; render(); } }), T.isHideRejected)),
      (data.errors || []).length > 0 && h('div.acq-errs', { role: 'alert' }, data.errors.map(e => h('p.bad.small', null, e.name + ': ' + srv(e.error)))),
      rows.length ? h('div.scroll-x', null, h('table.tbl.is-tbl', null, h('thead', null, head), h('tbody', null, rows.map(row)))) : h('p.empty', null, T.acqNoReleases));
  }
  load().then(d => { data = { results: d.results || [], errors: d.errors || [] }; render(); }, e => put(body, h('p.bad', null, e.message)));
}

function movieInteractive(tmdbId, title, libraryId, requestId, profileId = 0) {
  interactiveSearch({
    title,
    load: () => GET('/api/admin/acquire/releases?tmdb_id=' + tmdbId + '&profile_id=' + profileId, { quiet: true }),
    grab: rel => POST('/api/admin/acquire/grab', { tmdb_id: tmdbId, library_id: libraryId, release: rel, request_id: requestId || 0, profile_id: profileId }),
  });
}

function acqStatsPanel(st) {
  const d = st.downloads, r = st.requests;
  const tile = (n, l) => h('div.acq-stat', null, h('b', null, n), h('span.muted.small', null, l));
  const rate = d.done + d.failed > 0 ? Math.round(d.done / (d.done + d.failed) * 100) + '%' : '–';
  return h('section.acq-stats', { 'aria-label': T.statTitle },
    h('div.acq-stat-row', null, tile(num(d.done, 0), T.statDone), tile(fmtBytes(d.bytes), T.statBytes), tile(rate, T.statSuccess),
      tile(num(d.active, 0), T.statActive), tile(num(r.open, 0), T.statOpenReq), tile(num(st.series, 0), T.statFollowed)),
    (st.requesters.length > 0 || st.indexers.length > 0) && h('div.acq-stat-lists', null,
      st.requesters.length > 0 && h('div', null, h('h3.small.muted', null, T.statRequesters), h('ol.small', null, st.requesters.map(x => h('li', null, x.name + ' · ' + T.statReqCount(x.count, x.done))))),
      st.indexers.length > 0 && h('div', null, h('h3.small.muted', null, T.statIndexers), h('ol.small', null, st.indexers.map(x => h('li', null, x.name + ' · ' + T.statIdxCount(x.count, x.done)))))));
}
