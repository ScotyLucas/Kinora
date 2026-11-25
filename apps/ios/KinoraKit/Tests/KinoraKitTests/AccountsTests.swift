import XCTest
@testable import KinoraKit

final class AccountsTests: XCTestCase {
    private func acc(_ id: Int, _ ago: TimeInterval) -> SavedAccount {
        SavedAccount(userId: id, username: "u\(id)", lastUsed: Date(timeIntervalSince1970: 1_000_000 - ago))
    }

    func testMergePutsNewestFirstAndReplacesSameUser() {
        let list = [acc(1, 10), acc(2, 20)]
        let (kept, dropped) = SavedAccount.merge(acc(2, 0), into: list)
        XCTAssertEqual(kept.map(\.userId), [2, 1])
        XCTAssertTrue(dropped.isEmpty)
    }

    func testMergeCapsTheList() {
        let list = (1...SavedAccount.max).map { acc($0, TimeInterval($0)) }
        let (kept, dropped) = SavedAccount.merge(acc(99, 0), into: list)
        XCTAssertEqual(kept.count, SavedAccount.max)
        XCTAssertEqual(kept.first?.userId, 99)
        XCTAssertEqual(dropped.map(\.userId), [SavedAccount.max])
    }

    func testParkedListIsPerServer() {
        let suite = UserDefaults(suiteName: "accounts-test-\(UUID())")!
        let store = ServerStore(defaults: suite, keychain: Keychain(service: "kinora.test.\(UUID())"))
        let data = try! JSONEncoder().encode([acc(1, 5), acc(2, 1)])
        suite.set(data, forKey: "kinora.accounts.A")
        XCTAssertEqual(store.parkedAccounts(for: "A").map(\.userId), [2, 1])
        XCTAssertTrue(store.parkedAccounts(for: "B").isEmpty)
        store.forgetAccount(2, server: "A")
        XCTAssertEqual(store.parkedAccounts(for: "A").map(\.userId), [1])
        store.forgetAccounts(server: "A")
        XCTAssertTrue(store.parkedAccounts(for: "A").isEmpty)
    }

    func testSwitchLockIsPerServerAndClearedWithTheServer() {
        let suite = UserDefaults(suiteName: "accounts-test-\(UUID())")!
        let store = ServerStore(defaults: suite, keychain: Keychain(service: "kinora.test.\(UUID())"))
        XCTAssertFalse(store.isSwitchLocked(server: "A"))
        store.setSwitchLocked(true, server: "A")
        XCTAssertTrue(store.isSwitchLocked(server: "A"))
        XCTAssertFalse(store.isSwitchLocked(server: "B"))
        store.forgetAccounts(server: "A")
        XCTAssertFalse(store.isSwitchLocked(server: "A"))
    }
}
