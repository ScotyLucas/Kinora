import Foundation

/// The timeline preview pictures of an item: a frame every `interval` seconds, `width`×`height`
/// pixels, laid out `cols`×`rows` on JPEG sheets (GET /api/items/{id}/trickplay).
public struct TrickplayManifest: Codable, Sendable, Equatable {
    public var interval: Int
    public var width: Int
    public var height: Int
    public var cols: Int
    public var rows: Int
    public var count: Int
    public var sheets: Int
    /// Changes with the source file; part of the sheet URL so a cached sheet is never stale.
    public var sig: String

    public init(interval: Int, width: Int, height: Int, cols: Int, rows: Int, count: Int, sheets: Int, sig: String) {
        self.interval = interval
        self.width = width
        self.height = height
        self.cols = cols
        self.rows = rows
        self.count = count
        self.sheets = sheets
        self.sig = sig
    }

    /// Where the picture for a time is: the sheet and the pixel rectangle on it.
    public struct Frame: Sendable, Equatable {
        public var sheet: Int
        public var x: Int
        public var y: Int
        public var width: Int
        public var height: Int
    }

    public func frame(at seconds: Double) -> Frame? {
        guard count > 0, interval > 0, cols > 0, rows > 0, seconds.isFinite else { return nil }
        let i = min(count - 1, max(0, Int((seconds / Double(interval)).rounded())))
        let per = cols * rows
        let k = i % per
        return Frame(sheet: i / per, x: (k % cols) * width, y: (k / cols) * height, width: width, height: height)
    }

    /// The server path of sheet n.
    public func sheetPath(item: Int, sheet n: Int) -> String {
        "/api/items/\(item)/trickplay/\(n).jpg?v=" + (sig.addingPercentEncoding(withAllowedCharacters: .urlQueryValueAllowed) ?? sig)
    }
}

struct TrickplayBody: Decodable, Sendable {
    var ready: Bool
    var manifest: TrickplayManifest?
}

extension KinoraClient {
    /// The item's preview sheets, or nil while the server is still making them (or an older
    /// server has none).
    public func trickplay(_ id: Int) async throws -> TrickplayManifest? {
        let b = try await get("/api/items/\(id)/trickplay", as: TrickplayBody.self)
        guard b.ready, let m = b.manifest, m.count > 0 else { return nil }
        return m
    }
}
