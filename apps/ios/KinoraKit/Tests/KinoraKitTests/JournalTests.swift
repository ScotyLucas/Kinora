import Foundation
import Testing
@testable import KinoraKit

struct JournalTests {
    let cal: Calendar = {
        var c = Calendar(identifier: .gregorian)
        c.timeZone = TimeZone(identifier: "Europe/Budapest")!
        return c
    }()

    func at(_ y: Int, _ m: Int, _ d: Int, _ h: Int = 21) -> Int64 {
        Int64(cal.date(from: DateComponents(year: y, month: m, day: d, hour: h))!.timeIntervalSince1970)
    }

    @Test func decodesServerAnswer() throws {
        let json = #"{"from":0,"to":10,"events":[{"at":5,"kind":"episode","title":"Pilot","series_title":"Show","season":1,"episode":2,"runtime_sec":1200,"genres":["Dráma"],"source":"play","item_id":7,"image_id":3,"updated_at":9},{"at":6,"kind":"movie","title":"Gone","runtime_sec":0,"genres":[],"source":"backfill"}]}"#
        let r = try JSONDecoder.kinora.decode(JournalResponse.self, from: Data(json.utf8))
        #expect(r.events.count == 2)
        #expect(r.events[0].showTitle == "Show" && r.events[0].imageId == 3 && r.events[0].runtimeSec == 1200)
        #expect(r.events[1].itemId == nil && !r.events[1].isEpisode)
    }

    @Test func summary() {
        let now = cal.date(from: DateComponents(year: 2026, month: 11, day: 26))!
        let evs = [
            JournalEvent(at: at(2026, 3, 2), title: "A", year: 1994, runtimeSec: 7200, genres: ["Sci-fi", "Dráma"]),
            JournalEvent(at: at(2026, 11, 3, 1), title: "B", year: 1999, runtimeSec: 3600, genres: ["Sci-fi"]),
            // a binge: three episodes of one series on one day count once in the genres
            JournalEvent(at: at(2026, 11, 7), kind: "episode", title: "e1", seriesTitle: "S", runtimeSec: 1800, genres: ["Vígjáték"]),
            JournalEvent(at: at(2026, 11, 7, 22), kind: "episode", title: "e2", seriesTitle: "S", runtimeSec: 1800, genres: ["Vígjáték"]),
            JournalEvent(at: at(2026, 11, 7, 23), kind: "episode", title: "e3", seriesTitle: "S", runtimeSec: 1800, genres: ["Vígjáték"]),
        ]
        let s = JournalSummary(events: evs, period: .year, now: now, calendar: cal)
        #expect(s.films == 2 && s.episodes == 3 && s.shows == 1)
        #expect(s.seconds == 7200 + 3600 + 3 * 1800)
        #expect(s.genres.first?.name == "Sci-fi" && s.genres.first?.count == 2)
        #expect(s.genreBase == 3 && s.topGenreShare == 67)
        #expect(s.decades.first?.decade == 1990 && s.decades.first?.count == 2)
        #expect(s.buckets.count == 12 && s.buckets[10].events.count == 4 && s.buckets[2].events.count == 1)
        #expect(s.latestBucket?.key == 11)
        #expect(s.afterMidnight == 1)
        #expect(s.weekdays[5] == 3) // 2026-11-07 was a Saturday
        #expect(s.topTitles.first?.title == "S" && s.topTitles.first?.count == 3 && s.topTitles.first?.isSeries == true)
        #expect(s.topTitles.map(\.title) == ["S", "B", "A"]) // B (November) was watched after A (March)
        #expect(s.busiestBucket?.key == 11)
        #expect(s.biggestDay?.count == 3 && cal.component(.day, from: s.biggestDay!.date) == 7)

        let m = JournalSummary(events: evs.filter { $0.at >= at(2026, 11, 1, 0) }, period: .month, now: now, calendar: cal)
        #expect(m.buckets.count == 30 && m.buckets[6].events.count == 3)
        #expect(JournalSummary(events: [], period: .all, now: now, calendar: cal).isEmpty)
    }

    @Test func previousRangeAndGoal() {
        let now = cal.date(from: DateComponents(year: 2026, month: 3, day: 31, hour: 12))!
        // the same days of the month before, cut at its end (February has no 31st)
        let m = JournalPeriod.month.previousRange(now: now, calendar: cal)!
        #expect(m.from == cal.date(from: DateComponents(year: 2026, month: 2, day: 1)))
        #expect(m.to == cal.date(from: DateComponents(year: 2026, month: 3, day: 1)))
        let y = JournalPeriod.year.previousRange(now: now, calendar: cal)!
        #expect(cal.component(.year, from: y.from) == 2025 && cal.component(.month, from: y.to) == 3 && cal.component(.year, from: y.to) == 2025)
        let l = JournalPeriod.lastYear.previousRange(now: now, calendar: cal)!
        #expect(l.from == cal.date(from: DateComponents(year: 2024, month: 1, day: 1)) && l.to == cal.date(from: DateComponents(year: 2025, month: 1, day: 1)))
        #expect(JournalPeriod.all.previousRange(now: now, calendar: cal) == nil)

        let mid = cal.date(from: DateComponents(year: 2026, month: 7, day: 2, hour: 12))! // about half the year
        let p = JournalGoal.pace(films: 40, goal: 100, now: mid, calendar: cal)
        #expect(p.due == 49 && p.ahead == -9)
    }
}
