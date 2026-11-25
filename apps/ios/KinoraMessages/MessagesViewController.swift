import KinoraKit
import Messages
import SwiftUI
import UIKit

/// The Kinora iMessage app: invite to a watch party, recommend a title, send a sticker.
/// Tapping a Kinora bubble opens it here, with a button into the app.
final class MessagesViewController: MSMessagesAppViewController {
    private let model = MessagesModel()
    private var host: UIHostingController<MessagesRoot>?

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = UIColor(Theme.black)
        model.actions = self
        let host = UIHostingController(rootView: MessagesRoot(model: model))
        host.view.backgroundColor = .clear
        host.sizingOptions = []
        addChild(host)
        host.view.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(host.view)
        NSLayoutConstraint.activate([
            host.view.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            host.view.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            host.view.topAnchor.constraint(equalTo: view.topAnchor),
            host.view.bottomAnchor.constraint(equalTo: view.bottomAnchor),
        ])
        host.didMove(toParent: self)
        self.host = host
    }

    override func willBecomeActive(with conversation: MSConversation) {
        model.style = presentationStyle
        model.show(selected: conversation.selectedMessage, in: conversation)
        model.loadIfNeeded()
    }

    override func didSelect(_ message: MSMessage, conversation: MSConversation) {
        model.show(selected: message, in: conversation)
    }

    override func willTransition(to presentationStyle: MSMessagesAppPresentationStyle) {
        model.style = presentationStyle
        // Back to compact (a swipe down): the received invite closes with the sheet.
        if presentationStyle == .compact { model.received = nil }
    }

    override func didTransition(to presentationStyle: MSMessagesAppPresentationStyle) {
        model.style = presentationStyle
    }
}

extension MessagesViewController: MessagesActions {
    func expand() { if presentationStyle != .expanded { requestPresentationStyle(.expanded) } }

    func collapse() { if presentationStyle != .compact { requestPresentationStyle(.compact) } }

    func insert(_ message: MSMessage) async throws {
        guard let conversation = activeConversation else { throw KinoraError.network }
        try await conversation.insert(message)
    }

    func open(_ url: URL) {
        extensionContext?.open(url)
    }
}
