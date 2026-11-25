# Változásnapló / Changelog

A kiadások részletes jegyzete: [docs/releases/](docs/releases/). Ez a fájl a még ki nem adott változásokat gyűjti.
/ Detailed notes of each release: [docs/releases/](docs/releases/). This file collects the changes not released yet.

## Kiadatlan / Unreleased

## 1.1.7 – 2026-11-25

- **Újdonság (iOS, tvOS):** fiókváltás egy szerveren belül. Több fiók maradhat bejelentkezve ugyanazon az eszközön, és
  jelszó nélkül válthatsz köztük: iPhone-on a fiókmenüből és a Beállítások → Fiókok részből, Apple TV-n a
  Beállításokból. A „Másik felhasználó” gomb már nem jelentkeztet ki, kijelentkezés után pedig a következő
  megjegyzett fiók veszi át az eszközt.
- **New (iOS, tvOS):** switching accounts within a server. Several accounts can stay signed in on one device and you
  switch without a password: on iPhone from the account menu and Settings → Accounts, on Apple TV from Settings.
  "Switch user" no longer signs you out, and after signing out the next remembered account takes over.
- **Újdonság (iOS):** Kinora az Üzenetekben. Az iMessage-appból watch party-meghívót és ajánlást küldhetsz a
  könyvtáradból, a buborékra koppintva a többiek a Kinorában csatlakoznak, és 12 mozis matrica is jár hozzá.
- **New (iOS):** Kinora in Messages. From the iMessage app you send watch party invites and recommendations from your
  library, the others join in Kinora by tapping the bubble, and 12 movie-night stickers come with it.
- **Javítás (borítók):** ha a szerver újra feldolgozott egy filmfájlt (új felirat, újraolvasott beágyazott feliratok),
  törölte a TMDB-ről letöltött borítót és hátteret, ha a mappában nem volt saját kép, és azok nem is jöttek vissza.
  Az újraszkennelés most megtartja őket, és a frissítés utáni első metaadat-frissítés egyszer pótolja a hiányzókat.
- **Fix (posters):** re-processing a movie file (a new subtitle, re-read embedded subtitles) deleted the poster and
  backdrop downloaded from TMDB when the folder had no artwork of its own, and they never came back. Re-scans now keep
  them, and the first metadata refresh after updating restores the missing ones once.

## 1.1.6 – 2026-11-25

- **Javítás (webes lejátszó, Safari):** a lejátszás kb. 80 másodpercenként megállt. Nem a szerver volt a hiba: a
  Safari „takarékos” lejátszási módja (ManagedMediaSource) leállította a videó további letöltését, és nem indította
  újra. A webes lejátszó most a hagyományos módot használja, és ha a kép mégis 4 másodpercnél tovább áll, magától
  újraindítja a töltést.
- **Fix (web player, Safari):** playback stopped about every 80 seconds. The server was not at fault: Safari's
  ManagedMediaSource paused loading and never resumed it. The web player now uses classic MediaSource, and restarts
  loading by itself when playback stalls for more than 4 seconds.

## 1.1.5 – 2026-11-24

- **Javítás (Windows):** a lejátszás 1.1.4-gyel is megállt kb. másfél perc után. Windowson az ffmpeg már nem
  cseréli (átnevezéssel) a lejátszási listát, hanem helyben írja újra, így nem maradhat meg egy korai, rövid lista.
  A napló minden ffmpeg-futás végén rögzíti, hány szegmens készült.
- **Fix (Windows):** playback still stopped about a minute and a half in with 1.1.4. On Windows ffmpeg no longer
  replaces the playlist by a rename but rewrites it in place, so no early, short playlist can be left behind. The
  log records how many segments each ffmpeg run made.

## 1.1.4 – 2026-11-24

- **Javítás (Windows):** a lejátszás kb. másfél perc után megállt, mintha vége lenne a résznek. Az ffmpeg a
  lejátszási lista utolsó cseréjét néha nem tudta elvégezni, és egy korai, rövid lista maradt meg. A szerver most az
  ffmpeg végeztével a helyére teszi a teljes listát és szegmenseket.
- **Fix (Windows):** playback stopped about a minute and a half in, as if the episode had ended. ffmpeg sometimes
  failed its last playlist rename and an early, short playlist was left behind. The server now puts the complete
  playlist and segments in place once ffmpeg is done.
- **Javítás / Fix:** a frissítés fájlcseréjének hibái már nem vesznek el. / errors of the update's file swap are no longer lost.

## 1.1.3 – 2026-11-24

- **Javítás (Windows):** az irányítópult **Frissítés most** gombja nem csinált semmit. A szerver most saját maga
  frissít, PowerShell nélkül: letölt, ellenőriz, ment, cserél, és a szolgáltatáskezelő újraindítja; ha az új verzió
  nem indul el rendben, a régi program és adatbázis visszakerül.
- **Fix (Windows):** the dashboard's **Update now** button did nothing. The server now updates itself without
  PowerShell: it downloads, verifies, backs up, swaps and is restarted by the service manager; if the new version
  does not start properly, the old program and database are put back.

## 1.1.2 – 2026-11-24

- **Javítás (Windows):** a lejátszás pár percenként megállt, remuxnál is, mert a szerver olvasás közben zárolta
  az ffmpeg lejátszási listáját, és a csere elakadt. Most a szerver úgy olvassa, hogy az ffmpeg közben lecserélheti.
- **Fix (Windows):** playback stopped every few minutes, remux included: the server locked ffmpeg's playlist while
  reading it, so ffmpeg could not replace it. The server now reads it in a way that lets ffmpeg replace it.
- **Biztonság:** a kezdő beállítás (az első admin fiók) csak a helyi hálózatról, loopbackről vagy Tailscale-ről
  végezhető el; `KINORA_ALLOW_REMOTE_SETUP=1` feloldja. A beállító oldal előre elmagyarázza, ha innen nem lehet (#7).
- **Security:** the initial setup (the first admin account) is accepted only from the local network, loopback or
  Tailscale; `KINORA_ALLOW_REMOTE_SETUP=1` lifts this. The setup page explains it up front when it applies (#7).

- **Új:** egy médiakönyvtár több mappát is tartalmazhat. **New:** a library can span several folders.
- **Javítás / Fix:** iOS letöltési Live Activity; hallássérült-felirat jelölés a Jellyfin-kompatibilis API-ban.
  / iOS downloads Live Activity; hearing-impaired subtitle flag in the Jellyfin-compatible API.

## 1.1.1 – 2026-11-24

- **Biztonság:** `gorilla/websocket` a GO-2026-6278 javításával (gyenge véletlenszám a WebSocket maszkkulcsához).
  Az 1.1.0-hoz nem készült Docker-image, a Dockeres telepítések ezzel kapják meg az 1.1-es változásokat.
- **Security:** `gorilla/websocket` with the fix for GO-2026-6278 (weak random numbers for the WebSocket mask key).
  No Docker image was built for 1.1.0; Docker installs get the 1.1 changes with this release.

## 1.1.0 – 2026-11-24

### Magyar

**Új**
- **Váltás Jellyfinről:** Admin → Import → Jellyfin. Felhasználók (jelszó nélkül, egyszer használható jelszóbeállító
  linkkel), megnézett állapot, folytatási pozíció, kedvencek és utolsó lejátszás. Párosítás fájlútvonal (útvonal-
  leképezéssel), TMDB/IMDb/TVDB azonosító, epizódnál évad és epizódszám alapján; előnézet, jóváhagyás, idempotens
  újrafuttatás, letölthető jelentés.
- **Váltás Sonarrról, Radarrról, Prowlarrról:** figyelt sorozatok és filmek, minőségprofilok, indexerek,
  letöltőkliensek és gyökérmappák; ugyanígy előnézettel és jelentéssel.
- **Jellyfin-kompatibilis API** (alapból kikapcsolva, külön port): Jellyfin-kliensek csatlakozhatnak, a Kinora
  jogosultságaival; Quick Connect profil- és PIN-választással. Kodi + JellyCon-nal kipróbálva.
- **Kezdő beállítási varázsló:** médiamappák, TMDB-kulcs, hardveres gyorsítás tesztje, letöltőkliens és indexer,
  távoli elérés, összefoglaló; minden lépés kihagyható és később pótolható (HU/EN/DE).
- **Offline letöltés:** az Android-app a rendszer letöltéskezelőjével tölt le, és az indítóból szerver nélkül is
  lejátszik; az iOS-app szerver nélkül indulva a letöltéseket mutatja. A letöltések profilhoz kötöttek (gyerekprofil
  más profil letöltését nem látja, offline sem); az offline megnézett pozíció a következő csatlakozáskor a szerverre
  kerül.
- **Benchmark:** `scripts/bench/` – Kinora és Jellyfin mérése egymás mellett Docker Compose-zal.

**Javítva**
- CSRF-védelem a sütis munkamenetekhez; a belső hibák részletei nem jutnak ki; jogosultsági rések bezárva.
- Közös letöltési feladatok, korlátos sorok és átkódolási limit; a háttérfolyamatok túlélik a pánikot.
- Újabb verzió által írt adatbázist a szerver nem nyit meg.
- A fuzz-tesztek által talált hibák a fájlnév-, release- és Cardigann-elemzőkben.
- Jellyfin-kompatibilis API: WebSocket, `/Years` és `/Tags`, típusszűrés a „Latest” listán, feliratmezők, token nélküli
  stream-URL a lejátszási munkamenettel (a Kodi-teszt során találva).
- Teljesítmény: a tétlen SQLite-kapcsolatok nyitva maradnak, és az SQLite a globális memóriastatisztika-zár nélkül
  fordul; párhuzamos kéréseknél ez többszörös áteresztőképességet adott (a mérés: docs/BENCHMARKS.md).

**Fejlesztés**
- CI: `staticcheck`, `govulncheck`, fuzz-tesztek, az appok tesztjei, problémás médiafájlok generátora és integrációs
  tesztjei; CI-jelvény a README-ben.
- Dokumentáció: ARCHITECTURE, AUDIT, IMPORT, JELLYFIN_COMPAT, OFFLINE, BENCHMARKS, CONTRIBUTING, SECURITY; issue- és
  PR-sablonok; a README összehasonlító táblázata átírva.

### English

**New**
- **Switching from Jellyfin:** Admin → Import → Jellyfin. Users (without passwords, with a one-time set-password link),
  watched state, resume positions, favourites and last played. Matching by file path (with path mapping), TMDB/IMDb/TVDB
  id, and season and episode number; preview, approval, idempotent re-runs, downloadable report.
- **Switching from Sonarr, Radarr, Prowlarr:** monitored series and films, quality profiles, indexers, download clients
  and root folders; with the same preview and report.
- **Jellyfin-compatible API** (off by default, separate port): Jellyfin clients can connect, with Kinora's permissions;
  Quick Connect with profile and PIN choice. Tried with Kodi + JellyCon.
- **First-run wizard:** media folders, TMDB key, hardware acceleration test, download client and indexer, remote
  access, summary; every step can be skipped and done later (HU/EN/DE).
- **Offline downloads:** the Android app downloads with the system's download manager and plays from the launcher
  without the server; the iOS app shows the downloads when it starts without the server. Downloads belong to profiles
  (a kids profile never sees another profile's, offline either); positions watched offline reach the server on the
  next connection.
- **Benchmark:** `scripts/bench/` – Kinora and Jellyfin measured side by side with Docker Compose.

**Fixed**
- CSRF protection for cookie sessions; internal error details no longer leak; permission gaps closed.
- Shared download jobs, bounded queues and a transcode limit; background loops survive panics.
- The server refuses a database written by a newer version.
- Bugs found by fuzzing in the file name, release and Cardigann parsers.
- Jellyfin-compatible API: WebSocket, `/Years` and `/Tags`, type filter on the Latest list, subtitle fields, stream URLs
  without a token via the play session (found while testing with Kodi).
- Performance: idle SQLite connections stay open, and SQLite is built without its global memory-statistics lock;
  under concurrent requests this gave several times the throughput (measured: docs/BENCHMARKS.md).

**Development**
- CI: `staticcheck`, `govulncheck`, fuzz tests, app tests, a problem-media generator with integration tests; CI badge in
  the README.
- Documentation: ARCHITECTURE, AUDIT, IMPORT, JELLYFIN_COMPAT, OFFLINE, BENCHMARKS, CONTRIBUTING, SECURITY; issue and PR
  templates; the README comparison table rewritten.
