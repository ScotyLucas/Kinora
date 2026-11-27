# Push-közvetítő (iOS)

A hivatalos iOS app APNs-értesítéseit egy közvetítő (relay) juttatja el az Apple-hez. A Kinora szerverek a felhasználók
gépén futnak, az APNs-kulcs viszont a hivatalos app fejlesztői fiókjához tartozik, ezért azt nem kaphatja meg egyetlen
szerver sem. A közvetítő kódja külön repóban van (`gitlab.com/Zsozsi/kinorarelay`, Cloudflare Worker), ez a dokumentum a szerver, az app és
a közvetítő közötti szerződés.

## Alapelvek

- **Szerver nem lát APNs-tokent.** Az app a tokent közvetlenül a közvetítőnek adja, és cserébe egy **handle**-t kap
  (véletlen, 32 bájtos azonosító). A Kinora szerver csak a handle-t ismeri.
- **Handle csak egy szerverhez érvényes.** A handle egy (eszközkulcs, szerver) párhoz tartozik; más szerver nem küldhet rá.
- **Csak a hivatalos app regisztrálhat.** Az eszközregisztrációt App Attest igazolja (`TEAMID.bundleid`). Fork, szkript
  vagy szimulátor nem kap handle-t (kivétel: sandbox fejlesztői kapcsoló, lent).
- **Kötött formájú értesítés.** A szerver nem adhat meg `aps` mezőt. A közvetítő mindig ugyanazt a tartalék szöveget
  (lokalizációs kulcsot) küldi `mutable-content: 1`-gyel, mellette a titkosított csomagot. Hang, kritikus értesítés,
  csendes push nincs.
- **A közvetítő nem látja a tartalmat.** A csomagot a szerver az eszközzel közös AES-256-GCM kulccsal titkosítja; az app
  Notification Service Extensionje fejti vissza.
- **Korlátok és vészfék.** Handle-enként és szerverenként óránkénti keret; szerver és eszköz azonnal letiltható.

## Szereplők és titkok

| Ki | Mit tárol | Mit nem lát |
|---|---|---|
| iOS app | App Attest kulcs (Secure Enclave), handle-k szerverenként, szerverenkénti AES-kulcs (Keychain, app group) | – |
| Kinora szerver | `server_id` + `server_secret` (a közvetítőtől), eszközönként handle + AES-kulcs | APNs-token |
| Közvetítő | szerverek (`server_id`, secret hash), eszközök (App Attest kulcs, APNs-token, környezet), handle → (eszköz, szerver) | értesítés tartalma, AES-kulcs |

## Folyamat

1. **Szerverregisztráció** (egyszer, a push bekapcsolásakor): `POST /v1/servers` → `server_id`, `server_secret`.
   A szerver ezt a saját adatbázisában tárolja. Önmagában semmire nem jogosít, handle nélkül nem lehet küldeni.
2. **Eszközregisztráció** (az appban, párosításkor vagy tokenváltozáskor):
   1. `GET /v1/challenge` → egyszer használható `challenge` (5 percig érvényes).
   2. Új kulcsnál `DCAppAttestService.attestKey(keyId, clientDataHash: SHA256(clientData))`, ismert kulcsnál
      `generateAssertion(keyId, clientDataHash:)`.
   3. `POST /v1/devices` → `handle`.
3. **Párosítás a szerverrel:** az app a saját Kinora szerverének elküldi a handle-t és egy frissen generált
   AES-256 kulcsot (`POST /api/push/ios`, a felhasználó saját munkamenetével). A szerver a `server_id`-t az app
   kérésére előre közli (`GET /api/push/ios` → `relay_url`, `server_id`), mert a handle ehhez kötődik.
4. **Küldés:** a szerver eseménynél `POST /v1/send` aláírt kéréssel, a handle-lel és a titkosított csomaggal.
5. **Megszűnt token:** ha az APNs `410`-et vagy `BadDeviceToken`-t ad, a közvetítő törli az eszközt, és a
   `/v1/send` `410 {"error":"gone"}`-nal válaszol; a szerver ekkor törli a handle-t.

## Végpontok

Minden kérés és válasz JSON (`content-type: application/json`), base64 mezők **base64url** kódolásúak, kitöltés nélkül.
Hiba esetén: `{"error": "<kód>"}` megfelelő HTTP-státusszal.

### `POST /v1/servers`

Kérés: `{}` (vagy `{"name": "..."}`, legfeljebb 64 karakter, csak tájékoztató).

Válasz `201`: `{"server_id": "srv_…", "server_secret": "<base64url, 32 bájt>"}`.

A közvetítő a secretet nem tárolja: `HMAC-SHA256(SERVER_SECRET_KEY, "server:" + server_id)`-ből számolja újra. Ha a
`SERVER_SECRET_KEY` cserélődik, minden szervernek újra kell regisztrálnia.

IP-címenként 24 óránként legfeljebb 2 regisztráció (naptári napra, UTC szerint).

### `GET /v1/challenge`

Válasz `200`: `{"challenge": "<base64url, 32 bájt>", "expires_in": 300}`.

### `POST /v1/devices`

```json
{
  "key_id": "<App Attest keyId, base64>",
  "server_id": "srv_…",
  "apns_token": "<hex>",
  "environment": "production | sandbox",
  "challenge": "<a kapott challenge>",
  "attestation": "<base64url, csak új kulcsnál>",
  "assertion": "<base64url, ismert kulcsnál>"
}
```

A `clientData` amit az app hash-el (UTF-8, pontosan ebben a formában):

```
kinora-relay-v1\n<challenge>\n<server_id>\n<apns_token>\n<environment>
```

A közvetítő ellenőrzi:

- `attestation`: CBOR `apple-appattest` formátum, a tanúsítványlánc az Apple App Attest Root CA-ig, a nonce
  (`SHA256(authData || clientDataHash)`) egyezik a levéltanúsítvány `1.2.840.113635.100.8.2` kiterjesztésével,
  `rpIdHash == SHA256(APP_ID)`, a `keyId == SHA256(nyilvános kulcs)`, a számláló 0, az `aaguid`
  `appattestdevelop` (sandbox) vagy `appattest` + 8 nulla bájt (production) – ez a `environment` mezővel egyezik.
- `assertion`: a tárolt nyilvános kulccsal az ECDSA-aláírás a `SHA256(authData || clientDataHash)`-en,
  `rpIdHash == SHA256(APP_ID)`, a számláló nagyobb a tároltnál.

Válasz `200`: `{"handle": "<base64url, 32 bájt>"}`. Ugyanarra a (kulcs, szerver) párra mindig ugyanaz a handle;
új APNs-token frissíti a tárolt tokent.

**Fejlesztői kapcsoló:** ha a közvetítőben `ALLOW_UNATTESTED_SANDBOX=true`, akkor `environment: "sandbox"` mellett
igazolás nélkül is regisztrálhat egy eszköz (szimulátor). Élesben kikapcsolva marad.

### `POST /v1/send` (szerver → közvetítő)

Fejlécek:

| Fejléc | Érték |
|---|---|
| `x-kinora-server` | `server_id` |
| `x-kinora-timestamp` | Unix idő másodpercben; ±300 mp-en kívül elutasítva |
| `x-kinora-signature` | hex `HMAC-SHA256(server_secret, timestamp + "\n" + METÓDUS + "\n" + útvonal + "\n" + nyers body)` |

Body:

```json
{
  "handle": "<base64url>",
  "payload": "<base64url: CryptoKit AES.GCM combined = nonce(12) || ciphertext || tag(16)>",
  "collapse_id": "opcionális, legfeljebb 64 karakter"
}
```

A `payload` dekódolva legfeljebb 2048 bájt. Válaszok: `200 {"ok":true}`, `401 unauthorized`, `403 disabled`,
`404 unknown_handle`, `410 gone`, `413 payload_too_large`, `429 rate_limited` (`retry-after` fejléccel),
`502 apns_error`.

A közvetítő az APNs-nek ezt küldi (`apns-push-type: alert`, `apns-priority: 10`, `apns-expiration`: +1 óra):

```json
{
  "aps": {
    "alert": { "title-loc-key": "PUSH_FALLBACK_TITLE", "loc-key": "PUSH_FALLBACK_BODY" },
    "mutable-content": 1
  },
  "k": "<payload>",
  "s": "<server_id>"
}
```

### `DELETE /v1/devices/<handle>` (szerver → közvetítő)

Ugyanazzal az aláírással, mint a `/v1/send` (`DELETE`, a saját útvonalával, üres body). A handle-t és – ha más szerverhez már nem tartozik – az
eszközt törli. Válasz `200 {"ok":true}`.

### `POST /v1/live/tokens` (eszköz → közvetítő)

Az app itt adja le a Live Activity tokenjeit. A hitelesítés a handle: csak az eszköz és a saját szervere ismeri.

```json
{ "handle": "<handle>", "token": "<hex APNs token>", "ref": "<aktivitás azonosító>" }
```

- `ref` nélkül a push-to-start token (ezzel a szerver új aktivitást indíthat).
- `ref`-fel egy futó aktivitás frissítő tokenje; `token` nélkül (`null`) a `ref` törlődik.
- A tokenek 12 óráig élnek; a régieket a következő leadáskor törli.
- Hibák: `400 bad_request` / `bad_ref` / `bad_token`, `404 unknown_handle`, `403 disabled`, `429`.

### `POST /v1/live` (szerver → közvetítő)

Aláírt Live Activity indítás, frissítés vagy lezárás, ugyanazzal az aláírással, mint a `/v1/send`.

```json
{ "handle": "<handle>", "ref": "<aktivitás azonosító>", "event": "start|update|end",
  "payload": "<base64 titkosított állapot>", "stale_date": 1764000000, "dismissal_date": 1764003600, "priority": 5 }
```

- A `payload` ugyanúgy titkosított, mint a `/v1/send` csomagja, legfeljebb 1024 bájt; a közvetítő nem látja a tartalmát.
- `start` a push-to-start tokennel megy és mindig 10-es prioritású; a `RequestActivityAttributes` aktivitást indítja (`s` = szerver, `ref`).
  `update` és `end` a `ref` tokenjével megy, alapból 5-ös prioritással (`priority: 10` kérhető).
- `stale_date` és `dismissal_date` Unix-másodperc, nem kötelező.
- Ha nincs token, vagy az Apple már nem fogadja: `404 no_live_token`. Ez szándékosan nem `410`, mert az az egész párosítást törölné a szerveren.
- `end` után, és ha az Apple elutasítja a tokent, a közvetítő elfelejti azt.
- Egyéb hibák: `400 bad_request` / `bad_date` / `bad_payload`, `413 payload_too_large`, `502 apns_error`, `503 send_disabled`.

A letöltések élő aktivitása (a készülékre töltés) helyben fut az appban, nem a közvetítőn megy.

## A titkosított csomag

A titkosítatlan tartalom JSON, legfeljebb ~1,5 KB:

```json
{ "v": 1, "type": "…", "title": "…", "body": "…", "item": 123, "href": "#/item/123", "image": "/api/images/123/poster", "tag": "…" }
```

Az `image` relatív út a saját szerveren; az NSE a saját munkamenetével tölti le. A szerverenkénti AES-kulcsot az app
generálja, a szerverrel párosításkor osztja meg, és Keychainben tárolja (app group, hogy az NSE is elérje). A
csomagban lévő `s` mező alapján választja ki a kulcsot.

## Korlátok

| Mire | Alapérték | Beállítás |
|---|---|---|
| küldés handle-enként | 30 / óra | `LIMIT_HANDLE_HOURLY` |
| küldés szerverenként (a `/v1/live` is) | 600 / óra | `LIMIT_SERVER_HOURLY` |
| szerverregisztráció IP-nként | 2 / 24 óra | `LIMIT_REGISTER_DAILY` |
| eszközregisztráció IP-nként | 10 / 24 óra | `LIMIT_DEVICE_DAILY` |
| Live Activity küldés handle-enként | 120 / óra | `LIMIT_LIVE_HOURLY` |
| Live Activity token leadás handle-enként | 48 / 24 óra (IP-nként a négyszerese) | `LIMIT_LIVE_TOKENS_DAILY` |
| challenge kérés IP-nként | az eszközkeret 4-szerese / 24 óra | – |

## Vészfék

- Szerver letiltása: `POST /v1/admin/servers/<server_id>/disable` (`authorization: Bearer <ADMIN_TOKEN>`).
- Eszköz letiltása: `POST /v1/admin/handles/<handle>/disable`.
- Minden küldés leállítása: `SEND_DISABLED=true` változó és újratelepítés.

## A Kinora szerver oldala

- **Beállítás** (Beállítások → Hálózat, vagy környezeti változó): `push_enabled` (`KINORA_PUSH`, 0/1) és
  `push_relay_url` (`KINORA_PUSH_RELAY`). Alapérték a hivatalos közvetítő: `https://kinorarelay.projectone.dev`.
- **Szerverregisztráció:** az első `GET /api/push/ios`-kor (vagy küldéskor), és újra, ha a közvetítő URL-je
  változik vagy a közvetítő `401 unauthorized`-dal válaszol. A `push_server_id`, `push_server_secret` és
  `push_server_relay` belső beállítás, az admin felületen nem látszik.
- **Párosítás:** `push_ios` tábla, a munkamenet `device_id`-jához kötve, így kijelentkezéskor vagy az eszköz
  eltávolításakor törlődik.
- **App API** (bejelentkezett munkamenettel, stream-tokennel nem):

  | Kérés | Leírás |
  |---|---|
  | `GET /api/push/ios` | `{"enabled": false}` vagy `{"enabled": true, "relay_url", "server_id", "paired"}` |
  | `POST /api/push/ios` | `{"handle": "<43 karakter base64url>", "key": "<32 bájt, base64 vagy base64url>"}`; új handle-nél a régit törli a közvetítőnél |
  | `DELETE /api/push/ios` | párosítás bontása, a handle törlése a közvetítőnél |

- **Android (közvetítő nélkül):** az Android app nem kap pusht; a szerver az eszköznek szóló csomagokat a
  `push_inbox` táblában tartja (eszközönként a legújabb 50-et), az app pedig a háttérben negyedóránként lekéri.
  A párosítás ugyanabban a `push_ios` táblában van, `inbox:<device_id>` handle-lel és kulcs nélkül (nincs mit
  titkosítani: a csomag a saját munkamenetével, HTTPS-en vagy a helyi hálózaton megy). A közvetítő kikapcsolva
  is működik; élő tevékenység nincs.

  | Kérés | Leírás |
  |---|---|
  | `GET /api/push/android` | `{"enabled": true, "categories", "paired", "muted"}` |
  | `POST /api/push/android` | bekapcsolás ezen az eszközön (`{"muted": [...]}` opcionális); a régi közvetítős párosítás megszűnik |
  | `PATCH /api/push/android` | `{"muted": [...]}` |
  | `DELETE /api/push/android` | kikapcsolás |
  | `GET /api/push/android/inbox` | `{"messages": [<csomag>, …]}`, és a kiadottakat törli |

- **Küldés:** a háttérben 30 mp-enként a teljesült kérések (`acq_requests.status='available'`, `pushed=0`)
  a kérő összes párosított eszközére mennek `collapse_id: "req-<id>"`-vel. Ami több mint 24 órája teljesült
  (a push ki volt kapcsolva), azt küldés nélkül megjelöli. Ha a közvetítő nem érhető el (`5xx`, `429`),
  a következő körben újrapróbálja. `410` vagy `unknown_handle` esetén törli a párosítást.
- **Csomag:** `{"v":1, "type":"request_available"|"test", "title", "body", "item", "href":"#/item/<id>",
  "image":"/api/images/<id>/poster", "tag"}`. A cím legfeljebb 150, a szöveg legfeljebb 400 karakter, és a
  JSON legfeljebb 1500 bájt.
