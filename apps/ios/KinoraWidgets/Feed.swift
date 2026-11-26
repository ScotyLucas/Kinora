import Foundation
import UIKit
import WidgetKit
import KinoraKit

/// One title on a widget. Images travel as small JPEG data, because widget views
/// cannot load anything themselves.
struct Card: Codable, Hashable, Identifiable {
    var id: Int
    var title: String
    var subtitle: String?
    /// Watched share 0…1 for started titles.
    var progress: Double?
    var image: Data?
    var link: URL
    /// Seconds still to watch, for started titles.
    var remaining: Double?
    /// Not started yet: offered because there is nothing to continue.
    var suggested: Bool?
    /// The series' next episodes after this one (the large "Next episode" widget).
    var upcoming: [Upcoming]?
    /// Watched and all episodes of the season, for the season bar.
    var season: SeasonProgress?

    struct Upcoming: Codable, Hashable { var id: Int; var label: String; var link: URL }
    struct SeasonProgress: Codable, Hashable { var number: Int; var watched: Int; var total: Int }

    var isSuggestion: Bool { suggested == true }
}

/// "Még 42 perc": the time left of a started title, in the app's language.
func timeLeft(_ seconds: Double?) -> String? {
    guard let s = seconds, s > 0 else { return nil }
    let minutes = max(1, Int((s / 60).rounded()))
    let h = minutes / 60, m = minutes % 60
    let span = h == 0 ? L("{0} perc", m) : (m == 0 ? L("{0} óra", h) : L("{0} ó {1} perc", h, m))
    return L("Még {0}", span)
}

struct FeedEntry: TimelineEntry {
    enum State: Equatable {
        case cards([Card])
        case empty
        case signedOut
        case offline
    }

    var date: Date
    var state: State
    /// From the last good fetch, because the server could not be reached now.
    var stale = false
    /// When the shown data was fetched, for a stale entry.
    var updated: Date?

    var cards: [Card] {
        if case .cards(let c) = state { c } else { [] }
    }
}

enum FeedKind: String {
    case continueWatching = "continue"
    case recentlyAdded = "recent"

    var limit: Int { self == .continueWatching ? 4 : 8 }
    /// Wide stills for "continue", posters for "new".
    var imageWidth: CGFloat { self == .continueWatching ? 480 : 220 }
}

/// Fetches /api/home with the app's login, and keeps the last good result in the
/// App Group container for when the server is out of reach (away from home).
enum Feed {
    /// Continue watching, or the newest titles (of one library, when the widget is set to one).
    static func load(_ kind: FeedKind, library: Int? = nil) async -> FeedEntry {
        await load(key: kind.rawValue + (library.map { "-\($0)" } ?? "")) { client in
            let items: [Item]
            switch kind {
            case .continueWatching:
                let home = try await client.home()
                items = WidgetFeed.continueWatching(home)
                // Nothing to continue: offer the newest titles instead of an empty widget.
                if items.isEmpty {
                    var cards: [Card] = []
                    for item in WidgetFeed.recentlyAdded(home).prefix(2) {
                        var c = await card(item, kind: kind, client: client)
                        c.suggested = true
                        c.link = KinoraLink.item(item.id).url
                        cards.append(c)
                    }
                    return cards
                }
            case .recentlyAdded:
                if let library {
                    items = try await client.items(library: library, sort: .added, limit: kind.limit).items
                } else {
                    items = WidgetFeed.recentlyAdded(try await client.home())
                }
            }
            var cards: [Card] = []
            for item in items.prefix(kind.limit) {
                cards.append(await card(item, kind: kind, client: client))
            }
            return cards
        }
    }

    /// The next episode to watch of one series; empty when every episode is watched.
    static func nextEpisode(of series: Int) async -> FeedEntry {
        await load(key: "series-\(series)") { client in
            guard let next = try await client.item(series).playNext else { return [] }
            var c = await card(next, kind: .continueWatching, client: client)
            // The rest of the season: the large widget lists what comes after, and a season bar.
            if let season = next.parentId, let eps = try? await client.children(of: season) {
                let sorted = eps.filter { $0.kind == .episode }.sorted { ($0.episodeNum ?? 0) < ($1.episodeNum ?? 0) }
                if let i = sorted.firstIndex(where: { $0.id == next.id }) {
                    c.upcoming = sorted[(i + 1)...].prefix(3).map {
                        Card.Upcoming(id: $0.id, label: [$0.episodeLabel, $0.title].compactMap { $0 }.joined(separator: " · "),
                                      link: KinoraLink.play($0.id).url)
                    }
                }
                if let n = next.seasonNum, !sorted.isEmpty {
                    c.season = Card.SeasonProgress(number: n, watched: sorted.filter { $0.userData?.played == true }.count,
                                                   total: sorted.count)
                }
            }
            return [c]
        }
    }

    /// Runs one fetch with the app's login; falls back to the last good result.
    private static func load(key: String, _ fetch: (KinoraClient) async throws -> [Card]) async -> FeedEntry {
        Language.shared.reloadIfNeeded() // the app may have switched or updated the language
        let store = ServerStore.shared()
        guard let server = store.current, let token = store.token(for: server.id) else {
            return FeedEntry(date: .now, state: .signedOut)
        }
        let client = KinoraClient(server: server, token: token)
        do {
            let cards = try await fetch(client)
            save(cards, key: key)
            return FeedEntry(date: .now, state: cards.isEmpty ? .empty : .cards(cards))
        } catch KinoraError.unauthorized {
            return FeedEntry(date: .now, state: .signedOut)
        } catch {
            if let cached = cached(key), !cached.isEmpty {
                return FeedEntry(date: .now, state: .cards(cached), stale: true, updated: savedAt(key))
            }
            return FeedEntry(date: .now, state: .offline)
        }
    }

    /// The app's login, for the widget intents (marking a title watched).
    static func client() -> KinoraClient? {
        let store = ServerStore.shared()
        guard let server = store.current, let token = store.token(for: server.id) else { return nil }
        return KinoraClient(server: server, token: token)
    }

    private static func card(_ item: Item, kind: FeedKind, client: KinoraClient) async -> Card {
        let episode = item.kind == .episode
        let title = episode ? (item.seriesTitle ?? item.title) : item.title
        let subtitle: String? = episode
            ? [item.episodeLabel, item.title].compactMap { $0 }.joined(separator: " · ")
            : item.year.map(String.init)
        let imageKind: ImageKind = switch kind {
        case .recentlyAdded: .poster
        case .continueWatching: episode ? .thumb : .backdrop
        }
        var data = try? await client.image(item.id, imageKind)
        if data == nil, kind == .continueWatching, episode, let series = item.seriesId {
            data = try? await client.image(series, .backdrop)
        }
        let progress = kind == .continueWatching ? item.progress : nil
        return Card(id: item.id, title: title, subtitle: subtitle,
                    progress: progress,
                    image: data.flatMap { shrink($0, width: kind.imageWidth) },
                    link: (kind == .continueWatching ? KinoraLink.play(item.id) : .item(item.id)).url,
                    remaining: progress == nil ? nil : (item.runtimeSec ?? 0) - (item.userData?.position ?? 0))
    }

    /// Widgets have a tight memory budget: keep images just big enough for their spot.
    private static func shrink(_ data: Data, width: CGFloat) -> Data? {
        guard let img = UIImage(data: data) else { return nil }
        guard img.size.width > width else { return img.jpegData(compressionQuality: 0.8) }
        let size = CGSize(width: width, height: (img.size.height * width / img.size.width).rounded())
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        return UIGraphicsImageRenderer(size: size, format: format)
            .image { _ in img.draw(in: CGRect(origin: .zero, size: size)) }
            .jpegData(compressionQuality: 0.8)
    }

    // MARK: - last good result

    private static func file(_ key: String) -> URL? {
        AppGroup.container?.appending(path: "Library/Caches/widget-\(key).json")
    }

    private static func save(_ cards: [Card], key: String) {
        guard let url = file(key) else { return }
        try? FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try? JSONEncoder().encode(cards).write(to: url, options: .atomic)
    }

    private static func cached(_ key: String) -> [Card]? {
        guard let url = file(key), let data = try? Data(contentsOf: url) else { return nil }
        return try? JSONDecoder().decode([Card].self, from: data)
    }

    private static func savedAt(_ key: String) -> Date? {
        guard let url = file(key) else { return nil }
        return (try? url.resourceValues(forKeys: [.contentModificationDateKey]))?.contentModificationDate
    }

    /// What the Control Center button plays: the first title of the last "continue" fetch.
    static func lastContinueLink() -> URL? {
        cached(FeedKind.continueWatching.rawValue)?.first { !$0.isSuggestion }?.link
    }

    /// Drops a title marked watched from every saved list, so the widget changes at once, even offline.
    static func forget(_ id: Int) {
        guard let dir = AppGroup.container?.appending(path: "Library/Caches"),
              let files = try? FileManager.default.contentsOfDirectory(at: dir, includingPropertiesForKeys: nil) else { return }
        for url in files where url.lastPathComponent.hasPrefix("widget-") && url.lastPathComponent != "widget-journal.json" {
            guard let data = try? Data(contentsOf: url), let cards = try? JSONDecoder().decode([Card].self, from: data),
                  cards.contains(where: { $0.id == id }) else { continue }
            try? JSONEncoder().encode(cards.filter { $0.id != id }).write(to: url, options: .atomic)
        }
    }
}

struct FeedProvider: TimelineProvider {
    let kind: FeedKind

    func placeholder(in context: Context) -> FeedEntry {
        FeedEntry(date: .now, state: .cards(Sample.cards(kind)))
    }

    func getSnapshot(in context: Context, completion: @escaping (FeedEntry) -> Void) {
        if context.isPreview { return completion(placeholder(in: context)) }
        let done = Handoff(completion)
        let kind = kind
        Task { done.call(await Feed.load(kind)) }
    }

    func getTimeline(in context: Context, completion: @escaping (Timeline<FeedEntry>) -> Void) {
        let done = Handoff(completion)
        let kind = kind
        Task {
            let entry = await Feed.load(kind)
            // The app asks for a reload after playback; this is for changes made elsewhere.
            let next = Date.now.addingTimeInterval(entry.stale || entry.state == .offline ? 15 * 60 : 30 * 60)
            done.call(Timeline(entries: [entry], policy: .after(next)))
        }
    }
}

/// WidgetKit's completion handlers predate Sendable; each is called once, from one task.
struct Handoff<T>: @unchecked Sendable {
    let call: (T) -> Void
    init(_ call: @escaping (T) -> Void) { self.call = call }
}

/// Shown in the widget gallery before anything is loaded.
enum Sample {
    static func cards(_ kind: FeedKind) -> [Card] {
        switch kind {
        case .continueWatching:
            [Card(id: 1, title: L("Csillagközi utazás"), subtitle: "2014", progress: 0.4, link: KinoraLink.play(1).url),
             Card(id: 2, title: L("A sorozat"), subtitle: L("S2 · E5 · A visszatérés"), progress: 0.7, link: KinoraLink.play(2).url),
             Card(id: 3, title: L("Dokumentumfilm"), subtitle: "2021", progress: 0.15, link: KinoraLink.play(3).url),
             Card(id: 4, title: L("Vígjáték"), subtitle: "2019", progress: 0.55, link: KinoraLink.play(4).url)]
        case .recentlyAdded:
            (1...8).map { Card(id: $0, title: L("Film {0}", $0), link: KinoraLink.item($0).url) }
        }
    }
}
