import Foundation
import UserNotifications
import KinoraKit

// Opens the end-to-end encrypted notification: the relay only forwards `k` (the sealed
// message) and `s` (the server's relay id) with a placeholder alert. The key of that server
// is in the App Group's Keychain, put there when the app paired with the server.
final class NotificationService: UNNotificationServiceExtension, @unchecked Sendable {
    private let lock = NSLock()
    private var handler: ((UNNotificationContent) -> Void)?
    private var content: UNMutableNotificationContent?
    private var download: Task<Void, Never>?

    override func didReceive(_ request: UNNotificationRequest, withContentHandler contentHandler: @escaping (UNNotificationContent) -> Void) {
        guard let content = request.content.mutableCopy() as? UNMutableNotificationContent else {
            return contentHandler(request.content)
        }
        lock.withLock { handler = contentHandler; self.content = content }

        let info = request.content.userInfo
        let push = PushStore.shared()
        guard let k = info["k"] as? String, let s = info["s"] as? String,
              let key = push.key(relayServer: s),
              let message = try? PushCrypto.message(k, key: key) else {
            // Not ours to open (a forgotten server, a newer format): the placeholder stays.
            return finish()
        }
        content.title = message.title
        content.body = message.body
        if let tag = message.tag { content.threadIdentifier = tag }
        let pairing = push.pairing(relayServer: s)
        if let pairing { content.userInfo[PushKeys.server] = pairing.savedServerId }
        if let item = message.item { content.userInfo[PushKeys.link] = KinoraLink.item(Int(item)).url.absoluteString }
        // The recaps open the Napló with the web link's period and month.
        if message.type?.hasPrefix("recap") == true, let href = message.href, let q = href.split(separator: "?", maxSplits: 1).dropFirst().first {
            content.userInfo[PushKeys.link] = "\(KinoraLink.scheme)://journal?\(q)"
        }

        let servers = ServerStore.shared()
        guard let image = message.image, image.hasPrefix("/"), let pairing,
              let saved = servers.servers.first(where: { $0.id == pairing.savedServerId }),
              let token = servers.token(for: saved.id) else { return finish() }
        download = Task {
            if let attachment = await Self.poster(image, from: saved, token: token) {
                lock.withLock { self.content?.attachments = [attachment] }
            }
            finish()
        }
    }

    override func serviceExtensionTimeWillExpire() {
        download?.cancel()
        finish()
    }

    private func finish() {
        let (h, c) = lock.withLock {
            defer { handler = nil }
            return (handler, content)
        }
        if let h, let c { h(c) }
    }

    private static func poster(_ path: String, from server: SavedServer, token: String) async -> UNNotificationAttachment? {
        let config = URLSessionConfiguration.ephemeral
        config.timeoutIntervalForRequest = 8
        config.timeoutIntervalForResource = 15
        let client = KinoraClient(server: server, token: token, session: URLSession(configuration: config))
        guard let data = try? await client.data(path), !data.isEmpty else { return nil }
        let dir = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString, directoryHint: .isDirectory)
        // Attachments take JPEG, PNG and GIF; anything else stays text only.
        let ext: String
        if data.starts(with: [0xFF, 0xD8, 0xFF]) { ext = "jpg" }
        else if data.starts(with: [0x89, 0x50, 0x4E, 0x47]) { ext = "png" }
        else if data.starts(with: [0x47, 0x49, 0x46, 0x38]) { ext = "gif" }
        else { return nil }
        let file = dir.appending(path: "poster." + ext)
        do {
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            try data.write(to: file)
            return try UNNotificationAttachment(identifier: "poster", url: file)
        } catch {
            return nil
        }
    }
}
