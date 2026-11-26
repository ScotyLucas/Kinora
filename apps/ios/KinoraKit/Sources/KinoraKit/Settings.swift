import Foundation

/// PATCH /api/profiles/{id} – only the fields that are set change.
public struct ProfileUpdate: Codable, Sendable, Equatable {
    public var subLangs: String?
    public var audioLangs: String?
    public var subMode: String?
    public var autoplayNext: Bool?
    public var maxBitrate: Int?
    public var skipIntro: String?

    public init(subLangs: String? = nil, audioLangs: String? = nil, subMode: String? = nil,
                autoplayNext: Bool? = nil, maxBitrate: Int? = nil, skipIntro: String? = nil) {
        self.subLangs = subLangs; self.audioLangs = audioLangs; self.subMode = subMode
        self.autoplayNext = autoplayNext; self.maxBitrate = maxBitrate; self.skipIntro = skipIntro
    }
}

/// POST /api/profiles – a new profile of the account; the rest of its settings are edited later.
public struct ProfileCreate: Codable, Sendable, Equatable {
    public var name: String
    public var color: String
    public var isKids: Bool

    public init(name: String, color: String = ProfileCreate.colors.randomElement()!, isKids: Bool = false) {
        self.name = name; self.color = color; self.isKids = isKids
    }

    /// The web client's profile colours.
    public static let colors = ["#c97c5d", "#b5a27a", "#8fa37a", "#7fa6a0", "#c2b4a3", "#a3826b", "#b07a7a", "#8a9a8f"]
    /// The server's limit per account.
    public static let maxProfiles = 8
}

/// GET /api/devices – one logged-in device of the user.
public struct Device: Codable, Sendable, Equatable, Identifiable, Hashable {
    public var id: String
    public var name: String
    public var platform: String
    public var appVersion: String
    public var ip: String
    public var lastSeen: Int64
    public var current: Bool

    public init(id: String, name: String, platform: String, appVersion: String = "", ip: String = "",
                lastSeen: Int64 = 0, current: Bool = false) {
        self.id = id; self.name = name; self.platform = platform; self.appVersion = appVersion
        self.ip = ip; self.lastSeen = lastSeen; self.current = current
    }

    /// Readable platform name, as the web settings show it.
    public var platformLabel: String {
        switch platform {
        case "ios": "iOS"
        case "android": "Android"
        case "androidtv": "Android TV"
        case "webos": "LG TV"
        case "tizen": "Samsung TV"
        case "tvos": "Apple TV"
        case "web": L("Böngésző")
        default: platform
        }
    }

    public var isTV: Bool { ["tizen", "webos", "androidtv", "tvos"].contains(platform) }
}

public struct DeviceList: Codable, Sendable {
    public var devices: [Device]
}

/// POST /api/quickconnect/lookup – the device waiting for approval.
public struct QuickConnectRequest: Codable, Sendable, Equatable {
    public var code: String
    public var deviceName: String?
    public var platform: String?
    public var appVersion: String?
    public var ip: String?
    public var expiresIn: Int?
}

/// Choices of the settings screen. Keys are what the server stores.
public enum SettingChoices {
    public static var subModes: [(String, String)] { [
        ("auto", L("Automatikus (idegen nyelvű hangnál)")), ("always", L("Mindig")), ("off", L("Kikapcsolva")),
    ] }
    public static var skipModes: [(String, String)] { [
        ("manual", L("Gomb mutatása")), ("auto", L("Automatikus átugrás")), ("off", L("Kikapcsolva")),
    ] }
    public static var bitrates: [(Int, String)] { [
        (0, L("Eredeti")), (20000, "20 Mbps"), (8000, "8 Mbps"), (4000, "4 Mbps"), (2000, "2 Mbps"),
    ] }

    /// "most", "5 perce", "3 órája", "2 napja" – same rounding as the web settings.
    public static func ago(_ seconds: Int64) -> String {
        let m = Int((Double(max(0, seconds)) / 60).rounded())
        if m < 1 { return L("most") }
        if m < 60 { return L("{0} perce", m) }
        let h = Int((Double(m) / 60).rounded())
        if h < 48 { return L("{0} órája", h) }
        return L("{0} napja", Int((Double(h) / 24).rounded()))
    }
}

extension KinoraClient {
    public func updateProfile(_ id: Int, _ update: ProfileUpdate) async throws -> Profile {
        try await patch("/api/profiles/\(id)", update)
    }

    public func createProfile(_ new: ProfileCreate) async throws -> Profile {
        try await post("/api/profiles", new)
    }

    public func devices() async throws -> [Device] {
        try await get("/api/devices", as: DeviceList.self).devices
    }

    public func removeDevice(_ id: String) async throws {
        try await delete("/api/devices/" + (id.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? id))
    }

    /// Logs out every device except this one.
    public func removeOtherDevices() async throws {
        try await delete("/api/devices")
    }

    public func lookupQuickConnect(_ code: String) async throws -> QuickConnectRequest {
        try await post("/api/quickconnect/lookup", QCBody(code: code))
    }

    /// profileID 0: the device shows its own profile picker.
    public func approveQuickConnect(_ code: String, profileID: Int) async throws {
        try await post("/api/quickconnect/approve", QCBody(code: code, profileId: profileID))
    }

    public func denyQuickConnect(_ code: String) async throws {
        try await post("/api/quickconnect/approve", QCBody(code: code, action: "deny"))
    }
}

struct QCBody: Codable, Sendable {
    var code: String
    var profileId: Int?
    var action: String?
}
