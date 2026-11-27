import Foundation

// Watch party: the server's room (internal/party) over a WebSocket.
//
// The server owns the clock. Its state says where the room is (`pos` at server time `at`) and
// whether it plays; a client estimates the server clock from ping/pong round trips and follows
// it: small drift is nudged with the playback rate, large drift is a seek. Same rules as the web
// player, so web and app viewers stay together in one room.

/// A room, as `POST /api/party` and `GET /api/party/{code}` return it.
public struct PartyInfo: Codable, Sendable, Equatable {
    public var code: String
    public var itemId: Int
    public var members: Int
    public var everyone: Bool
    public var playing: Bool
}

public struct PartyMember: Codable, Sendable, Equatable, Identifiable {
    public var id: Int
    public var name: String
    public var color: String
    public var buffering: Bool
    public var host: Bool
    /// The person's stable key (user.profile) on this server; nil from an older server.
    public var from: String?

    public init(id: Int, name: String, color: String, buffering: Bool = false, host: Bool = false, from: String? = nil) {
        self.id = id
        self.name = name
        self.color = color
        self.buffering = buffering
        self.host = host
        self.from = from
    }
}

/// The room's playback state.
public struct PartyState: Codable, Sendable, Equatable {
    public var code: String
    public var itemId: Int
    /// Actually playing: intended, and nobody is buffering.
    public var playing: Bool
    /// What the controller asked for.
    public var intended: Bool
    /// Seconds from the start of the file, at server time `at`.
    public var pos: Double
    /// Server clock, milliseconds.
    public var at: Double
    /// Names of the members the room waits for.
    public var waiting: [String]
    public var members: [PartyMember]
    /// Everyone may control, not only the host.
    public var everyone: Bool
    public var by: Int
    public var seq: Int

    public init(code: String = "", itemId: Int, playing: Bool, intended: Bool? = nil, pos: Double, at: Double,
                waiting: [String] = [], members: [PartyMember] = [], everyone: Bool = false, by: Int = 0, seq: Int = 0) {
        self.code = code
        self.itemId = itemId
        self.playing = playing
        self.intended = intended ?? playing
        self.pos = pos
        self.at = at
        self.waiting = waiting
        self.members = members
        self.everyone = everyone
        self.by = by
        self.seq = seq
    }

    /// Where the room is at server time `serverNow` (ms).
    public func position(at serverNow: Double) -> Double {
        playing ? pos + max(0, serverNow - at) / 1000 : pos
    }
}

/// A message from the room.
public enum PartyEvent: Sendable, Equatable {
    case hello(id: Int, reactions: [String])
    case pong(client: Double, server: Double)
    case state(PartyState)
    /// `id` numbers the line in the room (0 from an older server: it cannot be reported);
    /// `from` is the sender's stable key, for blocking.
    case chat(id: Int, from: String, name: String, color: String, text: String)
    case system(String)
    case react(name: String, color: String, emoji: String)

    private struct Wire: Decodable {
        var t: String
        var id: Int?
        var reactions: [String]?
        var c: Double?
        var s: Double?
        var name: String?
        var color: String?
        var text: String?
        var emoji: String?
        var from: String?
    }

    /// nil for a message this client does not know (newer server) or cannot read.
    public static func decode(_ data: Data) -> PartyEvent? {
        guard let w = try? JSONDecoder.kinora.decode(Wire.self, from: data) else { return nil }
        switch w.t {
        case "hello": return .hello(id: w.id ?? 0, reactions: w.reactions ?? [])
        case "pong":
            guard let c = w.c, let s = w.s else { return nil }
            return .pong(client: c, server: s)
        case "state": return (try? JSONDecoder.kinora.decode(PartyState.self, from: data)).map(PartyEvent.state)
        case "chat": return .chat(id: w.id ?? 0, from: w.from ?? "", name: w.name ?? "", color: w.color ?? "", text: w.text ?? "")
        case "sys": return .system(w.text ?? "")
        case "react":
            guard let e = w.emoji else { return nil }
            return .react(name: w.name ?? "", color: w.color ?? "", emoji: e)
        default: return nil
        }
    }
}

/// The people this viewer blocked in watch party chat, per server: their lines are hidden and they
/// are marked in the member list. Kept on the device; the key is the sender's `from`.
public struct PartyBlocklist {
    private let defaults: UserDefaults
    private let key: String

    /// `server` is the server's id (`SavedServer.id`), which survives address changes.
    public init(server: String, defaults: UserDefaults = .standard) {
        self.defaults = defaults
        self.key = "partyBlocked." + server.lowercased().trimmingCharacters(in: CharacterSet(charactersIn: "/"))
    }

    public var all: Set<String> { Set(defaults.stringArray(forKey: key) ?? []) }
    public func contains(_ from: String) -> Bool { !from.isEmpty && all.contains(from) }

    public func block(_ from: String) {
        guard !from.isEmpty else { return }
        var s = all
        s.insert(from)
        defaults.set(s.sorted(), forKey: key)
    }

    public func unblock(_ from: String) {
        var s = all
        s.remove(from)
        defaults.set(s.sorted(), forKey: key)
    }
}

/// A message to the room. Control messages are refused (and the state sent back) unless
/// this viewer is the host or the host let everyone control.
public enum PartyCommand: Sendable, Equatable {
    case ping(client: Double)
    case play
    case pause(at: Double)
    case seek(to: Double)
    case skip(to: Double, kind: Segment.Kind)
    case item(Int)
    case buffering(Bool)
    case everyone(Bool)
    case chat(String)
    case react(String)
    /// Sends a chat line (by its id) to the server admin as objectionable.
    case report(id: Int, reason: String)

    public var json: Data {
        var m: [String: Any]
        switch self {
        case .ping(let c): m = ["t": "ping", "c": c]
        case .play: m = ["t": "play"]
        case .pause(let p): m = ["t": "pause", "pos": p]
        case .seek(let p): m = ["t": "seek", "pos": p]
        case .skip(let p, let kind): m = ["t": "skip", "pos": p, "kind": kind.rawValue]
        case .item(let id): m = ["t": "item", "item_id": id, "pos": 0]
        case .buffering(let v): m = ["t": "buffering", "v": v]
        case .everyone(let v): m = ["t": "everyone", "v": v]
        case .chat(let text): m = ["t": "chat", "text": text]
        case .react(let e): m = ["t": "react", "emoji": e]
        case .report(let id, let reason): m = ["t": "report", "id": id, "reason": reason]
        }
        // NaN/Infinity (a player time before the item is ready) makes JSONSerialization raise an
        // Objective-C exception that `try?` cannot catch: check first.
        guard JSONSerialization.isValidJSONObject(m) else { return Data() }
        return (try? JSONSerialization.data(withJSONObject: m, options: [.sortedKeys])) ?? Data()
    }
}

/// The server clock, estimated NTP-style from ping/pong: the fastest round trips are trusted.
public struct PartyClock: Sendable {
    /// Server ms minus local ms.
    public private(set) var offset: Double = 0
    private var bestRtt = Double.infinity

    public init() {}

    public mutating func pong(client c: Double, server s: Double, now: Double) {
        let rtt = now - c
        guard rtt >= 0, rtt <= bestRtt * 1.5 || rtt < 60 else { return }
        bestRtt = min(bestRtt, rtt)
        offset = s - (c + rtt / 2)
    }

    public func serverNow(_ now: Double) -> Double { now + offset }

    /// Local wall clock in ms.
    public static var localNow: Double { Date.now.timeIntervalSince1970 * 1000 }
}

/// How far behind or ahead this viewer may be before it is corrected.
public enum PartySync {
    public struct Decision: Sendable, Equatable {
        /// Jump here (seconds from the start of the file).
        public var seek: Double?
        /// Playback rate while playing: 1, or slightly faster / slower to catch up.
        public var rate: Float
    }

    /// Over 1.5 s: seek (on a fresh state, or while playing); 0.15–1.5 s while playing: run 5%
    /// faster or slower; otherwise normal speed.
    public static func decide(target: Double, current: Double, playing: Bool, fromState: Bool) -> Decision {
        let drift = target - current
        if abs(drift) > 1.5, fromState || playing { return Decision(seek: target, rate: 1) }
        if playing, abs(drift) > 0.15 { return Decision(seek: nil, rate: drift > 0 ? 1.05 : 0.95) }
        return Decision(seek: nil, rate: 1)
    }

    /// A typed code: upper case, without spaces and dashes. nil unless it can be a room code.
    public static func normalizedCode(_ s: String) -> String? {
        let code = s.uppercased().filter { !$0.isWhitespace && $0 != "-" }
        guard code.count == 6, code.allSatisfy({ $0.isASCII && ($0.isLetter || $0.isNumber) }) else { return nil }
        return code
    }

    /// A room code from a scanned QR code: the app link (`kinora://party/ABC234`), the web
    /// player's link (`https://…/#/party/ABC234`) or the bare code. nil for anything else.
    public static func code(scanned raw: String) -> String? {
        let s = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        if let url = URL(string: s), url.scheme != nil {
            if case .party(let code) = KinoraLink(url: url) { return code }
            guard ["http", "https"].contains(url.scheme?.lowercased() ?? "") else { return nil }
            // The web player routes in the fragment: #/party/ABC234 (a path is accepted too).
            let route = (url.fragment ?? "").isEmpty ? url.path : url.fragment!
            let parts = route.split(separator: "?").first.map { $0.split(separator: "/").map(String.init) } ?? []
            guard let i = parts.lastIndex(where: { $0.lowercased() == "party" }), i + 1 < parts.count else { return nil }
            return normalizedCode(parts[i + 1])
        }
        return normalizedCode(s)
    }
}

public extension KinoraClient {
    /// Starts a room on a movie or episode; the caller becomes the host when it joins.
    func createParty(itemId: Int, everyone: Bool = false) async throws -> PartyInfo {
        struct Body: Encodable, Sendable { var itemId: Int; var everyone: Bool }
        return try await post("/api/party", Body(itemId: itemId, everyone: everyone))
    }

    /// The room behind a code; 404 when it expired or the code is wrong.
    func party(_ code: String) async throws -> PartyInfo {
        try await get("/api/party/\(code)")
    }

    /// The room's WebSocket, with the login.
    func partySocketRequest(_ code: String) -> URLRequest? {
        guard var c = URLComponents(string: baseURL + "/api/party/\(code)/ws") else { return nil }
        c.scheme = c.scheme?.lowercased() == "https" ? "wss" : "ws"
        guard let url = c.url else { return nil }
        var req = URLRequest(url: url)
        if let token { req.setValue("Bearer " + token, forHTTPHeaderField: "Authorization") }
        return req
    }

    /// The web player's join link, for people without the app. A path rather than the
    /// player's #/party/ route, so chat apps can show a preview; the server sends the
    /// browser on to the route.
    func partyWebLink(_ code: String) -> URL? { URL(string: baseURL + "/party/\(code)") }
}
