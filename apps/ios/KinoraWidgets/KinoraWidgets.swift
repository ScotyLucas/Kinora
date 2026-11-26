import SwiftUI
import WidgetKit
import KinoraKit

@main
struct KinoraWidgets: WidgetBundle {
    var body: some Widget {
        ContinueWidget()
        RecentWidget()
        SeriesWidget()
        JournalWidget()
        DownloadsLiveActivity()
        PartyLiveActivity()
        RequestLiveActivity()
    }
}

// MARK: - colors (the app's "foyer" palette)

enum Palette {
    static let black = Color(red: 0x14 / 255, green: 0x12 / 255, blue: 0x10 / 255)
    static let panel = Color(red: 0x22 / 255, green: 0x1D / 255, blue: 0x19 / 255)
    static let raised = Color(red: 0x2B / 255, green: 0x25 / 255, blue: 0x20 / 255)
    static let plaster = Color(red: 0xCF / 255, green: 0xC5 / 255, blue: 0xB6 / 255)
    static let bright = Color(red: 0xEF / 255, green: 0xE7 / 255, blue: 0xDA / 255)
    static let muted = Color(red: 0x9D / 255, green: 0x93 / 255, blue: 0x87 / 255)
    static let amber = Color(red: 0xF0 / 255, green: 0xB4 / 255, blue: 0x4C / 255)

    static func display(_ size: CGFloat) -> Font { .system(size: size, weight: .heavy).width(.condensed) }
}

// MARK: - continue watching

struct ContinueWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "continue", provider: FeedProvider(kind: .continueWatching)) { entry in
            ContinueView(entry: entry)
        }
        .configurationDisplayName(L("Folytatás"))
        .description(L("Ahol abbahagytad: egy koppintás, és megy tovább."))
        .supportedFamilies([.systemSmall, .systemMedium, .systemLarge,
                            .accessoryRectangular, .accessoryCircular, .accessoryInline])
        .contentMarginsDisabled()
    }
}

struct ContinueView: View {
    @Environment(\.widgetFamily) private var family
    var entry: FeedEntry

    var body: some View {
        Group {
            switch family {
            case .accessoryInline, .accessoryCircular, .accessoryRectangular:
                accessory
            default:
                if entry.cards.isEmpty {
                    EmptyState(state: entry.state, empty: L("Nincs félbehagyott film vagy sorozat."))
                } else {
                    system
                }
            }
        }
        .containerBackground(for: .widget) { Palette.black }
    }

    @ViewBuilder private var system: some View {
        let cards = entry.cards
        switch family {
        case .systemSmall:
            StillCard(card: cards[0], titleSize: 17)
                .widgetURL(cards[0].link)
        case .systemMedium:
            HStack(spacing: 4) {
                ForEach(cards.prefix(2)) { c in
                    Link(destination: c.link) { StillCard(card: c, titleSize: 16) }
                }
            }
        default:
            VStack(alignment: .leading, spacing: 10) {
                Header(title: L("Folytatás"), stale: entry.stale)
                ForEach(cards.prefix(4)) { c in
                    HStack(spacing: 10) {
                        Link(destination: c.link) { ContinueRow(card: c) }
                        WatchedButton(id: c.id)
                    }
                }
                Spacer(minLength: 0)
            }
            .padding(14)
        }
    }

    @ViewBuilder private var accessory: some View {
        let card = entry.cards.first
        switch family {
        case .accessoryInline:
            if let card {
                Label(card.title, systemImage: "play.fill")
            } else {
                Text("Kinora")
            }
        case .accessoryCircular:
            Gauge(value: card?.progress ?? 0) {
                Image(systemName: "play.fill")
            } currentValueLabel: {
                Image(systemName: card == nil ? "film" : "play.fill")
            }
            .gaugeStyle(.accessoryCircularCapacity)
            .widgetURL(card?.link)
        default:
            VStack(alignment: .leading, spacing: 2) {
                Label(L("Folytatás"), systemImage: "play.fill").font(.caption2.weight(.semibold))
                Text(card?.title ?? L("Nincs félbehagyott cím")).font(.headline).lineLimit(1)
                if let sub = card?.subtitle { Text(sub).font(.caption).lineLimit(1) }
                if let p = card?.progress { ProgressView(value: p).tint(.primary) }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .widgetURL(card?.link)
        }
    }
}

/// Still or backdrop filling the space, title and progress on top.
private struct StillCard: View {
    var card: Card
    var titleSize: CGFloat

    var body: some View {
        ZStack(alignment: .bottomLeading) {
            Artwork(data: card.image, seed: card.id)
            LinearGradient(colors: [.clear, Palette.black.opacity(0.9)], startPoint: .center, endPoint: .bottom)
            VStack(alignment: .leading, spacing: 3) {
                Text(card.title)
                    .font(Palette.display(titleSize))
                    .foregroundStyle(Palette.bright)
                    .lineLimit(2)
                if let sub = card.subtitle {
                    Text(sub).font(.caption2).foregroundStyle(Palette.plaster).lineLimit(1)
                }
                if let p = card.progress { Bar(value: p).padding(.top, 3) }
            }
            .padding(12)
        }
    }
}

private struct ContinueRow: View {
    var card: Card

    var body: some View {
        HStack(spacing: 12) {
            Artwork(data: card.image, seed: card.id)
                .frame(width: 108, height: 61)
                .clipShape(.rect(cornerRadius: 6))
            VStack(alignment: .leading, spacing: 3) {
                Text(card.title).font(.subheadline.weight(.semibold)).foregroundStyle(Palette.bright).lineLimit(1)
                if let sub = card.subtitle {
                    Text(sub).font(.caption).foregroundStyle(Palette.muted).lineLimit(1)
                }
                if let p = card.progress { Bar(value: p) }
            }
            Spacer(minLength: 0)
            Image(systemName: "play.circle.fill").font(.title2).foregroundStyle(Palette.amber)
        }
    }
}

// MARK: - recently added

struct RecentWidget: Widget {
    var body: some WidgetConfiguration {
        AppIntentConfiguration(kind: "recent", intent: RecentConfig.self, provider: RecentProvider()) { entry in
            RecentView(entry: entry)
        }
        .configurationDisplayName(L("Új a könyvtárban"))
        .description(L("A legutóbb hozzáadott filmek és sorozatok, egy vagy minden könyvtárból."))
        .supportedFamilies([.systemSmall, .systemMedium, .systemLarge])
    }
}

struct RecentView: View {
    @Environment(\.widgetFamily) private var family
    var entry: FeedEntry

    var body: some View {
        Group {
            if entry.cards.isEmpty {
                EmptyState(state: entry.state, empty: L("Még nincs új tartalom."))
            } else if family == .systemSmall {
                Poster(card: entry.cards[0]).widgetURL(entry.cards[0].link)
            } else {
                VStack(alignment: .leading, spacing: 8) {
                    Header(title: L("Új a könyvtárban"), stale: entry.stale)
                    let rows = family == .systemLarge ? 2 : 1
                    Grid(horizontalSpacing: 8, verticalSpacing: 8) {
                        ForEach(0..<rows, id: \.self) { r in
                            GridRow {
                                ForEach(0..<4, id: \.self) { c in
                                    let i = r * 4 + c
                                    if i < entry.cards.count {
                                        Link(destination: entry.cards[i].link) { Poster(card: entry.cards[i]) }
                                    } else {
                                        Color.clear
                                    }
                                }
                            }
                        }
                    }
                    if family == .systemLarge { Spacer(minLength: 0) }
                }
            }
        }
        .containerBackground(for: .widget) { Palette.black }
    }
}

private struct Poster: View {
    var card: Card

    var body: some View {
        Artwork(data: card.image, seed: card.id, title: card.title)
            .aspectRatio(2 / 3, contentMode: .fit)
            .clipShape(.rect(cornerRadius: 6))
    }
}

// MARK: - pieces

private struct Header: View {
    var title: String
    var stale: Bool

    var body: some View {
        HStack {
            Text(title).font(Palette.display(17)).foregroundStyle(Palette.amber)
            Spacer()
            if stale {
                Image(systemName: "wifi.slash").font(.caption).foregroundStyle(Palette.muted)
                    .accessibilityLabel(L("A szerver most nem érhető el"))
            }
        }
    }
}

struct Bar: View {
    var value: Double

    var body: some View {
        GeometryReader { g in
            ZStack(alignment: .leading) {
                Capsule().fill(.white.opacity(0.25))
                Capsule().fill(Palette.amber).frame(width: g.size.width * min(1, max(0, value)))
            }
        }
        .frame(height: 3)
    }
}

/// The image, or a quiet gradient with the title when there is none.
struct Artwork: View {
    var data: Data?
    var seed: Int
    var title: String?

    var body: some View {
        if let data, let img = UIImage(data: data) {
            Color.clear.overlay {
                Image(uiImage: img).resizable().scaledToFill()
            }
            .clipped()
        } else {
            let hue = Double(abs(seed * 47) % 360) / 360
            LinearGradient(colors: [Color(hue: hue, saturation: 0.25, brightness: 0.28), Palette.panel],
                           startPoint: .topLeading, endPoint: .bottomTrailing)
                .overlay {
                    if let title {
                        Text(title).font(Palette.display(13)).foregroundStyle(Palette.plaster)
                            .multilineTextAlignment(.center).padding(6)
                    }
                }
        }
    }
}

struct EmptyState: View {
    var state: FeedEntry.State
    var empty: String

    var body: some View {
        VStack(spacing: 6) {
            Image(systemName: icon).font(.title2).foregroundStyle(Palette.amber)
            Text(message).font(.caption).foregroundStyle(Palette.plaster).multilineTextAlignment(.center)
        }
        .padding()
    }

    private var icon: String {
        switch state {
        case .signedOut: "person.crop.circle"
        case .offline: "wifi.slash"
        default: "film.stack"
        }
    }

    private var message: String {
        switch state {
        case .signedOut: L("Jelentkezz be a Kinora appban.")
        case .offline: L("A szerver most nem érhető el.")
        default: empty
        }
    }
}
