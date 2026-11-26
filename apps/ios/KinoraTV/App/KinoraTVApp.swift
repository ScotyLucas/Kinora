import SwiftUI
import KinoraKit

/// Kinora for Apple TV. The model, the player logic and the artwork cache are the iPhone
/// app's; the screens here are made for the Siri Remote and the focus engine.
///
/// Control from an iPhone, even a locked one, needs nothing extra on the phone: the player
/// publishes Now Playing info and answers the remote commands (PlayerModel), so the
/// iPhone's lock screen and Control Center show the Apple TV playback and drive it.
@main
struct KinoraTVApp: App {
    @State private var model = AppModel()
    @Environment(\.scenePhase) private var phase

    var body: some Scene {
        WindowGroup {
            TVRootView()
                .id(model.languageGeneration)
                .environment(\.locale, Language.shared.locale)
                .environment(model)
                .preferredColorScheme(.dark)
                .tint(Theme.amber)
                .task { _ = NetworkStatus.shared; HomeScenes.shared.warmUp(); await model.start() }
                .onOpenURL { model.open($0) }
                .onReceive(NotificationCenter.default.publisher(for: NetworkStatus.cameOnline)) { _ in model.syncOffline() }
        }
        .onChange(of: phase) { _, now in
            if now == .active { model.syncOffline() }
        }
    }
}

struct TVRootView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        ZStack {
            Theme.black.ignoresSafeArea()
            switch model.screen {
            case .launching:
                ProgressView()
            case .servers:
                TVServersView()
            case .login:
                TVLoginView()
            case .quickConnect:
                TVQuickConnectView()
            case .profiles:
                TVProfilesView()
            case .home:
                TVMainView()
            case .offline: // iOS only (downloads); never set on tvOS
                TVServersView()
            }
            if model.showsSnow { SnowfallView().transition(.opacity) }
        }
        .animation(.default, value: model.screen)
    }
}
