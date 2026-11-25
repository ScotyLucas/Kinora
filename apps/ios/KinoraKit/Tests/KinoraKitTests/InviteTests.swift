import XCTest
@testable import KinoraKit

final class InviteTests: XCTestCase {
    func testPartyInviteRoundTrips() throws {
        let invite = KinoraInvite(kind: .party(code: "ABC234"), serverId: "srv-1", serverURL: "https://kino.example.com:8443",
                                  itemId: 42, title: "Dűne: Második rész & co", subtitle: "2024")
        let url = try XCTUnwrap(invite.messageURL)
        XCTAssertEqual(url.fragment, "/party/ABC234")
        XCTAssertEqual(KinoraInvite(messageURL: url), invite)
        XCTAssertEqual(invite.webLink?.absoluteString, "https://kino.example.com:8443/#/party/ABC234")
        XCTAssertEqual(invite.appLink.absoluteString, "kinora://party/ABC234?server=srv-1")
        XCTAssertEqual(KinoraLink(url: invite.appLink), .party("ABC234"))
        XCTAssertEqual(KinoraLink.serverId(in: invite.appLink), "srv-1")
    }

    func testTitleInviteOnAPlainAddress() throws {
        let invite = KinoraInvite(kind: .title, serverId: "s", serverURL: "http://192.168.1.10:8096", itemId: 7, title: "Up")
        let url = try XCTUnwrap(invite.messageURL)
        XCTAssertEqual(KinoraInvite(messageURL: url), invite)
        XCTAssertEqual(invite.appLink.absoluteString, "kinora://item/7?server=s")
        XCTAssertEqual(invite.webLink?.absoluteString, "http://192.168.1.10:8096/#/item/7")
    }

    func testOtherURLsAreNotInvites() {
        XCTAssertNil(KinoraInvite(messageURL: URL(string: "https://example.com/?q=1")!))
        XCTAssertNil(KinoraInvite(messageURL: URL(string: "kinora://party/ABC234")!))
        XCTAssertNil(KinoraInvite(messageURL: URL(string: "https://x/?kinora=party&sid=a&item=1&code=no")!))
        XCTAssertNil(KinoraLink.serverId(in: URL(string: "kinora://party/ABC234")!))
    }
}
