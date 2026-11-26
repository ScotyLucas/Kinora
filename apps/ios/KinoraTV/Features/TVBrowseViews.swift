import SwiftUI
import KinoraKit

/// Tabs along the top, as on every Apple TV app; the player covers everything.
struct TVMainView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        @Bindable var model = model
        TabView(selection: $model.tab) {
            NavigationStack(path: $model.homePath) { TVHomeTab().tvDestinations() }
                .tabItem { Label(L("Kezdőlap"), systemImage: "house") }
                .tag(AppModel.Tab.home)
            NavigationStack { TVLibrariesTab().tvDestinations() }
                .tabItem { Label(L("Könyvtárak"), systemImage: "square.grid.2x2") }
                .tag(AppModel.Tab.libraries)
            NavigationStack { TVSearchTab().tvDestinations() }
                .tabItem { Label(L("Keresés"), systemImage: "magnifyingglass") }
                .tag(AppModel.Tab.search)
            NavigationStack { TVSettingsTab() }
                .tabItem { Label(L("Beállítások"), systemImage: "gearshape") }
                .tag(AppModel.Tab.downloads)
        }
        .onAppear { model.handlePendingLink() }
        .id(model.me?.profile?.id)
        .alert(L("Watch party"), isPresented: Binding(get: { model.partyError != nil && !model.playerVisible },
                                                   set: { if !$0 { model.partyError = nil } })) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(model.partyError ?? "")
        }
        .fullScreenCover(isPresented: Binding(get: { model.playerVisible && model.player != nil },
                                              set: { if !$0 { model.player?.close() } })) {
            if let p = model.player { TVPlayerView(player: p) }
        }
    }
}

extension View {
    func tvDestinations() -> some View {
        navigationDestination(for: Item.self) { TVDetailView(item: $0) }
            .navigationDestination(for: Library.self) { TVLibraryGrid(library: $0) }
    }
}

// MARK: - cards and rows

/// Poster (2:3) or landscape (16:9) card that opens the title, or plays it for episodes.
struct TVItemCard: View {
    @Environment(AppModel.self) private var model
    var item: Item
    var landscape = false
    var width: CGFloat = 250
    /// Movies and episodes start playing instead of opening their page.
    var playsDirectly = false

    var body: some View {
        Group {
            if playsDirectly {
                Button { model.play(item) } label: { label }
            } else {
                NavigationLink(value: item) { label }
            }
        }
        .buttonStyle(TVCardStyle())
        .contextMenu {
            if item.kind == .movie || item.kind == .episode {
                Button(L("Lejátszás"), systemImage: "play.fill") { model.play(item) }
                if item.progress != nil {
                    Button(L("Lejátszás elölről"), systemImage: "arrow.counterclockwise") { model.play(item, from: 0) }
                }
            }
            if playsDirectly {
                NavigationLink(value: item) { Label(L("Adatlap"), systemImage: "info.circle") }
            }
        }
    }

    private var label: some View {
        VStack(alignment: .leading, spacing: 12) {
            TVArtwork(item: item, kind: landscape ? (item.kind == .episode ? .thumb : .backdrop) : .poster,
                      ratio: landscape ? 16 / 9 : 2 / 3)
            TVCardCaption(item: item, landscape: landscape)
        }
        .frame(width: width)
    }
}

private struct TVCardCaption: View {
    @Environment(\.tvCardFocused) private var focused
    var item: Item
    var landscape: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(landscape && item.kind == .episode ? (item.seriesTitle ?? item.title) : item.title)
                .font(.callout.weight(.semibold))
                .foregroundStyle(focused ? Theme.plasterBright : Theme.plaster)
            Text(landscape && item.kind == .episode
                 ? [item.episodeLabel, item.title].compactMap { $0 }.joined(separator: " · ")
                 : Format.subtitle(item))
                .font(.caption).foregroundStyle(Theme.muted)
        }
        .lineLimit(1)
    }
}

/// A titled horizontal row of cards.
struct TVShelf: View {
    var title: String
    var items: [Item]
    var landscape = false
    var playsDirectly = false

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            TVHeading(text: title).padding(.horizontal, 80)
            ScrollView(.horizontal) {
                LazyHStack(alignment: .top, spacing: 44) {
                    ForEach(items) { item in
                        TVItemCard(item: item, landscape: landscape, width: landscape ? 420 : 250,
                                   playsDirectly: playsDirectly && (item.kind == .movie || item.kind == .episode))
                    }
                }
                .padding(.horizontal, 80)
                .padding(.vertical, 30)
            }
            .scrollClipDisabled()
        }
        .focusSection()
    }
}

/// Full-bleed backdrop behind a screen, fading into the foyer black.
struct TVBackdrop: View {
    var item: Item
    var body: some View {
        ArtworkView(item: item, kind: .backdrop)
            .overlay {
                LinearGradient(colors: [Theme.black.opacity(0.2), Theme.black.opacity(0.85), Theme.black],
                               startPoint: .top, endPoint: .bottom)
            }
            .overlay {
                LinearGradient(colors: [Theme.black.opacity(0.9), .clear], startPoint: .leading, endPoint: .trailing)
            }
            .ignoresSafeArea()
    }
}

// MARK: - home

struct TVHomeTab: View {
    @Environment(AppModel.self) private var model
    @State private var data: HomeData?
    @State private var error: String?

    var body: some View {
        ScrollView {
            if let data {
                LazyVStack(alignment: .leading, spacing: 50) {
                    if let hero = data.hero { TVHero(item: hero) }
                    ForEach(data.rows) { row in
                        if !row.items.isEmpty {
                            TVShelf(title: Language.shared.server(row.title), items: row.items, landscape: row.style == .landscape,
                                    playsDirectly: row.style == .landscape)
                        }
                    }
                    if data.hero == nil && data.rows.allSatisfy({ $0.items.isEmpty }) {
                        ContentUnavailableView(L("Még üres a könyvtár"), systemImage: "film.stack",
                                               description: Text(L("Az adminisztrátor a webes felületen adhat hozzá könyvtárat.")))
                            .padding(.top, 120)
                    }
                }
                .padding(.bottom, 80)
            } else {
                LoadingState(error: error) { Task { await load() } }
            }
        }
        .scrollClipDisabled()
        .background(Theme.black)
        .task { if data == nil { await load() } }
        .onChange(of: model.playbackGeneration) { Task { await load() } }
    }

    private func load() async {
        error = nil
        do { data = try await model.api { try await $0.home() } }
        catch is CancellationError {}
        catch { if data == nil { self.error = error.localizedDescription } }
    }
}

/// The featured title: backdrop, title, overview and two buttons.
private struct TVHero: View {
    @Environment(AppModel.self) private var model
    var item: Item

    var body: some View {
        ZStack(alignment: .bottomLeading) {
            ArtworkView(item: item, kind: .backdrop)
                .frame(height: 640)
                .frame(maxWidth: .infinity)
                .overlay {
                    LinearGradient(colors: [.clear, Theme.black.opacity(0.6), Theme.black], startPoint: .top, endPoint: .bottom)
                }
                .overlay {
                    LinearGradient(colors: [Theme.black.opacity(0.85), .clear], startPoint: .leading, endPoint: .center)
                }
            VStack(alignment: .leading, spacing: 18) {
                Text(item.title).font(Theme.display(76)).lineLimit(2)
                Text(Format.meta(item)).font(.callout).foregroundStyle(Theme.plaster)
                if let o = item.overview, !o.isEmpty {
                    Text(o).font(.callout).foregroundStyle(Theme.plaster).lineLimit(3).frame(maxWidth: 900, alignment: .leading)
                }
                TVButtonRow {
                    if item.kind == .movie || item.kind == .episode {
                        Button { model.play(item) } label: {
                            Label(item.progress != nil ? L("Folytatás") : L("Lejátszás"), systemImage: "play.fill")
                        }
                        .buttonStyle(TVPillStyle(prominent: true))
                    }
                    NavigationLink(value: item) { Label(L("Részletek"), systemImage: "info.circle") }
                        .buttonStyle(TVPillStyle())
                }
                .padding(.top, 10)
            }
            .padding(.horizontal, 80)
            .padding(.bottom, 20)
        }
        .focusSection()
    }
}

// MARK: - detail

struct TVDetailView: View {
    @Environment(AppModel.self) private var model
    var item: Item

    @State private var detail: ItemDetail?
    @State private var userData = UserData()
    @State private var error: String?
    @State private var season: Int?
    @State private var episodes: [Int: [Item]] = [:]
    @State private var episodesError: String?
    /// A failed favorite or watched toggle.
    @State private var actionError: String?
    @FocusState private var playFocused: Bool
    /// The version of a film picked here; nil = the one played last.
    @State private var selectedVersion: Int?
    @State private var askVersion = false
    @State private var subSearch: Item?
    /// Other sites' ratings and the box office (films and series), loaded after the page.
    @State private var scores: Scores?

    private var current: Item { detail?.item ?? item }

    private var playTarget: Item? {
        switch current.kind {
        case .movie: chosenVersion.map { $0.item(from: current) } ?? current
        case .episode: current
        default: detail?.playNext
        }
    }

    private var versions: [FilmVersion] {
        guard current.kind == .movie, let vs = detail?.versions, vs.count > 1 else { return [] }
        return vs
    }

    /// The version Play starts: picked here, else played last, else this page's file.
    private var chosenVersion: FilmVersion? {
        let vs = versions
        guard !vs.isEmpty else { return nil }
        let id = selectedVersion ?? detail?.preferredVersion ?? current.id
        return vs.first { $0.id == id } ?? vs.first { $0.id == current.id } ?? vs.first
    }

    var body: some View {
        ZStack {
            TVBackdrop(item: current)
            ScrollView {
                VStack(alignment: .leading, spacing: 50) {
                    header
                    if let message = error ?? actionError { TVErrorText(message: message).padding(.horizontal, 80) }
                    if current.kind == .series || current.kind == .season { seasonsSection }
                    if let similar = detail?.similar, !similar.isEmpty {
                        TVShelf(title: L("Hasonlók"), items: similar)
                    }
                }
                .padding(.top, 60)
                .padding(.bottom, 80)
            }
            .scrollClipDisabled()
        }
        .task { await load() }
        .onChange(of: model.playbackGeneration) { Task { await load() } }
        .task(id: season) { await loadEpisodes() }
        .task(id: current.id) { await loadScores() }
        .confirmationDialog(L("Változat"), isPresented: $askVersion, titleVisibility: .visible) {
            ForEach(versions) { v in
                Button((v.id == chosenVersion?.id ? "✓ " : "") + [v.title, v.details].filter { !$0.isEmpty }.joined(separator: " — ")) {
                    selectedVersion = v.id
                }
            }
            Button(L("Mégse"), role: .cancel) {}
        } message: {
            Text(L("{0} változat közül választhatsz", versions.count))
        }
        .sheet(item: $subSearch) { TVSubtitleSearchView(item: $0) }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 20) {
            if current.kind == .episode, let s = current.seriesTitle {
                Text(s).font(.title3.weight(.semibold)).foregroundStyle(Theme.amber)
            }
            Text(current.title).font(Theme.display(84)).lineLimit(2)
            Text([current.kind == .episode ? current.episodeLabel : nil,
                  Format.meta(current, rating: scores?.ratings.isEmpty != false)]
                .compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "))
                .font(.callout).foregroundStyle(Theme.plaster)
            if let g = current.genres, !g.isEmpty {
                Text(g.prefix(4).joined(separator: " · ")).font(.callout).foregroundStyle(Theme.muted)
            }
            if let o = current.overview, !o.isEmpty {
                Text(o).font(.callout).foregroundStyle(Theme.plaster).lineLimit(5).frame(maxWidth: 1100, alignment: .leading)
            }
            reception
            if let v = chosenVersion {
                Text([L("Változat: {0}", v.title), v.details].filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(.callout).foregroundStyle(Theme.plaster)
            }
            if let t = playTarget, let p = t.progress {
                HStack(spacing: 16) {
                    ProgressView(value: p).tint(Theme.amber).frame(width: 260)
                    if let left = Format.runtime((t.runtimeSec ?? 0) - (t.userData?.position ?? 0)) {
                        Text(L("Még {0}", left)).font(.callout).foregroundStyle(Theme.muted)
                    }
                }
            }
            TVButtonRow {
                Button { if let t = playTarget { model.play(t) } } label: {
                    Label(playLabel, systemImage: "play.fill")
                }
                .buttonStyle(TVPillStyle(prominent: true))
                .disabled(playTarget == nil)
                .focused($playFocused)
                if let t = playTarget, t.progress != nil {
                    Button { model.play(t, from: 0) } label: { Label(L("Elölről"), systemImage: "arrow.counterclockwise") }
                        .buttonStyle(TVPillStyle())
                }
                if !versions.isEmpty {
                    Button { askVersion = true } label: { Label(L("Változat"), systemImage: "square.stack.3d.up") }
                        .buttonStyle(TVPillStyle())
                }
                if let t = playTarget, t.kind == .movie || t.kind == .episode {
                    Button { model.startParty(t) } label: { Label(L("Watch party"), systemImage: "person.2") }
                        .buttonStyle(TVPillStyle())
                        .disabled(model.partyJoining)
                    if current.kind == .movie || current.kind == .episode {
                        Button { subSearch = t } label: { Label(L("Felirat"), systemImage: "text.magnifyingglass") }
                            .buttonStyle(TVPillStyle())
                    }
                }
                Button { Task { await setPlayed(!userData.played) } } label: {
                    Label(userData.played ? L("Megnézve") : L("Megnézettnek jelölés"),
                          systemImage: userData.played ? "checkmark.circle.fill" : "checkmark.circle")
                }
                .buttonStyle(TVPillStyle())
                Button { Task { await setFavorite(!userData.favorite) } } label: {
                    Label(userData.favorite ? L("Kedvenc") : L("Kedvencekhez"), systemImage: userData.favorite ? "heart.fill" : "heart")
                }
                .buttonStyle(TVPillStyle())
            }
            .padding(.top, 16)
            .focusSection()
        }
        .padding(.horizontal, 80)
    }

    /// Ratings beside the budget and gross, read at a glance above the buttons.
    @ViewBuilder private var reception: some View {
        let ratings = scores?.ratings ?? []
        let box = scores.flatMap { $0.hasBoxOffice && current.kind == .movie ? $0 : nil }
        if !ratings.isEmpty || box != nil {
            HStack(alignment: .top, spacing: 60) {
                if !ratings.isEmpty {
                    ScoresStrip(ratings: ratings, style: .tv)
                        .frame(maxWidth: CGFloat(ratings.count) * 290, alignment: .leading)
                }
                if let box {
                    VStack(alignment: .leading, spacing: 14) {
                        Text(L("Költségvetés és bevétel")).font(.callout.weight(.semibold)).foregroundStyle(Theme.plaster)
                        BoxOfficeView(scores: box, style: .tv)
                    }
                    .frame(width: 600, alignment: .leading)
                }
            }
            .padding(.top, 10)
            .transition(.opacity)
        }
    }

    /// Films and series only; on an older server the page simply goes without.
    private func loadScores() async {
        guard current.kind == .movie || current.kind == .series, scores == nil else { return }
        let found = try? await model.api { [id = current.id] in try await $0.itemScores(id) }
        withAnimation(.easeOut(duration: 0.3)) { scores = found }
    }

    private var playLabel: String {
        guard let t = playTarget else { return L("Lejátszás") }
        if t.progress != nil { return L("Folytatás") }
        if t.kind == .episode, current.kind != .episode, let e = t.episodeNum {
            if let s = t.seasonNum { return L("Lejátszás: {0}. évad {1}. rész", s, e) }
            return L("Lejátszás: {0}. rész", e)
        }
        return L("Lejátszás")
    }

    @ViewBuilder private var seasonsSection: some View {
        if current.kind == .series, let seasons = detail?.seasons, seasons.count > 1 {
            ScrollView(.horizontal) {
                HStack(spacing: 20) {
                    ForEach(seasons) { s in
                        Button { season = s.id } label: { Text(s.title) }
                            .buttonStyle(TVPillStyle(prominent: s.id == season))
                    }
                }
                .padding(.horizontal, 80)
                .padding(.vertical, 20)
            }
            .scrollClipDisabled()
            .focusSection()
        }
        if let season, let list = episodes[season] {
            if list.isEmpty {
                Text(L("Ebben az évadban nincs rész.")).foregroundStyle(Theme.muted).padding(.horizontal, 80)
            } else {
                TVShelf(title: L("Részek"), items: list, landscape: true, playsDirectly: true)
            }
        } else if let episodesError {
            HStack(spacing: 30) {
                TVErrorText(message: episodesError)
                Button { Task { await loadEpisodes() } } label: { Label(L("Újra"), systemImage: "arrow.clockwise") }
                    .buttonStyle(TVPillStyle())
            }
            .padding(.horizontal, 80)
            .focusSection()
        } else if season != nil {
            ProgressView().padding(.horizontal, 80)
        }
    }

    private func load() async {
        error = nil
        do {
            let d = try await model.api { [id = item.id] in try await $0.item(id) }
            let first = detail == nil
            detail = d
            // A series has nothing to play until its detail arrives, so the focus started
            // elsewhere (on "mark as watched"): hand it to the play button once it can take it.
            if first, current.kind == .series || current.kind == .season, playTarget != nil {
                try? await Task.sleep(for: .milliseconds(50))
                playFocused = true
            }
            userData = d.item.userData ?? UserData()
            if current.kind == .season {
                season = d.item.id
            } else if season == nil, let seasons = d.seasons, !seasons.isEmpty {
                season = seasons.first { $0.id == d.playNext?.parentId }?.id ?? seasons.first?.id
            }
            if let season, let list = try? await model.api({ try await $0.children(of: season) }) {
                episodes = [season: list]
            }
        } catch is CancellationError {
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func loadEpisodes() async {
        guard let season, episodes[season] == nil else { return }
        episodesError = nil
        do { episodes[season] = try await model.api { try await $0.children(of: season) } }
        catch is CancellationError {}
        catch { if self.season == season { episodesError = error.localizedDescription } }
    }

    private func setFavorite(_ on: Bool) async {
        actionError = nil
        do { userData = try await model.api { [id = current.id] in try await $0.setFavorite(id, on) } }
        catch is CancellationError {}
        catch { actionError = L("Nem sikerült menteni: ") + error.localizedDescription }
    }

    private func setPlayed(_ on: Bool) async {
        actionError = nil
        let u: UserData
        do { u = try await model.api { [id = current.id] in try await $0.setPlayed(id, on) } }
        catch is CancellationError { return }
        catch {
            actionError = L("Nem sikerült menteni: ") + error.localizedDescription
            return
        }
        userData = u
        if current.kind == .series || current.kind == .season { await load() }
    }
}

// MARK: - libraries

struct TVLibrariesTab: View {
    @Environment(AppModel.self) private var model
    @State private var libraries: [Library]?
    @State private var error: String?

    var body: some View {
        ScrollView {
            if let libraries, libraries.isEmpty {
                ContentUnavailableView(L("Nincs könyvtár"), systemImage: "square.grid.2x2",
                                       description: Text(L("Az adminisztrátor a webes felületen adhat hozzá könyvtárat.")))
                    .padding(.top, 120)
            } else if let libraries {
                LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 50), count: 3), spacing: 50) {
                    ForEach(libraries) { lib in
                        NavigationLink(value: lib) {
                            VStack(alignment: .leading, spacing: 10) {
                                Image(systemName: icon(lib.kind)).font(.system(size: 56)).foregroundStyle(Theme.amber)
                                Text(lib.name).font(.title3.weight(.semibold))
                                if let n = lib.count { Text(L("{0} cím", n)).font(.callout).opacity(0.75) }
                            }
                            .padding(.vertical, 18)
                        }
                        .buttonStyle(TVRowStyle())
                    }
                }
                .padding(80)
            } else {
                LoadingState(error: error) { Task { await load() } }
            }
        }
        .scrollClipDisabled()
        .background(Theme.black)
        .task { if libraries == nil { await load() } }
    }

    private func icon(_ kind: String) -> String {
        switch kind {
        case "shows", "series", "tvshows": "tv"
        case "music": "music.note"
        default: "film"
        }
    }

    private func load() async {
        error = nil
        do { libraries = try await model.api { try await $0.libraries() } }
        catch is CancellationError {}
        catch { if libraries == nil { self.error = error.localizedDescription } }
    }
}

struct TVLibraryGrid: View {
    @Environment(AppModel.self) private var model
    var library: Library
    @State private var items: [Item] = []
    @State private var total = 0
    /// The same remembered sort as on the iPhone.
    @AppStorage("kinora.librarySort") private var sort: ItemSort = .title
    @State private var loaded = false
    @State private var loadingMore = false
    @State private var error: String?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 30) {
                HStack(spacing: 20) {
                    Text(library.name).font(Theme.display(64)).lineLimit(1)
                    Spacer()
                    ForEach(ItemSort.allCases) { s in
                        Button(s.label) { sort = s }
                            .buttonStyle(TVPillStyle(prominent: s == sort))
                    }
                }
                .focusSection()
                if loaded {
                    LazyVGrid(columns: Array(repeating: GridItem(.fixed(250), spacing: 44), count: 6), alignment: .leading, spacing: 60) {
                        ForEach(items) { item in
                            TVItemCard(item: item)
                                .onAppear { if item.id == items.last?.id { Task { await more() } } }
                        }
                    }
                    if loadingMore { ProgressView().frame(maxWidth: .infinity) }
                    if items.isEmpty {
                        ContentUnavailableView(L("Üres könyvtár"), systemImage: "film.stack").padding(.top, 80)
                    }
                } else {
                    LoadingState(error: error) { Task { await load() } }
                }
            }
            .padding(80)
        }
        .scrollClipDisabled()
        .background(Theme.black)
        .task(id: sort) { await load() }
        .onChange(of: model.playbackGeneration) { Task { await load() } }
    }

    private func load() async {
        error = nil
        do {
            let page = try await model.api { [library, sort] in try await $0.items(library: library.id, sort: sort) }
            items = page.items
            total = page.total
            loaded = true
        } catch is CancellationError {
        } catch {
            if !loaded { self.error = error.localizedDescription }
        }
    }

    private func more() async {
        guard !loadingMore, items.count < total else { return }
        loadingMore = true
        defer { loadingMore = false }
        let offset = items.count
        if let page = try? await model.api({ [library, sort] in try await $0.items(library: library.id, sort: sort, offset: offset) }) {
            items += page.items.filter { n in !items.contains { $0.id == n.id } }
            total = page.total
        }
    }
}

// MARK: - search

struct TVSearchTab: View {
    @Environment(AppModel.self) private var model
    @State private var query = ""
    @State private var results: [Item] = []
    @State private var searching = false
    @State private var error: String?

    var body: some View {
        ScrollView {
            if let error, results.isEmpty {
                LoadingState(error: error) { Task { await search() } }
            } else if results.isEmpty {
                Text(query.trimmingCharacters(in: .whitespaces).count < 2
                     ? L("Írj be legalább két betűt.") : (searching ? L("Keresés…") : L("Nincs találat.")))
                    .foregroundStyle(Theme.muted)
                    .frame(maxWidth: .infinity)
                    .padding(.top, 60)
            } else {
                LazyVGrid(columns: Array(repeating: GridItem(.fixed(250), spacing: 44), count: 6), alignment: .leading, spacing: 60) {
                    ForEach(results) { TVItemCard(item: $0) }
                }
                .padding(80)
            }
        }
        .scrollClipDisabled()
        .searchable(text: $query, prompt: L("Filmek, sorozatok, részek"))
        .task(id: query) { await search() }
    }

    private func search() async {
        let q = query.trimmingCharacters(in: .whitespaces)
        guard q.count >= 2 else { results = []; error = nil; return }
        searching = true
        defer { searching = false }
        try? await Task.sleep(for: .milliseconds(350))
        guard !Task.isCancelled else { return }
        do {
            let r = try await model.api { try await $0.search(q) }
            guard !Task.isCancelled else { return }
            results = r
            error = nil
        } catch is CancellationError {
        } catch {
            results = []
            self.error = error.localizedDescription
        }
    }
}

// MARK: - settings

struct TVSettingsTab: View {
    @Environment(AppModel.self) private var model
    @State private var subtitleSize = AppSettings.subtitleStyle.size
    @State private var confirmLogout = false
    @State private var joining = false
    @State private var partyCode = ""

    var body: some View {
        ScrollView {
            HStack(alignment: .top, spacing: 80) {
                VStack(alignment: .leading, spacing: 22) {
                    if let p = model.me?.profile {
                        HStack(spacing: 30) {
                            TVProfileFace(profile: p, size: 120, showsName: false)
                            VStack(alignment: .leading, spacing: 8) {
                                Text(p.name).font(.title3)
                                let user = model.me?.user.username
                                let sub = [user == p.name ? nil : user, model.server?.name, p.isKids ? L("Gyerek") : nil]
                                    .compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ")
                                if !sub.isEmpty { Text(sub).foregroundStyle(Theme.muted) }
                            }
                        }
                        .padding(.bottom, 20)
                    }
                    Button { partyCode = ""; joining = true } label: {
                        Label(L("Csatlakozás watch partyhoz"), systemImage: "person.2.wave.2")
                    }
                    .buttonStyle(TVRowStyle())
                    .disabled(model.partyJoining)
                    if model.me?.profileLocked != true {
                        Button { model.switchProfile() } label: { Label(L("Profilváltás"), systemImage: "person.2") }
                            .buttonStyle(TVRowStyle())
                    }
                    if model.canSwitchAccount {
                        ForEach(model.accounts) { a in
                            Button { Task { await model.switchAccount(to: a) } } label: {
                                Label(L("Fiókváltás: {0}", a.username), systemImage: "person.crop.circle")
                            }
                            .buttonStyle(TVRowStyle())
                        }
                        Button { Task { await model.addAccount() } } label: {
                            Label(L("Fiók hozzáadása"), systemImage: "person.crop.circle.badge.plus")
                        }
                        .buttonStyle(TVRowStyle())
                    }
                    Button { model.showServers() } label: { Label(L("Másik szerver"), systemImage: "server.rack") }
                        .buttonStyle(TVRowStyle())
                    NavigationLink { TVHomeScenesView() } label: { Label(L("Otthon-jelenetek"), systemImage: "house") }
                        .buttonStyle(TVRowStyle())
                    if let p = model.me?.profile, !p.isKids, model.me?.profileLocked != true {
                        let on = p.snow ?? true
                        Button { Task { try? await model.updateProfile(ProfileUpdate(snow: !on)) } } label: {
                            Label(L("Hóesés") + ": " + (on ? L("Be") : L("Ki")), systemImage: "snowflake")
                        }
                        .buttonStyle(TVRowStyle())
                    }
                    Button { confirmLogout = true } label: { Label(L("Kijelentkezés"), systemImage: "rectangle.portrait.and.arrow.right") }
                        .buttonStyle(TVRowStyle())

                    TVHeading(text: L("Felirat mérete")).padding(.top, 30)
                    TVButtonRow(spacing: 20) {
                        ForEach(SubtitleStyle.Size.allCases, id: \.self) { s in
                            Button(sizeLabel(s)) {
                                subtitleSize = s
                                var style = AppSettings.subtitleStyle
                                style.size = s
                                AppSettings.subtitleStyle = style
                            }
                            .buttonStyle(TVPillStyle(prominent: s == subtitleSize))
                        }
                    }
                    .focusSection()

                    TVHeading(text: L("Nyelv")).padding(.top, 30)
                    ScrollView(.horizontal) {
                        HStack(spacing: 20) {
                            languageButton(L("Automatikus"), code: "")
                            ForEach(model.languages) { l in languageButton(l.name, code: l.code) }
                        }
                        .padding(.vertical, 20)
                    }
                    .scrollClipDisabled()
                    .focusSection()
                    Text(L("A nyelveket a szerver adja: ha az adminisztrátor a webes felületen új nyelvet ad hozzá, itt is megjelenik. A letöltött nyelvek ezen az eszközön maradnak, a szerver nélkül is."))
                        .font(.caption).foregroundStyle(Theme.muted)
                }
                .frame(width: 820)

                VStack(alignment: .leading, spacing: 26) {
                    TVHeading(text: L("Vezérlés iPhone-ról"))
                    Text(L("Lejátszás közben az iPhone zárolási képernyőjén és a Vezérlőközpontban megjelenik, mi megy az Apple TV-n: onnan megállíthatod, tekerheted, és 10 másodpercet ugorhatsz előre vagy vissza, a telefon feloldása nélkül."))
                        .foregroundStyle(Theme.plaster)
                    Text(L("A Vezérlőközpont Apple TV Remote gombjával a teljes távirányító is a telefonodon van, a szövegmezőkbe pedig a telefon billentyűzetével is írhatsz."))
                        .foregroundStyle(Theme.muted)
                    Text("Kinora \(AppModel.appVersion)")
                        .font(.callout).foregroundStyle(Theme.muted).padding(.top, 30)
                }
                .font(.callout)
            }
            .padding(80)
        }
        .background(Theme.black)
        .alert(L("Csatlakozás watch partyhoz"), isPresented: $joining) {
            TextField(L("6 karakteres kód"), text: $partyCode)
                .textInputAutocapitalization(.characters)
                .autocorrectionDisabled()
            Button(L("Csatlakozás")) { model.joinParty(partyCode) }
            Button(L("Mégse"), role: .cancel) {}
        } message: {
            Text(L("A kódot a party indítója látja a lejátszóban, vagy a meghívó üzenetben."))
        }
        .alert(L("Kijelentkezel?"), isPresented: $confirmLogout) {
            Button(L("Kijelentkezés"), role: .destructive) { Task { await model.logout() } }
            Button(L("Mégse"), role: .cancel) {}
        }
    }

    private func languageButton(_ title: String, code: String) -> some View {
        Button { model.languageChoice = code } label: {
            HStack(spacing: 12) {
                Text(title)
                if model.languageLoading && model.languageChoice == code && Language.shared.code != code { ProgressView() }
            }
        }
        .buttonStyle(TVPillStyle(prominent: model.languageChoice == code))
    }

    private func sizeLabel(_ s: SubtitleStyle.Size) -> String {
        switch s {
        case .small: L("Kicsi")
        case .medium: L("Közepes")
        case .large: L("Nagy")
        case .huge: L("Óriási")
        }
    }
}
