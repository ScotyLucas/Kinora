import SwiftUI
import WidgetKit
import KinoraKit

// MARK: - Napló: this month in numbers

/// This month's viewings for the widget, worked out from the Napló's events (one count per day).
struct JournalStat: Codable, Equatable {
    var month: Date
    var films: Int
    var episodes: Int
    var days: [Int]
    /// The index of today in `days` (later days are still ahead).
    var today: Int

    var total: Int { films + episodes }
}

struct JournalEntry: TimelineEntry {
    enum State: Equatable {
        case stat(JournalStat)
        case signedOut
        case offline
    }

    var date: Date
    var state: State
    var stale = false
}

enum JournalFeed {
    static func load() async -> JournalEntry {
        Language.shared.reloadIfNeeded()
        guard let client = Feed.client() else { return JournalEntry(date: .now, state: .signedOut) }
        let now = Date.now, cal = Calendar.current
        let r = JournalPeriod.month.range(now: now, calendar: cal)
        do {
            let evs = try await client.journal(from: r.from, to: r.to)
            let s = JournalSummary(events: evs, period: .month, now: now, calendar: cal)
            let stat = JournalStat(month: r.from, films: s.films, episodes: s.episodes,
                                   days: s.buckets.map(\.events.count),
                                   today: cal.component(.day, from: now) - 1)
            save(stat)
            return JournalEntry(date: now, state: .stat(stat))
        } catch KinoraError.unauthorized {
            return JournalEntry(date: now, state: .signedOut)
        } catch {
            // Last month's numbers are no answer for this month.
            if let c = cached(), cal.isDate(c.month, equalTo: now, toGranularity: .month) {
                return JournalEntry(date: now, state: .stat(c), stale: true)
            }
            return JournalEntry(date: now, state: .offline)
        }
    }

    private static var file: URL? { AppGroup.container?.appending(path: "Library/Caches/widget-journal.json") }

    private static func save(_ s: JournalStat) {
        guard let file, let data = try? JSONEncoder().encode(s) else { return }
        try? FileManager.default.createDirectory(at: file.deletingLastPathComponent(), withIntermediateDirectories: true)
        try? data.write(to: file, options: .atomic)
    }

    private static func cached() -> JournalStat? {
        guard let file, let data = try? Data(contentsOf: file) else { return nil }
        return try? JSONDecoder().decode(JournalStat.self, from: data)
    }

    static var sample: JournalStat {
        let cal = Calendar.current
        let start = cal.dateInterval(of: .month, for: .now)?.start ?? .now
        let days = cal.range(of: .day, in: .month, for: .now)?.count ?? 30
        let pattern = [0, 1, 0, 2, 0, 0, 3, 1, 0, 0, 1, 0, 4, 2, 0, 1, 0, 0, 2, 0, 1, 3, 0, 0, 1, 0, 2, 1, 0, 0, 1]
        let today = cal.component(.day, from: .now) - 1
        let d = (0..<days).map { $0 <= today ? pattern[$0 % pattern.count] : 0 }
        return JournalStat(month: start, films: d.reduce(0, +) / 3, episodes: d.reduce(0, +) - d.reduce(0, +) / 3, days: d, today: today)
    }
}

struct JournalProvider: TimelineProvider {
    func placeholder(in context: Context) -> JournalEntry {
        JournalEntry(date: .now, state: .stat(JournalFeed.sample))
    }

    func getSnapshot(in context: Context, completion: @escaping (JournalEntry) -> Void) {
        if context.isPreview { return completion(placeholder(in: context)) }
        let done = Handoff(completion)
        Task { done.call(await JournalFeed.load()) }
    }

    func getTimeline(in context: Context, completion: @escaping (Timeline<JournalEntry>) -> Void) {
        let done = Handoff(completion)
        Task {
            let entry = await JournalFeed.load()
            // The app reloads the widgets after playback; this catches viewings elsewhere and the new day.
            var next = Date.now.addingTimeInterval(entry.stale || entry.state == .offline ? 15 * 60 : 60 * 60)
            if let midnight = Calendar.current.nextDate(after: .now, matching: DateComponents(hour: 0, minute: 1), matchingPolicy: .nextTime),
               midnight < next { next = midnight }
            done.call(Timeline(entries: [entry], policy: .after(next)))
        }
    }
}

struct JournalWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "journal", provider: JournalProvider()) { entry in
            JournalWidgetView(entry: entry)
        }
        .configurationDisplayName(L("Napló"))
        .description(L("Hány filmet és epizódot láttál ebben a hónapban."))
        .supportedFamilies([.systemSmall, .systemMedium, .accessoryRectangular, .accessoryInline])
    }
}

/// The widget's link into the app: the Napló sheet on this month.
let journalLink = URL(string: "\(KinoraLink.scheme)://journal")!

struct JournalWidgetView: View {
    @Environment(\.widgetFamily) private var family
    var entry: JournalEntry

    var body: some View {
        Group {
            switch entry.state {
            case .stat(let s):
                switch family {
                case .accessoryInline: inline(s)
                case .accessoryRectangular: rectangular(s)
                case .systemMedium: medium(s)
                default: small(s)
                }
            case .signedOut:
                message(L("Jelentkezz be a Kinora appban."), icon: "person.crop.circle")
            case .offline:
                message(L("A szerver most nem érhető el."), icon: "wifi.slash")
            }
        }
        .widgetURL(journalLink)
        .containerBackground(for: .widget) { Palette.black }
    }

    private func monthName(_ s: JournalStat) -> String {
        let m = s.month.formatted(.dateTime.month(.wide))
        return m.prefix(1).uppercased() + m.dropFirst()
    }

    private func small(_ s: JournalStat) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            kicker(s)
            Spacer(minLength: 0)
            Text(String(s.total)).font(Palette.display(48)).foregroundStyle(Palette.amber)
                .minimumScaleFactor(0.5).lineLimit(1).contentTransition(.numericText())
            Text(L("{0} film", s.films) + " · " + L("{0} epizód", s.episodes))
                .font(.caption2).foregroundStyle(Palette.plaster).lineLimit(1).minimumScaleFactor(0.7)
            Spacer(minLength: 0)
            JournalBars(stat: s).frame(height: 26)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
    }

    private func medium(_ s: JournalStat) -> some View {
        HStack(alignment: .top, spacing: 16) {
            VStack(alignment: .leading, spacing: 4) {
                kicker(s)
                Spacer(minLength: 0)
                Text(String(s.total)).font(Palette.display(56)).foregroundStyle(Palette.amber)
                    .minimumScaleFactor(0.5).lineLimit(1).contentTransition(.numericText())
                Text(Self.word(L("{0} nézés", s.total), s.total)).font(.caption).foregroundStyle(Palette.plaster).lineLimit(1)
                Spacer(minLength: 0)
            }
            .frame(width: 104, alignment: .leading)
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 14) {
                    figure(s.films, L("{0} film", s.films))
                    figure(s.episodes, L("{0} epizód", s.episodes))
                }
                Spacer(minLength: 0)
                JournalBars(stat: s).frame(maxHeight: 56)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
        }
    }

    private func rectangular(_ s: JournalStat) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(L("{0} nézés", s.total)).font(.headline).widgetAccentable()
            Text(L("{0} film", s.films) + " · " + L("{0} epizód", s.episodes)).font(.caption2).lineLimit(1)
            JournalBars(stat: s, plain: true).frame(height: 14)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func inline(_ s: JournalStat) -> some View {
        Label(monthName(s) + ": " + L("{0} nézés", s.total), systemImage: "chart.bar.xaxis")
    }

    private func kicker(_ s: JournalStat) -> some View {
        HStack(spacing: 4) {
            Image(systemName: "chart.bar.xaxis").imageScale(.small)
            Text(monthName(s)).lineLimit(1)
            if entry.stale { Image(systemName: "wifi.slash").imageScale(.small) }
        }
        .font(.caption.weight(.bold)).foregroundStyle(Palette.plaster)
    }

    private func figure(_ n: Int, _ label: String) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(String(n)).font(Palette.display(24)).foregroundStyle(Palette.bright)
            Text(Self.word(label, n))
                .font(.caption2).foregroundStyle(Palette.muted).lineLimit(1)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(label)
    }

    /// "18 nézés" → "nézés": the label under a big number that already says 18.
    private static func word(_ label: String, _ n: Int) -> String {
        label.replacingOccurrences(of: String(n), with: "").trimmingCharacters(in: .whitespaces)
    }

    private func message(_ text: String, icon: String) -> some View {
        VStack(spacing: 6) {
            Image(systemName: icon).font(.title3).foregroundStyle(Palette.amber)
            Text(text).font(.caption).foregroundStyle(Palette.plaster).multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

/// The month day by day: one bar per day, today in amber, days still ahead as dots.
struct JournalBars: View {
    var stat: JournalStat
    /// On the lock screen: no colours of its own, the system tints it.
    var plain = false

    var body: some View {
        let top = max(1, stat.days.max() ?? 1)
        GeometryReader { geo in
            let n = max(1, stat.days.count)
            let gap: CGFloat = n > 20 ? 1.5 : 2
            let w = max(1, (geo.size.width - gap * CGFloat(n - 1)) / CGFloat(n))
            HStack(alignment: .bottom, spacing: gap) {
                ForEach(Array(stat.days.enumerated()), id: \.offset) { i, c in
                    RoundedRectangle(cornerRadius: min(1.5, w / 2))
                        .fill(color(i, c))
                        .frame(width: w, height: c == 0 ? 2 : max(3, geo.size.height * CGFloat(c) / CGFloat(top)))
                }
            }
            .frame(width: geo.size.width, height: geo.size.height, alignment: .bottomLeading)
        }
        .accessibilityHidden(true)
    }

    private func color(_ i: Int, _ c: Int) -> Color {
        if plain { return .primary.opacity(i > stat.today ? 0.2 : (c == 0 ? 0.35 : 1)) }
        if i > stat.today { return Palette.plaster.opacity(0.12) }
        if i == stat.today && c > 0 { return Palette.amber }
        return c == 0 ? Palette.plaster.opacity(0.22) : Palette.amber.opacity(0.6)
    }
}
