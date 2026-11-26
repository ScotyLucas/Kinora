import ActivityKit
import DeviceCheck
import SwiftUI
import UIKit
import UserNotifications
import KinoraKit

// Push notifications through the push relay (docs/PUSH_RELAY.md): the app proves itself to
// the relay with App Attest, gets a handle bound to one Kinora server, and gives the server
// that handle and a fresh key. The Notification Service Extension decrypts what arrives.

@MainActor @Observable
final class PushManager {
    static let shared = PushManager()

    /// The user turned notifications on in Settings → Értesítések.
    private(set) var wanted = UserDefaults.standard.bool(forKey: PushManager.wantedKey) {
        didSet { UserDefaults.standard.set(wanted, forKey: Self.wantedKey) }
    }
    private(set) var authorization: UNAuthorizationStatus = .notDetermined
    /// The current server's answer to GET /api/push/ios.
    private(set) var status: PushStatus?
    /// The current server sends to this device.
    private(set) var paired = false
    private(set) var working = false
    var lastError: String?

    private static let wantedKey = "kinora.push.wanted"
    private static let mutedKey = "kinora.push.muted"
    private static let liveKey = "kinora.push.live"

    /// Categories the user turned off (PushCategory); the same on every server.
    private(set) var muted = Set(UserDefaults.standard.stringArray(forKey: PushManager.mutedKey) ?? []) {
        didSet { UserDefaults.standard.set(muted.sorted(), forKey: Self.mutedKey) }
    }
    /// The user wants the server's downloads of their requests on the Lock Screen (on by default).
    private(set) var liveWanted = UserDefaults.standard.object(forKey: PushManager.liveKey) as? Bool ?? true {
        didSet { UserDefaults.standard.set(liveWanted, forKey: Self.liveKey) }
    }
    /// The server can only start a Live Activity through push from iOS 17.2, and only if the user allows them.
    var liveAvailable: Bool {
        guard #available(iOS 17.2, *) else { return false }
        return ActivityAuthorizationInfo().areActivitiesEnabled
    }
    var prefs: PushPrefs { PushPrefs(muted: muted.sorted(), live: liveWanted && liveAvailable) }
    /// The current push-to-start token of request activities (hex).
    private var liveStartToken: String?
    private var watchedActivities = Set<String>()
    private var apnsToken: String?
    /// One sync at a time: each one may spend a registration of the relay's daily limit.
    private var syncing = false
    private let store = PushStore.shared()
    private let servers = ServerStore.shared()
    /// A notification tapped before the app was ready.
    private var pendingOpen: (server: String?, link: URL)?

    weak var model: AppModel? {
        didSet {
            if let open = pendingOpen, let model { pendingOpen = nil; model.openNotification(serverId: open.server, url: open.link) }
        }
    }

    // MARK: - lifecycle

    /// At launch: APNs may hand out a new token, so ask for it every time.
    func launched() {
        observeLiveActivities()
        Task {
            await refreshAuthorization()
            if wanted, authorization.allowsPush { UIApplication.shared.registerForRemoteNotifications() }
        }
    }

    func refreshAuthorization() async {
        authorization = await UNUserNotificationCenter.current().notificationSettings().authorizationStatus
    }

    func tokenReceived(_ token: Data) {
        let hex = token.map { String(format: "%02x", $0) }.joined()
        guard hex != apnsToken else { return }
        apnsToken = hex
        Task { await syncCurrent() }
    }

    func registrationFailed(_ error: Error) {
        #if targetEnvironment(simulator)
        lastError = L("A szimulátor nem kap push-értesítést; próbáld valódi eszközön.")
        #else
        lastError = error.localizedDescription
        #endif
    }

    // MARK: - settings

    func enable() async {
        lastError = nil
        do {
            _ = try await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge])
        } catch {
            lastError = error.localizedDescription
        }
        await refreshAuthorization()
        guard authorization.allowsPush else {
            wanted = false
            lastError = L("Az értesítések le vannak tiltva. Engedélyezd őket az iOS Beállítások appban a Kinoránál.")
            return
        }
        wanted = true
        UIApplication.shared.registerForRemoteNotifications()
        if apnsToken != nil { await syncCurrent() }
    }

    /// Stops every server from sending here. A server that cannot be reached keeps its
    /// pairing on the device and is asked again on the next start.
    func disable() async {
        wanted = false
        lastError = nil
        working = true
        defer { working = false }
        for p in store.pairings { await unpair(p) }
        paired = false
    }

    func setMuted(_ category: String, _ off: Bool) async {
        if off { muted.insert(category) } else { muted.remove(category) }
        await sendPrefs()
    }

    func setLive(_ on: Bool) async {
        liveWanted = on
        await sendPrefs()
    }

    /// Tells every paired server what this device wants now.
    private func sendPrefs() async {
        lastError = nil
        for p in store.pairings {
            guard let saved = servers.servers.first(where: { $0.id == p.savedServerId }),
                  let t = servers.token(for: saved.id) else { continue }
            do {
                try await KinoraClient(server: saved, token: t).setPushPrefs(prefs)
            } catch {
                if saved.id == model?.server?.id { lastError = error.localizedDescription }
            }
        }
    }

    // MARK: - live activities

    /// Hands the relay the tokens it needs to start and update request activities: the app-wide
    /// push-to-start token, and each running activity's own token.
    private func observeLiveActivities() {
        if #available(iOS 17.2, *) {
            Task {
                for await data in Activity<RequestActivityAttributes>.pushToStartTokenUpdates {
                    liveStartToken = data.map { String(format: "%02x", $0) }.joined()
                    for p in store.pairings { await handInStartToken(p) }
                }
            }
        }
        Task {
            for a in Activity<RequestActivityAttributes>.activities { watch(a) }
            for await a in Activity<RequestActivityAttributes>.activityUpdates { watch(a) }
        }
    }

    private func watch(_ activity: Activity<RequestActivityAttributes>) {
        guard watchedActivities.insert(activity.id).inserted else { return }
        let attrs = activity.attributes
        Task {
            for await data in activity.pushTokenUpdates {
                guard let p = store.pairing(relayServer: attrs.s), let relay = PushRelay(p.relayUrl) else { continue }
                try? await relay.liveToken(handle: p.handle, token: data.map { String(format: "%02x", $0) }.joined(), ref: attrs.ref)
            }
        }
    }

    private func handInStartToken(_ p: PushPairing) async {
        guard let token = liveStartToken, p.liveStartToken != token, let relay = PushRelay(p.relayUrl) else { return }
        do {
            try await relay.liveToken(handle: p.handle, token: token)
            store.setLiveStartToken(token, saved: p.savedServerId)
        } catch {}
    }

    // MARK: - pairing

    /// Pairs the current server when notifications are on, and drops leftovers when they are off.
    func syncCurrent() async {
        guard wanted else {
            for p in store.pairings { await unpair(p) }
            return
        }
        guard !syncing, let client = model?.client, let server = model?.server, servers.token(for: server.id) != nil else { return }
        syncing = true
        defer { syncing = false }
        await sync(client, server)
        // Pairings of other servers made with an older APNs token stop working; refresh them too.
        guard let token = apnsToken else { return }
        for p in store.pairings where p.savedServerId != server.id && p.apnsToken != token {
            guard let saved = servers.servers.first(where: { $0.id == p.savedServerId }),
                  let t = servers.token(for: saved.id) else { store.remove(saved: p.savedServerId); continue }
            await sync(KinoraClient(server: saved, token: t), saved, quiet: true)
        }
    }

    private func sync(_ client: KinoraClient, _ server: SavedServer, quiet: Bool = false) async {
        guard authorization.allowsPush, let token = apnsToken else { return }
        let current = !quiet
        if current { working = true }
        defer { if current { working = false } }
        do {
            let st = try await client.pushStatus()
            if current { status = st }
            guard st.enabled, let relayUrl = st.relayUrl, let relayServer = st.serverId, let relay = PushRelay(relayUrl) else {
                if current { paired = false }
                return
            }
            let env = PushEnvironment.current()
            var pairing: PushPairing
            if let p = store.pairing(saved: server.id), p.relayServerId == relayServer, p.relayUrl == relayUrl,
               p.apnsToken == token, p.environment == env {
                if st.paired == true, store.key(relayServer: relayServer) != nil {
                    if current { paired = true; lastError = nil }
                    // Keeps the language and the choices fresh; an older server without the endpoint just says no.
                    try? await client.setPushPrefs(prefs)
                    await handInStartToken(p)
                    return
                }
                // The server forgot this device (a new session): the relay's handle still holds.
                pairing = p
            } else {
                let handle = try await register(relay, server: relayServer, token: token, environment: env)
                pairing = PushPairing(savedServerId: server.id, relayUrl: relayUrl, relayServerId: relayServer,
                                      handle: handle, apnsToken: token, environment: env)
            }
            let key = PushCrypto.newKey()
            try await client.pairPush(handle: pairing.handle, key: key, prefs: prefs)
            pairing.updated = .now
            store.save(pairing, key: key)
            if current { paired = true; lastError = nil }
            await handInStartToken(pairing)
        } catch {
            if current { paired = false; lastError = error.localizedDescription }
        }
    }

    /// Registers this device at the relay for one server, proving with App Attest that it
    /// is the official app: an attestation the first time, an assertion after that.
    private func register(_ relay: PushRelay, server: String, token: String, environment: String) async throws -> String {
        let attest = DCAppAttestService.shared
        guard attest.isSupported else { throw PushSetupError.attestUnavailable }
        var lastError: Error = PushSetupError.attestUnavailable
        for _ in 0..<3 {
            var key: PushStore.AttestKey
            if let k = store.attest, k.environment == environment {
                key = k
            } else {
                key = PushStore.AttestKey(id: try await attest.generateKey(), attested: false, environment: environment)
                store.attest = key
            }
            var r = PushDeviceRegistration(keyId: key.id, serverId: server, apnsToken: token, environment: environment,
                                           challenge: try await relay.challenge())
            let hash = PushRelay.clientDataHash(r.clientData)
            do {
                if key.attested {
                    r.assertion = try await attest.generateAssertion(key.id, clientDataHash: hash).base64URLEncoded
                } else {
                    r.attestation = try await attest.attestKey(key.id, clientDataHash: hash).base64URLEncoded
                }
                let handle = try await relay.register(r)
                if !key.attested { key.attested = true; store.attest = key }
                return handle
            } catch let e as PushRelayError where e.code == "key_already_attested" {
                key.attested = true
                store.attest = key
                lastError = e
            } catch let e as PushRelayError where e.code == "unknown_key" {
                store.attest = nil
                lastError = e
            } catch let e as DCError where e.code == .invalidKey || e.code == .invalidInput {
                store.attest = nil
                lastError = e
            }
        }
        throw lastError
    }

    private func unpair(_ p: PushPairing) async {
        guard let saved = servers.servers.first(where: { $0.id == p.savedServerId }),
              let token = servers.token(for: saved.id) else { return store.remove(saved: p.savedServerId) }
        do {
            try await KinoraClient(server: saved, token: token).unpairPush()
        } catch KinoraError.unauthorized {
        } catch KinoraError.http(let code, _) where code == 404 || code == 403 {
        } catch {
            return
        }
        store.remove(saved: p.savedServerId)
    }

    /// Before the session ends (logout, forgetting the server): the server deletes the pairing
    /// with the session anyway; this one tells it right away and drops the key.
    func signedOut(_ saved: SavedServer, client: KinoraClient?) async {
        if store.pairing(saved: saved.id) != nil { try? await client?.unpairPush() }
        store.remove(saved: saved.id)
        if model?.server?.id == saved.id { paired = false; status = nil }
    }

    // MARK: - taps

    func opened(server: String?, link: String?) {
        guard let link, let url = URL(string: link) else { return }
        if let model { model.openNotification(serverId: server, url: url) } else { pendingOpen = (server, url) }
    }
}

enum PushSetupError: Error, LocalizedError {
    case attestUnavailable

    var errorDescription: String? {
        L("Ezen az eszközön nem érhető el az App Attest, ezért az értesítések nem kapcsolhatók be.")
    }
}

extension UNAuthorizationStatus {
    var allowsPush: Bool { self == .authorized || self == .provisional || self == .ephemeral }
}

/// Receives the APNs token and the notification taps.
final class AppDelegate: NSObject, UIApplicationDelegate, UNUserNotificationCenterDelegate {
    func application(_ application: UIApplication, didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        UNUserNotificationCenter.current().delegate = self
        PushManager.shared.launched()
        return true
    }

    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        PushManager.shared.tokenReceived(deviceToken)
    }

    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
        PushManager.shared.registrationFailed(error)
    }

    /// In the foreground too, as a banner.
    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification) async -> UNNotificationPresentationOptions {
        [.banner, .list, .sound]
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        let info = response.notification.request.content.userInfo
        let server = info[PushKeys.server] as? String
        let link = info[PushKeys.link] as? String
        await PushManager.shared.opened(server: server, link: link)
    }
}
