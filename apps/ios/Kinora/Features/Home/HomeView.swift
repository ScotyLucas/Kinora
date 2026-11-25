import SwiftUI
import KinoraKit

/// Signed in with a profile: home, libraries, search, Felfedezés (when requests are on) and downloads tabs.
struct HomeView: View {
    @Environment(AppModel.self) private var model
    /// Requested titles that reached the library since the user last looked.
    @State private var arrived: [AcqRequest] = []

    var body: some View {
        @Bindable var model = model
        TabView(selection: $model.tab) {
            NavigationStack(path: $model.homePath) { HomeTab().browseDestinations() }
                .tabItem { Label(L("Kezdőlap"), systemImage: "house") }
                .tag(AppModel.Tab.home)
            NavigationStack { LibrariesView().browseDestinations() }
                .tabItem { Label(L("Könyvtárak"), systemImage: "square.grid.2x2") }
                .tag(AppModel.Tab.libraries)
            NavigationStack { SearchView().browseDestinations() }
                .tabItem { Label(L("Keresés"), systemImage: "magnifyingglass") }
                .tag(AppModel.Tab.search)
            if model.canRequest {
                DiscoverTab()
                    .tabItem { Label(L("Felfedezés"), systemImage: "sparkles") }
                    .tag(AppModel.Tab.discover)
            }
            NavigationStack { DownloadsView() }
                .tabItem { Label(L("Letöltések"), systemImage: "arrow.down.circle") }
                .tag(AppModel.Tab.downloads)
        }
        // A widget link that arrived while signing in.
        .onAppear { model.handlePendingLink() }
        .task(id: model.me?.profile?.id) { await announceArrivals() }
        .alert(L("Megérkezett, amit kértél"), isPresented: Binding(get: { !arrived.isEmpty && !model.playerVisible },
                                                                set: { if !$0 { arrived = [] } })) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(arrived.prefix(5).map(\.title).joined(separator: "\n"))
        }
        // A new profile sees its own home: rebuild everything when it changes.
        .id(model.me?.profile?.id)
        .sheet(isPresented: $model.settingsVisible) { SettingsView() }
        .alert(L("Watch party"), isPresented: Binding(get: { model.partyError != nil && !model.playerVisible && !model.partyJoinVisible },
                                                   set: { if !$0 { model.partyError = nil } })) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(model.partyError ?? "")
        }
        .fullScreenCover(isPresented: Binding(get: { model.playerVisible && model.player != nil },
                                              set: { model.playerVisible = $0 })) {
            if let p = model.player { PlayerView(player: p) }
        }
    }
}

extension HomeView {
    /// Like the web client: titles the user asked for that arrived are announced once.
    private func announceArrivals() async {
        guard model.canRequest else { return }
        if let list = try? await model.api({ try await $0.unseenRequests() }), !list.isEmpty {
            arrived = list
        }
    }
}

struct HomeTab: View {
    @Environment(AppModel.self) private var model
    @State private var data: HomeData?
    @State private var error: String?
    @Environment(\.metrics) private var metrics

    var body: some View {
        ScrollView {
            if let data {
                LazyVStack(alignment: .leading, spacing: 28) {
                    if let hero = data.hero { HeroCard(item: hero) }
                    ForEach(data.rows) { row in
                        if !row.items.isEmpty { Shelf(row: row) }
                    }
                    if data.hero == nil && data.rows.allSatisfy({ $0.items.isEmpty }) {
                        ContentUnavailableView(L("Még üres a könyvtár"),
                                               systemImage: "film.stack",
                                               description: Text(L("Az adminisztrátor a webes felületen adhat hozzá könyvtárat.")))
                            .padding(.top, 60)
                    }
                }
                .padding(.bottom, 24)
            } else {
                LoadingState(error: error) { Task { await load() } }
            }
        }
        .refreshable { await load() }
        .task { if data == nil { await load() } }
        .onChange(of: model.playbackGeneration) { Task { await load() } }
        .kinoraScreen()
        .navigationTitle(L("Kezdőlap"))
        .toolbar(.hidden, for: .navigationBar)
        .safeAreaInset(edge: .top) { header }
        .sheet(isPresented: Binding(get: { model.partyJoinVisible }, set: { model.partyJoinVisible = $0 })) {
            PartyJoinView()
        }
    }

    private var header: some View {
        HStack {
            BrandMark(size: 28)
            Spacer()
            Button { model.partyJoinVisible = true } label: {
                Image(systemName: "person.2.wave.2")
                    .font(.system(size: 17, weight: .semibold))
                    .frame(width: 44, height: 44)
                    .contentShape(.rect)
            }
            .foregroundStyle(Theme.plaster)
            .accessibilityLabel(L("Csatlakozás watch partyhoz"))
            AccountMenu()
        }
        .padding(.horizontal, metrics.gutter)
        .padding(.vertical, 8)
        .clearOfWindowControls()
        .background(Theme.black.opacity(0.92))
    }

    private func load() async {
        error = nil
        do {
            let d = try await model.api { try await $0.home() }
            data = d
            Spotlight.index((d.hero.map { [$0] } ?? []) + d.rows.flatMap(\.items))
        }
        catch is CancellationError {}
        catch { if data == nil { self.error = error.localizedDescription } }
    }
}

/// The featured title at the top of home.
private struct HeroCard: View {
    var item: Item
    @Environment(\.verticalSizeClass) private var vSize
    @Environment(\.metrics) private var metrics

    var body: some View {
        NavigationLink(value: item) {
            ZStack(alignment: .bottomLeading) {
                ArtworkView(item: item, kind: .backdrop)
                    .heroFrame(metrics.heroRatio(16 / 10), compactHeight: vSize == .compact)
                LinearGradient(colors: [.clear, Theme.black.opacity(0.95)], startPoint: .center, endPoint: .bottom)
                VStack(alignment: .leading, spacing: 6) {
                    Text(item.title)
                        .font(Theme.display(metrics.regular ? 48 : 34, weight: .bold))
                        .lineLimit(2)
                    Text(Format.meta(item)).font(metrics.regular ? .subheadline : .footnote).foregroundStyle(Theme.plaster)
                    if let o = item.overview, !o.isEmpty {
                        Text(o).font(metrics.regular ? .subheadline : .footnote).foregroundStyle(Theme.muted).lineLimit(2)
                    }
                }
                .frame(maxWidth: metrics.regular ? 640 : nil, alignment: .leading)
                .padding(metrics.regular ? 32 : 20)
            }
            .clipShape(.rect(cornerRadius: 14))
            .pointerLift(cornerRadius: 14)
            .padding(.horizontal, metrics.regular ? metrics.gutter : 16)
        }
        .buttonStyle(.plain)
    }
}

/// A titled horizontal row of cards.
private struct Shelf: View {
    @Environment(AppModel.self) private var model
    @Environment(\.metrics) private var metrics
    var row: HomeRow

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(Language.shared.server(row.title))
                .font(Theme.display(metrics.sectionTitle, weight: .bold))
                .padding(.horizontal, metrics.gutter)
                .accessibilityAddTraits(.isHeader)
            ScrollView(.horizontal, showsIndicators: false) {
                LazyHStack(alignment: .top, spacing: metrics.spacing) {
                    ForEach(row.items) { item in
                        NavigationLink(value: item) {
                            if row.style == .landscape { WideCard(item: item) } else { PosterCard(item: item) }
                        }
                        .buttonStyle(.plain)
                        .contextMenu {
                            if let why = row.reasons?[String(item.id)] { Text(Language.shared.server(why)) }
                            if item.kind == .movie || item.kind == .episode {
                                Button(item.progress != nil ? L("Folytatás") : L("Lejátszás"), systemImage: "play.fill") {
                                    model.play(item)
                                }
                            }
                        }
                    }
                }
                .padding(.horizontal, metrics.gutter)
            }
        }
    }
}
