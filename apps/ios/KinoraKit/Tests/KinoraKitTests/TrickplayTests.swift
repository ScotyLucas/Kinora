import Foundation
import Testing
@testable import KinoraKit

struct TrickplayTests {
    let m = TrickplayManifest(interval: 10, width: 320, height: 180, cols: 10, rows: 10, count: 150, sheets: 2, sig: "1-2")

    @Test func decodesServerAnswer() throws {
        let json = #"{"ready":true,"manifest":{"interval":10,"width":320,"height":180,"cols":10,"rows":10,"count":150,"sheets":2,"sig":"6068608-1795728717"}}"#
        let b = try JSONDecoder.kinora.decode(TrickplayBody.self, from: Data(json.utf8))
        #expect(b.ready && b.manifest?.count == 150 && b.manifest?.sig == "6068608-1795728717")
        let pending = try JSONDecoder.kinora.decode(TrickplayBody.self, from: Data(#"{"ready":false}"#.utf8))
        #expect(!pending.ready && pending.manifest == nil)
    }

    @Test func frames() {
        #expect(m.frame(at: 0) == .init(sheet: 0, x: 0, y: 0, width: 320, height: 180))
        // 33 s rounds to frame 3
        #expect(m.frame(at: 33) == .init(sheet: 0, x: 960, y: 0, width: 320, height: 180))
        // frame 105: second sheet, column 5, row 0
        #expect(m.frame(at: 1050) == .init(sheet: 1, x: 1600, y: 0, width: 320, height: 180))
        // frame 123: second sheet, column 3, row 2
        #expect(m.frame(at: 1230)?.y == 360)
        // past the end and before the start it holds the last and first picture
        #expect(m.frame(at: 99_999)?.sheet == 1 && m.frame(at: 99_999)?.x == 9 * 320 && m.frame(at: 99_999)?.y == 4 * 180)
        #expect(m.frame(at: -5)?.x == 0)
        #expect(m.frame(at: .nan) == nil)
    }

    @Test func sheetPath() {
        #expect(m.sheetPath(item: 7, sheet: 1) == "/api/items/7/trickplay/1.jpg?v=1-2")
    }
}
