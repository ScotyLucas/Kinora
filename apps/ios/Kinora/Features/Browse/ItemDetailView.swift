import SwiftUI
import KinoraKit

/// Movie, series, season or episode page.
struct ItemDetailView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.verticalSizeClass) private var vSize
    @Environment(\.metrics) private var metrics
    /// What the previous screen knew; shown at once while the full detail loads.
    var item: Item

    @State private var detail: ItemDetail?
    /// Height of the visible page: caps the hero picture in a wide window.
    @State private var viewportHeight: CGFloat = 0
    @State private var userData = UserData()
    @State private var error: String?
    @State private var season: Int?
    @State private var episodes: [Int: [Item]] = [:]
    @State private var overviewExpanded = false
    @State private var fetchingSeason: Int?
    @State private var askSeasonQuality = false
    @State private var episodesError: String?
    /// A short message after an action: a season download, a failed toggle.
    @State private var notice: Notice?
    /// The version of a film picked here; nil = the one played last.
    @State private var selectedVersion: Int?
    @State private var subSearch: Item?
    @State private var langsFor: Item?
    /// Other sites' ratings and the box office (films and series), loaded after the page.
    @State private var scores: Scores?

    private struct Notice {
        var title: String
        var message: String
    }

    private var current: Item { detail?.item ?? item }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                header
                actions
                ratings
                overview
                boxOffice
                if current.kind == .series || current.kind == .season { episodesSection }
                if let similar = detail?.similar, !similar.isEmpty { similarSection(similar) }
                if let error, detail == nil {
                    LoadingState(error: error) { Task { await load() } }
                }
            }
            .padding(.bottom, 32)
        }
        .refreshable { await load() }
        // Runs again on returning from an episode, so watched marks stay current.
        .task { await load() }
        .onChange(of: model.playbackGeneration) { Task { await load() } }
        .task(id: season) { await loadEpisodes() }
        .task(id: current.id) { await loadScores() }
        .confirmationDialog(L("Évad letöltése"), isPresented: $askSeasonQuality, titleVisibility: .visible) {
            ForEach(DownloadButton.qualities, id: \.0) { q in
                Button(q.1) { downloadSeason(q.0) }
            }
        } message: {
            Text(L("Az évad még nem letöltött részei sorban kerülnek az eszközre."))
        }
        .alert(notice?.title ?? "", isPresented: Binding(get: { notice != nil }, set: { if !$0 { notice = nil } })) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(notice?.message ?? "")
        }
        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { viewportHeight = $0 }
        .sheet(item: $subSearch) { SubtitleSearchView(item: $0) }
        .sheet(item: $langsFor) { TitleLangsSheet(item: $0) }
        .kinoraScreen()
        .navigationTitle(current.title)
        .navigationBarTitleDisplayMode(.inline)
    }

    // MARK: - sections

    private var header: some View {
        VStack(alignment: .leading, spacing: 0) {
            ArtworkView(item: current, kind: current.kind == .episode ? .thumb : .backdrop)
                .heroFrame(metrics.heroRatio(16 / 9), compactHeight: vSize == .compact, maxHeight: viewportHeight / 2)
                .overlay {
                    LinearGradient(colors: [.clear, Theme.black], startPoint: .center, endPoint: .bottom)
                }

            HStack(alignment: .bottom, spacing: 16) {
                if current.kind != .episode {
                    ArtworkView(item: current, kind: .poster)
                        .frame(width: metrics.regular ? 168 : 104, height: metrics.regular ? 252 : 156)
                        .clipShape(.rect(cornerRadius: 8))
                        .shadow(radius: 10)
                }
                VStack(alignment: .leading, spacing: 6) {
                    if current.kind == .episode || current.kind == .season, let s = current.seriesTitle {
                        Text(s).font(.subheadline.weight(.semibold)).foregroundStyle(Theme.amber)
                    }
                    if let label = current.episodeLabel {
                        Text(label).font(.caption.weight(.semibold)).foregroundStyle(Theme.muted)
                    }
                    Text(current.title)
                        .font(Theme.display(metrics.regular ? 48 : 32, weight: .bold))
                        .lineLimit(3)
                        .minimumScaleFactor(0.7)
                    let meta = Format.meta(current, rating: scores?.ratings.isEmpty != false)
                    if !meta.isEmpty { Text(meta).font(.footnote).foregroundStyle(Theme.plaster) }
                    if let g = current.genres, !g.isEmpty {
                        Text(g.joined(separator: ", ")).font(.footnote).foregroundStyle(Theme.muted)
                    }
                }
            }
            .padding(.horizontal, metrics.gutter)
            .padding(.top, current.kind == .episode ? 0 : (metrics.regular ? -110 : -60))
        }
    }

    private var playTarget: Item? {
        switch current.kind {
        case .movie: chosenVersion.map { $0.item(from: current) } ?? current
        case .episode: current
        default: detail?.playNext
        }
    }

    /// The film's versions when there are several (Director's Cut, 4K…).
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

    private var actions: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 12) {
                Button { if let t = playTarget { Haptic.tap.play(); model.play(t) } } label: {
                    Label(playLabel, systemImage: "play.fill").frame(maxWidth: .infinity)
                }
                .buttonStyle(PrimaryButtonStyle())
                .disabled(playTarget == nil)

                if let t = playTarget, current.kind == .movie || current.kind == .episode {
                    DownloadButton(item: t).id(t.id)
                }
                IconToggle(on: userData.favorite, onIcon: "heart.fill", offIcon: "heart",
                           label: userData.favorite ? L("Kedvenc – eltávolítás") : L("Kedvencekhez")) {
                    await setFavorite(!userData.favorite)
                }
                IconToggle(on: userData.played, onIcon: "checkmark.circle.fill", offIcon: "checkmark.circle",
                           label: userData.played ? L("Megnézve – visszavonás") : L("Megnézettnek jelölés")) {
                    await setPlayed(!userData.played)
                }
            }
            if let chosen = chosenVersion {
                versionPicker(chosen)
            }
            if let t = playTarget, t.kind == .movie || t.kind == .episode {
                HStack(spacing: 20) {
                    Button { Haptic.tap.play(); model.startParty(t) } label: {
                        Label(L("Watch party indítása"), systemImage: "person.2")
                            .padding(.vertical, 6)
                            .contentShape(.rect)
                    }
                    .disabled(model.partyJoining)
                    if current.kind == .movie || current.kind == .episode {
                        Button { Haptic.tap.play(); subSearch = t } label: {
                            Label(L("Felirat keresése"), systemImage: "text.magnifyingglass")
                                .padding(.vertical, 6)
                                .contentShape(.rect)
                        }
                    }
                    // the film's or series' own audio and subtitle language
                    Button { Haptic.tap.play(); langsFor = current } label: {
                        Label(L("Nyelv"), systemImage: "captions.bubble")
                            .padding(.vertical, 6)
                            .contentShape(.rect)
                    }
                }
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(Theme.plaster)
            }
            if let t = playTarget, let p = t.progress {
                HStack(spacing: 10) {
                    ProgressView(value: p).tint(Theme.amber).frame(maxWidth: 120)
                    if let left = Format.runtime((t.runtimeSec ?? 0) - (t.userData?.position ?? 0)) {
                        Text(L("még {0}", left)).font(.caption).foregroundStyle(Theme.muted)
                    }
                    Spacer()
                    Button { Haptic.tap.play(); model.play(t, from: 0) } label: {
                        Label(L("Lejátszás elölről"), systemImage: "arrow.counterclockwise")
                            .padding(.vertical, 12)
                            .contentShape(.rect)
                    }
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(Theme.plaster)
                }
            }
        }
        // On an iPad the play button and the toggles stay together, not stretched across the screen.
        .frame(maxWidth: metrics.regular ? 600 : nil, alignment: .leading)
        .padding(.horizontal, metrics.gutter)
    }

    /// Which version Play starts; each row names the cut, the picture and the sound.
    private func versionPicker(_ chosen: FilmVersion) -> some View {
        Menu {
            ForEach(versions) { v in
                Toggle(isOn: Binding(get: { v.id == chosen.id },
                                     set: { if $0 { Haptic.select.play(); selectedVersion = v.id } })) {
                    Text(v.title)
                    Text(v.details)
                }
            }
        } label: {
            HStack(spacing: 8) {
                Image(systemName: "square.stack.3d.up")
                Text(L("Változat:"))
                    .foregroundStyle(Theme.muted)
                Text(chosen.title)
                    .fontWeight(.semibold)
                    .lineLimit(1)
                Image(systemName: "chevron.up.chevron.down")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(Theme.muted)
            }
            .font(.subheadline)
            .padding(.horizontal, 14).padding(.vertical, 9)
            .background(Color.white.opacity(0.08), in: .capsule)
            .contentShape(.capsule)
        }
        .foregroundStyle(Theme.plaster)
        .accessibilityLabel(L("Változat: {0}", chosen.title))
        .accessibilityHint(L("{0} változat közül választhatsz", versions.count))
    }

    private var playLabel: String {
        guard let t = playTarget else { return L("Lejátszás") }
        if t.progress != nil { return L("Folytatás") }
        if t.kind == .episode, current.kind != .episode, let l = t.episodeLabel { return L("{0} lejátszása", l) }
        return L("Lejátszás")
    }

    @ViewBuilder private var ratings: some View {
        if let s = scores, !s.ratings.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                ScoresStrip(ratings: s.ratings, style: .page, columns: metrics.regular ? 4 : 2)
                if !s.omdb, model.me?.user.isAdmin == true { OmdbHint() }
            }
            .frame(maxWidth: metrics.readableWidth, alignment: .leading)
            .padding(.horizontal, metrics.gutter)
            .transition(.opacity)
        }
    }

    @ViewBuilder private var boxOffice: some View {
        if let s = scores, s.hasBoxOffice, current.kind == .movie {
            VStack(alignment: .leading, spacing: 12) {
                Text(L("Költségvetés és bevétel")).font(Theme.display(metrics.sectionTitle, weight: .bold))
                BoxOfficeView(scores: s, style: .page)
            }
            .frame(maxWidth: metrics.readableWidth, alignment: .leading)
            .padding(.horizontal, metrics.gutter)
            .transition(.opacity)
        }
    }

    /// Films and series only; offline or on an older server the page simply goes without.
    private func loadScores() async {
        guard current.kind == .movie || current.kind == .series, scores == nil else { return }
        let found = try? await model.api { [id = current.id] in try await $0.itemScores(id) }
        withAnimation(.easeOut(duration: 0.25)) { scores = found }
    }

    @ViewBuilder private var overview: some View {
        if let o = current.overview, !o.isEmpty {
            // Short texts fit in four lines anyway: no "more" for them.
            let long = o.count > 240 || o.split(separator: "\n").count > 4
            Button {
                withAnimation { overviewExpanded.toggle() }
            } label: {
                VStack(alignment: .leading, spacing: 6) {
                    Text(o)
                        .foregroundStyle(Theme.plaster)
                        .lineLimit(overviewExpanded ? nil : 4)
                        .multilineTextAlignment(.leading)
                    if long {
                        Text(overviewExpanded ? L("Kevesebb") : L("Bővebben"))
                            .font(.subheadline.weight(.semibold))
                            .foregroundStyle(Theme.amber)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .disabled(!long)
            .accessibilityRemoveTraits(long ? [] : .isButton)
            .frame(maxWidth: metrics.readableWidth, alignment: .leading)
            .padding(.horizontal, metrics.gutter)
        }
    }

    @ViewBuilder private var episodesSection: some View {
        let seasons = detail?.seasons ?? []
        VStack(alignment: .leading, spacing: 14) {
            if current.kind == .series {
                HStack {
                    Text(L("Évadok")).font(Theme.display(metrics.sectionTitle, weight: .bold))
                    Spacer()
                    if seasons.count > 1 {
                        Picker(L("Évad"), selection: $season) {
                            ForEach(seasons) { s in Text(s.title).tag(Optional(s.id)) }
                        }
                        .pickerStyle(.menu)
                    } else if let s = seasons.first {
                        Text(s.title).foregroundStyle(Theme.muted)
                    }
                }
            } else {
                Text(L("Részek")).font(Theme.display(metrics.sectionTitle, weight: .bold))
            }

            if let season, let list = episodes[season] {
                if list.isEmpty {
                    Text(L("Ebben az évadban nincs rész.")).foregroundStyle(Theme.muted)
                } else if !missing(list).isEmpty {
                    Button {
                        if let room = DownloadStore.shared.roomError() {
                            notice = Notice(title: L("Évad letöltése"), message: room)
                        } else {
                            askSeasonQuality = true
                        }
                    } label: {
                        Label(L("Évad letöltése ({0} rész)", missing(list).count), systemImage: "arrow.down.circle")
                            .font(.subheadline.weight(.semibold))
                    }
                    .tint(Theme.amber)
                }
                // Two columns on an iPad, one on a phone.
                LazyVGrid(columns: episodeColumns, alignment: .leading, spacing: metrics.regular ? 22 : 14) {
                    ForEach(list) { ep in
                        NavigationLink(value: ep) { EpisodeRow(item: ep, isNext: ep.id == detail?.playNext?.id) }
                            .buttonStyle(.plain)
                            .contextMenu {
                                Button(L("Lejátszás"), systemImage: "play.fill") { model.play(ep) }
                                if !missing([ep]).isEmpty {
                                    Button(L("Letöltés (720p)"), systemImage: "arrow.down.circle") { download(ep) }
                                }
                            }
                    }
                }
            } else if let episodesError {
                VStack(alignment: .leading, spacing: 10) {
                    Text(episodesError).foregroundStyle(Theme.plaster)
                    Button(L("Újra")) { Task { await loadEpisodes() } }.buttonStyle(.bordered)
                }
            } else if detail != nil, !(current.kind == .series && seasons.isEmpty) {
                ProgressView().frame(maxWidth: .infinity).padding()
            }
        }
        .padding(.horizontal, metrics.gutter)
    }

    private var episodeColumns: [GridItem] {
        Array(repeating: GridItem(.flexible(), spacing: 28, alignment: .top), count: metrics.regular ? 2 : 1)
    }

    private func similarSection(_ items: [Item]) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(L("Hasonlók")).font(Theme.display(metrics.sectionTitle, weight: .bold)).padding(.horizontal, metrics.gutter)
            ScrollView(.horizontal, showsIndicators: false) {
                LazyHStack(alignment: .top, spacing: metrics.spacing) {
                    ForEach(items) { it in
                        NavigationLink(value: it) { PosterCard(item: it) }.buttonStyle(.plain)
                    }
                }
                .padding(.horizontal, metrics.gutter)
            }
        }
    }

    // MARK: - loading

    private func load() async {
        error = nil
        do {
            let d = try await model.api { [id = item.id] in try await $0.item(id) }
            detail = d
            Spotlight.index([d.item] + (d.similar ?? []))
            userData = d.item.userData ?? UserData()
            if current.kind == .season {
                season = d.item.id
            } else if season == nil, let seasons = d.seasons, !seasons.isEmpty {
                // Open the season the next episode is in.
                season = seasons.first { $0.id == d.playNext?.parentId }?.id ?? seasons.first?.id
            }
            // Refresh the open season in place; the others reload when picked.
            if let season, let list = try? await fetchEpisodes(season) { episodes = [season: list] }
        } catch is CancellationError {
        } catch {
            self.error = error.localizedDescription
        }
    }

    /// Episodes of the list that are neither saved nor downloading.
    private func missing(_ list: [Item]) -> [Item] {
        let store = DownloadStore.shared
        return list.filter { store.download(for: $0.id) == nil && store.phases[$0.id] == nil }
    }

    private func downloadSeason(_ quality: String) {
        guard let season, let list = episodes[season] else { return }
        let todo = missing(list)
        Haptic.firm.play()
        Task {
            do {
                let n = try await model.api { try await DownloadStore.shared.startAll(todo, quality: quality, client: $0) }
                if n < todo.count {
                    notice = Notice(title: L("Évad letöltése"),
                                    message: L("{0} részből {1} letöltése indult el. ", todo.count, n)
                                        + (DownloadStore.shared.roomError() ?? L("A többit később próbáld újra.")))
                }
            } catch {
                notice = Notice(title: L("Évad letöltése"), message: error.localizedDescription)
            }
        }
    }

    private func loadEpisodes() async {
        guard let season, episodes[season] == nil, fetchingSeason != season else { return }
        episodesError = nil
        do { episodes[season] = try await fetchEpisodes(season) }
        catch is CancellationError {}
        catch { if self.season == season { episodesError = error.localizedDescription } }
    }

    private func download(_ ep: Item) {
        if let room = DownloadStore.shared.roomError() {
            notice = Notice(title: L("Nem sikerült a letöltés"), message: room)
            Haptic.error.play()
            return
        }
        Haptic.firm.play()
        Task {
            do { try await model.api { try await DownloadStore.shared.start(ep, quality: "720p", client: $0) } }
            catch { notice = Notice(title: L("Nem sikerült a letöltés"), message: error.localizedDescription) }
        }
    }

    private func fetchEpisodes(_ id: Int) async throws -> [Item] {
        fetchingSeason = id
        defer { if fetchingSeason == id { fetchingSeason = nil } }
        return try await model.api { try await $0.children(of: id) }
    }

    private func setFavorite(_ on: Bool) async {
        do { userData = try await model.api { [id = current.id] in try await $0.setFavorite(id, on) } }
        catch is CancellationError {}
        catch { notice = Notice(title: L("Nem sikerült menteni"), message: error.localizedDescription) }
    }

    private func setPlayed(_ on: Bool) async {
        let u: UserData
        do { u = try await model.api { [id = current.id] in try await $0.setPlayed(id, on) } }
        catch is CancellationError { return }
        catch {
            notice = Notice(title: L("Nem sikerült menteni"), message: error.localizedDescription)
            return
        }
        userData = u
        // Marking a series or season changes its episodes and the next one up.
        if current.kind == .series || current.kind == .season { await load() }
    }
}

private struct IconToggle: View {
    var on: Bool
    var onIcon: String
    var offIcon: String
    var label: String
    var action: () async -> Void
    @State private var busy = false

    var body: some View {
        Button {
            busy = true
            Task { await action(); busy = false }
        } label: {
            Image(systemName: on ? onIcon : offIcon)
                .font(.title3)
                .foregroundStyle(on ? Theme.amber : Theme.plaster)
                .frame(width: 50, height: 50)
                .background(Theme.panel, in: .rect(cornerRadius: 10))
        }
        .disabled(busy)
        .accessibilityLabel(label)
        // Switched on (a favourite, a watched mark) knocks; switched off ticks.
        .sensoryFeedback(trigger: on) { _, new in new ? .impact(weight: .medium) : .selection }
    }
}

private struct EpisodeRow: View {
    var item: Item
    var isNext: Bool
    @Environment(\.metrics) private var metrics

    var body: some View {
        HStack(alignment: .top, spacing: metrics.regular ? 16 : 12) {
            ArtworkView(item: item, kind: .thumb)
                .frame(width: metrics.regular ? 192 : 136, height: metrics.regular ? 108 : 76.5)
                .overlay(alignment: .bottom) { ProgressStripe(item: item) }
                .overlay(alignment: .topTrailing) { WatchedBadge(item: item) }
                .clipShape(.rect(cornerRadius: 6))
                .pointerLift(cornerRadius: 6)
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    if let n = item.episodeNum { Text("\(n).").foregroundStyle(Theme.muted) }
                    Text(item.title).fontWeight(.semibold).lineLimit(2)
                }
                .font(.subheadline)
                HStack(spacing: 6) {
                    if isNext { Text(L("Következő")).foregroundStyle(Theme.amber).fontWeight(.semibold) }
                    if let r = Format.runtime(item.runtimeSec) { Text(r).foregroundStyle(Theme.muted) }
                }
                .font(.caption)
                if let o = item.overview, !o.isEmpty {
                    Text(o).font(.caption).foregroundStyle(Theme.muted).lineLimit(2)
                }
            }
            Spacer(minLength: 0)
        }
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
    }
}
