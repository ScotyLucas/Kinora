import Foundation

/// One finished viewing in the Napló (GET /api/journal). The server keeps a snapshot of the title,
/// so a viewing stays after its file is gone; `itemId` and `imageId` are then nil.
public struct JournalEvent: Codable, Sendable, Hashable {
    public var at: Int64
    public var kind: String
    public var title: String
    public var seriesTitle: String?
    public var season: Int?
    public var episode: Int?
    public var year: Int?
    public var runtimeSec: Int
    public var genres: [String]
    public var source: String
    public var itemId: Int?
    public var imageId: Int?
    public var updatedAt: Int64?

    // The client decodes with .convertFromSnakeCase: series_title arrives as seriesTitle.
    enum CodingKeys: String, CodingKey {
        case at, kind, title, seriesTitle, season, episode, year, runtimeSec, genres, source, itemId, imageId, updatedAt
    }

    public init(at: Int64, kind: String = "movie", title: String, seriesTitle: String? = nil, season: Int? = nil, episode: Int? = nil,
                year: Int? = nil, runtimeSec: Int = 0, genres: [String] = [], source: String = "play",
                itemId: Int? = nil, imageId: Int? = nil, updatedAt: Int64? = nil) {
        self.at = at
        self.kind = kind
        self.title = title
        self.seriesTitle = seriesTitle
        self.season = season
        self.episode = episode
        self.year = year
        self.runtimeSec = runtimeSec
        self.genres = genres
        self.source = source
        self.itemId = itemId
        self.imageId = imageId
        self.updatedAt = updatedAt
    }

    public var isEpisode: Bool { kind == "episode" }
    /// The film, or the series of an episode.
    public var showTitle: String { isEpisode ? (seriesTitle?.isEmpty == false ? seriesTitle! : title) : title }
    public var date: Date { Date(timeIntervalSince1970: TimeInterval(at)) }

    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        at = try c.decode(Int64.self, forKey: .at)
        kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? "movie"
        title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
        seriesTitle = try c.decodeIfPresent(String.self, forKey: .seriesTitle)
        season = try c.decodeIfPresent(Int.self, forKey: .season)
        episode = try c.decodeIfPresent(Int.self, forKey: .episode)
        year = try c.decodeIfPresent(Int.self, forKey: .year)
        runtimeSec = try c.decodeIfPresent(Int.self, forKey: .runtimeSec) ?? 0
        genres = try c.decodeIfPresent([String].self, forKey: .genres) ?? []
        source = try c.decodeIfPresent(String.self, forKey: .source) ?? "play"
        itemId = try c.decodeIfPresent(Int.self, forKey: .itemId)
        imageId = try c.decodeIfPresent(Int.self, forKey: .imageId)
        updatedAt = try c.decodeIfPresent(Int64.self, forKey: .updatedAt)
    }
}

struct JournalResponse: Codable, Sendable {
    var from: Int64
    var to: Int64
    var events: [JournalEvent]
}

extension KinoraClient {
    /// The profile's viewings in [from, to), oldest first.
    public func journal(from: Date, to: Date) async throws -> [JournalEvent] {
        let r: JournalResponse = try await get("/api/journal?from=\(Int64(from.timeIntervalSince1970))&to=\(Int64(to.timeIntervalSince1970))")
        return r.events
    }

    /// Closes the home page's recap card of a month (YYYY-MM) on every device of the profile.
    public func journalRecapSeen(month: String) async throws {
        try await post("/api/journal/recap-seen", ["month": month])
    }
}

/// The periods the Napló offers, and the bars of each one's timeline.
public enum JournalPeriod: String, CaseIterable, Sendable, Identifiable {
    case month, year, lastYear, all
    public var id: String { rawValue }

    public enum Unit: Sendable { case day, month, year }

    /// [start, end) and the bar unit, in `calendar`'s time zone.
    public func range(now: Date = Date(), calendar: Calendar = .current) -> (from: Date, to: Date, unit: Unit) {
        let y = calendar.component(.year, from: now), m = calendar.component(.month, from: now)
        let d = { (y: Int, m: Int) in calendar.date(from: DateComponents(year: y, month: m, day: 1))! }
        switch self {
        case .month: return (d(y, m), calendar.date(byAdding: .month, value: 1, to: d(y, m))!, .day)
        case .year: return (d(y, 1), d(y + 1, 1), .month)
        case .lastYear: return (d(y - 1, 1), d(y, 1), .month)
        case .all: return (Date(timeIntervalSince1970: 0), d(y + 1, 1), .year)
        }
    }

    /// The stretch the period is compared with: this month and this year against the same days of the month and
    /// year before, last year against the year before it; nothing for all time.
    public func previousRange(now: Date = Date(), calendar: Calendar = .current) -> (from: Date, to: Date)? {
        let r = range(now: now, calendar: calendar)
        switch self {
        case .month, .year:
            let from = calendar.date(byAdding: self == .month ? .month : .year, value: -1, to: r.from)!
            return (from, min(from.addingTimeInterval(now.timeIntervalSince(r.from)), r.from))
        case .lastYear: return (calendar.date(byAdding: .year, value: -1, to: r.from)!, r.from)
        case .all: return nil
        }
    }
}

/// The Napló's yearly goal: how many films should have been watched by `now` to keep the pace.
public enum JournalGoal {
    /// Films due by `now` (rounded down) and how far the profile is ahead of that (negative: behind).
    public static func pace(films: Int, goal: Int, now: Date = Date(), calendar: Calendar = .current) -> (due: Int, ahead: Int) {
        let y = calendar.component(.year, from: now)
        let start = calendar.date(from: DateComponents(year: y, month: 1, day: 1))!
        let end = calendar.date(from: DateComponents(year: y + 1, month: 1, day: 1))!
        let due = Int(Double(goal) * now.timeIntervalSince(start) / end.timeIntervalSince(start))
        return (due, films - due)
    }
}

/// Everything the Napló screen shows, worked out from the viewings of a period.
public struct JournalSummary: Sendable {
    public struct Bucket: Sendable, Identifiable, Hashable {
        /// Day of month, month (1…12) or year.
        public var key: Int
        public var start: Date
        public var events: [JournalEvent]
        public var id: Int { key }
    }

    /// A film or series by how often it was watched in the period (a series counts every episode).
    public struct TopTitle: Sendable, Hashable {
        public var title: String
        public var count: Int
        public var imageId: Int?
        public var isSeries: Bool
    }

    public var films: Int
    public var episodes: Int
    public var shows: Int
    public var seconds: Int
    /// Most watched first. A series counts once per viewing day, so a binge does not drown out the films.
    public var genres: [(name: String, count: Int)]
    public var genreBase: Int
    public var decades: [(decade: Int, count: Int)]
    /// Monday first.
    public var weekdays: [Int]
    public var hours: [Int]
    public var buckets: [Bucket]
    public var unit: JournalPeriod.Unit
    /// Most watched first; on a tie the one watched last comes first.
    public var topTitles: [TopTitle]
    /// The day with the most viewings (the earliest of equals), for the recap's "marathon" page.
    public var biggestDay: (date: Date, count: Int)?

    public var isEmpty: Bool { films + episodes == 0 }
    public var peakHour: Int { hours.indices.max { hours[$0] < hours[$1] } ?? 21 }
    /// Viewings that started between midnight and five in the morning.
    public var afterMidnight: Int { hours[0..<5].reduce(0, +) }
    public var busiestWeekday: Int { weekdays.indices.max { weekdays[$0] < weekdays[$1] } ?? 0 }
    public var topGenreShare: Int? {
        guard let g = genres.first, genreBase > 0 else { return nil }
        return Int((Double(g.count) / Double(genreBase) * 100).rounded())
    }
    /// The latest bucket with something in it (the one shown open at first).
    public var latestBucket: Bucket? { buckets.last { !$0.events.isEmpty } }
    /// The bucket with the most viewings (the earliest of equals).
    public var busiestBucket: Bucket? {
        buckets.reduce(nil) { best, b in b.events.isEmpty || (best?.events.count ?? 0) >= b.events.count ? best : b }
    }

    public init(events: [JournalEvent], period: JournalPeriod, now: Date = Date(), calendar: Calendar = .current) {
        let (from, _, unit) = period.range(now: now, calendar: calendar)
        self.unit = unit
        let films = events.filter { !$0.isEpisode }, eps = events.filter(\.isEpisode)
        self.films = films.count
        episodes = eps.count
        shows = Set(eps.map(\.showTitle)).count
        seconds = events.reduce(0) { $0 + $1.runtimeSec }

        var seen = Set<String>()
        let dayKey = { (e: JournalEvent) in "\(e.showTitle)|\(calendar.startOfDay(for: e.date).timeIntervalSince1970)" }
        let base = films + eps.filter { seen.insert(dayKey($0)).inserted }
        genreBase = base.count
        var g: [String: Int] = [:]
        for e in base { for x in e.genres where !x.isEmpty { g[x, default: 0] += 1 } }
        genres = g.map { ($0.key, $0.value) }.sorted { $0.1 != $1.1 ? $0.1 > $1.1 : $0.0 < $1.0 }
        var dec: [Int: Int] = [:]
        for e in events { if let y = e.year, y > 0 { dec[y / 10 * 10, default: 0] += 1 } }
        decades = dec.map { ($0.key, $0.value) }.sorted { $0.1 != $1.1 ? $0.1 > $1.1 : $0.0 > $1.0 }

        var wd = Array(repeating: 0, count: 7), hr = Array(repeating: 0, count: 24)
        for e in events {
            wd[(calendar.component(.weekday, from: e.date) + 5) % 7] += 1 // Sunday = 1
            hr[calendar.component(.hour, from: e.date)] += 1
        }
        weekdays = wd
        hours = hr

        let comp: Calendar.Component = unit == .day ? .day : unit == .month ? .month : .year
        var keys: [Int]
        let startOf: (Int) -> Date
        switch unit {
        case .day:
            keys = Array(calendar.range(of: .day, in: .month, for: from) ?? 1..<31)
            startOf = { calendar.date(byAdding: .day, value: $0 - 1, to: from)! }
        case .month:
            keys = Array(1...12)
            startOf = { calendar.date(byAdding: .month, value: $0 - 1, to: from)! }
        case .year:
            let last = calendar.component(.year, from: now)
            let first = events.first.map { calendar.component(.year, from: $0.date) } ?? last
            keys = Array(min(first, last)...last)
            startOf = { calendar.date(from: DateComponents(year: $0, month: 1, day: 1))! }
        }
        var by: [Int: [JournalEvent]] = [:]
        for e in events { by[calendar.component(comp, from: e.date), default: []].append(e) }
        buckets = keys.map { Bucket(key: $0, start: startOf($0), events: by[$0] ?? []) }

        var tops: [String: (TopTitle, Int64)] = [:]
        for e in events {
            var t = tops[e.showTitle]?.0 ?? TopTitle(title: e.showTitle, count: 0, imageId: e.imageId, isSeries: e.isEpisode)
            t.count += 1
            if t.imageId == nil { t.imageId = e.imageId }
            tops[e.showTitle] = (t, max(tops[e.showTitle]?.1 ?? 0, e.at))
        }
        topTitles = tops.values.sorted { $0.0.count != $1.0.count ? $0.0.count > $1.0.count : $0.1 > $1.1 }.map(\.0)
        var perDay: [Date: Int] = [:]
        for e in events { perDay[calendar.startOfDay(for: e.date), default: 0] += 1 }
        biggestDay = perDay.sorted { $0.value != $1.value ? $0.value > $1.value : $0.key < $1.key }.first.map { ($0.key, $0.value) }
    }
}
