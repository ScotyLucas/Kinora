/* Kinora web client – app mode for the packaged apps (Samsung Tizen, LG webOS, Titan OS, Android TV, Android phone).
   The apps are thin launchers: they pick a server, then open this page from that server with
   ?app=<platform>&launcher=<url>&model=<name>&ver=<version>. From then on the web client runs as the app:
   remote-control keys, Back that closes what is open (and leaves the app on the home screen), the 10-foot layout on TVs,
   keeping the screen on during playback, and the TV's own codecs for direct play.
   Without ?app= (and nothing saved from an earlier start) this file does nothing: the browser client stays as it is.
   Written in ES2015 without newer syntax: it also runs on 2019 TV engines before the legacy app.js does. */
'use strict';
(function () {
  var KEY = 'kinora-app';
  var PLATFORMS = { tizen: 'Samsung TV', webos: 'LG TV', titan: 'Titan OS TV', androidtv: 'Android TV', android: 'Android' };
  var TV = { tizen: true, webos: true, titan: true, androidtv: true };

  function load() { try { return JSON.parse(localStorage.getItem(KEY) || 'null'); } catch (e) { return null; } }
  function save(v) { try { localStorage.setItem(KEY, JSON.stringify(v)); } catch (e) { /* storage blocked */ } }

  var q = new URLSearchParams(location.search);
  var info = load();
  if (q.get('app') && PLATFORMS[q.get('app')]) {
    info = { platform: q.get('app'), launcher: q.get('launcher') || '', model: q.get('model') || '', ver: q.get('ver') || '' };
    save(info);
  } else if (q.get('app') === 'off') {
    info = null;
    try { localStorage.removeItem(KEY); } catch (e) { /* storage blocked */ }
  }
  var native = window.KinoraAndroid; // Android: injected by MainActivity
  if (!info && native) info = { platform: native.isTv() ? 'androidtv' : 'android', launcher: 'http://localhost/', model: '', ver: '' };
  var tvTest = q.get('tv') === '1'; // try the TV layout in a desktop browser: /?tv=1
  if (q.get('app') || q.get('tv') || q.get('launcher')) {
    // the start parameters are saved; keep them out of the address so a reload or a shared link stays clean
    try { history.replaceState(history.state, '', location.pathname + location.hash); } catch (e) { /* ignore */ }
  }
  if (!info && !tvTest) { window.KinoraApp = null; return; }
  if (!info) info = { platform: 'web', launcher: '', model: '', ver: '' };

  var platform = info.platform;
  var tv = !!TV[platform] || tvTest;
  var root = document.documentElement;
  root.classList.add('app', 'app-' + platform);
  if (tv) root.classList.add('tv');

  function model() {
    if (info.model) return info.model;
    try { if (native && native.model) return native.model(); } catch (e) { /* old app */ }
    try { if (window.webapis && webapis.productinfo) return webapis.productinfo.getRealModel(); } catch (e) { /* not on this page */ }
    return PLATFORMS[platform] || '';
  }

  // ------------------------------------------------------------------ leaving: exit the app, or back to the server list
  function overlay(text) {
    var o = document.createElement('div');
    o.className = 'app-leaving';
    o.textContent = text || '';
    document.body.appendChild(o);
  }
  function toLauncher(intent, text) {
    overlay(text || '…');
    var l = info.launcher || '';
    if (/^https?:/.test(l)) { location.href = l.split('#')[0] + '#' + (intent || 'servers'); return; }
    // A launcher on file:// (Tizen, webOS) cannot be navigated to from here: go back to the first history entry,
    // where it started. The router stands still meanwhile (KinoraApp.leaving), so no redirect adds entries.
    window.KinoraApp.leaving = true;
    history.go(-(history.length - 1));
    // with forward entries the jump is too long and ignored: then one step at a time
    var step = function () { history.back(); setTimeout(step, 150); };
    setTimeout(step, 400);
  }
  function exit() {
    try {
      if (native) return native.exit();
      if (platform === 'tizen' && window.tizen) return tizen.application.getCurrentApplication().exit();
      if (platform === 'webos' && window.webOS && webOS.platformBack) return webOS.platformBack();
    } catch (e) { /* fall through */ }
    if (platform === 'titan') { try { window.close(); } catch (e) { /* ignore */ } }
    // nothing could close the app from this page: the launcher can
    setTimeout(function () { toLauncher('exit'); }, 200);
  }

  // ------------------------------------------------------------------ Back
  function isTyping(el) {
    return !!el && (el.isContentEditable || /^(INPUT|TEXTAREA)$/.test(el.tagName) && !/^(checkbox|radio|range|button|submit)$/.test(el.type));
  }
  var sending = false;
  function sendKey(key) {
    var t = document.activeElement && document.activeElement !== document.body ? document.activeElement : (document.querySelector('.player') || document.body);
    sending = true;
    try { t.dispatchEvent(new KeyboardEvent('keydown', { key: key, bubbles: true, cancelable: true })); } finally { sending = false; }
  }
  function atRoot() {
    var h = location.hash.replace(/\?.*$/, '');
    return /^(#?\/?|#\/home|#\/login|#\/login\/code|#\/profiles|#\/setup)$/.test(h);
  }
  /** One step back. Returns false when there is nothing left to close (the app should exit). */
  function back() {
    var a = document.activeElement;
    if (tv && isTyping(a)) { a.blur(); return true; }
    var menus = document.querySelectorAll('.menu-pop:not(.hidden)');
    if (menus.length) {
      for (var i = 0; i < menus.length; i++) { if (menus[i].classList.contains('floating')) menus[i].remove(); else menus[i].classList.add('hidden'); }
      var av = document.querySelector('.menu .avatar');
      if (av && tv) av.focus();
      return true;
    }
    if (document.fullscreenElement && document.exitFullscreen) { document.exitFullscreen().catch(function () {}); return true; }
    if (document.querySelector('.modal-back') || document.querySelector('.player')) { sendKey('Escape'); return true; }
    if (document.querySelector('.app-leaving')) return true;
    if (atRoot()) return false;
    history.back();
    return true;
  }
  // Android: the system Back (button, gesture, remote) comes from MainActivity, not as a key event
  window.kinoraBack = back;

  // ------------------------------------------------------------------ remote keys
  // keyCodes: Tizen (10009 back, 10252 play/pause…), webOS / HbbTV (461 back, 415 play, 19 pause, 413 stop, 417 ff, 412 rw),
  // Android TV and keyboards (179 play/pause, 178 stop, 176/177 next/prev, 227/228 rw/ff).
  var KEYS = {
    10009: 'back', 461: 'back',
    10252: 'playpause', 179: 'playpause', 415: 'play', 19: 'pause', 413: 'stop', 178: 'stop',
    417: 'ff', 228: 'ff', 412: 'rw', 227: 'rw', 10233: 'next', 176: 'next',
  };
  function action(e) {
    if (e.key === 'GoBack' || e.key === 'BrowserBack') return 'back';
    if (e.key === 'MediaPlayPause') return 'playpause';
    if (e.key === 'MediaFastForward') return 'ff';
    if (e.key === 'MediaRewind') return 'rw';
    if (e.key === 'MediaStop') return 'stop';
    if (e.key === 'MediaTrackNext') return 'next';
    var k = KEYS[e.keyCode];
    if (k) return k;
    if (tv && (e.key === 'Escape' || e.keyCode === 8 && !isTyping(e.target))) return 'back'; // Titan OS sends Backspace
    return null;
  }
  window.addEventListener('keydown', function (e) {
    if (sending) return;
    var act = action(e);
    if (!act) {
      // OK on a checkbox: the remote has no space bar
      if (tv && e.key === 'Enter' && e.target && e.target.tagName === 'INPUT' && /^(checkbox|radio)$/.test(e.target.type)) { e.preventDefault(); e.target.click(); }
      return;
    }
    e.preventDefault();
    e.stopImmediatePropagation();
    var player = document.querySelector('.player');
    var video = player && player.querySelector('video');
    switch (act) {
      case 'back': if (!back()) exit(); break;
      case 'playpause': if (video) sendKey(' '); break;
      case 'play': if (video && video.paused) sendKey(' '); break;
      case 'pause': if (video && !video.paused) sendKey(' '); break;
      case 'stop': if (player) sendKey('Escape'); break;
      case 'ff': if (video) { if (document.activeElement && player.contains(document.activeElement)) document.activeElement.blur(); sendKey('ArrowRight'); } break;
      case 'rw': if (video) { if (document.activeElement && player.contains(document.activeElement)) document.activeElement.blur(); sendKey('ArrowLeft'); } break;
      case 'next': if (video) sendKey('n'); break;
    }
  }, true);

  // Tizen and webOS deliver only the basic keys unless the app asks for the media keys
  // (the launcher already did; repeating it here covers a TV that kept this page open).
  try {
    if (window.tizen && tizen.tvinputdevice) {
      ['MediaPlayPause', 'MediaPlay', 'MediaPause', 'MediaStop', 'MediaFastForward', 'MediaRewind', 'MediaTrackNext', 'MediaTrackPrevious']
        .forEach(function (k) { try { tizen.tvinputdevice.registerKey(k); } catch (e) { /* not on this model */ } });
    }
  } catch (e) { /* not a packaged app page */ }

  // ------------------------------------------------------------------ keep the screen on while a video plays
  var lock = null;
  function keepAwake(on) {
    try { if (native) { native.keepAwake(!!on); return; } } catch (e) { /* old app */ }
    try {
      if (window.webapis && webapis.appcommon && webapis.appcommon.setScreenSaver) {
        var S = webapis.appcommon.AppCommonScreenSaverState;
        webapis.appcommon.setScreenSaver(on ? S.SCREEN_SAVER_OFF : S.SCREEN_SAVER_ON);
        return;
      }
    } catch (e) { /* not on this page */ }
    if (on && !lock && navigator.wakeLock) navigator.wakeLock.request('screen').then(function (l) { lock = l; l.addEventListener('release', function () { lock = null; }); }).catch(function () {});
    if (!on && lock) { lock.release().catch(function () {}); lock = null; }
  }

  // ------------------------------------------------------------------ direct play
  // TV engines often answer "" to canPlayType for what their native player handles from a plain <video src>
  // (MKV, AC-3/E-AC-3, HEVC): these are added to what probing finds, so direct play wins more often.
  // Sources: Samsung Tizen and LG webOS TV media specifications (2019+ models).
  var EXTRA_CAPS = {
    tizen: { video: ['h264', 'hevc', 'vp9'], audio: ['aac', 'mp3', 'ac3', 'eac3', 'flac', 'opus'], containers: ['mp4', 'mkv', 'webm', 'mov'] },
    webos: { video: ['h264', 'hevc', 'vp9'], audio: ['aac', 'mp3', 'ac3', 'eac3', 'flac'], containers: ['mp4', 'mkv', 'webm', 'mov'] },
  };
  function extraCaps(caps) {
    var x = EXTRA_CAPS[platform];
    if (!x) return caps;
    ['video', 'audio', 'containers'].forEach(function (k) { x[k].forEach(function (v) { if (caps[k].indexOf(v) < 0) caps[k].push(v); }); });
    try { if (platform === 'tizen' && window.webapis && webapis.avinfo && webapis.avinfo.isHdrTvSupport) caps.hdr = !!webapis.avinfo.isHdrTvSupport() && caps.video.indexOf('hevc') >= 0; } catch (e) { /* old firmware */ }
    return caps;
  }

  // ------------------------------------------------------------------ the Android app's native parts
  // (KinoraAndroid.java). An older app lacks some methods: each call is then a no-op.
  function call(name) {
    var args = Array.prototype.slice.call(arguments, 1);
    try { if (native && typeof native[name] === 'function') return native[name].apply(native, args); } catch (e) { /* old app */ }
    return undefined;
  }
  function has(name) { try { return !!native && typeof native[name] === 'function'; } catch (e) { return false; } }

  // the server's web client is up: the app remembers this server (widgets, notifications) and hands over
  // the link it was opened with (a shortcut, a widget, a notification: kinora://play/42 → #/play/42)
  if (native && /^https?:$/.test(location.protocol)) {
    var link = call('hello');
    if (typeof link === 'string' && /^#\/[\w\/?=&%.-]*$/.test(link)) location.hash = link;
  }

  // picture-in-picture of the app: the page shows only the picture meanwhile
  window.kinoraPip = function (on) {
    document.documentElement.classList.toggle('app-pip', !!on);
    try { window.dispatchEvent(new CustomEvent('kinora-pip', { detail: !!on })); } catch (e) { /* old engine */ }
  };
  // Chromecast session events: {event: 'connected' | 'disconnected', device}
  window.kinoraCast = function (ev) {
    try { window.dispatchEvent(new CustomEvent('kinora-cast', { detail: ev || {} })); } catch (e) { /* old engine */ }
  };

  // Playback outside the app: the player describes itself through navigator.mediaSession (as in a browser);
  // here that goes to the media notification, the lock screen and headset buttons (MediaService.java),
  // whose buttons come back as kinoraMedia(action, seconds).
  if (has('media')) {
    var handlers = {}, meta = null, pbState = 'none', posState = null, sentPos = 0, sendT = null;
    var sendMedia = function () {
      sendT = null;
      var v = document.querySelector('video');
      var st = !meta || pbState === 'none' ? 'none' : pbState === 'playing' ? 'playing' : 'paused';
      var art = '';
      try {
        var a = meta && meta.artwork && meta.artwork[0] && new URL(meta.artwork[0].src, location.href);
        if (a && a.origin === location.origin) art = a.pathname + a.search;
      } catch (e) { /* no artwork */ }
      sentPos = Date.now();
      call('media', JSON.stringify({
        state: st, title: meta ? meta.title || '' : '', artist: meta ? meta.artist || '' : '', album: meta ? meta.album || '' : '', art: art,
        duration: posState ? posState.duration || 0 : 0, position: posState ? posState.position || 0 : 0,
        rate: posState ? posState.playbackRate || 1 : 1, next: !!handlers.nexttrack,
        w: v ? v.videoWidth || 0 : 0, h: v ? v.videoHeight || 0 : 0,
      }));
    };
    var queue = function () { if (!sendT) sendT = setTimeout(sendMedia, 30); };
    var Meta = function (o) {
      o = o || {};
      this.title = o.title || ''; this.artist = o.artist || ''; this.album = o.album || ''; this.artwork = o.artwork || [];
    };
    var session = {
      setActionHandler: function (a, f) { handlers[a] = f || null; queue(); },
      setPositionState: function (s) {
        posState = s || null;
        if (Date.now() - sentPos > 5000) queue(); // the notification runs its clock on its own in between
      },
    };
    Object.defineProperty(session, 'metadata', { get: function () { return meta; }, set: function (m) { meta = m || null; queue(); } });
    Object.defineProperty(session, 'playbackState', { get: function () { return pbState; }, set: function (s) { pbState = s; queue(); } });
    try {
      Object.defineProperty(navigator, 'mediaSession', { value: session, configurable: true });
      window.MediaMetadata = Meta;
    } catch (e) { /* the engine's own stays */ }
    window.kinoraMedia = function (action, secs) {
      var f = handlers[action];
      if (f) { f({ action: action, seekTime: secs }); return; }
      var v = document.querySelector('video');
      if (!v) return;
      if (action === 'play') v.play().catch(function () {});
      if (action === 'pause') v.pause();
    };
  }

  // Chromecast (KinoraCast.java): the player plays there instead, by the stream's address
  var cast = null;
  if (has('castAvailable') && call('castAvailable')) {
    cast = {
      connected: function () { return !!call('castConnected'); },
      pick: function () { call('castPick'); },
      load: function (o) { return !!call('castLoad', JSON.stringify(o)); },
      cmd: function (c, arg) { call('castCmd', c, +arg || 0); },
      state: function () { try { return JSON.parse(call('castState') || '{}'); } catch (e) { return {}; } },
    };
  }

  window.KinoraApp = {
    platform: platform,
    tv: tv,
    canSwitchServer: platform !== 'web', // the launcher is either a URL or (file://) the start of the history
    device: function () { return { device_name: model(), platform: platform, app_version: info.ver || '' }; },
    back: back,
    exit: exit,
    switchServer: function (text) { toLauncher('servers', text); },
    leaving: false, // set while the app walks back to its launcher
    keepAwake: keepAwake,
    extraCaps: extraCaps,
    // tick | light | medium | heavy | success | warning | error
    haptic: function (kind) { call('haptic', kind || 'light'); },
    // the system share sheet; false where the app has none (the page then copies the text)
    share: has('share') ? function (title, text) { call('share', title || '', text || ''); return true; } : null,
    canPip: function () { return !!call('canPip'); },
    pip: function () { return !!call('pip'); },
    // this device's notifications, fetched by the app itself (GET /api/push/android/inbox)
    pushAvailable: has('push'),
    push: function (on) { call('push', !!on); },
    // widgets and Android TV's Play next row catch up (something was watched)
    refresh: function () { call('refresh'); },
    cast: cast,
  };
})();
