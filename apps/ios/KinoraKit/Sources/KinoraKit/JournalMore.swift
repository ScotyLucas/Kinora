import Foundation

// The Napló's second half: stars, viewings added by hand, goals of several kinds, challenges, the titles
// left unfinished, and what the screen works out from the viewings (people, countries, the hour map, records).

/// A goal of the profile beyond the yearly film goal: films, hours, episodes or films of a genre, in a year or a month.
public struct JournalTarget: Codable, Sendable, Hashable, Identifiable {
    public var id: Int64
    /// "year" or "month".
    public var period: String
    /// "films", "hours", "episodes" or "genre".
    public var metric: String
    public var genre: String
    public var target: Int

    public init(id: Int64 = 0, period: String, metric: String, genre: String = "", target: Int) {
        self.id = id
        self.period = period
        self.metric = metric
        self.genre = genre
        self.target = target
    }

    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        id = try c.decodeIfPresent(Int64.self, forKey: .id) ?? 0
        period = try c.decodeIfPresent(String.self, forKey: .period) ?? "year"
        metric = try c.decodeIfPresent(String.self, forKey: .metric) ?? "films"
        genre = try c.decodeIfPresent(String.self, forKey: .genre) ?? ""
        target = try c.decodeIfPresent(Int.self, forKey: .target) ?? 1
    }

    /// The goal's current span: this month or this year.
    public func span(now: Date = Date(), calendar: Calendar = .current) -> (from: Date, to: Date) {
        let c = calendar.dateComponents([.year, .month], from: now)
        if period == "month" {
            let a = calendar.date(from: DateComponents(year: c.year, month: c.month, day: 1))!
            return (a, calendar.date(byAdding: .month, value: 1, to: a)!)
        }
        let a = calendar.date(from: DateComponents(year: c.year, month: 1, day: 1))!
        return (a, calendar.date(byAdding: .year, value: 1, to: a)!)
    }

    /// How far the profile has got in the current span, from its viewings.
    public func value(_ events: [JournalEvent], now: Date = Date(), calendar: Calendar = .current) -> Int {
        let (a, b) = span(now: now, calendar: calendar)
        let list = events.filter { $0.date >= a && $0.date < b }
        switch metric {
        case "hours": return list.reduce(0) { $0 + $1.runtimeSec } / 3600
        case "episodes": return list.filter(\.isEpisode).count
        case "genre":
            let g = genre.lowercased()
            return list.filter { !$0.isEpisode && $0.genres.contains { $0.lowercased() == g } }.count
        default: return list.filter { !$0.isEpisode }.count
        }
    }

    /// What should be done by `now` to keep the pace (rounded down).
    public func due(now: Date = Date(), calendar: Calendar = .current) -> Int {
        let (a, b) = span(now: now, calendar: calendar)
        return Int(Double(target) * now.timeIntervalSince(a) / b.timeIntervalSince(a))
    }
}

/// A tick-off list: every film of a director or an actor, a film series, or the profile's own list.
public struct JournalChallenge: Codable, Sendable, Hashable, Identifiable {
    public struct Title: Codable, Sendable, Hashable, Identifiable {
        public var kind: String
        public var tmdbId: Int64
        public var title: String
        public var year: Int?
        public var poster: String?
        public var watched: Bool
        /// The library item, when the title is in it.
        public var itemId: Int?
        public var id: String { "\(kind)-\(tmdbId)" }

        public init(kind: String = "movie", tmdbId: Int64, title: String, year: Int? = nil, poster: String? = nil, watched: Bool = false, itemId: Int? = nil) {
            self.kind = kind
            self.tmdbId = tmdbId
            self.title = title
            self.year = year
            self.poster = poster
            self.watched = watched
            self.itemId = itemId
        }

        public init(from d: Decoder) throws {
            let c = try d.container(keyedBy: CodingKeys.self)
            kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? "movie"
            tmdbId = try c.decode(Int64.self, forKey: .tmdbId)
            title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
            year = try c.decodeIfPresent(Int.self, forKey: .year)
            poster = try c.decodeIfPresent(String.self, forKey: .poster)
            watched = try c.decodeIfPresent(Bool.self, forKey: .watched) ?? false
            itemId = try c.decodeIfPresent(Int.self, forKey: .itemId)
        }
    }

    public var id: Int64
    public var title: String
    public var items: [Title]
    public var done: Int
    public var total: Int

    public init(id: Int64, title: String, items: [Title]) {
        self.id = id
        self.title = title
        self.items = items
        done = items.filter(\.watched).count
        total = items.count
    }

    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        id = try c.decode(Int64.self, forKey: .id)
        title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
        items = try c.decodeIfPresent([Title].self, forKey: .items) ?? []
        done = try c.decodeIfPresent(Int.self, forKey: .done) ?? items.filter(\.watched).count
        total = try c.decodeIfPresent(Int.self, forKey: .total) ?? items.count
    }

    public var isComplete: Bool { total > 0 && done >= total }
}

/// A film or series found on TMDB (adding a viewing by hand, adding titles to a challenge).
public struct JournalFound: Codable, Sendable, Hashable, Identifiable {
    public var id: Int64
    public var title: String
    public var year: Int?
    public var poster: String?
}

/// A person found on TMDB, to build a challenge from their films.
public struct JournalPerson: Codable, Sendable, Hashable, Identifiable {
    public var id: Int64
    public var name: String
    /// "Directing", "Acting", …
    public var department: String?
    public var photo: String?
    public var knownFor: String?
}

/// A film series (TMDB collection).
public struct JournalCollection: Codable, Sendable, Hashable, Identifiable {
    public var id: Int64
    public var name: String
    public var poster: String?
}

/// What a new challenge is filled from.
public enum JournalChallengeSource: Sendable, Hashable {
    case director(Int64), actor(Int64), collection(Int64), own
}

struct JournalChallengeBody: Encodable, Sendable {
    var title: String
    var items: [JournalChallengeItemBody]
    var personId: Int64?
    var role: String?
    var collectionId: Int64?
}

struct JournalChallengeItemBody: Encodable, Sendable {
    var kind: String
    var tmdbId: Int64
    var title: String
    var year: Int?
    var poster: String?
}

struct JournalIDResponse: Decodable, Sendable { var id: Int64 }

extension KinoraClient {
    /// Stars for a viewing's title, 0…5 (0 clears them); the same stars as on the title's page.
    public func setJournalRating(event: Int64, rating: Int) async throws {
        let _: Empty = try await put("/api/journal/events/\(event)/rating", ["rating": rating])
    }

    /// A film seen outside Kinora, found on TMDB; returns the viewing's id.
    @discardableResult
    public func addJournalViewing(tmdbId: Int64, at: Date, rating: Int) async throws -> Int64 {
        struct Body: Encodable, Sendable { var tmdbId: Int64; var at: Int64; var rating: Int }
        let r: JournalIDResponse = try await post("/api/journal/events", Body(tmdbId: tmdbId, at: Int64(at.timeIntervalSince1970), rating: rating))
        return r.id
    }

    /// Deletes a viewing added by hand (the server refuses the others).
    public func deleteJournalViewing(_ id: Int64) async throws {
        try await delete("/api/journal/events/\(id)")
    }

    private func journalSearchPath(_ q: String, _ type: String) -> String {
        "/api/journal/search?type=\(type)&q=" + (q.addingPercentEncoding(withAllowedCharacters: .urlQueryValueAllowed) ?? q)
    }

    public func journalSearchFilms(_ q: String) async throws -> [JournalFound] {
        try await get(journalSearchPath(q, "movie"))
    }

    public func journalSearchPeople(_ q: String) async throws -> [JournalPerson] {
        try await get(journalSearchPath(q, "person"))
    }

    public func journalSearchCollections(_ q: String) async throws -> [JournalCollection] {
        try await get(journalSearchPath(q, "collection"))
    }

    /// Titles started, stopped before a fifth of the runtime and not touched for two weeks.
    public func journalAbandoned() async throws -> [Item] {
        try await get("/api/journal/abandoned")
    }

    /// Puts a title (an episode: its series) away from the unfinished list.
    public func hideAbandoned(_ itemId: Int) async throws {
        try await post("/api/journal/abandoned/\(itemId)/hide")
    }

    public func journalTargets() async throws -> [JournalTarget] {
        try await get("/api/journal/goals")
    }

    @discardableResult
    public func addJournalTarget(_ t: JournalTarget) async throws -> JournalTarget {
        struct Body: Encodable, Sendable { var period: String; var metric: String; var genre: String; var target: Int }
        return try await post("/api/journal/goals", Body(period: t.period, metric: t.metric, genre: t.genre, target: t.target))
    }

    public func deleteJournalTarget(_ id: Int64) async throws {
        try await delete("/api/journal/goals/\(id)")
    }

    public func journalChallenges() async throws -> [JournalChallenge] {
        try await get("/api/journal/challenges")
    }

    /// A new challenge; an empty title takes the person's or the series' name. Returns its id.
    @discardableResult
    public func addJournalChallenge(title: String, from source: JournalChallengeSource) async throws -> Int64 {
        var b = JournalChallengeBody(title: title, items: [])
        switch source {
        case .director(let id): b.personId = id; b.role = "directing"
        case .actor(let id): b.personId = id; b.role = "acting"
        case .collection(let id): b.collectionId = id
        case .own: break
        }
        let r: JournalIDResponse = try await post("/api/journal/challenges", b)
        return r.id
    }

    public func addJournalChallengeTitles(_ challenge: Int64, _ titles: [JournalFound]) async throws {
        let items = titles.map { JournalChallengeItemBody(kind: "movie", tmdbId: $0.id, title: $0.title, year: $0.year, poster: $0.poster) }
        try await post("/api/journal/challenges/\(challenge)/items", JournalChallengeBody(title: "", items: items))
    }

    public func renameJournalChallenge(_ id: Int64, title: String) async throws {
        let _: Empty = try await patch("/api/journal/challenges/\(id)", ["title": title])
    }

    public func deleteJournalChallenge(_ id: Int64) async throws {
        try await delete("/api/journal/challenges/\(id)")
    }

    public func removeJournalChallengeTitle(_ challenge: Int64, _ t: JournalChallenge.Title) async throws {
        try await delete("/api/journal/challenges/\(challenge)/items/\(t.kind)/\(t.tmdbId)")
    }
}

/// What the Napló works out from a period's viewings beyond the summary: rewatches, stars, people,
/// countries, the weekday × hour map and the records.
public struct JournalInsights: Sendable {
    public struct Count: Sendable, Hashable, Identifiable {
        public var name: String
        public var count: Int
        public var id: String { name }
    }

    /// The how-manieth viewing of its film within the period, by the viewing's position in `events` (1: the first).
    public var nth: [Int]
    /// Films watched more than once in the period, most first.
    public var rewatched: [Count]
    public var directors: [Count]
    public var cast: [Count]
    /// ISO country codes.
    public var countries: [Count]
    /// The average stars of the rated films, and the rated films themselves (best first, one per film).
    public var averageRating: Double?
    public var rated: [JournalEvent]
    /// [weekday (Monday first)][hour] viewings.
    public var heat: [[Int]]

    public var longestStreak: Int
    public var streakEnd: Date?
    public var biggestDay: (date: Date, count: Int)?
    /// The Monday of the busiest week.
    public var biggestWeek: (start: Date, count: Int)?
    /// Weekends with four or more episodes of one series, the biggest first.
    public var binges: [(series: String, count: Int)]

    public var streakStart: Date? {
        streakEnd.flatMap { Calendar.current.date(byAdding: .day, value: -(longestStreak - 1), to: $0) }
    }

    public init(events: [JournalEvent], calendar: Calendar = .current) {
        func top(_ m: [String: Int]) -> [Count] {
            m.map { Count(name: $0.key, count: $0.value) }.sorted { $0.count != $1.count ? $0.count > $1.count : $0.name < $1.name }
        }
        let filmKey = { (e: JournalEvent) in e.tmdbId > 0 ? "t\(e.tmdbId)" : "\(e.title)|\(e.year ?? 0)" }
        var seen: [String: Int] = [:]
        nth = events.map { e in
            guard !e.isEpisode else { return 0 }
            seen[filmKey(e), default: 0] += 1
            return seen[filmKey(e)]!
        }
        var again: [String: Int] = [:], names: [String: String] = [:]
        for e in events where !e.isEpisode { again[filmKey(e), default: 0] += 1; names[filmKey(e)] = e.title }
        rewatched = again.filter { $0.value > 1 }.map { Count(name: names[$0.key] ?? $0.key, count: $0.value) }
            .sorted { $0.count != $1.count ? $0.count > $1.count : $0.name < $1.name }

        // a series counts once per title for its people and countries, a film once per viewing
        var d: [String: Int] = [:], c: [String: Int] = [:], k: [String: Int] = [:], seriesSeen = Set<String>()
        for e in events {
            if e.isEpisode && !seriesSeen.insert(e.showTitle).inserted { continue }
            for x in e.directors where !x.isEmpty { d[x, default: 0] += 1 }
            for x in e.cast where !x.isEmpty { c[x, default: 0] += 1 }
            for x in e.countries where !x.isEmpty { k[x, default: 0] += 1 }
        }
        directors = top(d)
        cast = top(c)
        countries = top(k)

        var best: [String: JournalEvent] = [:]
        for e in events where !e.isEpisode && e.rating > 0 { if (best[filmKey(e)]?.at ?? 0) < e.at { best[filmKey(e)] = e } }
        rated = best.values.sorted { $0.rating != $1.rating ? $0.rating > $1.rating : $0.at > $1.at }
        averageRating = rated.isEmpty ? nil : Double(rated.reduce(0) { $0 + $1.rating }) / Double(rated.count)

        var h = Array(repeating: Array(repeating: 0, count: 24), count: 7)
        for e in events { h[(calendar.component(.weekday, from: e.date) + 5) % 7][calendar.component(.hour, from: e.date)] += 1 }
        heat = h

        var perDay: [Date: Int] = [:]
        for e in events { perDay[calendar.startOfDay(for: e.date), default: 0] += 1 }
        var run = 0, best1 = 0, end: Date?, prev: Date?
        for day in perDay.keys.sorted() {
            let next = prev.flatMap { calendar.date(byAdding: .day, value: 1, to: $0) }
            run = next.map { calendar.isDate($0, inSameDayAs: day) } == true ? run + 1 : 1
            if run > best1 { best1 = run; end = day }
            prev = day
        }
        longestStreak = best1
        streakEnd = end
        biggestDay = perDay.sorted { $0.value != $1.value ? $0.value > $1.value : $0.key < $1.key }.first.map { ($0.key, $0.value) }

        var weeks: [Date: Int] = [:]
        for e in events {
            let day = calendar.startOfDay(for: e.date)
            let monday = calendar.date(byAdding: .day, value: -((calendar.component(.weekday, from: day) + 5) % 7), to: day)!
            weeks[monday, default: 0] += 1
        }
        biggestWeek = weeks.sorted { $0.value != $1.value ? $0.value > $1.value : $0.key < $1.key }.first.map { ($0.key, $0.value) }

        var b: [String: (String, Int)] = [:]
        for e in events where e.isEpisode {
            let wd = calendar.component(.weekday, from: e.date) // Sunday = 1, Saturday = 7
            guard wd == 1 || wd == 7 else { continue }
            let sat = calendar.date(byAdding: .day, value: wd == 1 ? -1 : 0, to: calendar.startOfDay(for: e.date))!
            let key = "\(sat.timeIntervalSince1970)|\(e.showTitle)"
            b[key] = (e.showTitle, (b[key]?.1 ?? 0) + 1)
        }
        binges = b.values.filter { $0.1 >= 4 }.sorted { $0.1 != $1.1 ? $0.1 > $1.1 : $0.0 < $1.0 }.map { (series: $0.0, count: $0.1) }
    }
}
