import Foundation
import Security

/// A server the user has connected to. The token is not stored here but in the Keychain.
public struct SavedServer: Codable, Sendable, Equatable, Identifiable, Hashable {
    /// server_id from /api/status.
    public var id: String
    /// The address that worked last (normalized base URL, no trailing slash).
    public var url: String
    /// Every known address of the server: home IP, public domain…
    public var urls: [String]
    public var name: String
    public var username: String?
    public var deviceId: String?
    public var lastUsed: Date

    public init(id: String, url: String, urls: [String] = [], name: String,
                username: String? = nil, deviceId: String? = nil, lastUsed: Date = .now) {
        self.id = id; self.url = url; self.urls = ServerAddress.merge([url], urls)
        self.name = name; self.username = username; self.deviceId = deviceId; self.lastUsed = lastUsed
    }

    /// Every address, the last working one first.
    public var addresses: [String] { ServerAddress.merge([url], urls) }
}

/// Saved servers (UserDefaults) and their tokens (Keychain).
/// `ServerStore.shared()` puts both in the App Group, so the widgets see them too.
public final class ServerStore: @unchecked Sendable {
    let defaults: UserDefaults
    let keychain: Keychain
    private let listKey = "kinora.servers"
    private let currentKey = "kinora.current"

    public init(defaults: UserDefaults = .standard, keychain: Keychain = Keychain()) {
        self.defaults = defaults
        self.keychain = keychain
    }

    public var servers: [SavedServer] {
        guard let data = defaults.data(forKey: listKey),
              let list = try? JSONDecoder().decode([SavedServer].self, from: data) else { return [] }
        return list.sorted { $0.lastUsed > $1.lastUsed }
    }

    public var currentID: String? {
        get { defaults.string(forKey: currentKey) }
        set { defaults.set(newValue, forKey: currentKey) }
    }

    public var current: SavedServer? {
        guard let id = currentID else { return nil }
        return servers.first { $0.id == id }
    }

    /// Adds or updates a server and makes it the current one. Addresses are merged, so the
    /// same server reached at home and from outside keeps both.
    public func upsert(_ server: SavedServer) {
        var s = server
        var list = servers
        if let prev = list.first(where: { $0.id == s.id }) {
            s.urls = ServerAddress.merge([s.url], s.urls, prev.addresses)
        }
        list.removeAll { $0.id == s.id }
        list.insert(s, at: 0)
        save(Array(list.prefix(12)))
        currentID = s.id
    }

    public func remove(_ id: String) {
        save(servers.filter { $0.id != id })
        keychain.delete(account: id)
        forgetAccounts(server: id)
        if currentID == id { currentID = nil }
    }

    public func token(for id: String) -> String? { keychain.read(account: id) }

    public func setToken(_ token: String?, for id: String) {
        if let token { keychain.write(token, account: id) } else { keychain.delete(account: id) }
    }

    /// Copies servers saved before the App Group existed (v0.3 and older), once.
    func adopt(from old: UserDefaults) {
        guard old !== defaults, defaults.data(forKey: listKey) == nil,
              let data = old.data(forKey: listKey) else { return }
        defaults.set(data, forKey: listKey)
        if currentID == nil { currentID = old.string(forKey: currentKey) }
    }

    private func save(_ list: [SavedServer]) {
        defaults.set(try? JSONEncoder().encode(list), forKey: listKey)
    }
}

/// Minimal generic-password Keychain wrapper. Readable after the first unlock so widgets
/// and background refresh can use the token too.
public struct Keychain: Sendable {
    public var service: String
    public var accessGroup: String?

    public init(service: String = "io.github.scotylucas.kinora.token", accessGroup: String? = nil) {
        self.service = service
        self.accessGroup = accessGroup
    }

    private func query(_ account: String) -> [String: Any] {
        var q: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
        if let accessGroup { q[kSecAttrAccessGroup as String] = accessGroup }
        return q
    }

    /// Looks in every group this build can reach. With an access group set, a token found
    /// elsewhere (saved before the widgets shared it) is moved into the group.
    public func read(account: String) -> String? {
        var q = query(account)
        q.removeValue(forKey: kSecAttrAccessGroup as String)
        q[kSecReturnData as String] = true
        q[kSecReturnAttributes as String] = true
        q[kSecMatchLimit as String] = kSecMatchLimitOne
        var out: AnyObject?
        guard SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess,
              let attrs = out as? [String: Any], let data = attrs[kSecValueData as String] as? Data,
              let value = String(data: data, encoding: .utf8) else { return nil }
        if let accessGroup, let found = attrs[kSecAttrAccessGroup as String] as? String, found != accessGroup,
           write(value, account: account) {
            var old = query(account)
            old[kSecAttrAccessGroup as String] = found
            SecItemDelete(old as CFDictionary)
        }
        return value
    }

    /// Stores the value; returns false if the keychain refused it
    /// (e.g. a build without the keychain entitlement).
    @discardableResult
    public func write(_ value: String, account: String) -> Bool {
        let attrs: [String: Any] = [
            kSecValueData as String: Data(value.utf8),
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlock,
        ]
        var status = SecItemUpdate(query(account) as CFDictionary, attrs as CFDictionary)
        if status != errSecSuccess {
            SecItemDelete(query(account) as CFDictionary)
            status = SecItemAdd(query(account).merging(attrs) { $1 } as CFDictionary, nil)
        }
        return status == errSecSuccess
    }

    /// Removes the item from every group, so an old copy cannot log the user back in.
    public func delete(account: String) {
        var q = query(account)
        q.removeValue(forKey: kSecAttrAccessGroup as String)
        SecItemDelete(q as CFDictionary)
    }
}
