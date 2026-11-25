import Foundation

/// Another account of the same server, signed in on this device but not active.
/// Its token is in the Keychain under `"<server id>#<user id>"`. The active account's token
/// stays where it always was (under the server id), so the widgets, Top Shelf and
/// Siri intents keep following whoever is signed in now.
///
/// The server's own account switch (`/api/accounts`) keeps the parked sessions in a
/// browser cookie; a native app holds its tokens itself, so it parks them here.
public struct SavedAccount: Codable, Sendable, Equatable, Hashable, Identifiable {
    public var userId: Int
    public var username: String
    public var lastUsed: Date

    public var id: Int { userId }

    public init(userId: Int, username: String, lastUsed: Date = .now) {
        self.userId = userId; self.username = username; self.lastUsed = lastUsed
    }

    /// The same cap as the server's remembered accounts.
    public static let max = 8

    /// Adds or refreshes an account: newest first, one entry per user, at most `max`.
    /// Returns the list and the accounts that fell off the end.
    static func merge(_ account: SavedAccount, into list: [SavedAccount]) -> (kept: [SavedAccount], dropped: [SavedAccount]) {
        var all = list.filter { $0.userId != account.userId }
        all.sort { $0.lastUsed > $1.lastUsed }
        all.insert(account, at: 0)
        return (Array(all.prefix(max)), Array(all.dropFirst(max)))
    }
}

extension ServerStore {
    private func accountsKey(_ server: String) -> String { "kinora.accounts.\(server)" }
    private func tokenAccount(_ server: String, _ user: Int) -> String { "\(server)#\(user)" }

    /// The parked accounts of a server, the most recently used first.
    public func parkedAccounts(for server: String) -> [SavedAccount] {
        guard let data = defaults.data(forKey: accountsKey(server)),
              let list = try? JSONDecoder().decode([SavedAccount].self, from: data) else { return [] }
        return list.sorted { $0.lastUsed > $1.lastUsed }
    }

    /// Keeps a signed-in account's token aside, so it can come back without a password.
    /// Returns false when the Keychain refused the token (nothing is parked then).
    @discardableResult
    public func park(_ token: String, user: User, server: String) -> Bool {
        guard keychain.write(token, account: tokenAccount(server, user.id)) else { return false }
        let (kept, dropped) = SavedAccount.merge(SavedAccount(userId: user.id, username: user.username),
                                                 into: parkedAccounts(for: server))
        for d in dropped { keychain.delete(account: tokenAccount(server, d.userId)) }
        saveAccounts(kept, server: server)
        return true
    }

    /// Takes a parked account back out: its token, and the entry is gone from the list.
    public func unpark(_ userId: Int, server: String) -> String? {
        let token = keychain.read(account: tokenAccount(server, userId))
        forgetAccount(userId, server: server)
        return token
    }

    /// Drops a parked account and its token.
    public func forgetAccount(_ userId: Int, server: String) {
        keychain.delete(account: tokenAccount(server, userId))
        saveAccounts(parkedAccounts(for: server).filter { $0.userId != userId }, server: server)
    }

    /// Drops every parked account of a server (the server itself is being forgotten).
    public func forgetAccounts(server: String) {
        for a in parkedAccounts(for: server) { keychain.delete(account: tokenAccount(server, a.userId)) }
        defaults.removeObject(forKey: accountsKey(server))
        setSwitchLocked(false, server: server)
    }

    private func lockKey(_ server: String) -> String { "kinora.accounts.locked.\(server)" }

    /// True once a session locked to one profile was used with this server on this device.
    /// It outlives that session's sign-out (and an app restart), so leaving the locked
    /// session does not open the parked accounts without a password. Only a fresh sign-in
    /// to an unlocked session clears it.
    public func isSwitchLocked(server: String) -> Bool { defaults.bool(forKey: lockKey(server)) }

    public func setSwitchLocked(_ locked: Bool, server: String) {
        if locked { defaults.set(true, forKey: lockKey(server)) } else { defaults.removeObject(forKey: lockKey(server)) }
    }

    private func saveAccounts(_ list: [SavedAccount], server: String) {
        if list.isEmpty { defaults.removeObject(forKey: accountsKey(server)); return }
        defaults.set(try? JSONEncoder().encode(list), forKey: accountsKey(server))
    }
}
