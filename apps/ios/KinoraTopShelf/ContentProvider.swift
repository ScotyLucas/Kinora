import Foundation
import TVServices
import KinoraKit

/// The Apple TV home screen's top shelf: while the Kinora icon has the focus, the titles in
/// progress (with progress) and the newest ones show above the apps, both as posters.
///
/// It uses the app's login through the App Group, like the iPhone widgets. Images are fetched
/// with the token and stored in the App Group container, because the system loads them from a
/// URL and the server's image endpoints need the login. The last good shelf stays there for
/// when the server cannot be reached.
final class ContentProvider: TVTopShelfContentProvider {
    override func loadTopShelfContent(completionHandler: @escaping ((any TVTopShelfContent)?) -> Void) {
        let done = Handoff(completionHandler)
        Task { done.call(await Self.load()?.content) }
    }

    private static func load() async -> Shelf? {
        Language.shared.reloadIfNeeded()
        let store = ServerStore.shared()
        guard let server = store.current, let token = store.token(for: server.id) else { return nil }
        let client = KinoraClient(server: server, token: token)
        guard let home = try? await client.home() else { return Shelf.cached() }

        let shelf = Shelf(
            resume: await Shelf.cards(WidgetFeed.continueWatching(home).prefix(12), resume: true, client: client),
            recent: await Shelf.cards(WidgetFeed.recentlyAdded(home).prefix(12), resume: false, client: client)
        )
        // An empty answer (a profile just left, say) must not wipe the last good shelf.
        if shelf.resume.isEmpty && shelf.recent.isEmpty { return Shelf.cached() ?? shelf }
        shelf.save()
        return shelf
    }
}

/// The completion handler predates Sendable; it is called once, from one task.
private struct Handoff: @unchecked Sendable {
    let completion: ((any TVTopShelfContent)?) -> Void
    init(_ c: @escaping ((any TVTopShelfContent)?) -> Void) { completion = c }
    func call(_ v: (any TVTopShelfContent)?) { completion(v) }
}

/// What the shelf shows, kept as JSON next to the images.
struct Shelf: Codable, Sendable {
    struct Card: Codable, Sendable {
        var id: Int
        var title: String
        var progress: Double?
        var image: String?
    }

    var resume: [Card]
    var recent: [Card]

    var content: (any TVTopShelfContent)? {
        var sections: [TVTopShelfItemCollection<TVTopShelfSectionedItem>] = []
        if !resume.isEmpty { sections.append(Self.section(L("Folytatás"), resume, resume: true)) }
        if !recent.isEmpty { sections.append(Self.section(L("Új a könyvtárban"), recent, resume: false)) }
        return sections.isEmpty ? nil : TVTopShelfSectionedContent(sections: sections)
    }

    private static func section(_ title: String, _ cards: [Card], resume: Bool) -> TVTopShelfItemCollection<TVTopShelfSectionedItem> {
        let items = cards.map { c in
            let item = TVTopShelfSectionedItem(identifier: "\(resume ? "r" : "n")\(c.id)")
            item.title = c.title
            item.imageShape = .poster
            if let name = c.image, let dir = Self.images {
                item.setImageURL(dir.appending(path: name), for: [.screenScale1x, .screenScale2x])
            }
            if let p = c.progress, p > 0 { item.playbackProgress = min(1, p) }
            // A click opens the title (or resumes it), the play button always plays.
            item.displayAction = TVTopShelfAction(url: (resume ? KinoraLink.play(c.id) : .item(c.id)).url)
            item.playAction = TVTopShelfAction(url: KinoraLink.play(c.id).url)
            return item
        }
        let collection = TVTopShelfItemCollection(items: items)
        collection.title = title
        return collection
    }

    static func cards(_ items: ArraySlice<Item>, resume: Bool, client: KinoraClient) async -> [Card] {
        var out: [Card] = []
        for item in items {
            let episode = item.kind == .episode
            let title = episode
                ? [item.seriesTitle, item.episodeLabel].compactMap { $0 }.joined(separator: " · ")
                : item.title
            // Episodes show their series' poster, like the recently added row in the app.
            var data = try? await client.image(item.seriesId ?? item.id, .poster)
            // No poster (a home video, say): the still still beats an empty card.
            if data == nil { data = try? await client.image(item.id, episode ? .thumb : .backdrop) }
            let name = "poster-\(item.id).jpg"
            var stored: String?
            if let data, let dir = images {
                try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
                if (try? data.write(to: dir.appending(path: name), options: .atomic)) != nil { stored = name }
            }
            out.append(Card(id: item.id, title: title, progress: resume ? item.progress : nil, image: stored))
        }
        return out
    }

    // MARK: - storage

    // The shelf itself is not kept in Library/Caches: tvOS empties caches often, and then a server
    // that cannot be reached (or a session without a chosen profile) leaves the shelf empty.
    static var directory: URL? { AppGroup.container?.appending(path: "Library/Application Support/TopShelf") }
    // The posters stay in Library/Caches: the home screen loads them from there, not from
    // Application Support. Every refresh fetches them again.
    static var images: URL? { AppGroup.container?.appending(path: "Library/Caches/TopShelf") }

    func save() {
        guard let dir = Self.directory else { return }
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        try? JSONEncoder().encode(self).write(to: dir.appending(path: "shelf.json"), options: .atomic)
        // Posters the previous build stored next to the shelf.
        for name in (try? FileManager.default.contentsOfDirectory(atPath: dir.path())) ?? [] where name != "shelf.json" {
            try? FileManager.default.removeItem(at: dir.appending(path: name))
        }
        // Drop images of titles that left the shelf.
        guard let images = Self.images else { return }
        let keep = Set((resume + recent).compactMap(\.image))
        for name in (try? FileManager.default.contentsOfDirectory(atPath: images.path())) ?? [] where !keep.contains(name) {
            try? FileManager.default.removeItem(at: images.appending(path: name))
        }
    }

    static func cached() -> Shelf? {
        guard let dir = directory, let data = try? Data(contentsOf: dir.appending(path: "shelf.json")) else { return nil }
        return try? JSONDecoder().decode(Shelf.self, from: data)
    }
}
