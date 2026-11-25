import Foundation

/// What a Kinora bubble in Messages carries: a watch party invite or a title recommendation.
///
/// It travels as the message's URL, which has to be a web address: a device without the
/// iMessage app (a Mac, a friend without Kinora) opens it in the browser, on the server's
/// own web player (`/#/party/<code>`, `/#/item/<id>`). The fields ride in the query, before
/// the fragment, where the web player ignores them; the iMessage app reads them back.
public struct KinoraInvite: Equatable, Sendable {
    public enum Kind: Equatable, Sendable {
        /// A watch party room on `itemId`.
        case party(code: String)
        /// "Watch this": the title's detail page.
        case title
    }

    public var kind: Kind
    /// server_id of the server the title (and room) lives on.
    public var serverId: String
    /// The server's address the sender used: the browser fallback opens it.
    public var serverURL: String
    public var itemId: Int
    public var title: String
    /// Year, or "S01E03 · Episode title"; may be empty.
    public var subtitle: String

    public init(kind: Kind, serverId: String, serverURL: String, itemId: Int, title: String, subtitle: String = "") {
        self.kind = kind
        self.serverId = serverId
        self.serverURL = serverURL
        self.itemId = itemId
        self.title = title
        self.subtitle = subtitle
    }

    public var partyCode: String? {
        if case .party(let code) = kind { return code }
        return nil
    }

    /// The link in the app: `kinora://party/ABC234?server=<id>` or `kinora://item/42?server=<id>`.
    public var appLink: URL {
        let base = partyCode.map { KinoraLink.party($0) } ?? .item(itemId)
        var c = URLComponents(url: base.url, resolvingAgainstBaseURL: false)!
        c.queryItems = [URLQueryItem(name: "server", value: serverId)]
        return c.url!
    }

    /// The server's web player at the same place (no query): for the browser button.
    public var webLink: URL? {
        let route = partyCode.map { "party/\($0)" } ?? "item/\(itemId)"
        return URL(string: serverURL + "/#/" + route)
    }

    /// The message URL: the web player link with the invite in its query.
    public var messageURL: URL? {
        guard var c = URLComponents(string: serverURL) else { return nil }
        if c.path.isEmpty { c.path = "/" }
        var q = [URLQueryItem(name: "kinora", value: partyCode == nil ? "title" : "party"),
                 URLQueryItem(name: "sid", value: serverId),
                 URLQueryItem(name: "item", value: String(itemId)),
                 URLQueryItem(name: "t", value: title)]
        if let code = partyCode { q.append(URLQueryItem(name: "code", value: code)) }
        if !subtitle.isEmpty { q.append(URLQueryItem(name: "st", value: subtitle)) }
        c.queryItems = q
        c.fragment = partyCode.map { "/party/\($0)" } ?? "/item/\(itemId)"
        return c.url
    }

    /// Reads a message URL back; nil for anything that is not a Kinora invite.
    public init?(messageURL url: URL) {
        guard let c = URLComponents(url: url, resolvingAgainstBaseURL: false),
              let scheme = c.scheme?.lowercased(), scheme == "http" || scheme == "https" else { return nil }
        let q = Dictionary((c.queryItems ?? []).compactMap { i in i.value.map { (i.name, $0) } },
                           uniquingKeysWith: { a, _ in a })
        guard let sid = q["sid"], !sid.isEmpty, let item = q["item"].flatMap(Int.init) else { return nil }
        switch q["kinora"] {
        case "party":
            guard let code = q["code"].flatMap(PartySync.normalizedCode) else { return nil }
            kind = .party(code: code)
        case "title":
            kind = .title
        default:
            return nil
        }
        var base = c
        base.query = nil
        base.fragment = nil
        base.path = ""
        guard let origin = base.url?.absoluteString else { return nil }
        serverId = sid
        serverURL = origin
        itemId = item
        title = q["t"] ?? ""
        subtitle = q["st"] ?? ""
    }
}

extension KinoraLink {
    /// The `server` of a `kinora://…?server=<id>` link: the invite names the server it belongs to.
    public static func serverId(in url: URL) -> String? {
        URLComponents(url: url, resolvingAgainstBaseURL: false)?
            .queryItems?.first { $0.name == "server" }?.value.flatMap { $0.isEmpty ? nil : $0 }
    }
}
