import SwiftUI
import UIKit
#if os(iOS)
import WidgetKit
#elseif os(tvOS)
import TVServices
#endif
import KinoraKit

/// App-wide state: which server, which session, which screen.
@MainActor @Observable
final class AppModel {
    enum Screen: Equatable {
        case launching
        case servers
        case login
        case quickConnect
        case profiles
        case home
        /// The server can't be reached, but the last profile used here has downloads: only those (iOS).
        case offline
    }

    private(set) var screen: Screen = .launching {
        didSet {
            guard screen == .home else { return }
            syncOffline()
            #if os(iOS)
            if oldValue != .home { Task { await PushManager.shared.syncCurrent() } }
            #endif
        }
    }
    private(set) var servers: [SavedServer] = []
    private(set) var server: SavedServer?
    private(set) var status: ServerStatus?
    private(set) var me: Me? { didSet { meChanged() } }
    /// The other accounts of this server signed in on this device, the last used first.
    private(set) var accounts: [SavedAccount] = []
    /// The profile shown on the offline screen (the last one used with this server).
    private(set) var offlineProfile: Profile?
    /// A message shown once on the next screen (e.g. "session expired").
    var notice: String?
    /// The current playback; it outlives the player screen in picture in picture.
    private(set) var player: PlayerModel?
    /// The full-screen player is shown.
    var playerVisible = false
    /// The settings sheet is shown.
    var settingsVisible = false
    /// A watch party could not be started or joined (shown as an alert, or on the join screen).
    var partyError: String? { didSet { if partyError != nil { Haptic.error.play() } } }
    /// The join screen (code or QR) is shown; it shows join errors itself.
    var partyJoinVisible = false
    /// Joining a party is in progress (a spinner on the join screen).
    private(set) var partyJoining = false
    /// Bumped when a playback ends, so screens refresh progress and watched marks.
    private(set) var playbackGeneration = 0
    /// Bumped when the app's language changes; the root view is rebuilt with it.
    private(set) var languageGeneration = 0
    /// The languages the picker offers (the device's and the server's last list).
    private(set) var languages: [LanguageInfo] = Language.shared.known
    /// A language is being downloaded for the picker.
    private(set) var languageLoading = false

    enum Tab: Hashable { case home, libraries, search, discover, downloads }
    /// The selected tab and the home tab's stack, so links from widgets can open a title.
    var tab: Tab = .home
    /// Felfedezés: a title the library search did not find, handed over to the request search.
    var discoverQuery = ""
    /// Films and series can be requested from this profile (the server's acquisition is on).
    var canRequest: Bool { me?.canRequest == true }
    var homePath = NavigationPath()
    /// A link that arrived before the user reached home (still launching, picking a profile).
    private var pendingLink: KinoraLink?
    /// Where a pending play link starts (a handoff from another device); nil resumes.
    private var pendingStart: Double?

    private let store = ServerStore.shared()
    private(set) var client: KinoraClient?

    static let appVersion = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "0"

    var device: DeviceInfo {
        #if os(tvOS)
        DeviceInfo(deviceName: UIDevice.current.name, platform: "tvos", appVersion: Self.appVersion)
        #else
        DeviceInfo(deviceName: UIDevice.current.name, platform: "ios", appVersion: Self.appVersion)
        #endif
    }

    // MARK: - start

    func start() async {
        guard screen == .launching else { return }
        servers = store.servers
        guard let saved = store.current else { return screen = .servers }
        use(saved)
        guard store.token(for: saved.id) != nil else { return screen = .login }
        // Show the profile picker or home right away; a network error just leaves the
        // user on a retryable screen instead of logging them out.
        do {
            try await loadMe()
            screen = me?.profile == nil ? .profiles : .home
        } catch KinoraError.unauthorized {
            sessionLost()
        } catch {
            #if os(iOS)
            if enterOffline(saved) { return }
            #endif
            notice = error.localizedDescription
            screen = .servers
        }
        Task { await refreshStatus() }
    }

    // MARK: - language

    /// "" follows the system language; a code picks that language. Languages come from the server and stay on the device.
    var languageChoice: String {
        get { _ = languageGeneration; return Language.shared.choice }
        set {
            guard newValue != Language.shared.choice else { return }
            let before = Language.shared.code
            Language.shared.choice = newValue
            if Language.shared.code != before { languageChanged() }
            // picked one the device does not have yet: fetch it, then switch
            if !newValue.isEmpty, Language.shared.code != newValue, let client {
                languageLoading = true
                Task {
                    await syncLanguages(client)
                    languageLoading = false
                }
            }
        }
    }

    private func syncLanguages(_ c: KinoraClient) async {
        let before = Language.shared.code
        let changed = await Language.shared.sync(c)
        languages = Language.shared.known
        // a new version of the language in use applies quietly; a different language rebuilds the screens
        if Language.shared.code != before { languageChanged() } else if changed { reloadWidgets() }
    }

    private func languageChanged() {
        languages = Language.shared.known
        languageGeneration += 1
        reloadWidgets()
        #if os(iOS)
        KinoraShortcuts.updateAppShortcutParameters()
        #endif
    }

    // MARK: - servers

    /// What the user typed or picked from discovery → saved and current server.
    func connect(to input: String) async throws {
        let r = try await Discovery.resolve(input)
        let id = r.status.serverId ?? r.url
        var s = store.servers.first { $0.id == id } ?? SavedServer(id: id, url: r.url, name: r.status.serverName)
        s.url = r.url
        s.urls = ServerAddress.merge([r.url], s.urls, [r.status.publicUrl])
        s.name = r.status.serverName
        s.lastUsed = .now
        store.upsert(s)
        servers = store.servers
        use(store.current ?? s)
        status = r.status
        screen = store.token(for: id) == nil ? .login : .profiles
        if screen == .profiles { try? await loadMe() }
    }

    func open(_ saved: SavedServer) async throws {
        guard let reached = await Discovery.reach(saved) else {
            #if os(iOS)
            if store.token(for: saved.id) != nil {
                use(saved)
                if enterOffline(saved) { return }
            }
            #endif
            throw KinoraError.unreachable(ServerAddress.host(of: saved.url), isPrivate: ServerAddress.isPrivateHost(ServerAddress.host(of: saved.url)))
        }
        var s = reached
        s.lastUsed = .now
        store.upsert(s)
        servers = store.servers
        use(s)
        await refreshStatus()
        if store.token(for: s.id) == nil { screen = .login; return }
        do {
            try await loadMe()
            screen = me?.profile == nil ? .profiles : .home
        } catch KinoraError.unauthorized {
            sessionLost()
        }
    }

    func forget(_ saved: SavedServer) {
        #if os(iOS)
        let c = KinoraClient(server: saved, token: store.token(for: saved.id))
        Task { await PushManager.shared.signedOut(saved, client: c) }
        #endif
        store.remove(saved.id)
        #if os(iOS)
        LastProfile.forget(server: saved.id)
        #endif
        servers = store.servers
        if server?.id == saved.id { server = nil; client = nil; me = nil }
    }

    /// Switch between the auth screens of the current server.
    func go(_ s: Screen) {
        guard server != nil else { return showServers() }
        screen = s
    }

    func showServers() {
        settingsVisible = false
        servers = store.servers
        screen = .servers
    }

    private func use(_ s: SavedServer) {
        #if os(iOS)
        if let old = server, old.id != s.id { Spotlight.clear() }
        #endif
        server = s
        let c = KinoraClient(server: s, token: store.token(for: s.id))
        let store = self.store
        Task {
            await c.setOnAddressChange { moved in
                store.upsert(moved)
            }
        }
        client = c
        me = nil
        refreshAccounts()
        Task { await syncLanguages(c) }
        #if os(iOS)
        DownloadStore.shared.attach(c)
        Handoff.serverId = s.id
        #endif
    }

    private func refreshStatus() async {
        guard let server else { return }
        status = try? await Discovery.probe(server.url).status
    }

    // MARK: - login

    func login(username: String, password: String) async throws {
        guard let client, var server else { return }
        let session = try await client.login(username: username, password: password, device: device)
        server.username = username
        server.deviceId = session.deviceId
        server.lastUsed = .now
        await finishLogin(server, token: session.token)
        // A single profile without PIN: no need to ask "who's watching?".
        if let profiles = me?.profiles, profiles.count == 1, !profiles[0].hasPin,
           (try? await selectProfile(profiles[0], pin: nil)) != nil { return }
        screen = .profiles
    }

    func startQuickConnect() async throws -> QuickConnectStart {
        guard let client else { throw KinoraError.network }
        do {
            return try await client.post("/api/quickconnect/initiate", device)
        } catch KinoraError.http(403, _) {
            throw KinoraError.http(403, L("A belépés kóddal ki van kapcsolva ezen a szerveren."))
        }
    }

    /// One poll; returns the state. On approval it also finishes the login.
    func pollQuickConnect(_ start: QuickConnectStart) async throws -> QuickConnectState {
        guard let client, var server else { throw KinoraError.network }
        let secret = start.secret.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? start.secret
        let st: QuickConnectState
        do {
            st = try await client.get("/api/quickconnect/status?secret=" + secret)
        } catch KinoraError.http(let code, _) where code == 404 || code == 410 {
            return QuickConnectState(status: .expired)
        }
        if st.status == .approved, let token = st.token {
            server.username = st.user?.username
            server.deviceId = st.deviceId
            server.lastUsed = .now
            await finishLogin(server, token: token)
            screen = me?.profile != nil ? .home : .profiles
        }
        return st
    }

    private func finishLogin(_ s: SavedServer, token: String) async {
        store.upsert(s)
        store.setToken(token, for: s.id)
        servers = store.servers
        server = s
        await client?.setToken(token)
        try? await loadMe()
        // A fresh sign-in to an unlocked session opens the account switch again.
        if let me, me.profileLocked != true { store.setSwitchLocked(false, server: s.id) }
        // Signed in again to an account that was parked: the new session replaces the old one.
        if let user = me?.user, let old = store.parkedAccounts(for: s.id).first(where: { $0.userId == user.id }) {
            await forgetAccount(old)
        }
        refreshAccounts()
        reloadWidgets()
    }

    // MARK: - session

    func loadMe() async throws {
        guard let client else { return }
        me = try await client.me()
    }

    /// The server refuses it from a kids profile, on a device locked to one profile, and past the limit.
    var canAddProfile: Bool {
        guard let me else { return false }
        return me.profiles.count < ProfileCreate.maxProfiles && me.profileLocked != true && me.profile?.isKids != true
    }

    func createProfile(name: String, isKids: Bool) async throws {
        guard let client else { return }
        _ = try await client.createProfile(ProfileCreate(name: name, isKids: isKids))
        try await loadMe()
    }

    func selectProfile(_ p: Profile, pin: String?) async throws {
        guard let client else { return }
        do {
            try await client.selectProfile(p.id, pin: pin)
        } catch KinoraError.http(403, let message) where message.localizedCaseInsensitiveContains("PIN") {
            throw KinoraError.http(403, L("Hibás PIN."))
        }
        try await loadMe()
        screen = .home
        reloadWidgets()
    }

    func switchProfile() {
        settingsVisible = false
        player?.close()
        #if os(iOS)
        Spotlight.clear()
        #endif
        homePath = NavigationPath()
        tab = .home
        screen = .profiles
        // Leave the profile on the server too: otherwise a restart from the picker
        // drops the user back into the old profile.
        if let client, me?.profileLocked != true {
            Task { try? await client.leaveProfile() }
        }
    }

    func logout() async {
        guard let server else { return }
        settingsVisible = false
        player?.close()
        #if os(iOS)
        await PushManager.shared.signedOut(server, client: client)
        #endif
        await client?.logout()
        store.setToken(nil, for: server.id)
        #if os(iOS)
        LastProfile.forget(server: server.id) // signed out: no offline start on this account's downloads
        #endif
        await client?.setToken(nil)
        me = nil
        #if os(iOS)
        Spotlight.clear()
        #endif
        // Like the web: another account signed in on this device takes over.
        // Not after a session locked to one profile: the parked accounts stay closed then.
        if canSwitchAccount, let next = store.parkedAccounts(for: server.id).first {
            await switchAccount(to: next)
            return
        }
        screen = .login
        reloadWidgets()
    }

    // MARK: - accounts

    /// Switching accounts is refused on a device locked to one profile, as on the server.
    /// The lock is remembered past that session's sign-out, so leaving it does not open the others.
    var canSwitchAccount: Bool {
        guard me?.profileLocked != true else { return false }
        return !(server.map { store.isSwitchLocked(server: $0.id) } ?? false)
    }

    /// Empty while the switch is locked: the parked accounts are not offered then.
    private func refreshAccounts() {
        guard let server, !store.isSwitchLocked(server: server.id) else { accounts = []; return }
        accounts = store.parkedAccounts(for: server.id)
    }


    /// Keeps the active account signed in, set aside, and opens the login for another one.
    func addAccount() async {
        guard canSwitchAccount else { return await logout() }
        guard await parkCurrent() else { notice = L("A fiókot most nem lehet félretenni, mert a szerver nem érhető el. Próbáld újra később."); return }
        server?.username = nil // the login form starts empty, not with the parked account's name
        screen = .login
        reloadWidgets()
    }

    /// Makes a parked account the active one, without a password.
    func switchAccount(to account: SavedAccount) async {
        guard canSwitchAccount, var saved = server else { return }
        guard await parkCurrent() else { notice = L("A fiókot most nem lehet félretenni, mert a szerver nem érhető el. Próbáld újra később."); return }
        guard let token = store.unpark(account.userId, server: saved.id) else {
            server?.username = account.username
            refreshAccounts()
            notice = L("Ennek a fióknak lejárt a munkamenete. Lépj be újra.")
            screen = .login
            return
        }
        store.setToken(token, for: saved.id)
        await client?.setToken(token)
        // Saved before `me` loads, so an offline moment does not leave the server nameless.
        saved.username = account.username
        saved.lastUsed = .now
        store.upsert(saved)
        servers = store.servers
        server = servers.first { $0.id == saved.id } ?? saved
        refreshAccounts()
        do {
            try await loadMe()
        } catch KinoraError.unauthorized {
            // Signed out elsewhere (or the session expired): sign in again with that name.
            store.setToken(nil, for: saved.id)
            await client?.setToken(nil)
            server?.username = account.username
            notice = L("Ennek a fióknak lejárt a munkamenete. Lépj be újra.")
            screen = .login
            reloadWidgets()
            return
        } catch {
            // Offline for a moment: the picker loads `me` again when it appears.
            screen = .profiles
            reloadWidgets()
            return
        }
        if let name = me?.user.username, name != saved.username {
            saved.username = name
            store.upsert(saved)
            servers = store.servers
            server = servers.first { $0.id == saved.id } ?? saved
        }
        reloadWidgets()
        if let profiles = me?.profiles, profiles.count == 1, !profiles[0].hasPin,
           (try? await selectProfile(profiles[0], pin: nil)) != nil { return }
        screen = .profiles
    }

    /// Forgets a parked account: signs it out on the server, and drops its token here.
    func forgetAccount(_ account: SavedAccount) async {
        guard let saved = server else { return }
        if let token = store.unpark(account.userId, server: saved.id) {
            await KinoraClient(server: saved, token: token).logout()
        }
        refreshAccounts()
    }

    /// Sets the active account aside (still signed in) and leaves its screens.
    /// Returns false, and changes nothing, when the account cannot be told apart from the others
    /// because the server does not answer: dropping its token then would strand its session.
    /// Nothing to park (already signed out) counts as done.
    private func parkCurrent() async -> Bool {
        guard let saved = server, let token = store.token(for: saved.id) else { return true }
        if me == nil { try? await loadMe() }
        guard me?.user != nil else { return false }
        settingsVisible = false
        player?.close()
        homePath = NavigationPath()
        tab = .home
        #if os(iOS)
        // The push pairing belongs to the active account; the next one pairs again on home.
        await PushManager.shared.signedOut(saved, client: client)
        Spotlight.clear()
        #endif
        if let user = me?.user, store.park(token, user: user, server: saved.id) {
            // Back to "who's watching?" when it comes back, like a fresh sign-in.
            if me?.profile != nil { try? await client?.leaveProfile() }
        } else {
            await client?.logout() // could not keep it: do not leave a session nobody can reach
        }
        store.setToken(nil, for: saved.id)
        await client?.setToken(nil)
        me = nil
        refreshAccounts()
        return true
    }

    // MARK: - settings

    /// Saves profile settings and refreshes `me`, so the player picks them up next time.
    func updateProfile(_ update: ProfileUpdate) async throws {
        guard let id = me?.profile?.id else { return }
        _ = try await api { try await $0.updateProfile(id, update) }
        try await loadMe()
    }

    /// Adds another address (e.g. the public one) to the current server.
    /// Throws when a different Kinora server answers there.
    func addAddress(_ input: String) async throws {
        guard var saved = server else { return }
        let found = try await Discovery.resolve(input)
        if let other = found.status.serverId, !saved.id.hasPrefix("http"), other != saved.id {
            throw KinoraError.http(409, L("Ezen a címen egy másik Kinora-szerver válaszol."))
        }
        saved.urls = ServerAddress.merge(saved.urls, [found.url])
        store.upsert(saved)
        servers = store.servers
        server = servers.first { $0.id == saved.id } ?? saved
        await client?.setAddresses(server?.urls ?? saved.urls)
    }

    /// The token was rejected: back to login, keep the server.
    func sessionLost() {
        settingsVisible = false
        if let server {
            store.setToken(nil, for: server.id)
            #if os(iOS)
            LastProfile.forget(server: server.id)
            #endif
        }
        me = nil
        refreshAccounts()
        notice = KinoraError.unauthorized.errorDescription
        screen = .login
        reloadWidgets()
    }

    /// Runs a server call; a rejected token sends the user back to login.
    func guarded<T>(_ work: () async throws -> T) async throws -> T {
        do { return try await work() }
        catch KinoraError.unauthorized { sessionLost(); throw KinoraError.unauthorized }
    }

    // MARK: - playback

    /// Plays a movie or episode; `from` nil resumes where the profile left off.
    func play(_ item: Item, from start: Double? = nil, party: PartySession? = nil) {
        guard let client else { return }
        player?.close()
        #if os(iOS)
        // Siri learns what the user watches and offers it on the lock screen and in Spotlight.
        if party == nil { IntentRouter.donate(item) }
        #endif
        var resume = item.progress != nil ? (item.userData?.position ?? 0) : 0
        // watched offline, not sent yet: the queued position is the newer one
        if let queued = ProgressQueue.pending[item.id] { resume = queued.ended ? 0 : queued.position }
        // A downloaded copy plays from the device: no server load, works without a connection.
        #if os(iOS)
        let local = DownloadStore.shared.localInfo(for: item)
        #else
        let local: PlaybackInfo? = nil
        #endif
        // A party streams from the server, so everyone sees the same cut and the same segments.
        let p = PlayerModel(item: item, start: start ?? resume, client: client, profile: me?.profile ?? offlineProfile,
                            local: party == nil ? local : nil)
        p.party = party
        party?.player = p
        p.onClose = { [weak self, weak p] in
            guard let self, self.player === p else { return }
            self.player = nil
            self.playerVisible = false
            self.playbackGeneration += 1
            self.reloadWidgets(after: .seconds(2))
        }
        // Picture in picture: the full-screen player steps aside and comes back on restore.
        p.onPiPStart = { [weak self] in self?.playerVisible = false }
        p.onPiPRestore = { [weak self] in self?.playerVisible = true }
        player = p
        playerVisible = true
        Task { await p.begin() }
    }

    // MARK: - watch party

    /// Opens a room on a movie or episode and joins it as the host. From the player the
    /// playback goes on where it is; from the detail page it starts at the resume position.
    func startParty(_ item: Item) {
        guard let client, !partyJoining else { return }
        partyJoining = true
        Task {
            defer { partyJoining = false }
            do {
                let info = try await guarded { try await client.createParty(itemId: item.id) }
                if let p = player, p.item.id == item.id, p.party == nil, !p.isOffline {
                    let session = PartySession(code: info.code, client: client)
                    session.connect()
                    _ = await session.firstState()
                    session.adopt(position: p.livePosition, playing: p.isPlaying)
                    p.party = session
                    session.player = p
                    session.panelOpen = true
                    Haptic.success.play()
                } else {
                    let resume = item.progress != nil ? (item.userData?.position ?? 0) : 0
                    await enterParty(info.code, item: item, client: client, hostFrom: resume)
                }
                #if os(iOS)
                // In a FaceTime call the system offers to SharePlay the new room.
                if SharePlay.shared.callActive, let serverId = server?.id, player?.party?.code == info.code {
                    await SharePlay.shared.share(code: info.code, serverId: serverId, title: player?.title ?? item.title)
                }
                #endif
            } catch {
                partyError = error.localizedDescription
            }
        }
    }

    /// The room behind a code (typed, or from a kinora://party link).
    func joinParty(_ typed: String) {
        guard let client else { return }
        guard let code = PartySync.normalizedCode(typed) else {
            partyError = L("A kód 6 betű vagy szám.")
            return
        }
        guard !partyJoining else { return }
        partyJoining = true
        Task {
            defer { partyJoining = false }
            do {
                let info = try await guarded { try await client.party(code) }
                let detail = try await guarded { try await client.item(info.itemId) }
                await enterParty(code, item: detail.item, client: client)
            } catch KinoraError.http(404, _) {
                partyError = L("Nincs ilyen watch party. Ellenőrizd a kódot.")
            } catch {
                partyError = error.localizedDescription
            }
        }
    }

    /// Connects first, so playback starts where the room is instead of seeking there.
    private func enterParty(_ code: String, item: Item, client: KinoraClient, hostFrom: Double? = nil) async {
        let session = PartySession(code: code, client: client)
        session.connect()
        let st = await session.firstState()
        if session.gone {
            session.leave()
            partyError = session.status ?? L("Nem sikerült csatlakozni a watch partyhoz.")
            return
        }
        Haptic.success.play()
        if let hostFrom {
            session.adopt(position: hostFrom, playing: true)
            session.panelOpen = true
            play(item, from: hostFrom, party: session)
            return
        }
        var target = item
        if let st, st.itemId != item.id, let d = try? await client.item(st.itemId) { target = d.item }
        play(target, from: st?.pos ?? 0, party: session)
    }

    /// A guarded call on the current server's client (browse screens).
    func api<T: Sendable>(_ work: (KinoraClient) async throws -> T) async throws -> T {
        guard let client else { throw KinoraError.network }
        return try await guarded { try await work(client) }
    }

    // MARK: - offline

    /// Downloads belong to profiles: the download store follows the profile in use, and the last one is kept
    /// per server, so the app can start without a connection on that profile's downloads.
    private func meChanged() {
        // A session locked to one profile closes the account switch on this device until
        // a fresh sign-in to an unlocked session (see `ServerStore.isSwitchLocked`).
        if me?.profileLocked == true, let id = server?.id, !store.isSwitchLocked(server: id) {
            store.setSwitchLocked(true, server: id)
            refreshAccounts()
        }
        #if os(iOS)
        guard let me else { return } // between servers: the store keeps the last profile until the next one
        DownloadStore.shared.use(profile: me.profile)
        if let p = me.profile, let id = server?.id { LastProfile.save(p, server: id) }
        #endif
    }

    #if os(iOS)
    /// No connection to the server: when the last profile used with it has downloads, they are shown and play
    /// from the device. Switching profiles needs the server (a PIN can only be checked there).
    private func enterOffline(_ s: SavedServer) -> Bool {
        guard let p = LastProfile.load(server: s.id) else { return false }
        DownloadStore.shared.use(profile: p)
        guard !DownloadStore.shared.visible.isEmpty else { return false }
        offlineProfile = p
        screen = .offline
        return true
    }

    /// From the offline screen: try the server again; on success the app goes on as usual.
    func retryOnline() async {
        guard server != nil else { return }
        do {
            try await loadMe()
            offlineProfile = nil
            screen = me?.profile == nil ? .profiles : .home
        } catch KinoraError.unauthorized {
            offlineProfile = nil
            sessionLost()
        } catch {
            notice = error.localizedDescription
        }
    }

    /// From the offline screen: back to the server list.
    func leaveOffline() {
        offlineProfile = nil
        screen = .servers
    }
    #endif

    /// Sends progress saved while offline; on home, on foreground and when the network is back.
    func syncOffline() {
        guard screen == .home, let client else { return }
        Task { await ProgressQueue.flush(client) }
    }

    // MARK: - widgets and links

    /// Widgets show the profile's progress; ask for fresh data after it changed. The delay
    /// lets the last progress report reach the server first.
    func reloadWidgets(after delay: Duration = .zero) {
        Task {
            if delay > .zero { try? await Task.sleep(for: delay) }
            #if os(iOS)
            WidgetCenter.shared.reloadAllTimelines()
            #elseif os(tvOS)
            TVTopShelfContentProvider.topShelfContentDidChange()
            #endif
        }
    }

    /// kinora://item/42, kinora://play/42 or kinora://party/ABC234; waits until the user is on home.
    func open(_ url: URL) {
        #if os(iOS)
        // kinora://downloads, from the downloads Live Activity.
        if url.scheme == KinoraLink.scheme, url.host() == "downloads" {
            if screen == .home, player == nil || !playerVisible { tab = .downloads }
            return
        }
        #endif
        guard let link = KinoraLink(url: url) else { return }
        pendingLink = link
        pendingStart = nil
        #if os(iOS)
        // An invite from Messages names its server (kinora://party/ABC234?server=<id>): run it there.
        if let serverId = KinoraLink.serverId(in: url), screen != .launching || store.currentID != serverId {
            return goToServer(serverId, elsewhere: L("Ez a meghívó egy másik Kinora-szerverre szól, amelyre ezen az eszközön nem vagy bejelentkezve."))
        }
        #endif
        handlePendingLink()
    }

    #if os(iOS)
    /// Someone in the FaceTime call shared a watch party (or this device did): join its room.
    func sharePlayRoom(_ a: WatchTogether) {
        guard player?.party?.code != a.code else { return }
        joinRoom(a.code, on: a.serverId,
                 elsewhere: L("Ez a SharePlay egy másik Kinora-szerver watch partyja, amelyre ezen az eszközön nem vagy bejelentkezve."))
    }

    /// A title handed off from the user's other device: same server, same second (or same room).
    func continueHandoff(_ h: Handoff.Incoming) {
        pendingLink = h.party.map(KinoraLink.party) ?? .play(h.itemId)
        pendingStart = h.party == nil ? h.position : nil
        goToServer(h.serverId, elsewhere: L("Ez a cím egy másik Kinora-szerveren megy, amelyre ezen az eszközön nem vagy bejelentkezve."))
    }

    /// A tapped notification: its title, on the server that sent it.
    func openNotification(serverId: String?, url: URL) {
        guard let link = KinoraLink(url: url) else { return }
        pendingLink = link
        pendingStart = nil
        // Still starting up on that server: home picks the link up when it appears.
        guard let serverId, screen != .launching || store.currentID != serverId else { return }
        goToServer(serverId, elsewhere: L("Ez az értesítés egy másik Kinora-szerverről jött, amelyre ezen az eszközön nem vagy bejelentkezve."))
    }

    private func joinRoom(_ code: String, on serverId: String, elsewhere: String) {
        pendingLink = .party(code)
        pendingStart = nil
        goToServer(serverId, elsewhere: elsewhere)
    }

    /// Runs the pending link on the given server: right away on the current one, after
    /// switching on another saved one with a login, and not at all on an unknown one.
    private func goToServer(_ serverId: String, elsewhere: String) {
        if server?.id == serverId { return handlePendingLink() }
        guard let saved = store.servers.first(where: { $0.id == serverId }),
              store.token(for: saved.id) != nil else {
            pendingLink = nil
            partyError = elsewhere
            return
        }
        player?.close()
        Task {
            do { try await open(saved) } catch { partyError = error.localizedDescription }
        }
    }
    #endif

    func handlePendingLink() {
        guard screen == .home, let link = pendingLink, client != nil else { return }
        pendingLink = nil
        let start = pendingStart
        pendingStart = nil
        Task {
            switch link {
            case .item(let id):
                guard let detail = try? await api({ try await $0.item(id) }) else { return }
                playerVisible = false
                tab = .home
                homePath = NavigationPath([detail.item])
            case .party(let code):
                joinParty(code)
            case .play(let id):
                guard let detail = try? await api({ try await $0.item(id) }) else { return }
                // A series or season has nothing to play itself: show it instead.
                if detail.item.kind == .movie || detail.item.kind == .episode {
                    play(detail.item, from: start)
                } else {
                    tab = .home
                    homePath = NavigationPath([detail.item])
                }
            }
        }
    }
}
