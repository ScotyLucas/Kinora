import SwiftUI
import KinoraKit

struct RootView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        ZStack {
            Theme.black.ignoresSafeArea()
            switch model.screen {
            case .launching:
                ProgressView().controlSize(.large)
            case .servers:
                ServersView()
            case .login:
                LoginView()
            case .quickConnect:
                QuickConnectView()
            case .profiles:
                ProfilesView()
            case .home:
                HomeView()
            case .offline:
                OfflineView()
            }
        }
        .animation(.default, value: model.screen)
        .foregroundStyle(Theme.plasterBright)
    }
}

/// Scrollable, width-limited auth page frame (centered on iPad).
struct AuthPage<Content: View>: View {
    @ViewBuilder var content: Content

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                content
            }
            .frame(maxWidth: 520, alignment: .leading)
            .padding(.horizontal, 24)
            .padding(.vertical, 32)
            .frame(maxWidth: .infinity)
        }
        .scrollDismissesKeyboard(.interactively)
    }
}

/// The way back to the server list from the auth screens: a quiet, but clearly tappable link.
struct OtherServerButton: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        Button { model.showServers() } label: {
            Label(L("Másik szerver"), systemImage: "server.rack")
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(Theme.plaster)
                .frame(minHeight: 44)
                .contentShape(.rect)
        }
    }
}

/// Error line under a form.
struct ErrorText: View {
    var message: String?
    var body: some View {
        if let message {
            Text(message)
                .font(.callout)
                .foregroundStyle(Theme.danger)
                .fixedSize(horizontal: false, vertical: true)
                // VoiceOver reads a new error out loud; the user's focus stays on the button.
                .onAppear { AccessibilityNotification.Announcement(message).post() }
                .onChange(of: message) { _, m in AccessibilityNotification.Announcement(m).post() }
        }
    }
}
