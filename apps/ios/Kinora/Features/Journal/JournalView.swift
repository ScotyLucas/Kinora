import SwiftUI
import KinoraKit

/// Napló: what this profile watched, when and how much, for a month, a year or all time.
/// A sheet from the account menu; titles still in the library open their detail page.
struct JournalSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    @State private var path = NavigationPath()

    var body: some View {
        NavigationStack(path: $path) {
            JournalView(start: model.journalStart) { path.append($0) }
                .browseDestinations()
                .toolbar { ToolbarItem(placement: .cancellationAction) { Button(L("Bezárás")) { dismiss() } } }
        }
        .onDisappear { model.journalStart = nil }
    }
}

struct JournalView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.metrics) private var metrics
    var open: (Item) -> Void

    @State private var period: JournalPeriod = .month
    @State private var summary: JournalSummary?
    @State private var error: String?
    @State private var selected: Int?
    /// The recap picture the share button sends, once it is drawn.
    @State private var card: Image?
    @State private var recapVisible = false
    /// The same stretch of the period before, for the arrows next to the numbers (nil: nothing to compare with).
    @State private var previous: JournalSummary?
    @State private var goalEditing = false
    @State private var goalText = ""
    @State private var goalError: String?
    /// The bucket to select on the first load instead of the latest one.
    @State private var startBucket: Int?
    /// The period's viewings and what is worked out from them beyond the summary.
    @State private var events: [JournalEvent] = []
    @State private var insights: JournalInsights?
    /// The how-manieth viewing of its film a viewing is, by `viewKey`.
    @State private var nth: [String: Int] = [:]
    /// Stars given on this screen since the last load, by viewing id.
    @State private var stars: [Int64: Int] = [:]
    @State private var targets: [JournalTarget] = []
    @State private var challenges: [JournalChallenge] = []
    @State private var abandoned: [Item] = []
    @State private var addingViewing = false
    @State private var deletingViewing: JournalEvent?
    @State private var actionError: String?

    init(start: (period: JournalPeriod, bucket: Int)? = nil, open: @escaping (Item) -> Void) {
        self.open = open
        _period = State(initialValue: start?.period ?? .month)
        _startBucket = State(initialValue: start?.bucket)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 28) {
                Picker(L("Időszak"), selection: $period) {
                    ForEach(JournalPeriod.allCases) { Text(Self.periodName($0)).tag($0) }
                }
                .pickerStyle(.segmented)

                if let s = summary {
                    if s.isEmpty {
                        ContentUnavailableView(L("Ebben az időszakban még nincs megnézett film vagy epizód."), systemImage: "film.stack",
                                               description: Text(L("Amit végignézel (vagy megnézettnek jelölsz), itt jelenik meg.")))
                        goals(films: 0)
                    } else {
                        headline(s)
                        stats(s)
                        goals(films: s.films)
                        timeline(s)
                        if !s.genres.isEmpty { meters(L("Műfajok"), s.genres.prefix(6).map { ($0.name, $0.count) }) }
                        if !s.decades.isEmpty { meters(L("Évtizedek"), s.decades.prefix(6).map { ("\(String($0.decade))–\(String($0.decade + 9).suffix(2))", $0.count) }) }
                        when(s)
                        if let i = insights { more(i) }
                    }
                    JournalChallenges(challenges: challenges, canEdit: canEditJournal, open: open) { Task { await loadExtras() } }
                    JournalAbandoned(items: abandoned, open: open) { Task { await loadExtras() } }
                } else {
                    LoadingState(error: error) { Task { await load() } }
                }
            }
            .padding(.horizontal, metrics.gutter)
            .padding(.vertical, 16)
            .frame(maxWidth: metrics.readableWidth)
            .frame(maxWidth: .infinity)
        }
        .kinoraScreen()
        .navigationTitle(L("Napló"))
        .navigationBarTitleDisplayMode(.large)
        .toolbar {
            if canEditJournal {
                ToolbarItem(placement: .primaryAction) {
                    Button { addingViewing = true } label: { Label(L("Nézés hozzáadása"), systemImage: "plus") }
                }
            }
            if let s = summary, !s.isEmpty {
                ToolbarItem(placement: .primaryAction) {
                    if let card {
                        ShareLink(item: card, message: Text(shareText(s)), preview: SharePreview(L("Napló"), image: card)) {
                            Label(L("Megosztás"), systemImage: "square.and.arrow.up")
                        }
                    } else {
                        ShareLink(item: shareText(s)) { Label(L("Megosztás"), systemImage: "square.and.arrow.up") }
                    }
                }
            }
        }
        .fullScreenCover(isPresented: $recapVisible) {
            if let s = summary {
                JournalRecapView(summary: s, year: Self.periodLead(period), card: card, goal: period == .year ? goalValue : 0)
            }
        }
        .alert(L("Éves cél"), isPresented: $goalEditing) {
            TextField(L("Filmek száma"), text: $goalText).keyboardType(.numberPad)
            Button(L("Mentés")) {
                if let v = Int(goalText.trimmingCharacters(in: .whitespaces)), (1...1000).contains(v) { saveGoal(v) }
            }
            if goalValue > 0 { Button(L("Cél törlése"), role: .destructive) { saveGoal(0) } }
            Button(L("Mégse"), role: .cancel) {}
        } message: {
            Text(L("Hány filmet szeretnél idén megnézni?"))
        }
        .alert(L("Nem sikerült menteni"), isPresented: Binding(get: { goalError != nil }, set: { if !$0 { goalError = nil } })) {
            Button(L("Rendben"), role: .cancel) {}
        } message: {
            Text(goalError ?? "")
        }
        .sheet(isPresented: $addingViewing) { JournalAddViewingSheet { Task { await load() } } }
        .confirmationDialog(L("Törlöd ezt a kézzel felvett nézést?"), isPresented: Binding(get: { deletingViewing != nil }, set: { if !$0 { deletingViewing = nil } }),
                            titleVisibility: .visible) {
            Button(L("Törlés"), role: .destructive) {
                guard let e = deletingViewing else { return }
                Task {
                    do { try await model.api { try await $0.deleteJournalViewing(e.id) }; await load() } catch { actionError = error.localizedDescription }
                }
            }
        }
        .alert(L("Nem sikerült"), isPresented: Binding(get: { actionError != nil }, set: { if !$0 { actionError = nil } })) {
            Button(L("Rendben"), role: .cancel) {}
        } message: {
            Text(actionError ?? "")
        }
        .refreshable { await load() }
        .task(id: period) { await load() }
    }

    private func load() async {
        let r = period.range()
        do {
            let evs = try await model.api { try await $0.journal(from: r.from, to: r.to) }
            let s = JournalSummary(events: evs, period: period)
            var prev: JournalSummary?
            if let pr = period.previousRange(),
               let pe = try? await model.api({ try await $0.journal(from: pr.from, to: pr.to) }), !pe.isEmpty {
                prev = JournalSummary(events: pe, period: period)
            }
            previous = prev
            events = evs
            let ins = JournalInsights(events: evs)
            insights = ins
            nth = Dictionary(zip(evs.map(Self.viewKey), ins.nth), uniquingKeysWith: { a, _ in a })
            stars = [:]
            summary = s
            selected = startBucket ?? s.latestBucket?.key
            startBucket = nil
            error = nil
            card = nil
            await loadExtras()
            card = await renderCard(s)
        } catch {
            if summary == nil { self.error = error.localizedDescription }
        }
    }

    /// Goals, challenges and the unfinished titles; none of them is needed for the rest, so a failure leaves them out.
    private func loadExtras() async {
        async let t = try? model.api { try await $0.journalTargets() }
        async let c = try? model.api { try await $0.journalChallenges() }
        async let a = try? model.api { try await $0.journalAbandoned() }
        let (tt, cc, aa) = await (t, c, a)
        targets = tt ?? []
        challenges = cc ?? []
        abandoned = aa ?? []
    }

    static func viewKey(_ e: JournalEvent) -> String { "\(e.id)|\(e.at)|\(e.title)" }

    /// Draws the share card with the posters of the most watched titles (those already loadable).
    private func renderCard(_ s: JournalSummary) async -> Image? {
        guard !s.isEmpty else { return nil }
        var posters: [UIImage] = []
        if let client = model.client, let server = model.server?.id {
            let all = s.buckets.flatMap(\.events)
            var count: [Int: Int] = [:], order: [Int] = []
            for e in all { if let id = e.imageId { if count[id] == nil { order.append(id) }; count[id, default: 0] += 1 } }
            let top = order.enumerated().sorted { (count[$0.1]!, $0.0) > (count[$1.1]!, $1.0) }.map(\.1).prefix(8)
            for id in top where posters.count < 4 {
                if let img = await ArtworkCache.shared.load(client: client, server: server, id: id, kind: .poster) { posters.append(img) }
            }
        }
        let titles = Dictionary(grouping: s.buckets.flatMap(\.events), by: \.showTitle).mapValues(\.count)
            .sorted { $0.value != $1.value ? $0.value > $1.value : $0.key < $1.key }.map(\.key)
        let r = ImageRenderer(content: JournalShareCard(summary: s, lead: Self.periodLead(period), posters: posters, titles: titles)
            .environment(\.colorScheme, .dark))
        r.scale = 3
        return r.uiImage.map { Image(uiImage: $0) }
    }

    // MARK: - yearly goal

    private var goalValue: Int { model.me?.profile?.journalGoal ?? 0 }
    /// The server refuses profile changes from a kids profile and on a device locked to one profile.
    private var canEditGoal: Bool { model.me?.profile != nil && model.me?.profile?.isKids != true && model.me?.profileLocked != true }

    @ViewBuilder private func goal(films: Int) -> some View {
        let g = goalValue
        if g > 0 {
            let pace = JournalGoal.pace(films: films, goal: g)
            VStack(alignment: .leading, spacing: 8) {
                HStack(alignment: .firstTextBaseline) {
                    Text(L("Éves cél")).font(.headline).foregroundStyle(Theme.plasterBright)
                    Text(films >= g ? L("teljesítve!") : pace.ahead >= 0 ? L("{0} filmmel a terv előtt", pace.ahead) : L("{0} filmmel a terv mögött", -pace.ahead))
                        .font(.subheadline).foregroundStyle(films >= g || pace.ahead >= 0 ? Theme.amber : Theme.muted)
                    Spacer()
                    if canEditGoal { Button(L("Módosítás")) { goalText = String(g); goalEditing = true }.font(.subheadline) }
                }
                GeometryReader { geo in
                    ZStack(alignment: .leading) {
                        Capsule().fill(Theme.panel)
                        Capsule().fill(Theme.amber).frame(width: geo.size.width * min(1, CGFloat(films) / CGFloat(g)))
                        Rectangle().fill(Theme.plaster).frame(width: 2, height: 18)
                            .offset(x: geo.size.width * min(1, CGFloat(pace.due) / CGFloat(g)) - 1)
                    }
                }
                .frame(height: 10)
                .accessibilityElement()
                .accessibilityLabel(L("Éves cél"))
                .accessibilityValue(L("{0} / {1} film", films, g))
                Text(L("{0} / {1} film", films, g)).font(.footnote).foregroundStyle(Theme.muted)
            }
        }
    }

    /// Journal changes the server allows: not from a kids profile.
    private var canEditJournal: Bool { model.me?.profile != nil && model.me?.profile?.isKids != true }

    /// The yearly film goal (year view) and the other goals of the period, with the button for a new one.
    @ViewBuilder private func goals(films: Int) -> some View {
        if period == .year || period == .month {
            VStack(alignment: .leading, spacing: 18) {
                if period == .year { goal(films: films) }
                JournalTargets(targets: targets, events: events, monthOnly: period == .month, canEdit: canEditGoal,
                               genres: Array(Set(events.flatMap(\.genres))).sorted()) {
                    Task { await loadExtras() }
                }
            }
        }
    }

    /// People, countries, rewatches, stars, the hour map and the records.
    @ViewBuilder private func more(_ i: JournalInsights) -> some View {
        if !i.directors.isEmpty { meters(L("Rendezők és alkotók"), i.directors.prefix(6).map { ($0.name, $0.count) }) }
        if !i.cast.isEmpty { meters(L("Színészek"), i.cast.prefix(6).map { ($0.name, $0.count) }) }
        if !i.countries.isEmpty {
            meters(L("Országok"), i.countries.prefix(6).map { ("\(JournalCountry.flag($0.name)) \(JournalCountry.name($0.name))", $0.count) })
        }
        if !i.rewatched.isEmpty { meters(L("Újranézett filmek"), i.rewatched.prefix(6).map { ($0.name, $0.count) }) }
        if let avg = i.averageRating {
            VStack(alignment: .leading, spacing: 0) {
                sectionTitle(L("Értékeléseid")).padding(.bottom, 4)
                Text(L("{0} film átlaga: {1} csillag", i.rated.count, avg.formatted(.number.precision(.fractionLength(1)))))
                    .foregroundStyle(Theme.muted).padding(.bottom, 6)
                ForEach(Array(i.rated.prefix(5).enumerated()), id: \.offset) { _, e in
                    HStack {
                        Text(e.title).foregroundStyle(Theme.plaster).lineLimit(1)
                        Spacer()
                        JournalStars(rating: e.rating)
                    }
                    .padding(.vertical, 8)
                    .overlay(alignment: .bottom) { Rectangle().fill(Theme.rule).frame(height: 1) }
                    .accessibilityElement(children: .combine)
                }
            }
        }
        VStack(alignment: .leading, spacing: 12) {
            sectionTitle(L("Napszakok"))
            JournalHeatmap(heat: i.heat)
        }
        VStack(alignment: .leading, spacing: 12) {
            sectionTitle(L("Rekordok"))
            JournalRecords(insights: i)
        }
    }

    private func saveGoal(_ v: Int) {
        Task {
            do { try await model.setJournalGoal(v) } catch { goalError = error.localizedDescription }
        }
    }

    // MARK: - sections

    /// The sentence a monthly or yearly recap opens with.
    private func headline(_ s: JournalSummary) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(Self.periodLead(period)).font(.footnote.weight(.semibold)).textCase(.uppercase).foregroundStyle(Theme.muted)
            (Text(L("{0} filmet", s.films)).foregroundStyle(Theme.amber) + Text(" " + L("és {0} epizódot láttál.", s.episodes)))
                .font(Theme.display(34))
                .foregroundStyle(Theme.plasterBright)
                .fixedSize(horizontal: false, vertical: true)
            if let g = s.genres.first {
                Text(L("Leginkább {0}.", g.name.lowercased()))
                    .foregroundStyle(Theme.plaster)
            }
            if period == .year || period == .lastYear {
                Button { recapVisible = true } label: {
                    Label(L("Éves összefoglaló"), systemImage: "play.fill").font(.headline)
                }
                .buttonStyle(.borderedProminent)
                .tint(Theme.amber)
                .foregroundStyle(Theme.amberInk)
                .padding(.top, 8)
            }
        }
    }

    private func stats(_ s: JournalSummary) -> some View {
        let p = previous
        return VStack(alignment: .leading, spacing: 8) {
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 140), spacing: 12, alignment: .topLeading)], spacing: 12) {
                stat(L("Filmek"), s.films.formatted(), nil, delta: p.map { s.films - $0.films })
                stat(L("Epizódok"), s.episodes.formatted(), s.shows > 0 ? L("{0} sorozatból", s.shows) : nil, delta: p.map { s.episodes - $0.episodes })
                stat(L("Nézési idő"), s.seconds < 3600 ? L("{0} perc", s.seconds / 60) : L("{0} óra", Int((Double(s.seconds) / 3600).rounded())),
                     s.seconds >= 86400 ? L("≈ {0} teljes nap", (Double(s.seconds) / 86400).formatted(.number.precision(.fractionLength(1)))) : nil,
                     delta: p.map { Int((Double(s.seconds - $0.seconds) / 3600).rounded()) }, unit: { L("{0} óra", $0) })
                if let g = s.genres.first, let share = s.topGenreShare {
                    stat(L("Kedvenc műfaj"), g.name, L("a nézéseid {0}%-a", share))
                }
            }
            if p != nil {
                Text(period == .month ? L("A nyilak a múlt hónap ugyanennyi napjához mérnek.")
                     : period == .year ? L("A nyilak a tavalyi év ugyanezen pontjához mérnek.") : L("A nyilak az azt megelőző évhez mérnek."))
                    .font(.footnote).foregroundStyle(Theme.muted)
            }
        }
    }

    private func stat(_ label: String, _ value: String, _ sub: String?, delta: Int? = nil,
                      unit: (Int) -> String = { $0.formatted() }) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(label).font(.caption.weight(.semibold)).textCase(.uppercase).foregroundStyle(Theme.muted)
            Text(value).font(Theme.display(32)).foregroundStyle(Theme.plasterBright).lineLimit(1).minimumScaleFactor(0.5)
            if let d = delta {
                Text((d > 0 ? "▲ +" : d < 0 ? "▼ −" : "±") + unit(abs(d)))
                    .font(.footnote.weight(.semibold).monospacedDigit())
                    .foregroundStyle(d > 0 ? Theme.amber : Theme.muted)
            }
            if let sub { Text(sub).font(.footnote).foregroundStyle(Theme.muted) }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.vertical, 12)
        .overlay(alignment: .top) { Rectangle().fill(Theme.rule).frame(height: 1) }
        .accessibilityElement(children: .combine)
    }

    private func timeline(_ s: JournalSummary) -> some View {
        let maxCount = max(1, s.buckets.map(\.events.count).max() ?? 1)
        let bucket = s.buckets.first { $0.key == selected }
        return VStack(alignment: .leading, spacing: 14) {
            sectionTitle(L("Idővonal"))
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(alignment: .bottom, spacing: s.unit == .day ? 4 : 8) {
                    ForEach(s.buckets) { b in
                        Button { selected = b.key } label: {
                            VStack(spacing: 6) {
                                Spacer(minLength: 0)
                                RoundedRectangle(cornerRadius: 2)
                                    .fill(b.key == selected ? Theme.amber : b.events.isEmpty ? Theme.panel : Theme.rule)
                                    .frame(height: b.events.isEmpty ? 3 : max(6, CGFloat(b.events.count) / CGFloat(maxCount) * 140))
                                Text(barLabel(b, s.unit))
                                    .font(.caption2.weight(b.key == selected ? .bold : .regular))
                                    .foregroundStyle(b.key == selected ? Theme.plasterBright : Theme.muted)
                                    .lineLimit(1)
                                    .fixedSize()
                            }
                            .frame(width: s.unit == .day ? 14 : 30, height: 170)
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel("\(longLabel(b, s.unit)): \(L("{0} nézés", b.events.count))")
                        .accessibilityAddTraits(b.key == selected ? .isSelected : [])
                    }
                }
            }
            .defaultScrollAnchor(.trailing)
            if let bucket {
                Text("\(longLabel(bucket, s.unit)) · \(L("{0} nézés", bucket.events.count))")
                    .font(.headline).foregroundStyle(Theme.plasterBright)
                if bucket.events.isEmpty {
                    Text(L("Ekkor nem néztél semmit.")).foregroundStyle(Theme.muted)
                } else {
                    ScrollView(.horizontal, showsIndicators: false) {
                        LazyHStack(alignment: .top, spacing: 14) {
                            ForEach(Array(bucket.events.reversed().enumerated()), id: \.offset) { _, e in poster(e) }
                        }
                    }
                }
            }
        }
    }

    private func poster(_ e: JournalEvent) -> some View {
        let n = nth[Self.viewKey(e)] ?? 0
        let rating = stars[e.id] ?? e.rating
        return VStack(alignment: .leading, spacing: 4) {
            Button { Task { await openItem(e) } } label: {
                VStack(alignment: .leading, spacing: 4) {
                    Group {
                        if e.imageId == nil, let url = e.poster {
                            RemotePoster(url: url, title: e.showTitle)
                        } else {
                            JournalArtwork(imageId: e.imageId, title: e.showTitle).aspectRatio(2 / 3, contentMode: .fit)
                        }
                    }
                    .overlay(alignment: .topLeading) {
                        if n > 1 {
                            Text(L("{0}. nézés", n))
                                .font(.caption2.weight(.bold)).foregroundStyle(Theme.amberInk)
                                .padding(.horizontal, 6).padding(.vertical, 2)
                                .background(Theme.amber, in: Capsule())
                                .padding(5)
                        }
                    }
                    .overlay(alignment: .topTrailing) {
                        if e.isManual {
                            Image(systemName: "hand.point.up.left.fill").font(.caption2).foregroundStyle(Theme.plaster)
                                .padding(5).background(.black.opacity(0.5), in: Circle()).padding(5)
                                .accessibilityLabel(L("kézzel felvéve"))
                        }
                    }
                    .clipShape(RoundedRectangle(cornerRadius: 6))
                    Text(e.showTitle).font(.subheadline.weight(.semibold)).foregroundStyle(Theme.plasterBright).lineLimit(1)
                    Text(posterSub(e)).font(.caption).foregroundStyle(Theme.muted).lineLimit(1)
                }
            }
            .buttonStyle(.plain)
            .disabled(e.itemId == nil)
            .accessibilityElement(children: .combine)
            .contextMenu {
                if e.isManual && e.id > 0 && canEditJournal {
                    Button(role: .destructive) { deletingViewing = e } label: { Label(L("Nézés törlése"), systemImage: "trash") }
                }
            }
            if !e.isEpisode && e.id > 0 {
                JournalStars(rating: rating, set: { v in rate(e, v) })
            }
        }
        .frame(width: metrics.poster)
    }

    /// Stars for a film; they show at once and go back if the server refuses them.
    private func rate(_ e: JournalEvent, _ v: Int) {
        let before = stars[e.id] ?? e.rating
        // every viewing of the same film shows the same stars
        for x in events where !x.isEpisode && (x.id == e.id || (e.tmdbId > 0 && x.tmdbId == e.tmdbId)) { stars[x.id] = v }
        Task {
            do { try await model.api { try await $0.setJournalRating(event: e.id, rating: v) } }
            catch {
                for x in events where !x.isEpisode && (x.id == e.id || (e.tmdbId > 0 && x.tmdbId == e.tmdbId)) { stars[x.id] = before }
                actionError = error.localizedDescription
            }
        }
    }

    private func meters(_ title: String, _ rows: [(String, Int)]) -> some View {
        let top = max(1, rows.first?.1 ?? 1)
        return VStack(alignment: .leading, spacing: 0) {
            sectionTitle(title).padding(.bottom, 8)
            ForEach(Array(rows.enumerated()), id: \.offset) { i, r in
                HStack(spacing: 12) {
                    Text(r.0).foregroundStyle(Theme.plaster).frame(width: 110, alignment: .leading).lineLimit(1)
                    GeometryReader { g in
                        ZStack(alignment: .leading) {
                            Capsule().fill(Theme.raised)
                            Capsule().fill(i == 0 ? Theme.amber : Theme.muted).frame(width: g.size.width * CGFloat(r.1) / CGFloat(top))
                        }
                    }
                    .frame(height: 6)
                    .accessibilityHidden(true)
                    Text(r.1.formatted()).monospacedDigit().foregroundStyle(Theme.muted).frame(width: 36, alignment: .trailing)
                }
                .padding(.vertical, 10)
                .overlay(alignment: .bottom) { Rectangle().fill(Theme.rule).frame(height: 1) }
                .accessibilityElement(children: .combine)
            }
        }
    }

    private func when(_ s: JournalSummary) -> some View {
        let top = max(1, s.weekdays.max() ?? 1)
        return VStack(alignment: .leading, spacing: 12) {
            sectionTitle(L("Mikor nézel?"))
            HStack(spacing: 6) {
                ForEach(0..<7, id: \.self) { i in
                    VStack(spacing: 4) {
                        RoundedRectangle(cornerRadius: 3)
                            .fill(Theme.amber.opacity(s.weekdays[i] == 0 ? 0.08 : 0.25 + 0.75 * Double(s.weekdays[i]) / Double(top)))
                            .frame(height: 36)
                        Text(Self.weekdayName(i, short: true)).font(.caption2).foregroundStyle(Theme.muted)
                    }
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel("\(Self.weekdayName(i)): \(L("{0} nézés", s.weekdays[i]))")
                }
            }
            VStack(alignment: .leading, spacing: 0) {
                fact(L("Csúcsidő: {0}–{1} óra", s.peakHour, (s.peakHour + 1) % 24))
                fact(L("Legtöbbet: {0}", Self.weekdayName(s.busiestWeekday)))
                if s.afterMidnight > 0 { fact(L("{0} nézés éjfél után", s.afterMidnight)) }
            }
        }
    }

    private func fact(_ text: String) -> some View {
        Text(text).foregroundStyle(Theme.plaster).frame(maxWidth: .infinity, alignment: .leading).padding(.vertical, 10)
            .overlay(alignment: .bottom) { Rectangle().fill(Theme.rule).frame(height: 1) }
    }

    private func sectionTitle(_ t: String) -> some View {
        Text(t).font(Theme.display(metrics.sectionTitle)).foregroundStyle(Theme.plasterBright).accessibilityAddTraits(.isHeader)
    }

    // MARK: - helpers

    private func openItem(_ e: JournalEvent) async {
        guard let id = e.itemId, let d = try? await model.api({ try await $0.item(id) }) else { return }
        open(d.item)
    }

    private func posterSub(_ e: JournalEvent) -> String {
        let day = e.date.formatted(.dateTime.month(.abbreviated).day())
        if e.isEpisode, let s = e.season, let n = e.episode { return "\(L("S{0:00}E{1:00}", s, n)) · \(day)" }
        return day
    }

    private func barLabel(_ b: JournalSummary.Bucket, _ unit: JournalPeriod.Unit) -> String {
        switch unit {
        case .day: return b.key == 1 || b.key % 5 == 0 ? String(b.key) : ""
        case .month: return b.start.formatted(.dateTime.month(.abbreviated))
        case .year: return String(b.key)
        }
    }

    private func longLabel(_ b: JournalSummary.Bucket, _ unit: JournalPeriod.Unit) -> String {
        switch unit {
        case .day: return b.start.formatted(.dateTime.month(.wide).day())
        case .month: return b.start.formatted(.dateTime.year().month(.wide))
        case .year: return String(b.key)
        }
    }

    private func shareText(_ s: JournalSummary) -> String {
        var t = "\(Self.periodLead(period)): \(L("{0} filmet", s.films)) \(L("és {0} epizódot láttál.", s.episodes))"
        if let g = s.genres.first { t += " " + L("Kedvenc műfaj") + ": " + g.name + "." }
        return t + " — Kinora"
    }

    static func periodName(_ p: JournalPeriod) -> String {
        switch p {
        case .month: L("Ez a hónap")
        case .year: L("Idén")
        case .lastYear: L("Tavaly")
        case .all: L("Kezdettől")
        }
    }

    static func periodLead(_ p: JournalPeriod) -> String {
        switch p {
        case .month: Date().formatted(.dateTime.year().month(.wide))
        case .year: String(Calendar.current.component(.year, from: Date()))
        case .lastYear: String(Calendar.current.component(.year, from: Date()) - 1)
        case .all: L("Kezdettől")
        }
    }

    /// 0 = Monday.
    static func weekdayName(_ i: Int, short: Bool = false) -> String {
        let monday = Calendar.current.date(from: DateComponents(year: 2024, month: 1, day: 1 + i))! // 2024-01-01 was a Monday
        return monday.formatted(.dateTime.weekday(short ? .abbreviated : .wide))
    }
}

/// The poster of a viewing's film or series, or its title on a quiet card when the item is gone.
struct JournalArtwork: View {
    @Environment(AppModel.self) private var model
    var imageId: Int?
    var title: String
    @State private var image: UIImage?

    var body: some View {
        Rectangle()
            .fill(Theme.panel)
            .overlay {
                if let image {
                    Image(uiImage: image).resizable().scaledToFill()
                } else {
                    // Too narrow for the title to wrap at word boundaries: show its first letter instead.
                    GeometryReader { geo in
                        let narrow = geo.size.width < 60
                        Text(narrow ? String(title.prefix(1)) : title)
                            .font(Theme.display(narrow ? 22 : 18, weight: .bold))
                            .foregroundStyle(Theme.plaster.opacity(narrow ? 0.6 : 0.85))
                            .multilineTextAlignment(.center)
                            .lineLimit(3)
                            .minimumScaleFactor(0.6)
                            .padding(narrow ? 4 : 10)
                            .frame(width: geo.size.width, height: geo.size.height)
                    }
                }
            }
            .clipped()
            .accessibilityHidden(true)
            .task(id: imageId) {
                guard let id = imageId, let client = model.client, let server = model.server?.id else { return }
                if let hit = ArtworkCache.shared.cached(server: server, id: id, kind: .poster) { image = hit; return }
                image = await ArtworkCache.shared.load(client: client, server: server, id: id, kind: .poster)
            }
    }
}
