import Foundation

/// The protocol versions this client speaks (see apps/shared/src/platform.ts).
public enum KinoraProtocol {
    public static let clientAPI = 2
    public static let minServerAPI = 2
    public static let defaultPort = 8096
}

/// GET /api/status – public, answers before login.
public struct ServerStatus: Codable, Sendable, Equatable {
    public var product: String?
    public var version: String
    public var apiVersion: Int?
    public var minClientApi: Int?
    public var serverId: String?
    public var serverName: String
    public var logoUrl: String?
    public var quickconnectEnabled: Bool?
    public var setupNeeded: Bool?
    public var publicUrl: String?

    public init(product: String? = "kinora", version: String, apiVersion: Int? = 2, minClientApi: Int? = 1,
                serverId: String? = nil, serverName: String, logoUrl: String? = nil,
                quickconnectEnabled: Bool? = nil, setupNeeded: Bool? = nil, publicUrl: String? = nil) {
        self.product = product; self.version = version; self.apiVersion = apiVersion
        self.minClientApi = minClientApi; self.serverId = serverId; self.serverName = serverName
        self.logoUrl = logoUrl; self.quickconnectEnabled = quickconnectEnabled
        self.setupNeeded = setupNeeded; self.publicUrl = publicUrl
    }

    /// nil when this app and the server understand each other.
    public var incompatibility: KinoraError? {
        if (apiVersion ?? 1) < KinoraProtocol.minServerAPI { return .serverTooOld }
        if (minClientApi ?? 1) > KinoraProtocol.clientAPI { return .serverTooNew }
        return nil
    }
}

public struct User: Codable, Sendable, Equatable, Hashable {
    public var id: Int
    public var username: String
    public var isAdmin: Bool?
}

public struct Profile: Codable, Sendable, Equatable, Hashable, Identifiable {
    public var id: Int
    public var name: String
    public var color: String
    public var hasPin: Bool
    public var isKids: Bool
    public var subLangs: String?
    public var audioLangs: String?
    public var subMode: String?
    public var autoplayNext: Bool?
    public var maxBitrate: Int?
    public var skipIntro: String?
    /// Snowflakes fall on the screens outside the player (nil: an older server, on).
    public var snow: Bool?

    public init(id: Int, name: String, color: String, hasPin: Bool = false, isKids: Bool = false) {
        self.id = id; self.name = name; self.color = color; self.hasPin = hasPin; self.isKids = isKids
    }
}

/// GET /api/me
public struct Me: Codable, Sendable, Equatable {
    public var user: User
    public var profile: Profile?
    public var profiles: [Profile]
    public var profileLocked: Bool?
    public var deviceId: String
    /// Short-lived token for media URLs (`?st=`): images, HLS, subtitles.
    public var mediaToken: String
    public var serverName: String
    public var version: String
    /// Felfedezés: films and series can be requested (acquisition is on, not a kids profile).
    public var canRequest: Bool?
}

/// What the server records about this device (shown under Settings → Devices).
public struct DeviceInfo: Codable, Sendable {
    public var deviceName: String
    public var platform: String
    public var appVersion: String

    public init(deviceName: String, platform: String = "ios", appVersion: String) {
        self.deviceName = deviceName; self.platform = platform; self.appVersion = appVersion
    }
}

/// POST /api/login, and the approved Quick Connect status.
public struct Session: Codable, Sendable {
    public var token: String
    public var deviceId: String
    public var user: User?
}

/// POST /api/quickconnect/initiate
public struct QuickConnectStart: Codable, Sendable, Equatable {
    public var code: String
    public var secret: String
    public var pollInterval: Int?
    public var expiresIn: Int?
    public var approvePath: String?
}

/// GET /api/quickconnect/status?secret=
public struct QuickConnectState: Codable, Sendable {
    public enum Status: String, Codable, Sendable { case pending, approved, denied, expired }
    public var status: Status
    public var token: String?
    public var deviceId: String?
    public var user: User?
    public var needsPin: Bool?
    public var profileId: Int?

    public init(status: Status, token: String? = nil, deviceId: String? = nil, user: User? = nil,
                needsPin: Bool? = nil, profileId: Int? = nil) {
        self.status = status; self.token = token; self.deviceId = deviceId
        self.user = user; self.needsPin = needsPin; self.profileId = profileId
    }
}

/// A Kinora server found on the local network.
public struct DiscoveredServer: Sendable, Equatable, Hashable, Identifiable {
    public var id: String
    public var name: String
    public var address: String
    public var version: String
}

extension JSONDecoder {
    static let kinora: JSONDecoder = {
        let d = JSONDecoder()
        d.keyDecodingStrategy = .convertFromSnakeCase
        return d
    }()
}

extension JSONEncoder {
    static let kinora: JSONEncoder = {
        let e = JSONEncoder()
        e.keyEncodingStrategy = .convertToSnakeCase
        return e
    }()
}
