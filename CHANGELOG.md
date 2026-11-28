# Változásnapló / Changelog

A kiadások részletes jegyzete: [docs/releases/](docs/releases/). Ez a fájl a még ki nem adott változásokat gyűjti.
/ Detailed notes of each release: [docs/releases/](docs/releases/). This file collects the changes not released yet.

## Kiadatlan / Unreleased

## 1.1.101 – 2026-11-28

- **Új (hardveres dekódolás):** ha a kódoló a videokártyán fut, a dekódolás és a méretezés is ott történik, így a
  processzor szinte nem dolgozik. Windowson az Intel Quick Sync (D3D11 vagy QSV), a Direct3D 12 és az NVIDIA CUDA,
  Linuxon a VAAPI, a QSV és a CUDA, Macen a VideoToolbox működik. Az indításkori önteszt kipróbálja, melyik út
  működik a H.264, a HEVC és az AV1 forrásokkal. Ha egy út lejátszás közben hibát ad, a szerver processzoros
  dekódolással újraindítja az átkódolást. Feliratbeégetésnél és HDR→SDR tónusleképezésnél a dekódolás továbbra is
  a processzoron fut.
- **Új (Windows):** Direct3D 12 hardveres kódolók (H.264, HEVC, AV1) Intel, AMD és NVIDIA kártyákhoz.

## 1.1.100 – 2026-11-28

- **Javítás (Windows, Intel Arc):** az Intel Arc B580 (és minden olyan gép, ahol a képkockák feltöltése a videokártyára
  nem működik) most is hardveresen kódol: a QSV-kódoló a rendszermemóriából kapja a képkockákat. A naplóban a valódi
  ffmpeg-hiba látszik az általános „Nothing was written…” sor helyett, és Windowson a VAAPI már nem jelenik meg hibaként.

## 1.1.99 – 2026-11-28

- **Javítás (Windows, Intel):** Windowson az Intel Quick Sync (QSV) hardveres kódolás akkor is működik, ha a gépben
  több videokártya van, vagy ha a Kinora szolgáltatásként fut. A szerver előbb az Intel kártyán nyit D3D11-eszközt, és
  több beállítást kipróbál; ha a hardveres kódolás nem működik, az oka alap naplószinten is látszik.

## 1.1.98 – 2026-11-28

- **Új (web):** ország szerinti szűrés a Felfedezésben. Az „Ország” menüben választhatsz egy származási országot
  („Csak innen”), vagy kizárhatsz egyet („Kivéve”). Ha egy országot kizársz, a több ország közös gyártásai közül azok még
  megjelennek, amelyeknek van másik származási országuk is.
- **New (web):** filter the Felfedezés (Discover) by country. The "Country" menu keeps only the titles from one
  country of origin ("Only from") or leaves one out ("Except"). When you leave a country out, co-productions still
  show if they also have another country of origin.
- **Új (web):** előzetes a Felfedezésben. Egy film vagy sorozat lapján az „Előzetes” gombbal ott helyben elindul a
  YouTube-előzetes (a TMDB adatai alapján, elsőként a metaadat nyelvén). Ha a címnek nincs előzetese, a gomb nem jelenik meg.
- **New (web):** trailers in the Felfedezés (Discover). On a film or series page the "Trailer" button plays its YouTube
  trailer in place (from TMDB, in the metadata language first). The button does not show when a title has none.
- **Új:** linkelőnézet. A szerver Open Graph-címkéket ad a megosztott linkekhez; ezekből mutat címet és képet például az
  iMessage, a WhatsApp vagy a Discord – ha a szerver az internetről elérhető. A watch party linkje (`/party/KÓD`) a film vagy epizód címét és borítóját mutatja, amíg a party
  tart; a meghívó linkje (`/join/KÓD`) a szerver nevét, a meghívó megjegyzését nem. Minden más link a szerver nevét és
  ikonját. A kép címe a `KINORA_PUBLIC_URL` (nyilvános cím) alapján készül, ha be van állítva. A web és az iPhone/iPad
  app ilyen linket ad tovább; a régi `#/party/` és `#/join/` linkek továbbra is működnek. A bejelentkezés nélkül
  megnyitott party link bejelentkezés és profilválasztás után a partyba visz.
- **New:** link previews. The server gives shared links Open Graph tags, from which apps such as iMessage, WhatsApp or
  Discord show a title and a picture – when the server can be reached from the internet. A watch party link (`/party/CODE`) shows the film or episode title and its poster while the party is
  open; an invite link (`/join/CODE`) shows the server's name, never the invite's note. Every other link shows the
  server's name and icon. The picture's address uses `KINORA_PUBLIC_URL` (the public address) when it is set. The web
  client and the iPhone/iPad app share these links; the old `#/party/` and `#/join/` links still work. A party link
  opened while signed out takes you into the party after you sign in and pick a profile.

## 1.1.97 – 2026-11-27

- **Új (web, iPhone, iPad, Apple TV, Android):** idővonal-előnézet. A szerver tíz másodpercenként egy kis képet készít a
  filmekről és epizódokról (akkor dolgozik, amikor senki nem néz semmit; a megnyitott cím előre kerül), a lejátszó pedig
  tekerés közben megmutatja, hova ugrasz: weben az idővonal fölé víve az egeret vagy húzáskor, a ±10 másodperces
  ugrásnál is; iPhone-on és iPaden a csúszka húzásakor; Apple TV-n az ugrásnál. Az Androidos app a web lejátszóját
  használja. Az admin Átkódolás oldalán kapcsolható ki („Idővonal-előnézet”), vagy a `KINORA_TRICKPLAY=0` változóval.
- **New (web, iPhone, iPad, Apple TV, Android):** timeline preview. The server takes a small picture of films and
  episodes every ten seconds (while nobody is watching anything; the title you open moves to the front), and the player
  shows where you are going while you scrub: on the web when you hover over or drag the timeline, and on the ±10 second
  jumps; on iPhone and iPad while you drag the slider; on Apple TV when you jump. The Android app uses the web player.
  Turn it off on the admin Transcoding page ("Timeline preview") or with `KINORA_TRICKPLAY=0`.
- **Új (Android):** kép a képben (gombbal, Android 12-től magától is, amikor kilépsz egy futó videóból), médiavezérlő az
  értesítések között és a zárképernyőn, a lejátszás a háttérben is megy; Chromecast a lejátszóból; widgetek (Folytatás,
  Új a könyvtárban, Napló); parancsikonok az ikonon (Folytatás, Keresés, Napló, Letöltések); rezgés a lejátszó
  gombjainál; a party linkje a rendszer megosztójával; `kinora://` linkek. Android TV-n a „Lejátszás következő” sorban
  megjelenik, amit épp nézel. Értesítések Google-szolgáltatások nélkül: a szerver megőrzi őket, az app negyedóránként
  lekéri; a Fiók oldalon kapcsolható be, kategóriánként.
- **New (Android):** picture in picture (with a button, and from Android 12 on its own when you leave a playing video),
  media controls in the notifications and on the lock screen, playback continues in the background; Chromecast from the
  player; widgets (Continue watching, New in the library, Journal); app shortcuts (Resume, Search, Journal, Downloads);
  haptics on the player's buttons; the party link through the system share sheet; `kinora://` links. On Android TV what
  you are watching shows in the "Play next" row. Notifications without Google services: the server keeps them and the
  app fetches them every quarter of an hour; turn them on, by category, on the Account page.
- **Eltávolítva (iPhone, iPad):** a próbaértesítés gombja a beállításokban. Az iOS-alkalmazás 1.1.97-es.
- **Removed (iPhone, iPad):** the test notification button in settings. The iOS app is 1.1.97.

## 1.1.96 – 2026-11-26

- **Új (iPhone, iPad):** értesítés, amikor elkészül a havi összesítő (a hónap első hetében, délelőtt 10 után, ha az előző
  hónapban volt nézés), januárban pedig az éves összefoglaló; magyarul, angolul és németül. Az értesítésre koppintva a
  Napló a hónappal, illetve az éves összefoglalóval nyílik. A beállításokban „Havi és éves összesítő” néven kapcsolható ki.
- **Változott (web, iPhone, iPad):** a tavalyi éves összefoglaló csak január 1. és 15. között nézhető meg; az ideit
  továbbra is bármikor. Az iOS-alkalmazás 1.1.96-os.
- **New (iPhone, iPad):** a notification when the monthly recap is ready (in the first week of the month, after 10 am, if
  you watched anything the month before), and in January the year in review; in Hungarian, English and German. Tapping it
  opens the Journal on that month, or straight into the year in review. It can be turned off in settings as "Monthly and
  yearly recap".
- **Changed (web, iPhone, iPad):** last year's year in review can only be opened from January 1 to 15; this year's still
  any time. The iOS app is 1.1.96.

## 1.1.95 – 2026-11-26

- **Új (web, iPhone, iPad):** megújult Napló: nagy fejléc az időszakkal, saját időszakválasztó (telefonon két sorban),
  nagy számok a változással, az éves cél kiemelt sávban, a kiválasztott hónap filmjei külön panelen, rekordkártyák; az
  éves összefoglaló és a nézés hozzáadása a fejlécbe került. Telefonon a webes oldal nem lóg ki, a hónapok rövid nevet
  kapnak. Az iOS-alkalmazás 1.1.95-ös.
- **Javítva (iPhone, iPad):** németül a kézzel felvett nézés „Sichtung”, nem „Ansicht”.
- **New (web, iPhone, iPad):** a redesigned Journal: a big header with the period, a period picker of its own (two rows
  on a phone), big numbers with their change, the yearly goal in a panel of its own, the chosen month's titles in a panel,
  record cards; the year in review and adding a viewing moved into the header. On a phone the web page no longer runs off
  the screen and months get short names. The iOS app is 1.1.95.
- **Fixed (iPhone, iPad):** in German a viewing added by hand is a "Sichtung", not an "Ansicht".

## 1.1.94 – 2026-11-26

- **Új (web, iPhone, iPad):** bővebb Napló: csillagos értékelés, újranézés jelzése, félbehagyott címek (elrejthetők),
  rendezők, színészek, országok és évtizedek, napszak-hőtérkép, rekordok (sorozat, legnagyobb nap és hét, darálós
  hétvégék), órákra, epizódokra és műfajra szóló éves vagy havi célok, kihívások (rendező, színész, filmsorozat, saját
  lista), és kézzel felvehető nézés TMDB-kereséssel.
- **New (web, iPhone, iPad):** a richer Journal: star ratings, a rewatch badge, titles left unfinished (which can be
  hidden), directors, actors, countries and decades, a time-of-day heatmap, records (streak, biggest day and week, binge
  weekends), yearly or monthly goals for hours, episodes and a genre, challenges (a director, an actor, a film series, an
  own list), and viewings added by hand through a TMDB search.

## 1.1.93 – 2026-11-26

- **Javítva (web):** a felső menü keskenyebb ablakban (vagy sok könyvtárnál) nem törik két sorba, hanem saját, oldalra
  görgethető sorba kerül; telefonon álló helyzetben a felirat a lejátszó vezérlői fölé kerül, nem alájuk; tömörebb
  epizódlista telefonon; rendezettebb okosgyűjtemény-szerkesztő telefonon; a böngészőfül az oldal címét mutatja; a
  profilmenü Escape-re bezárul.
- **Fixed (web):** in a narrower window (or with many libraries) the top menu no longer wraps onto two lines but gets a
  row of its own that scrolls sideways; on a phone held upright subtitles sit above the player's controls, not under
  them; a more compact episode list on phones; a tidier smart collection editor on phones; the browser tab names the
  page; Escape closes the profile menu.

## 1.1.92 – 2026-11-26

- **Javítva (web, iPhone, iPad):** Napló: egyforma plakátok hosszú címnél is, szélesebb idővonal-sávok, halvány és nem
  kattintható jövőbeli napok, telefonon a kiválasztott napnál nyíló idővonal, nézésszám a hét napjainál, akadálymentes
  évösszegző (fókusz, Tab, visszalapozás az utolsó oldalról), percben mutatott kis időeltérés, megjegyzés, ha az
  összehasonlítás nem tölthető be, és minden eszközön egyszerre eltűnő havi kártya. „Mindig” helyett „Kezdettől”.
- **Fixed (web, iPhone, iPad):** Journal: even posters with long titles, wider timeline bars, faint days still ahead that
  cannot be pressed, the timeline opening at the chosen day on phones, viewing counts on the weekdays, an accessible year
  in review (focus, Tab, stepping back from the last page), small time differences in minutes, a note when the comparison
  cannot be loaded, and the monthly card closing on every device at once.

## 1.1.91 – 2026-11-26

- **Újdonság (iPhone, iPad):** megújult widgetek: hátralévő idő, ✓ gomb a közepes Folytatáson is és azonnali
  eltűnéssel, ajánlás üres Folytatás helyett, cím a kis „Új a könyvtárban” widgeten, nagy Következő rész és Napló
  widget, elavult adatnál az időpont, Vezérlőközpont-gomb (iOS 18), színezett mód és StandBy.
- **New (iPhone, iPad):** reworked widgets: time left, the ✓ button on the medium Continue too and gone at once,
  suggestions instead of an empty Continue, titles on the small "New in library", large Next episode and Journal
  widgets, the time of stale data, a Control Center control (iOS 18), tinted mode and StandBy.
- **Újdonság (web, iOS, tvOS):** a lejátszóban a hátralévő időre kattintva az látszik, hány órakor ér véget a film (pl. 22:38,
  a lejátszási sebességgel számolva); újabb kattintás visszavált. Apple TV-n a
  kiválasztó gomb hosszan nyomva vált. A választás megmarad.
- **New (web, iOS, tvOS):** in the player, clicking the time left shows the clock time the film ends (e.g. 22:38, at the
  current playback speed); another click switches back. On Apple TV a long
  press of the select button switches. The choice is remembered.

## 1.1.9 – 2026-11-26

- **Újdonság (minden platform):** Napló a fiókmenüben. Mit néztél, mikor és mennyit (ez a hónap, idén, tavaly,
  kezdettől), nyilakkal az előző időszakhoz képest, éves filmcéllal, megosztható havi képpel és évösszegzővel. A hónap
  első hetében a kezdőlapon kártya foglalja össze az előző hónapot. iPhone-on és iPaden Napló widget is van.
- **New (all platforms):** Journal in the account menu. What you watched, when and how much (this month, this year,
  last year, all time), with arrows against the period before, a yearly film goal, a shareable month picture and a
  year in review. In the first week of a month a home page card sums up the month before. iPhone and iPad also get a
  Journal widget.
- **Javítás:** a hóesés alapból ki van kapcsolva, és az iOS/tvOS appokból kikerült. A műfajok és címkék száma egy
  több változatban meglévő filmet egyszer számol.
- **Fix:** snowfall is off by default and gone from the iOS/tvOS apps. Genre and tag counts count a film with
  several versions once.

## 1.1.8 – 2026-11-26

- **Újdonság (minden platform):** hóesés. Amikor nem megy film vagy sorozat, kis fehér hópelyhek hullanak a
  képernyőn. Profilonként kapcsolható (web: Fiók → Megjelenés, iPhone: Beállítások → Lejátszás, Apple TV:
  Beállítások), és alapból mindenkinél be van kapcsolva. A „mozgás csökkentése” beállítás mellett nem esik.
- **New (all platforms):** snowfall. While no film or episode is playing, small white snowflakes fall across the
  screen. It is a per-profile switch (web: Account → Appearance, iPhone: Settings → Playback, Apple TV: Settings),
  on for everyone by default, and it stays off under Reduce Motion.
- **Javítás (iOS, elrendezés):** fekvő iPhone-on a watch party panel már nem tolja ki a lejátszót a képernyőről
  (a QR-kód alapból rejtve, a csevegés görgethető), iPad-ablakban a lejátszó bezárógombja és a kezdőlap logója nem
  kerül az ablakvezérlők alá, a részletek borítóképe legfeljebb a képernyő felét foglalja, és fekvő telefonon
  a telefonos méretek maradnak.
- **Fix (iOS, layout):** on a landscape iPhone the watch party panel no longer pushes the player off screen (the QR
  code is hidden by default, the chat scrolls); in an iPad window the player's close button and the home logo stay
  clear of the window controls; the detail hero takes at most half the screen; a landscape phone keeps phone sizes.

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
