import Foundation
import Testing
@testable import KinoraKit

struct JournalMoreTests {
    let cal: Calendar = {
        var c = Calendar(identifier: .gregorian)
        c.timeZone = TimeZone(identifier: "Europe/Budapest")!
        return c
    }()

    func at(_ y: Int, _ m: Int, _ d: Int, _ h: Int = 21) -> Int64 {
        Int64(cal.date(from: DateComponents(year: y, month: m, day: d, hour: h))!.timeIntervalSince1970)
    }

    @Test func decodesNewFields() throws {
        let json = #"{"from":0,"to":10,"events":[{"id":42,"at":5,"kind":"movie","title":"Sherlock Jr.","tmdb_id":992,"rating":4,"poster":"https://image.tmdb.org/t/p/w185/x.jpg","source":"manual","directors":["Buster Keaton"],"cast":["Buster Keaton","Kathryn McGuire"],"countries":["US"]},{"at":6,"title":"Old server"}]}"#
        let r = try JSONDecoder.kinora.decode(JournalResponse.self, from: Data(json.utf8))
        let e = r.events[0]
        #expect(e.id == 42 && e.tmdbId == 992 && e.rating == 4 && e.isManual)
        #expect(e.poster?.hasPrefix("https://image.tmdb.org/") == true)
        #expect(e.directors == ["Buster Keaton"] && e.cast.count == 2 && e.countries == ["US"])
        // an older server sends none of them
        #expect(r.events[1].id == 0 && r.events[1].rating == 0 && r.events[1].directors.isEmpty && !r.events[1].isManual)

        let ch = try JSONDecoder.kinora.decode([JournalChallenge].self, from: Data(#"[{"id":1,"title":"Keaton","items":[{"kind":"movie","tmdb_id":992,"title":"Sherlock Jr.","year":1924,"watched":true,"item_id":7},{"kind":"movie","tmdb_id":961,"title":"The General"}],"done":1,"total":2,"created_at":1}]"#.utf8))
        #expect(ch[0].done == 1 && ch[0].total == 2 && !ch[0].isComplete)
        #expect(ch[0].items[0].itemId == 7 && ch[0].items[1].watched == false && ch[0].items[1].id == "movie-961")

        let g = try JSONDecoder.kinora.decode([JournalTarget].self, from: Data(#"[{"id":3,"period":"month","metric":"genre","genre":"Horror","target":4}]"#.utf8))
        #expect(g[0].period == "month" && g[0].genre == "Horror" && g[0].target == 4)
    }

    @Test func targets() {
        let now = cal.date(from: DateComponents(year: 2026, month: 11, day: 16, hour: 0))! // half of November
        let evs = [
            JournalEvent(at: at(2026, 10, 30), title: "October", runtimeSec: 7200, genres: ["Horror"]),
            JournalEvent(at: at(2026, 11, 2), title: "A", runtimeSec: 5400, genres: ["horror", "Dráma"]),
            JournalEvent(at: at(2026, 11, 3), title: "B", runtimeSec: 5400, genres: ["Vígjáték"]),
            JournalEvent(at: at(2026, 11, 4), kind: "episode", title: "e1", seriesTitle: "S", runtimeSec: 1800, genres: ["Horror"]),
        ]
        let month = JournalTarget(period: "month", metric: "films", target: 4)
        #expect(month.value(evs, now: now, calendar: cal) == 2)
        #expect(month.due(now: now, calendar: cal) == 2)
        #expect(JournalTarget(period: "year", metric: "films", target: 50).value(evs, now: now, calendar: cal) == 3)
        #expect(JournalTarget(period: "month", metric: "hours", target: 10).value(evs, now: now, calendar: cal) == 3) // 3.5 hours, rounded down
        #expect(JournalTarget(period: "month", metric: "episodes", target: 10).value(evs, now: now, calendar: cal) == 1)
        // a genre goal counts films only, in any letter case
        #expect(JournalTarget(period: "month", metric: "genre", genre: "Horror", target: 2).value(evs, now: now, calendar: cal) == 1)
    }

    @Test func insights() {
        let evs = [
            // a three-day streak, 7 to 9 November; the film twice
            JournalEvent(at: at(2026, 11, 7, 10), title: "Sherlock Jr.", year: 1924, id: 1, tmdbId: 992, rating: 4,
                         directors: ["Buster Keaton"], cast: ["Buster Keaton"], countries: ["US"]),
            JournalEvent(at: at(2026, 11, 7, 11), kind: "episode", title: "e1", seriesTitle: "S", directors: ["Creator"], cast: ["Lead"], countries: ["GB"]),
            JournalEvent(at: at(2026, 11, 7, 12), kind: "episode", title: "e2", seriesTitle: "S", directors: ["Creator"], cast: ["Lead"], countries: ["GB"]),
            JournalEvent(at: at(2026, 11, 8, 13), kind: "episode", title: "e3", seriesTitle: "S", directors: ["Creator"], cast: ["Lead"], countries: ["GB"]),
            JournalEvent(at: at(2026, 11, 8, 14), kind: "episode", title: "e4", seriesTitle: "S", directors: ["Creator"], cast: ["Lead"], countries: ["GB"]),
            JournalEvent(at: at(2026, 11, 9), title: "Sherlock Jr.", year: 1924, id: 2, tmdbId: 992, rating: 4, directors: ["Buster Keaton"], cast: ["Buster Keaton"], countries: ["US"]),
            JournalEvent(at: at(2026, 11, 20), title: "The General", year: 1926, id: 3, tmdbId: 961, rating: 5, directors: ["Buster Keaton", "Clyde Bruckman"], countries: ["US"]),
        ]
        let i = JournalInsights(events: evs, calendar: cal)
        #expect(i.nth == [1, 0, 0, 0, 0, 2, 1])
        #expect(i.rewatched.map(\.name) == ["Sherlock Jr."] && i.rewatched[0].count == 2)
        #expect(i.directors.first?.name == "Buster Keaton" && i.directors.first?.count == 3)
        #expect(i.directors.first { $0.name == "Creator" }?.count == 1) // a series counts once
        #expect(i.countries.map(\.name) == ["US", "GB"])
        // one rating per film: the best first, the average over films
        #expect(i.rated.map(\.title) == ["The General", "Sherlock Jr."] && i.averageRating == 4.5)
        #expect(i.heat[5][10] == 1 && i.heat[5].reduce(0, +) == 3) // Saturday
        #expect(i.longestStreak == 3 && cal.component(.day, from: i.streakEnd!) == 9)
        #expect(i.biggestDay?.count == 3 && cal.component(.day, from: i.biggestDay!.date) == 7)
        #expect(i.biggestWeek?.count == 5 && cal.component(.day, from: i.biggestWeek!.start) == 2) // the week of Monday 2 Nov
        #expect(i.binges.count == 1 && i.binges[0].series == "S" && i.binges[0].count == 4)
    }
}
