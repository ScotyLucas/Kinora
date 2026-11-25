import KinoraKit
import Messages
import Observation
import UIKit

/// What the SwiftUI side asks of Messages.
@MainActor
protocol MessagesActions: AnyObject {
    func expand()
    func collapse()
    func insert(_ message: MSMessage) async throws
    func open(_ url: URL)
}

enum MessagesTab: Hashable, CaseIterable {
    case party, recommend, stickers
}

/// A title picked for a message: shown on the confirm sheet, then sent.
struct Pick: Identifiable, Equatable {
    enum Mode { case party, recommend }
    var item: Item
    var mode: Mode
    var id: String { "\(mode)-\(item.id)" }
}

/// A received (or sent) Kinora bubble, opened.
struct Received: Equatable {
    var invite: KinoraInvite
    var image: UIImage?
    /// This device sent it: the button starts the room instead of joining it.
    var mine: Bool
    /// The invite's server is saved on this device with a login.
    var known: Bool
}

@MainActor @Observable
final class MessagesModel {
    enum Phase: Equatable { case loading, signedOut, offline, ready }

    weak var actions: MessagesActions?

    var style: MSMessagesAppPresentationStyle = .compact
    var tab: MessagesTab = .party
    private(set) var phase: Phase = .loading
    private(set) var server: SavedServer?
    private(set) var continueItems: [Item] = []
    private(set) var recentItems: [Item] = []

    var query = "" { didSet { if query != oldValue { scheduleSearch() } } }
    private(set) var results: [Item] = []
    private(set) var searching = false

    var pick: Pick?
    var everyone = false
    private(set) var sending = false
    var error: String?
    /// The invite just put into the message field: the panel offers to start the room.
    var sent: KinoraInvite?
    var received: Received?

    private var client: KinoraClient?
    private var loadTask: Task<Void, Never>?
    private var searchTask: Task<Void, Never>?
    private let images = ImageCache()

    // MARK: - loading

    func loadIfNeeded() {
        guard loadTask == nil, phase != .ready else { return }
        loadTask = Task { await load(); loadTask = nil }
    }

    func reload() {
        loadTask?.cancel()
        loadTask = Task { await load(); loadTask = nil }
    }

    private func load() async {
        Language.shared.reloadIfNeeded() // the app may have switched or updated the language
        let store = ServerStore.shared()
        guard let server = store.current, let token = store.token(for: server.id) else {
            phase = .signedOut
            return
        }
        self.server = server
        let client = KinoraClient(server: server, token: token)
        self.client = client
        if continueItems.isEmpty && recentItems.isEmpty { phase = .loading }
        do {
            let home = try await client.home()
            continueItems = Array(WidgetFeed.continueWatching(home).prefix(12))
            recentItems = Array(WidgetFeed.recentlyAdded(home).prefix(24))
            phase = .ready
        } catch KinoraError.unauthorized {
            phase = .signedOut
        } catch is CancellationError {
        } catch {
            phase = .offline
        }
    }

    // MARK: - search

    private func scheduleSearch() {
        searchTask?.cancel()
        let q = query.trimmingCharacters(in: .whitespaces)
        guard q.count >= 2, let client else { results = []; searching = false; return }
        searching = true
        searchTask = Task {
            try? await Task.sleep(for: .milliseconds(300))
            guard !Task.isCancelled else { return }
            let found = (try? await client.search(q)) ?? []
            guard !Task.isCancelled else { return }
            results = found
            searching = false
        }
    }

    // MARK: - images

    func image(_ item: Item, _ kind: ImageKind) async -> UIImage? {
        guard let client else { return nil }
        return await images.image(item, kind, client: client)
    }

    // MARK: - sending

    func choose(_ item: Item, mode: Pick.Mode) {
        error = nil
        everyone = false
        pick = Pick(item: item, mode: mode)
        actions?.expand()
    }

    func cancelPick() {
        pick = nil
        error = nil
        actions?.collapse()
    }

    /// A series has nothing to play itself: its party plays the next episode.
    private func playable(_ item: Item, client: KinoraClient) async throws -> Item {
        switch item.kind {
        case .movie, .episode: return item
        default:
            guard let next = try await client.item(item.id).playNext else {
                throw MessagesError(L("Ebből a sorozatból már minden részt láttál."))
            }
            return next
        }
    }

    /// Opens the room (party) or builds the recommendation, and puts the bubble into the message field.
    func send() async {
        guard let pick, let client, let server, !sending else { return }
        sending = true
        error = nil
        defer { sending = false }
        do {
            let invite: KinoraInvite
            let art: Item
            // The address that answers now (after a failover it may be the public one).
            let base = await client.baseURL
            switch pick.mode {
            case .party:
                let target = try await playable(pick.item, client: client)
                let room = try await client.createParty(itemId: target.id, everyone: everyone)
                invite = KinoraInvite(kind: .party(code: room.code), serverId: server.id, serverURL: base,
                                      itemId: target.id, title: Self.title(of: target), subtitle: Self.subtitle(of: target))
                art = target
            case .recommend:
                invite = KinoraInvite(kind: .title, serverId: server.id, serverURL: base,
                                      itemId: pick.item.id, title: Self.title(of: pick.item), subtitle: Self.subtitle(of: pick.item))
                art = pick.item
            }
            let message = try await message(for: invite, art: art)
            try await actions?.insert(message)
            SentLog.add(message.url)
            Haptics.success()
            self.pick = nil
            if invite.partyCode != nil { sent = invite }
            actions?.collapse()
        } catch let e as MessagesError {
            error = e.text
        } catch {
            self.error = (error as? KinoraError)?.errorDescription ?? L("Nem sikerült elküldeni. Próbáld újra.")
        }
    }

    private func message(for invite: KinoraInvite, art item: Item) async throws -> MSMessage {
        guard let url = invite.messageURL else { throw MessagesError(L("Nem sikerült elküldeni. Próbáld újra.")) }
        let episode = item.kind == .episode
        var backdrop = await image(item, episode ? .thumb : .backdrop)
        if backdrop == nil, let series = item.seriesId, let client {
            backdrop = await images.image(id: series, .backdrop, client: client)
        }
        let poster: UIImage?
        if let series = item.seriesId, episode, let client {
            poster = await images.image(id: series, .poster, client: client)
        } else {
            poster = await image(item, .poster)
        }
        let layout = MSMessageTemplateLayout()
        layout.image = MessageCard.render(invite: invite, backdrop: backdrop, poster: poster)
        layout.caption = invite.title
        if let code = invite.partyCode {
            layout.subcaption = L("Watch party · kód: {0}", code)
        } else {
            layout.subcaption = invite.subtitle.isEmpty ? L("Ajánlás a Kinorából") : invite.subtitle
        }
        layout.trailingSubcaption = server?.name
        let message = MSMessage(session: MSSession())
        message.url = url
        message.layout = layout
        message.summaryText = invite.partyCode == nil
            ? L("Ajánlás: {0}", invite.title)
            : L("Watch party: {0}", invite.title)
        return message
    }

    // MARK: - opening

    /// The room just sent: the sender opens it first and becomes its host.
    func startSent() {
        guard let sent else { return }
        actions?.open(sent.appLink)
        self.sent = nil
    }

    func show(selected message: MSMessage?, in conversation: MSConversation) {
        guard let message, let url = message.url, let invite = KinoraInvite(messageURL: url) else {
            received = nil
            return
        }
        let store = ServerStore.shared()
        let known = store.servers.contains { $0.id == invite.serverId && store.token(for: $0.id) != nil }
        // The identifiers do not always match for a bubble this device sent, so the extension
        // also remembers what it sent.
        let mine = message.senderParticipantIdentifier == conversation.localParticipantIdentifier || SentLog.contains(url)
        received = Received(invite: invite, image: (message.layout as? MSMessageTemplateLayout)?.image,
                            mine: mine, known: known)
    }

    func openReceived() {
        guard let received else { return }
        actions?.open(received.invite.appLink)
    }

    func openReceivedInBrowser() {
        guard let url = received?.invite.webLink else { return }
        actions?.open(url)
    }

    func openApp() {
        actions?.open(URL(string: "\(KinoraLink.scheme)://")!)
    }

    // MARK: - text

    static func title(of item: Item) -> String {
        item.kind == .episode ? (item.seriesTitle ?? item.title) : item.title
    }

    static func subtitle(of item: Item) -> String {
        if item.kind == .episode {
            return [item.episodeLabel, item.title].compactMap { $0 }.joined(separator: " · ")
        }
        return item.year.map(String.init) ?? ""
    }
}

/// The last bubbles this device put into a conversation, by message URL.
enum SentLog {
    private static let key = "kinora.messages.sent"
    private static let limit = 60

    static func add(_ url: URL?) {
        guard let url else { return }
        var list = UserDefaults.standard.stringArray(forKey: key) ?? []
        list.removeAll { $0 == url.absoluteString }
        list.append(url.absoluteString)
        UserDefaults.standard.set(Array(list.suffix(limit)), forKey: key)
    }

    static func contains(_ url: URL) -> Bool {
        UserDefaults.standard.stringArray(forKey: key)?.contains(url.absoluteString) ?? false
    }
}

struct MessagesError: Error {
    var text: String
    init(_ text: String) { self.text = text }
}

/// Posters and backdrops, decoded and shrunk once, kept for the life of the extension.
actor ImageCache {
    private var cache: [String: UIImage] = [:]
    private var missing: Set<String> = []

    func image(_ item: Item, _ kind: ImageKind, client: KinoraClient) async -> UIImage? {
        await image(id: item.id, kind, client: client)
    }

    func image(id: Int, _ kind: ImageKind, client: KinoraClient) async -> UIImage? {
        let key = "\(id)-\(kind.rawValue)"
        if let hit = cache[key] { return hit }
        if missing.contains(key) { return nil }
        guard let data = try? await client.image(id, kind), let full = UIImage(data: data) else {
            missing.insert(key)
            return nil
        }
        let img = Self.shrink(full, width: kind == .poster ? 360 : 900)
        cache[key] = img
        return img
    }

    /// Messages extensions have a tight memory budget.
    private static func shrink(_ img: UIImage, width: CGFloat) -> UIImage {
        guard img.size.width > width else { return img }
        let size = CGSize(width: width, height: (img.size.height * width / img.size.width).rounded())
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        return UIGraphicsImageRenderer(size: size, format: format).image { _ in img.draw(in: CGRect(origin: .zero, size: size)) }
    }
}

enum Haptics {
    @MainActor static func success() { UINotificationFeedbackGenerator().notificationOccurred(.success) }
    @MainActor static func tap() { UIImpactFeedbackGenerator(style: .light).impactOccurred() }
}
