import AppIntents
import SwiftUI
import WidgetKit
import KinoraKit

// Widget settings (long-press → Edit widget) and the buttons on the widgets.

// MARK: - libraries

struct LibraryEntity: AppEntity {
    static let typeDisplayRepresentation: TypeDisplayRepresentation = "Könyvtár"
    static let defaultQuery = LibraryQuery()

    let id: Int
    let name: String

    var displayRepresentation: DisplayRepresentation { DisplayRepresentation(title: "\(name)") }
}

struct LibraryQuery: EntityQuery {
    func entities(for identifiers: [Int]) async throws -> [LibraryEntity] {
        try await suggestedEntities().filter { identifiers.contains($0.id) }
    }

    func suggestedEntities() async throws -> [LibraryEntity] {
        guard let client = Feed.client() else { return [] }
        return try await client.libraries().map { LibraryEntity(id: $0.id, name: $0.name) }
    }
}

// MARK: - series

struct SeriesEntity: AppEntity {
    static let typeDisplayRepresentation: TypeDisplayRepresentation = "Sorozat"
    static let defaultQuery = SeriesQuery()

    let id: Int
    let title: String

    var displayRepresentation: DisplayRepresentation { DisplayRepresentation(title: "\(title)") }
}

struct SeriesQuery: EntityStringQuery {
    func entities(for identifiers: [Int]) async throws -> [SeriesEntity] {
        guard let client = Feed.client() else { return [] }
        var found: [SeriesEntity] = []
        for id in identifiers {
            if let item = try? await client.item(id).item { found.append(SeriesEntity(id: item.id, title: item.title)) }
        }
        return found
    }

    func entities(matching string: String) async throws -> [SeriesEntity] {
        guard let client = Feed.client(), !string.isEmpty else { return [] }
        return try await client.search(string).filter { $0.kind == .series }
            .prefix(10).map { SeriesEntity(id: $0.id, title: $0.title) }
    }

    /// The series the user is in the middle of come first.
    func suggestedEntities() async throws -> [SeriesEntity] {
        guard let client = Feed.client() else { return [] }
        var seen = Set<Int>()
        return WidgetFeed.continueWatching(try await client.home()).compactMap { item in
            guard let id = item.kind == .series ? item.id : item.seriesId, seen.insert(id).inserted else { return nil }
            return SeriesEntity(id: id, title: item.seriesTitle ?? item.title)
        }
    }
}

// MARK: - configurations

struct RecentConfig: WidgetConfigurationIntent {
    static let title: LocalizedStringResource = "Új a könyvtárban"
    static let description = IntentDescription("Melyik könyvtár újdonságait mutassa.")

    @Parameter(title: "Könyvtár", description: "Üresen hagyva minden könyvtárét mutatja.")
    var library: LibraryEntity?
}

struct SeriesConfig: WidgetConfigurationIntent {
    static let title: LocalizedStringResource = "Következő rész"
    static let description = IntentDescription("Melyik sorozat következő részét mutassa.")

    @Parameter(title: "Sorozat")
    var series: SeriesEntity?
}

// MARK: - providers

private func refreshTimeline(_ entry: FeedEntry) -> Timeline<FeedEntry> {
    // The app asks for a reload after playback; this is for changes made elsewhere.
    let next = Date.now.addingTimeInterval(entry.stale || entry.state == .offline ? 15 * 60 : 30 * 60)
    return Timeline(entries: [entry], policy: .after(next))
}

struct RecentProvider: AppIntentTimelineProvider {
    func placeholder(in context: Context) -> FeedEntry {
        FeedEntry(date: .now, state: .cards(Sample.cards(.recentlyAdded)))
    }

    func snapshot(for config: RecentConfig, in context: Context) async -> FeedEntry {
        context.isPreview ? placeholder(in: context) : await Feed.load(.recentlyAdded, library: config.library?.id)
    }

    func timeline(for config: RecentConfig, in context: Context) async -> Timeline<FeedEntry> {
        refreshTimeline(await Feed.load(.recentlyAdded, library: config.library?.id))
    }
}

struct SeriesEntry: TimelineEntry {
    var date: Date
    var feed: FeedEntry
    /// Nil until the user picks a series.
    var seriesTitle: String?
}

struct SeriesProvider: AppIntentTimelineProvider {
    func placeholder(in context: Context) -> SeriesEntry {
        SeriesEntry(date: .now, feed: FeedEntry(date: .now, state: .cards([Sample.cards(.continueWatching)[1]])),
                    seriesTitle: L("A sorozat"))
    }

    func snapshot(for config: SeriesConfig, in context: Context) async -> SeriesEntry {
        if context.isPreview || config.series == nil { return placeholder(in: context) }
        return await load(config)
    }

    func timeline(for config: SeriesConfig, in context: Context) async -> Timeline<SeriesEntry> {
        let entry = await load(config)
        let t = refreshTimeline(entry.feed)
        return Timeline(entries: [entry], policy: t.policy)
    }

    private func load(_ config: SeriesConfig) async -> SeriesEntry {
        Language.shared.reloadIfNeeded()
        guard let series = config.series else {
            return SeriesEntry(date: .now, feed: FeedEntry(date: .now, state: .empty), seriesTitle: nil)
        }
        return SeriesEntry(date: .now, feed: await Feed.nextEpisode(of: series.id), seriesTitle: series.title)
    }
}

// MARK: - buttons

/// The ✓ on a widget row: marks the title watched on the server, then the widget reloads.
struct MarkWatchedIntent: AppIntent {
    static let title: LocalizedStringResource = "Megnézettnek jelölés"
    static let isDiscoverable = false

    @Parameter(title: "Azonosító") var itemId: Int

    init() {}
    init(itemId: Int) { self.itemId = itemId }

    func perform() async throws -> some IntentResult {
        guard let client = Feed.client() else { return .result() }
        _ = try await client.setPlayed(itemId, true)
        // Gone at once from every widget (the series, the Napló count too), not at the next refresh.
        Feed.forget(itemId)
        WidgetCenter.shared.reloadAllTimelines()
        return .result()
    }
}

struct WatchedButton: View {
    var id: Int
    /// On top of a picture: a dark disc behind the mark, so it reads on any still.
    var onImage = false

    var body: some View {
        Button(intent: MarkWatchedIntent(itemId: id)) {
            Image(systemName: onImage ? "checkmark.circle.fill" : "checkmark.circle")
                .font(.title2)
                .foregroundStyle(onImage ? Palette.bright : Palette.muted, Palette.black.opacity(0.55))
        }
        .buttonStyle(.plain)
        .accessibilityLabel(L("Megnézettnek jelölés"))
    }
}

// MARK: - next episode widget

struct SeriesWidget: Widget {
    var body: some WidgetConfiguration {
        AppIntentConfiguration(kind: "series", intent: SeriesConfig.self, provider: SeriesProvider()) { entry in
            SeriesView(entry: entry)
        }
        .configurationDisplayName(L("Következő rész"))
        .description(L("Egy kiválasztott sorozat következő része, egy koppintással indítható."))
        .supportedFamilies([.systemSmall, .systemMedium, .systemLarge, .accessoryRectangular])
        .contentMarginsDisabled()
    }
}

struct SeriesView: View {
    @Environment(\.widgetFamily) private var family
    var entry: SeriesEntry

    var body: some View {
        content.containerBackground(for: .widget) { Palette.black }
    }

    @ViewBuilder private var content: some View {
        let card = entry.feed.cards.first
        if family == .accessoryRectangular {
            VStack(alignment: .leading, spacing: 2) {
                Label(entry.seriesTitle ?? L("Következő rész"), systemImage: "play.rectangle.fill")
                    .font(.caption2.weight(.semibold)).lineLimit(1)
                Text(card?.subtitle ?? (entry.seriesTitle == nil ? L("Válassz sorozatot") : L("Minden részt láttál")))
                    .font(.headline).lineLimit(2)
                if let p = card?.progress { ProgressView(value: p).tint(.primary) }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .widgetURL(card?.link)
        } else if entry.seriesTitle == nil {
            message(L("Tartsd lenyomva, és válassz egy sorozatot a widget szerkesztésénél."))
        } else if let card, family == .systemLarge {
            large(card)
        } else if let card {
            ZStack(alignment: .bottomLeading) {
                Artwork(data: card.image, seed: card.id)
                LinearGradient(colors: [.clear, Palette.black.opacity(0.95)], startPoint: .center, endPoint: .bottom)
                VStack(alignment: .leading, spacing: 3) {
                    Text(L("KÖVETKEZŐ RÉSZ")).font(.caption2.weight(.bold)).foregroundStyle(Palette.amber)
                    Text(card.title).font(Palette.display(family == .systemSmall ? 17 : 20))
                        .foregroundStyle(Palette.bright).lineLimit(1)
                    if let sub = card.subtitle {
                        Text(sub).font(.caption).foregroundStyle(Palette.plaster).lineLimit(family == .systemSmall ? 2 : 1)
                    }
                    if let p = card.progress { Bar(value: p) }
                }
                .padding(12)
                if family == .systemMedium {
                    WatchedButton(id: card.id, onImage: true)
                        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topTrailing)
                        .padding(10)
                }
            }
            .overlay(alignment: .topLeading) {
                if entry.feed.stale { StaleBadge(updated: entry.feed.updated).padding(10) }
            }
            .invalidatableContent()
            .widgetURL(card.link)
        } else if entry.feed.state == .empty {
            message(L("{0} – minden részt láttál.", entry.seriesTitle ?? ""))
        } else {
            EmptyState(state: entry.feed.state, empty: "")
        }
    }

    /// The next episode big, the rest of the season below it, and how far the season is.
    private func large(_ card: Card) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            ZStack(alignment: .bottomLeading) {
                Artwork(data: card.image, seed: card.id)
                LinearGradient(colors: [.clear, Palette.black], startPoint: .center, endPoint: .bottom)
                VStack(alignment: .leading, spacing: 3) {
                    Text(L("KÖVETKEZŐ RÉSZ")).font(.caption2.weight(.bold)).foregroundStyle(Palette.amber).widgetAccentable()
                    Text(card.title).font(Palette.display(22)).foregroundStyle(Palette.bright).lineLimit(1)
                    if let sub = card.subtitle { Text(sub).font(Palette.meta).foregroundStyle(Palette.plaster).lineLimit(1) }
                    if let p = card.progress { Bar(value: p).padding(.top, 2) }
                }
                .padding(14)
            }
            .frame(maxHeight: .infinity)
            .overlay(alignment: .topTrailing) {
                HStack(spacing: 8) {
                    WatchedButton(id: card.id, onImage: true)
                    Link(destination: card.link) {
                        Image(systemName: "play.circle.fill").font(.largeTitle)
                            .foregroundStyle(Palette.amber, Palette.black.opacity(0.55)).widgetAccentable()
                    }
                    .accessibilityLabel(L("Lejátszás"))
                }
                .padding(10)
            }
            .overlay(alignment: .topLeading) {
                if entry.feed.stale { StaleBadge(updated: entry.feed.updated).padding(10) }
            }
            .invalidatableContent()
            VStack(alignment: .leading, spacing: 8) {
                if let season = card.season, season.total > 0 {
                    VStack(alignment: .leading, spacing: 4) {
                        HStack {
                            Text(L("{0}. évad", season.number)).font(.caption.weight(.bold)).foregroundStyle(Palette.plaster)
                            Spacer()
                            Text("\(season.watched)/\(season.total)").font(Palette.meta).foregroundStyle(Palette.muted)
                        }
                        Bar(value: Double(season.watched) / Double(season.total))
                    }
                }
                ForEach(card.upcoming ?? [], id: \.id) { u in
                    Link(destination: u.link) {
                        HStack(spacing: 8) {
                            Image(systemName: "play.fill").font(.caption2).foregroundStyle(Palette.amber).widgetAccentable()
                            Text(u.label).font(.system(size: 14, weight: .medium)).foregroundStyle(Palette.bright).lineLimit(1)
                            Spacer(minLength: 0)
                        }
                    }
                }
                if card.upcoming?.isEmpty ?? true {
                    Text(L("Ez az évad utolsó része.")).font(Palette.meta).foregroundStyle(Palette.muted)
                }
            }
            .padding(14)
        }
        .widgetURL(card.link)
    }

    private func message(_ text: String) -> some View {
        Text(text)
            .font(.footnote)
            .foregroundStyle(Palette.muted)
            .multilineTextAlignment(.center)
            .padding(14)
    }
}

// MARK: - Control Center

/// Plays the title the "continue" widget shows first; with nothing saved, opens the app.
@available(iOS 18.0, *)
struct ResumeControlIntent: AppIntent {
    static let title: LocalizedStringResource = "Folytatás"
    static let description = IntentDescription("Ott folytatja a lejátszást, ahol legutóbb abbahagytad.")
    static let isDiscoverable = false

    func perform() async throws -> some IntentResult & OpensIntent {
        .result(opensIntent: OpenURLIntent(Feed.lastContinueLink() ?? appLink))
    }
}

/// A Control Center, lock screen and Action button control: one press, and the last title plays on.
@available(iOS 18.0, *)
struct ResumeControl: ControlWidget {
    var body: some ControlWidgetConfiguration {
        StaticControlConfiguration(kind: "io.github.scotylucas.kinora.resume") {
            ControlWidgetButton(action: ResumeControlIntent()) {
                Label(L("Folytatás"), systemImage: "play.rectangle.fill")
            }
        }
        .displayName(LocalizedStringResource(stringLiteral: L("Kinora – folytatás")))
        .description(LocalizedStringResource(stringLiteral: L("Ott folytatja a lejátszást, ahol legutóbb abbahagytad.")))
    }
}
