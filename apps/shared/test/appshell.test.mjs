// The server's web/static/appshell.js inside the Android app: the bridge to window.KinoraAndroid
// (the app's link, haptics, sharing, the mediaSession → media notification shim, Chromecast).
// It runs in a bare VM context with a stand-in for the app's Java interface.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';

const file = join(dirname(fileURLToPath(import.meta.url)), '..', '..', '..', 'web', 'static', 'appshell.js');
const code = readFileSync(file, 'utf8');

function run(native, href = 'http://192.168.1.10:8096/#/home') {
  const calls = [];
  const rec = (name, ret) => (...a) => { calls.push([name, ...a]); return typeof ret === 'function' ? ret(...a) : ret; };
  const nat = native && Object.fromEntries(Object.entries(native).map(([k, v]) => [k, rec(k, v)]));
  const loc = new URL(href);
  const location = { protocol: loc.protocol, origin: loc.origin, pathname: loc.pathname, search: loc.search, hash: loc.hash, href };
  const classes = new Set();
  const video = { videoWidth: 1920, videoHeight: 800, play: () => Promise.resolve(), pause() { calls.push(['video.pause']); } };
  const timers = [];
  const window = {
    KinoraAndroid: nat, location,
    addEventListener() {}, dispatchEvent(e) { calls.push(['event', e.type, e.detail]); },
  };
  const ctx = {
    window, location, navigator: {}, URL, URLSearchParams, JSON, Object, Array, Promise, Date,
    CustomEvent: class { constructor(type, o) { this.type = type; this.detail = o && o.detail; } },
    localStorage: { getItem: () => null, setItem() {}, removeItem() {} },
    history: { state: null, replaceState() {}, length: 1 },
    document: { documentElement: { classList: { add: (...c) => c.forEach(x => classes.add(x)), toggle: (c, on) => (on ? classes.add(c) : classes.delete(c)) } }, querySelector: () => video },
    setTimeout: f => { timers.push(f); return timers.length; }, clearTimeout() {},
  };
  window.navigator = ctx.navigator;
  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  const flush = () => { while (timers.length) timers.shift()(); };
  return { app: window.KinoraApp, window, ctx, calls, classes, location, flush };
}

const android = { isTv: false, model: 'Pixel', hello: '#/play/42', haptic: undefined, share: undefined, canPip: true, pip: true, media: undefined, push: undefined, refresh: undefined };

test('the link the app was opened with becomes the page hash', () => {
  const r = run(android);
  assert.equal(r.location.hash, '#/play/42');
  assert.ok(r.calls.some(c => c[0] === 'hello'));
});

test('a link that is not a screen address is ignored', () => {
  const r = run({ ...android, hello: 'javascript:alert(1)' });
  assert.equal(r.location.hash, '#/home');
});

test('haptics, share, picture-in-picture and refresh reach the app', () => {
  const r = run(android);
  r.app.haptic('heavy');
  assert.equal(r.app.share('Party', 'http://x/#/party/ABCD'), true);
  assert.equal(r.app.canPip(), true);
  assert.equal(r.app.pip(), true);
  r.app.refresh();
  r.app.push(true);
  const names = r.calls.map(c => c[0]);
  for (const n of ['haptic', 'share', 'canPip', 'pip', 'refresh', 'push']) assert.ok(names.includes(n), n);
  assert.deepEqual(r.calls.find(c => c[0] === 'haptic'), ['haptic', 'heavy']);
  assert.equal(r.app.pushAvailable, true);
});

test('an older app without the new methods: no share, no push, calls are no-ops', () => {
  const r = run({ isTv: false, model: 'Old', keepAwake: undefined, exit: undefined });
  assert.equal(r.app.share, null);
  assert.equal(r.app.pushAvailable, false);
  assert.equal(r.app.cast, null);
  assert.doesNotThrow(() => { r.app.haptic('light'); r.app.pip(); r.app.refresh(); });
});

test('navigator.mediaSession goes to the media notification and its buttons come back', () => {
  const r = run(android);
  const ms = r.ctx.navigator.mediaSession;
  let seeked = null, played = 0;
  ms.setActionHandler('play', () => played++);
  ms.setActionHandler('seekto', d => { seeked = d.seekTime; });
  ms.metadata = new r.window.MediaMetadata({ title: 'Dűne', artist: '2021', album: 'Kinora', artwork: [{ src: 'http://192.168.1.10:8096/api/images/5/poster?v=1' }] });
  ms.playbackState = 'playing';
  ms.setPositionState({ duration: 9000, position: 60, playbackRate: 1 });
  r.flush();
  const last = JSON.parse(r.calls.filter(c => c[0] === 'media').pop()[1]);
  assert.equal(last.state, 'playing');
  assert.equal(last.title, 'Dűne');
  assert.equal(last.art, '/api/images/5/poster?v=1');
  assert.equal(last.w, 1920);
  assert.equal(last.next, false);
  r.window.kinoraMedia('play');
  r.window.kinoraMedia('seekto', 120);
  assert.equal(played, 1);
  assert.equal(seeked, 120);
  ms.metadata = null;
  r.flush();
  assert.equal(JSON.parse(r.calls.filter(c => c[0] === 'media').pop()[1]).state, 'none');
});

test('artwork of another site is not handed to the app', () => {
  const r = run(android);
  const ms = r.ctx.navigator.mediaSession;
  ms.metadata = new r.window.MediaMetadata({ title: 'x', artwork: [{ src: 'https://evil.example/a.jpg' }] });
  ms.playbackState = 'paused';
  r.flush();
  assert.equal(JSON.parse(r.calls.filter(c => c[0] === 'media').pop()[1]).art, '');
});

test('picture-in-picture and cast events reach the page', () => {
  const r = run(android);
  r.window.kinoraPip(true);
  assert.ok(r.classes.has('app-pip'));
  r.window.kinoraPip(false);
  assert.ok(!r.classes.has('app-pip'));
  r.window.kinoraCast({ event: 'connected', device: 'Nappali' });
  assert.deepEqual(r.calls.find(c => c[0] === 'event' && c[1] === 'kinora-cast').slice(1), ['kinora-cast', { event: 'connected', device: 'Nappali' }]);
});

test('Chromecast: load sends JSON, state is parsed', () => {
  const r = run({ ...android, castAvailable: true, castConnected: true, castPick: undefined, castLoad: true, castCmd: undefined,
    castState: '{"connected":true,"device":"Nappali","position":12.5,"playing":true}' });
  assert.ok(r.app.cast);
  assert.equal(r.app.cast.connected(), true);
  assert.equal(r.app.cast.load({ url: 'http://192.168.1.10:8096/api/stream/5?st=x', start: 30 }), true);
  assert.deepEqual(JSON.parse(r.calls.find(c => c[0] === 'castLoad')[1]), { url: 'http://192.168.1.10:8096/api/stream/5?st=x', start: 30 });
  r.app.cast.cmd('seek', '42');
  assert.deepEqual(r.calls.find(c => c[0] === 'castCmd'), ['castCmd', 'seek', 42]);
  assert.equal(r.app.cast.state().position, 12.5);
});

test('no Chromecast when the app says it has none (Android TV, no Play services)', () => {
  const r = run({ ...android, castAvailable: false });
  assert.equal(r.app.cast, null);
});
