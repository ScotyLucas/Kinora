# Kinora kliensappok

Az appok a **szerver saját webkliensét** futtatják. Egy kis indító (`launcher`) kiválasztja vagy megkeresi a szervert, aztán megnyitja
a szerver webkliensét app-módban (`http://szerver:8096/?app=<platform>`). Ami a webkliensbe bekerül, az az appokban is megjelenik,
külön portolás nélkül. Platformok: **Android** (telefon + Android TV, egy APK), **Samsung Tizen** (`.wgt`), **LG webOS** (`.ipk`)
és **Titan OS** (Philips, JVC… TV-k; hosztolt oldal). Az **iOS** (iPhone/iPad) app natív SwiftUI, külön projekt: [`ios/README.md`](ios/README.md).

```
apps/
  launcher/       az indító: szerverlista, cím megadása, keresés a hálózaton → a szerver webkliense app-módban (ES5, nincs build-lépés)
  android/        Capacitor-projekt (android/), build.mjs → dist/kinora.apk
  tizen/          config.xml + build.mjs → dist/kinora.wgt
  webos/          appinfo.json + build.mjs → dist/kinora_<ver>_all.ipk
  titan/          build.mjs → dist/site/ (statikus oldal a Titan OS-nek) + dist/kinora-titan.zip
  shared/         a régebbi, beépített TS app: már csak a 2018-as TV-k kapják (Tizen 4, webOS 4), lásd lent
  ios/            natív SwiftUI app (XcodeGen: project.yml) + KinoraKit Swift csomag – nem npm-munkaterület
  native-plugin/  a régi Capacitor-iOS KinoraNative pluginja (Swift, SPM) – már nem használja semmi
  branding/       logo.svg + banner.svg + brand.mjs: minden ikon, banner és splash egy forrásból
  scripts/        közös build-segédek (parancsfuttatás, zip, buildLauncher)
```

## Hogyan működik

1. **Indító** (`launcher/`): az első indításkor a szerver címét kéri, vagy megkeresi a hálózaton. Később rögtön az utoljára használt
   szervert nyitja meg: előbb az otthoni, aztán a külső címét. A Vissza gomb vagy a webkliens **Szerverváltás** menüpontja visszahoz
   a szerverlistához.
2. **App-mód a webkliensben** (`web/static/appshell.js`): a `?app=` paramétert elmenti, és ettől kezdve:
   - **TV-n** (Tizen, webOS, Titan OS, Android TV) a nyilakkal lehet mozogni (térbeli navigáció). A Vissza becsukja a nyitott
     menüt vagy dialógust, vagy kilép a lejátszóból, a főoldalon pedig az appból. A lejátszóban rejtett vezérlőknél a jobbra/balra
     teker, az OK megállít, a fel/le előhozza a vezérlőket. A médiagombok (lejátszás, szünet, előre, hátra, következő) működnek.
     A belépés kóddal (Quick Connect) az alapértelmezett, a hangerő, a teljes képernyő és a kép a képben gomb rejtve van.
   - **Mindenhol:** a TV saját formátumai bekerülnek a direct play képességekbe (MKV, AC-3/E-AC-3, HEVC), lejátszás közben ébren
     marad a képernyő, és az eszköz a saját nevével jelenik meg az eszközlistában (pl. „Samsung QE55Q80“, platform: tizen).
   - Böngészőben kipróbálható: `http://szerver:8096/?tv=1` (kikapcsolás: `?app=off`).
3. **Régebbi TV-motorok:** a webkliens Chromium 80-at igényel (`?.`, `??`). Ennél régebbi motoron (2019–2020-as TV-k, Chromium 63–79)
   az `index.html` az `app.legacy.js`-t tölti be. Ezt a szerver állítja elő az `app.js`-ből (esbuild, `internal/server/legacyjs.go`),
   és a `web/static/legacy-polyfills.js`-t teszi elé. Build-lépés nincs, a webkliens egyetlen forrás marad.
   Chromium 63 alatt (2018: Tizen 4.0, webOS 4.x) a webkliens nem fut: ott az indító a csomagban lévő régebbi appot (`legacy/`,
   `apps/shared`) indítja.

| Platform | Minimum | Megjegyzés |
|---|---|---|
| Samsung (Tizen) | Tizen 5.0 (2019) webklienssel, Tizen 4.0 (2018) a régi appal | `$WEBAPIS/webapis.js`: modellnév, saját IP a kereséshez |
| LG (webOS) | webOS 5 (2020) webklienssel, webOS 4.x a régi appal | `disableBackHistoryAPI`: a Vissza 461-es gombként érkezik |
| Titan OS | minden (friss Chromium) | hosztolt oldal, lásd lent |
| Android telefon / TV | Android 7+ friss WebView-val | a Vissza (gomb, gesztus, távirányító) a natív oldalról jut el a laphoz (`window.kinoraBack`) |

## Építés

Egyszer: `cd apps && npm install`. Utána minden platformnak egy parancsa van (Windowson a `build.cmd`/`build.ps1` ugyanezt hívja):

| Platform | Parancs | Kimenet | Ami a géphez kell |
|---|---|---|---|
| Android | `npm run build:android` | `android/dist/kinora.apk` (debug) | Docker **vagy** JDK 21 + Android SDK 36 (`JAVA_HOME`, `ANDROID_HOME`) |
| Android release | `npm run build:release -w android` | `android/dist/kinora-release.apk` | ugyanaz + aláírókulcs env-ben (lásd lent) |
| LG webOS | `npm run build:webos` | `webos/dist/kinora_<ver>_all.ipk` | semmi (a webOS CLI devDependency) |
| Samsung Tizen | `npm run build:tizen` | `tizen/dist/kinora.wgt`, CLI nélkül `kinora-unsigned.wgt` | Tizen Studio CLI + Samsung-tanúsítvány az aláíráshoz |
| Titan OS | `npm run build:titan` | `titan/dist/site/` + `titan/dist/kinora-titan.zip` | semmi |
| iOS | `cd ios && xcodegen`, majd Xcode (lásd [`ios/README.md`](ios/README.md)) | `ios/Kinora.xcodeproj` | macOS + Xcode 16+ + XcodeGen |
| régi TS app | `npm run build:shared`, `npm test` | `shared/dist/` | – |

Az appok verziója a `launcher/package.json`-ból jön.

### Titan OS

A Titan OS-es appok weboldalak: a TV egy fix **https** címről nyitja meg őket, ezt a Titan OS fejlesztői portálján kell regisztrálni.
A `titan/dist/site/` mappát kell kitenni egy ilyen címre (pl. GitHub Pages), vagy a zipet feltölteni. Egy https oldal nem olvashat
http-s szervert (mixed content). Ezért otthoni (http) címnél az indító ellenőrzés nélkül nyitja meg a szervert, ami viszont a saját
originjén fut, így ott már nincs korlát. https-es szervernél a `/api/status` bármely originről olvasható, így előbb ellenőrzi.
A hálózati keresés itt nem elérhető, a címet be kell írni.

### Android aláírás (release)

A kulcs soha nem kerül a repóba (`*.jks`, `*.keystore` a `.gitignore`-ban). Kulcs készítése egyszer:

```sh
keytool -genkeypair -v -keystore kinora.jks -alias kinora -keyalg RSA -keysize 4096 -validity 10000
```

Build: `KINORA_KEYSTORE=/út/kinora.jks KINORA_KEYSTORE_PASSWORD=… KINORA_KEY_ALIAS=kinora KINORA_KEY_PASSWORD=… npm run build:release -w android`.
A CI-ban ugyanezek GitHub Secrets-ként szerepelnek (`ANDROID_KEYSTORE_BASE64`, `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS`, `ANDROID_KEY_PASSWORD`).

## Telepítés

- **Android telefon:** az APK-t fájlként átmásolod és megnyitod, vagy `adb install -r android/dist/kinora.apk`.
- **Android TV:** `adb connect <tv-ip>:5555`, majd `adb install -r …`. Az app a leanback launcherben bannerrel jelenik meg, és TV-módban indul.
- **LG webOS:** Developer Mode app a TV-n → `npx ares-setup-device` → `npm run install-tv -w webos`. A Dev Mode session 50 óránként lejár, ilyenkor meg kell hosszabbítani (Extend).
- **Samsung Tizen:** a TV-n fejlesztői mód (Apps → 12345), Samsung-tanúsítvány a Certificate Managerrel, majd `KINORA_TV_IP=… npm run install-tv -w tizen`.
- **Titan OS:** a `titan/dist/site/` kitéve egy https címre, a Titan OS fejlesztői portálján ezzel a címmel regisztrálva (lásd fent).
- **iPhone:**
  - Ma, Mac nélkül: a webkliens PWA-ként is telepíthető (Safari → Megosztás → „Főképernyőhöz adás”).
  - Natív app: Mac + Xcode + XcodeGen kell. A Team ID a git-ignorált `ios/Config/Local.xcconfig`-ba kerül, utána `cd ios && xcodegen`, Xcode-ban megnyitni a `Kinora.xcodeproj`-ot → csatlakoztasd az iPhone-t → Run. Részletek: [`ios/README.md`](ios/README.md).
  - Az ingyenes fiókkal aláírt app **7 napig** fut, utána újra kell telepíteni. Tartósan, illetve TestFlightra vagy az App Store-ba fizetős Apple Developer Program (évi 99 USD) kell.
  - A CI (`.github/workflows/apps.yml`) macOS runneren lefuttatja a KinoraKit teszteket (`swift test`), majd XcodeGen után aláírás nélkül lefordítja a simulator buildet.

## Belépés

- **Jelszóval:** a szerver címe után felhasználónév + jelszó. Telefonon ez az alapértelmezett.
- **Kóddal (Quick Connect):** TV-n ez az alapértelmezett. A TV mutat egy 6 jegyű kódot. Egy már bejelentkezett eszközön
  (webkliens vagy telefonos app: profil menü → Eszköz csatlakoztatása) beírod a kódot, és jóváhagyod.

A szerver címe lehet `192.168.1.10`, `192.168.1.10:8096`, `nas.local`, `http://…` vagy `https://…`. Ha nincs séma, előbb `http`, ha nincs port, előbb `8096`.
Több szerver menthető. A webkliens profil menüjében (és a belépő oldalon) a **Szerverváltás** visszavisz az indító szerverlistájához.

**Szerver keresése:** Android/Android TV-n UDP-broadcast a 7359-es portra (`who is KinoraServer?`). iOS-en, Tizenen és webOS-en HTTP-s pásztázás a saját /24-es alhálózaton
(a TV-k böngészőmotorja nem tud UDP-t küldeni; iOS-en az UDP-broadcast fogadásához külön Apple-jogosultság kellene, ezért a natív app is pásztáz).
**Dockerben** a broadcast nem jut át a bridge hálózaton: a felderítéshez `network_mode: host` kell (vagy írd be kézzel a címet). Kikapcsolás: Admin → Beállítások, vagy `KINORA_DISCOVERY=0`.

## Miért kell a cleartext (HTTP) engedélyezése?

Egy otthoni médiaszerver jellemzően sima HTTP-n érhető el egy helyi IP-címen (`http://192.168.1.10:8096`). Ezt a címet buildeléskor nem lehet tudni,
a platformok alapból viszont csak HTTPS-t engednek:

- **Android:** `network_security_config.xml` → `base-config cleartextTrafficPermitted="true"` és `usesCleartextTraffic`. A `domain-config` csak előre ismert
  hostokra szűkíthetne, egy tetszőleges LAN-IP-re nem. HTTPS-szervereknél a rendszer tanúsítványellenőrzése változatlanul érvényes.
  A Capacitor `androidScheme: "http"`, hogy a WebView ne blokkolja a HTTPS-oldalról induló HTTP-videót (mixed content).
- **iOS:** `NSAllowsLocalNetworking` (helyi és `.local` címek) és `NSLocalNetworkUsageDescription` a helyi hálózati engedélykéréshez (`ios/project.yml`).
  Távoli szerverhez HTTPS kell.
- **Tizen:** `<access origin="*">` és `internet` jogosultság a `config.xml`-ben. **webOS:** a csomagolt webapp alapból bármilyen originre hívhat.

Biztonság: a webkliens app-módban is a szerver saját originjén fut, így ugyanaz a sütis munkamenet védi, mint a böngészőben. Az indító a többi
originről csak a nyilvános `/api/status`-t olvashatja. Androidon a WebView az indítóból bármelyik http(s) címre továbbléphet (ez a választott
szerver), a szerver oldalairól viszont csak ugyanarra a hostra; minden más link a böngészőben nyílik meg. A szerver oldalai a Capacitor-hidat
nem kapják meg, csak a szűk `window.KinoraAndroid` interfészt (TV-e, modellnév, képernyő ébren tartása, kilépés, és az alábbi natív
részek). Hogy egy hívás mit tehet, azt a Java oldal a WebView saját címéből dönti el, nem abból, amit a lap állít magáról: a Chromecastra
csak a lap saját szerverének címe mehet, a widgetek és értesítések szervere a legutóbb nyitott szerveroldal originje.

### Az Android app natív részei

A webkliens `web/static/appshell.js`-e köti be őket (`KinoraApp.haptic`, `share`, `pip`, `push`, `refresh`, `cast`); régebbi appban a hívások
üresen futnak le. Mindegyik a platform saját API-ját használja, a Chromecast kivételével új függőség nélkül.

| Funkció | Java | Megjegyzés |
|---|---|---|
| Kép a képben | `MainActivity`, `KinoraAndroid.pip` | gomb a lejátszóban; Android 12+ magától, 8–11-en a kezdőképernyőre lépéskor; TV-n nincs |
| Médiavezérlő, háttérlejátszás | `MediaService`, `MediaButtons` | a lejátszó `navigator.mediaSession`-je → értesítés, zárképernyő, fülhallgató gombjai; foreground service lejátszás közben |
| `kinora://` linkek | `Links`, `MainActivity` | `item/ID`, `play/ID`, `party/KÓD`, `journal`, `downloads`, `search`, `resume`, `home`; minden más link eldobva |
| Alkalmazás-parancsikonok | `res/xml/shortcuts.xml` | Folytatás, Keresés, Napló, Letöltések |
| Rezgés, megosztás | `KinoraAndroid.haptic`, `share` | lejátszás/tekerés, kedvenc, átugrás, következő rész; a party linkje a rendszer megosztójával |
| Widgetek | `Widgets`, `SyncJob` | Folytatás és Új a könyvtárban (3 poszter), Napló (e havi megtekintések, órák) |
| Android TV „Lejátszás következő” sor | `WatchNext` | a Folytatás és a következő részek; a poszter címe csak képre jó tokent visz (`media_token`) |
| Értesítések | `SyncJob` | Google push nélkül: a szerver megőrzi őket, az app negyedóránként lekéri (`GET /api/push/android/inbox`); a Fiók oldalon kapcsolható |
| Chromecast | `KinoraCast` | Default Media Receiver; a lejátszó H.264/AAC streamet kér hozzá, a címben stream-tokennel; partyban nincs |

A háttérmunkát (`SyncJob`, JobScheduler, hálózattal) a WebView `kinora_session` sütijével, `Authorization: Bearer`-ként végzi az app.
A régi app (`legacy/`, iOS) **Bearer tokennel** hívja az API-t: a szerver ezekre az app-originekre engedélyez CORS-t, `Allow-Credentials` nélkül,
és eldobja a sütit. A `<video>` és `<img>` egy rövid életű, aláírt **stream-tokent** kap (`?st=`).

## Platformismeret a lejátszóban

A lejátszó a `MediaSource.isTypeSupported`/`canPlayType` eredményét egy platformonkénti táblázattal egészíti ki (`web/static/appshell.js`, a régi appban `shared/src/player/caps.ts`).
A TV-k natív lejátszója sokszor üres választ ad olyan formátumra, amit közvetlenül le tud játszani (MKV, HEVC, AC-3/E-AC-3), és így a direct play minél
gyakrabban működik. HDR10-et csak akkor jelez, ha a platform igazolja (Tizen: `webapis.avinfo.isHdrTvSupport()`, máshol `(dynamic-range: high)`).
HLS: a webkliens hls.js-t használ, ahol van MediaSource (TV-k, Android), iOS-en AVPlayer.

## Tudatosan későbbre hagyva (v2)

- **Offline letöltés az Android appban:** a webkliens app-módban a letöltést a rendszer DownloadManagerének adja át
  (`window.KinoraOffline`, `KinoraOffline.java`), az indító pedig szerver nélkül is lejátssza az utolsó profil letöltéseit.
  Felépítés, profilszabályok, biztonság és a kipróbálási lista: [docs/OFFLINE.md](../docs/OFFLINE.md).
