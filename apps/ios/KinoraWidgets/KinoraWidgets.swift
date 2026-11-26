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
        if #available(iOS 18.0, *) { ResumeControl() }
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
    /// Year, episode and time left under a title: one size on every widget.
    static let meta = Font.system(size: 12, weight: .medium)
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
    @Environment(\.showsWidgetContainerBackground) private var showsBackground
    var entry: FeedEntry

    var body: some View {
        Group {
            switch family {
            case .accessoryInline, .accessoryCircular, .accessoryRectangular:
                accessory
            default:
                if entry.cards.isEmpty {
                    EmptyState(state: entry.state, empty: L("Még üres a könyvtár"))
                } else {
                    system
                }
            }
        }
        .containerBackground(for: .widget) { Palette.black }
    }

    private var suggesting: Bool { entry.cards.first?.isSuggestion == true }

    @ViewBuilder private var system: some View {
        let cards = entry.cards
        switch family {
        case .systemSmall:
            // StandBy shows the widget without its rounded container: round the picture itself.
            StillCard(card: cards[0], titleSize: 17, corner: .play, stale: entry.stale ? entry.updated : nil)
                .clipShape(.rect(cornerRadius: showsBackground ? 0 : 22))
                .widgetURL(cards[0].link)
        case .systemMedium:
            HStack(spacing: 4) {
                ForEach(Array(cards.prefix(2).enumerated()), id: \.element.id) { i, c in
                    Link(destination: c.link) {
                        StillCard(card: c, titleSize: 16, corner: c.isSuggestion ? .none : .watched,
                                  stale: i == 0 && entry.stale ? entry.updated : nil)
                    }
                }
            }
        default:
            VStack(alignment: .leading, spacing: 10) {
                Header(title: suggesting ? L("Kezdd el valami újat") : L("Folytatás"),
                       stale: entry.stale, updated: entry.updated)
                ForEach(cards.prefix(4)) { c in
                    HStack(spacing: 10) {
                        Link(destination: c.link) { ContinueRow(card: c) }
                        if !c.isSuggestion { WatchedButton(id: c.id) }
                    }
                    .invalidatableContent()
                }
                Spacer(minLength: 0)
            }
            .padding(14)
        }
    }

    @ViewBuilder private var accessory: some View {
        let card = entry.cards.first { !$0.isSuggestion }
        switch family {
        case .accessoryInline:
            if let card {
                Label([card.title, timeLeft(card.remaining)].compactMap { $0 }.joined(separator: " · "),
                      systemImage: "play.fill")
            } else {
                Label(L("Nincs félbehagyott cím"), systemImage: "film")
            }
        case .accessoryCircular:
            Gauge(value: card?.progress ?? 0) {
                Image(systemName: "play.fill")
            } currentValueLabel: {
                // The minutes left say more than a title squeezed into a circle.
                if let rem = card?.remaining, rem > 0 {
                    Text(Duration.seconds(rem).formatted(.units(allowed: [.hours, .minutes], width: .narrow, maximumUnitCount: 1)))
                        .minimumScaleFactor(0.6)
                } else {
                    Image(systemName: card == nil ? "film" : "play.fill")
                }
            }
            .gaugeStyle(.accessoryCircularCapacity)
            .widgetURL(card?.link)
        default:
            VStack(alignment: .leading, spacing: 2) {
                Label(L("Folytatás"), systemImage: "play.fill").font(.caption2.weight(.semibold)).widgetAccentable()
                Text(card?.title ?? L("Nincs félbehagyott cím")).font(.headline).lineLimit(1)
                if let line = [card?.subtitle, timeLeft(card?.remaining)].compactMap({ $0 }).first {
                    Text(line).font(.caption).lineLimit(1)
                }
                if let p = card?.progress { ProgressView(value: p).tint(.primary) }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .widgetURL(card?.link)
        }
    }
}

/// Still or backdrop filling the space, title and progress on top.
private struct StillCard: View {
    enum Corner { case none, play, watched }

    var card: Card
    var titleSize: CGFloat
    var corner: Corner = .none
    /// Set when the server could not be reached: when the shown data is from.
    var stale: Date??

    var body: some View {
        ZStack(alignment: .bottomLeading) {
            Artwork(data: card.image, seed: card.id)
            LinearGradient(colors: [.clear, Palette.black.opacity(0.9)], startPoint: .center, endPoint: .bottom)
            VStack(alignment: .leading, spacing: 3) {
                if card.isSuggestion {
                    Text(L("ÚJ · KEZDD EL")).font(.caption2.weight(.bold)).foregroundStyle(Palette.amber).widgetAccentable()
                }
                Text(card.title)
                    .font(Palette.display(titleSize))
                    .foregroundStyle(Palette.bright)
                    .lineLimit(2)
                if let sub = card.subtitle {
                    Text(sub).font(Palette.meta).foregroundStyle(Palette.plaster).lineLimit(1)
                }
                if let p = card.progress {
                    Bar(value: p).padding(.top, 3)
                    if let left = timeLeft(card.remaining) {
                        Text(left).font(Palette.meta).foregroundStyle(Palette.muted).lineLimit(1)
                    }
                }
            }
            .padding(12)
        }
        .overlay(alignment: .topTrailing) {
            switch corner {
            case .play:
                Image(systemName: "play.circle.fill")
                    .font(.title2).foregroundStyle(Palette.amber, Palette.black.opacity(0.55))
                    .widgetAccentable().padding(10)
                    .accessibilityHidden(true)
            case .watched:
                WatchedButton(id: card.id, onImage: true).padding(8)
            case .none:
                EmptyView()
            }
        }
        .overlay(alignment: .topLeading) {
            if let stale { StaleBadge(updated: stale).padding(10) }
        }
        .invalidatableContent()
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
                Text(card.title).font(.system(size: 15, weight: .semibold)).foregroundStyle(Palette.bright).lineLimit(1)
                if let line = [card.subtitle, timeLeft(card.remaining)].compactMap({ $0 }).joined(separator: " · ").nilIfEmpty {
                    Text(line).font(Palette.meta).foregroundStyle(Palette.muted).lineLimit(1)
                }
                if let p = card.progress { Bar(value: p) }
            }
            Spacer(minLength: 0)
            Image(systemName: "play.circle.fill").font(.title2).foregroundStyle(Palette.amber).widgetAccentable()
        }
    }
}

private extension String {
    var nilIfEmpty: String? { isEmpty ? nil : self }
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
        .contentMarginsDisabled()
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
                // One poster with its title: a picture alone does not say what it is.
                let c = entry.cards[0]
                ZStack(alignment: .bottomLeading) {
                    Artwork(data: c.image, seed: c.id)
                    LinearGradient(colors: [.clear, Palette.black.opacity(0.92)], startPoint: .center, endPoint: .bottom)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(L("ÚJ")).font(.caption2.weight(.bold)).foregroundStyle(Palette.amber).widgetAccentable()
                        Text(c.title).font(Palette.display(17)).foregroundStyle(Palette.bright).lineLimit(2)
                        if let sub = c.subtitle { Text(sub).font(Palette.meta).foregroundStyle(Palette.plaster) }
                    }
                    .padding(12)
                }
                .overlay(alignment: .topLeading) {
                    if entry.stale { StaleBadge(updated: entry.updated).padding(10) }
                }
                .widgetURL(c.link)
            } else {
                VStack(alignment: .leading, spacing: 8) {
                    Header(title: L("Új a könyvtárban"), stale: entry.stale, updated: entry.updated)
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
                .padding(14)
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

struct Header: View {
    var title: String
    var stale: Bool
    var updated: Date?

    var body: some View {
        HStack {
            Text(title).font(Palette.display(17)).foregroundStyle(Palette.amber).widgetAccentable()
            Spacer()
            if stale { StaleBadge(updated: updated, plain: true) }
        }
    }
}

/// "Server out of reach, data from 14:20": shown while a widget lives on its last good fetch.
struct StaleBadge: View {
    var updated: Date?
    /// Without the dark capsule, on the widget's own background.
    var plain = false

    var body: some View {
        HStack(spacing: 3) {
            Image(systemName: "wifi.slash")
            if let updated { Text(updated, style: .time) }
        }
        .font(.caption2.weight(.semibold))
        .foregroundStyle(plain ? Palette.muted : Palette.plaster)
        .padding(.horizontal, plain ? 0 : 6).padding(.vertical, plain ? 0 : 3)
        .background { if !plain { Capsule().fill(Palette.black.opacity(0.6)) } }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(updated.map { L("A szerver most nem érhető el. Adatok: {0}", $0.formatted(date: .omitted, time: .shortened)) }
                            ?? L("A szerver most nem érhető el"))
    }
}

struct Bar: View {
    var value: Double

    var body: some View {
        GeometryReader { g in
            ZStack(alignment: .leading) {
                Capsule().fill(.white.opacity(0.25))
                Capsule().fill(Palette.amber).frame(width: g.size.width * min(1, max(0, value))).widgetAccentable()
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
                if #available(iOS 18.0, *) {
                    // Tinted home screens: keep the picture readable in the tint instead of a flat block.
                    Image(uiImage: img).resizable().widgetAccentedRenderingMode(.desaturated).scaledToFill()
                } else {
                    Image(uiImage: img).resizable().scaledToFill()
                }
            }
            .clipped()
        } else {
            let hue = Double(abs(seed * 47) % 360) / 360
            LinearGradient(colors: [Color(hue: hue, saturation: 0.25, brightness: 0.28), Palette.panel],
                           startPoint: .topLeading, endPoint: .bottomTrailing)
                .overlay {
                    if let title {
                        Text(title).font(Palette.display(15)).foregroundStyle(Palette.plaster)
                            .multilineTextAlignment(.center).minimumScaleFactor(0.7).padding(6)
                    }
                }
        }
    }
}

/// Opens the app (its login when signed out).
let appLink = URL(string: "\(KinoraLink.scheme)://open")!

struct EmptyState: View {
    var state: FeedEntry.State
    var empty: String

    var body: some View {
        VStack(spacing: 8) {
            Image(systemName: icon).font(.title2).foregroundStyle(Palette.amber).widgetAccentable()
            Text(message).font(.caption).foregroundStyle(Palette.plaster).multilineTextAlignment(.center)
            if state == .signedOut {
                Text(L("Bejelentkezés"))
                    .font(.caption.weight(.bold)).foregroundStyle(Palette.black)
                    .padding(.horizontal, 12).padding(.vertical, 5)
                    .background(Capsule().fill(Palette.amber))
                    .widgetAccentable()
            }
        }
        .padding()
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .widgetURL(state == .signedOut ? appLink : nil)
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
