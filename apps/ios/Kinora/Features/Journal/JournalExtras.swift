import SwiftUI
import KinoraKit

// The Napló's second half on iOS: stars, viewings added by hand, people and countries, the hour map,
// records, goals of several kinds, challenges and the titles left unfinished.

// MARK: - stars

/// Five stars; a tap on the current value clears it. Read-only when `set` is nil.
struct JournalStars: View {
    var rating: Int
    var size: CGFloat = 13
    var set: ((Int) -> Void)?

    var body: some View {
        HStack(spacing: 2) {
            ForEach(1...5, id: \.self) { n in
                let on = n <= rating
                if let set {
                    Button { set(n == rating ? 0 : n) } label: { star(on) }
                        .buttonStyle(.plain)
                        .accessibilityLabel(L("{0} csillag", n))
                        .accessibilityAddTraits(n == rating ? .isSelected : [])
                } else {
                    star(on)
                }
            }
        }
        .accessibilityElement(children: set == nil ? .ignore : .contain)
        .accessibilityLabel(set == nil ? (rating > 0 ? L("{0} csillag", rating) : L("Nincs értékelve")) : L("Értékelés"))
    }

    private func star(_ on: Bool) -> some View {
        Image(systemName: on ? "star.fill" : "star")
            .font(.system(size: size))
            .foregroundStyle(on ? Theme.amber : Theme.muted.opacity(0.6))
            .frame(minWidth: size + 6, minHeight: size + 10)
            .contentShape(Rectangle())
    }
}

// MARK: - a viewing added by hand

/// Find a film on TMDB and record when it was seen (outside Kinora), with stars if you like.
struct JournalAddViewingSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    var saved: () -> Void

    @State private var query = ""
    @State private var results: [JournalFound] = []
    @State private var picked: JournalFound?
    @State private var when = Date()
    @State private var rating = 0
    @State private var error: String?
    @State private var busy = false

    var body: some View {
        NavigationStack {
            Form {
                if let f = picked {
                    Section {
                        HStack(spacing: 12) {
                            RemotePoster(url: f.poster, title: f.title).frame(width: 54).clipShape(.rect(cornerRadius: 4))
                            VStack(alignment: .leading) {
                                Text(f.title).font(.headline)
                                if let y = f.year, y > 0 { Text(String(y)).foregroundStyle(Theme.muted) }
                            }
                            Spacer()
                            Button(L("Másik")) { picked = nil }
                        }
                    }
                    Section {
                        DatePicker(L("Mikor láttad?"), selection: $when, in: ...Date(), displayedComponents: .date)
                        HStack {
                            Text(L("Értékelés"))
                            Spacer()
                            JournalStars(rating: rating, size: 18) { rating = $0 }
                        }
                    }
                } else {
                    Section {
                        if results.isEmpty && !query.trimmingCharacters(in: .whitespaces).isEmpty && error == nil {
                            Text(L("Nincs találat.")).foregroundStyle(Theme.muted)
                        }
                        ForEach(results) { f in
                            Button { picked = f } label: { JournalFoundRow(title: f.title, sub: f.year.map { $0 > 0 ? String($0) : "" } ?? "", poster: f.poster) }
                                .buttonStyle(.plain)
                        }
                    } footer: {
                        Text(L("Egy film, amit a Kinorán kívül láttál: moziban, tévében, máshol."))
                    }
                }
                if let error { Section { Text(error).foregroundStyle(.red) } }
            }
            .searchable(text: $query, placement: .navigationBarDrawer(displayMode: .always), prompt: L("Film címe"))
            .task(id: query) { await search() }
            .navigationTitle(L("Nézés hozzáadása"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(L("Mégse")) { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button(L("Mentés")) { Task { await save() } }.disabled(picked == nil || busy)
                }
            }
        }
    }

    private func search() async {
        let q = query.trimmingCharacters(in: .whitespaces)
        guard !q.isEmpty else { results = []; return }
        try? await Task.sleep(for: .milliseconds(350))
        guard !Task.isCancelled else { return }
        do { results = try await model.api { try await $0.journalSearchFilms(q) }; error = nil }
        catch { if !Task.isCancelled { self.error = error.localizedDescription } }
    }

    private func save() async {
        guard let f = picked else { return }
        busy = true
        defer { busy = false }
        // the chosen day at the current time of day, so today's viewing is not dated midnight
        let cal = Calendar.current, t = cal.dateComponents([.hour, .minute], from: Date())
        let at = cal.isDateInToday(when) ? Date() : cal.date(bySettingHour: t.hour ?? 20, minute: t.minute ?? 0, second: 0, of: when) ?? when
        do {
            try await model.api { try await $0.addJournalViewing(tmdbId: f.id, at: min(at, Date()), rating: rating) }
            saved()
            dismiss()
        } catch { self.error = error.localizedDescription }
    }
}

struct JournalFoundRow: View {
    var title: String
    var sub: String
    var poster: String?

    var body: some View {
        HStack(spacing: 12) {
            RemotePoster(url: poster, title: "").frame(width: 40).clipShape(.rect(cornerRadius: 4))
            VStack(alignment: .leading, spacing: 2) {
                Text(title).foregroundStyle(Theme.plasterBright).lineLimit(2)
                if !sub.isEmpty { Text(sub).font(.caption).foregroundStyle(Theme.muted).lineLimit(1) }
            }
            Spacer(minLength: 0)
        }
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
    }
}

// MARK: - the hour map and records

struct JournalHeatmap: View {
    var heat: [[Int]]

    var body: some View {
        let top = max(1, heat.flatMap { $0 }.max() ?? 1)
        VStack(alignment: .leading, spacing: 3) {
            ForEach(0..<7, id: \.self) { d in
                HStack(spacing: 2) {
                    Text(JournalView.weekdayName(d, short: true)).font(.caption2).foregroundStyle(Theme.muted).frame(width: 28, alignment: .leading)
                    ForEach(0..<24, id: \.self) { h in
                        let n = heat[d][h]
                        RoundedRectangle(cornerRadius: 2)
                            .fill(n == 0 ? Theme.raised : Theme.amber.opacity(0.25 + 0.75 * Double(n) / Double(top)))
                            .frame(maxWidth: .infinity)
                            .frame(height: 12)
                    }
                }
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(JournalView.weekdayName(d))
                .accessibilityValue(Self.describe(heat[d]))
            }
            HStack(spacing: 2) {
                Color.clear.frame(width: 28, height: 1)
                ForEach(0..<24, id: \.self) { h in
                    Text(h % 6 == 0 ? String(h) : "").font(.system(size: 9)).foregroundStyle(Theme.muted).frame(maxWidth: .infinity, alignment: .leading)
                }
            }
            .accessibilityHidden(true)
        }
    }

    private static func describe(_ row: [Int]) -> String {
        let busy = row.indices.filter { row[$0] > 0 }.sorted { row[$0] > row[$1] }.prefix(3)
        guard !busy.isEmpty else { return L("nincs nézés") }
        return busy.map { L("{0} órakor {1}", $0, row[$0]) }.joined(separator: ", ")
    }
}

struct JournalRecords: View {
    var insights: JournalInsights
    @Environment(\.horizontalSizeClass) private var sizeClass

    var body: some View {
        let i = insights
        LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 10, alignment: .topLeading), count: sizeClass == .regular ? 4 : 2), alignment: .leading, spacing: 10) {
            record(L("Leghosszabb sorozat"), L("{0} nap", i.longestStreak),
                   i.longestStreak > 1 ? "\(Self.day(i.streakStart)) – \(Self.day(i.streakEnd))" : Self.day(i.streakEnd))
            if let d = i.biggestDay { record(L("Legtöbb egy nap"), L("{0} nézés", d.count), Self.day(d.date)) }
            if let w = i.biggestWeek { record(L("Legnagyobb hét"), L("{0} nézés", w.count), L("{0} hete", Self.day(w.start))) }
            record(L("Darálós hétvégék"), String(i.binges.count),
                   i.binges.first.map { L("a legnagyobb: {0}, {1} epizód", $0.series, $0.count) } ?? L("4 epizód egy sorozatból egy hétvégén"))
        }
    }

    private static func day(_ d: Date?) -> String { d?.formatted(.dateTime.year().month(.abbreviated).day()) ?? "" }

    private func record(_ label: String, _ value: String, _ sub: String) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(label).font(.caption.weight(.semibold)).textCase(.uppercase).foregroundStyle(Theme.muted)
            Text(value).font(Theme.display(36)).foregroundStyle(Theme.plasterBright).lineLimit(1).minimumScaleFactor(0.5)
            Text(sub).font(.caption).foregroundStyle(Theme.muted).fixedSize(horizontal: false, vertical: true)
        }
        // a card per record, all of a row's cards as tall as the tallest
        .frame(maxWidth: .infinity, minHeight: 116, alignment: .topLeading)
        .padding(14)
        .background(Theme.panel, in: RoundedRectangle(cornerRadius: 6))
        .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(Theme.rule, lineWidth: 1))
        .accessibilityElement(children: .combine)
    }
}

enum JournalCountry {
    static func name(_ code: String) -> String { Locale.current.localizedString(forRegionCode: code) ?? code }
    static func flag(_ code: String) -> String {
        guard code.count == 2 else { return "" }
        return String(String.UnicodeScalarView(code.uppercased().unicodeScalars.compactMap { UnicodeScalar(127397 + $0.value) }))
    }
}

// MARK: - goals

/// The goals of the period beyond the yearly film goal, with their pace; a new one from the button below.
struct JournalTargets: View {
    @Environment(AppModel.self) private var model
    var targets: [JournalTarget]
    /// The viewings the values are counted from (the period's).
    var events: [JournalEvent]
    /// Month view: only the monthly goals.
    var monthOnly: Bool
    var canEdit: Bool
    var genres: [String]
    var changed: () -> Void

    @State private var adding = false
    @State private var deleting: JournalTarget?
    @State private var error: String?

    var body: some View {
        let shown = targets.filter { !monthOnly || $0.period == "month" }
        if !shown.isEmpty || canEdit {
            VStack(alignment: .leading, spacing: 14) {
                ForEach(shown) { t in row(t) }
                if canEdit {
                    Button { adding = true } label: { Label(L("Új cél"), systemImage: "plus") }
                        .buttonStyle(.bordered)
                        .tint(Theme.plaster)
                }
            }
            .sheet(isPresented: $adding) { JournalTargetSheet(genres: genres, monthOnly: monthOnly) { changed() } }
            .confirmationDialog(deleting.map { L("Törlöd a célt: {0}?", Self.name($0)) } ?? "", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }), titleVisibility: .visible) {
                Button(L("Törlés"), role: .destructive) {
                    guard let t = deleting else { return }
                    Task {
                        do { try await model.api { try await $0.deleteJournalTarget(t.id) }; changed() } catch { self.error = error.localizedDescription }
                    }
                }
            }
            .alert(L("Nem sikerült"), isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
                Button(L("Rendben"), role: .cancel) {}
            } message: { Text(error ?? "") }
        }
    }

    static func name(_ t: JournalTarget) -> String {
        let what: String
        switch t.metric {
        case "hours": what = L("órák")
        case "episodes": what = L("epizódok")
        case "genre": what = L("{0} műfajú filmek", t.genre.lowercased())
        default: what = L("filmek")
        }
        return t.period == "month" ? L("Havi cél: {0}", what) : L("Éves cél: {0}", what)
    }

    private func row(_ t: JournalTarget) -> some View {
        let v = t.value(events), due = t.due()
        let unit = t.metric == "hours" ? L("{0} / {1} óra", v, t.target) : t.metric == "episodes" ? L("{0} / {1} epizód", v, t.target) : L("{0} / {1} film", v, t.target)
        return VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline) {
                Text(Self.name(t)).font(.headline).foregroundStyle(Theme.plasterBright)
                Text(v >= t.target ? L("teljesítve!") : v - due >= 0 ? L("a tervhez képest +{0}", v - due) : L("a tervhez képest −{0}", due - v))
                    .font(.subheadline).foregroundStyle(v >= due ? Theme.amber : Theme.muted)
                Spacer()
                if canEdit {
                    Button { deleting = t } label: { Image(systemName: "trash") }
                        .accessibilityLabel(L("Törlés") + ": " + Self.name(t))
                        .tint(Theme.muted)
                }
            }
            GeometryReader { geo in
                ZStack(alignment: .leading) {
                    Capsule().fill(Theme.panel)
                    Capsule().fill(Theme.amber).frame(width: geo.size.width * min(1, CGFloat(v) / CGFloat(max(1, t.target))))
                    Rectangle().fill(Theme.plaster).frame(width: 2, height: 18)
                        .offset(x: geo.size.width * min(1, CGFloat(due) / CGFloat(max(1, t.target))) - 1)
                }
            }
            .frame(height: 10)
            .accessibilityElement()
            .accessibilityLabel(Self.name(t))
            .accessibilityValue(unit)
            Text(unit).font(.footnote).foregroundStyle(Theme.muted)
        }
    }
}

struct JournalTargetSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    var genres: [String]
    var monthOnly: Bool
    var saved: () -> Void

    @State private var period = "month"
    @State private var metric = "films"
    @State private var genre = ""
    @State private var target = 4
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Form {
                Picker(L("Időszak"), selection: $period) {
                    Text(L("Havonta")).tag("month")
                    Text(L("Az idei évben")).tag("year")
                }
                Picker(L("Mit számoljon?"), selection: $metric) {
                    Text(L("Filmek")).tag("films")
                    Text(L("Órák")).tag("hours")
                    Text(L("Epizódok")).tag("episodes")
                    Text(L("Egy műfaj filmjei")).tag("genre")
                }
                if metric == "genre" {
                    Picker(L("Műfaj"), selection: $genre) {
                        Text(L("Válassz…")).tag("")
                        ForEach(genres, id: \.self) { Text($0).tag($0) }
                    }
                }
                Stepper(value: $target, in: 1...(period == "year" && metric == "films" ? 1000 : 10000)) {
                    HStack { Text(L("Cél")); Spacer(); Text(target.formatted()).monospacedDigit().foregroundStyle(Theme.amber) }
                }
                if period == "year" && metric == "films" {
                    Text(L("Ez a profil éves filmes célja: minden eszközön és a widgeteken is ez látszik.")).font(.footnote).foregroundStyle(Theme.muted)
                }
                if let error { Text(error).foregroundStyle(.red) }
            }
            .navigationTitle(L("Új cél"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(L("Mégse")) { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button(L("Mentés")) { Task { await save() } }.disabled(metric == "genre" && genre.isEmpty)
                }
            }
        }
        .presentationDetents([.medium, .large])
    }

    private func save() async {
        do {
            // the yearly film goal is the profile's own (every device and the widgets show it), not a second one
            if period == "year" && metric == "films" {
                try await model.setJournalGoal(target)
            } else {
                let t = JournalTarget(period: period, metric: metric, genre: metric == "genre" ? genre : "", target: target)
                try await model.api { try await $0.addJournalTarget(t) }
            }
            saved()
            dismiss()
        } catch { self.error = error.localizedDescription }
    }
}

// MARK: - challenges

struct JournalChallenges: View {
    @Environment(AppModel.self) private var model
    var challenges: [JournalChallenge]
    var canEdit: Bool
    var open: (Item) -> Void
    var changed: () -> Void

    @State private var adding = false
    @State private var shown: JournalChallenge?

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text(L("Kihívások")).font(Theme.display(26)).foregroundStyle(Theme.plasterBright).accessibilityAddTraits(.isHeader)
                Spacer()
                if canEdit { Button { adding = true } label: { Label(L("Új kihívás"), systemImage: "plus") }.font(.subheadline).tint(Theme.amber) }
            }
            .padding(.bottom, 8)
            .overlay(alignment: .bottom) { Rectangle().fill(Theme.rule).frame(height: 1) }
            if challenges.isEmpty {
                Text(L("A kihívás egy kipipálható lista: egy rendező vagy színész minden filmje, egy filmsorozat összes része.")).foregroundStyle(Theme.muted)
            }
            ForEach(challenges) { c in
                Button { shown = c } label: {
                    VStack(alignment: .leading, spacing: 8) {
                        HStack(alignment: .firstTextBaseline) {
                            Text(c.title).font(.headline).foregroundStyle(Theme.plasterBright).lineLimit(1)
                            if c.isComplete { Image(systemName: "checkmark.seal.fill").foregroundStyle(Theme.amber) }
                            Spacer()
                            Text(L("{0} / {1} megnézve", c.done, c.total)).font(.subheadline).foregroundStyle(Theme.muted).monospacedDigit()
                            Image(systemName: "chevron.right").font(.caption).foregroundStyle(Theme.muted)
                        }
                        GeometryReader { g in
                            ZStack(alignment: .leading) {
                                Capsule().fill(Theme.panel)
                                Capsule().fill(Theme.amber).frame(width: g.size.width * (c.total > 0 ? CGFloat(c.done) / CGFloat(c.total) : 0))
                            }
                        }
                        .frame(height: 6)
                    }
                    .padding(.vertical, 6)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityElement(children: .combine)
            }
        }
        .sheet(isPresented: $adding) { JournalNewChallengeSheet { changed() } }
        .sheet(item: $shown) { c in
            JournalChallengeSheet(challenge: c, canEdit: canEdit, open: { item in shown = nil; open(item) }, changed: changed)
        }
    }
}

struct JournalNewChallengeSheet: View {
    enum Kind: String, CaseIterable, Identifiable { case director, actor, series, own; var id: String { rawValue } }

    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    var saved: () -> Void

    @State private var kind: Kind = .director
    @State private var query = ""
    @State private var people: [JournalPerson] = []
    @State private var collections: [JournalCollection] = []
    @State private var pickedPerson: JournalPerson?
    @State private var pickedCollection: JournalCollection?
    @State private var name = ""
    @State private var error: String?
    @State private var busy = false

    private var picked: Bool { kind == .own || pickedPerson != nil || pickedCollection != nil }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker(L("Miből?"), selection: $kind) {
                        Text(L("Egy rendező filmjei")).tag(Kind.director)
                        Text(L("Egy színész filmjei")).tag(Kind.actor)
                        Text(L("Egy filmsorozat")).tag(Kind.series)
                        Text(L("Saját lista")).tag(Kind.own)
                    }
                    .pickerStyle(.menu)
                }
                if kind == .own {
                    Section { Text(L("Üres listát kapsz, a filmeket utána adhatod hozzá.")).foregroundStyle(Theme.muted) }
                } else if let p = pickedPerson {
                    Section { chosen(p.name, p.knownFor ?? "", p.photo) { pickedPerson = nil } }
                } else if let c = pickedCollection {
                    Section { chosen(c.name, "", c.poster) { pickedCollection = nil } }
                } else {
                    Section {
                        TextField(kind == .series ? L("Filmsorozat neve") : L("Név"), text: $query)
                            .textInputAutocapitalization(.words)
                            .autocorrectionDisabled()
                        ForEach(people) { p in
                            Button { pickedPerson = p } label: { JournalFoundRow(title: p.name, sub: p.knownFor ?? "", poster: p.photo) }.buttonStyle(.plain)
                        }
                        ForEach(collections) { c in
                            Button { pickedCollection = c } label: { JournalFoundRow(title: c.name, sub: "", poster: c.poster) }.buttonStyle(.plain)
                        }
                    }
                }
                Section {
                    TextField(kind == .own ? L("Név") : L("üres: a választott neve"), text: $name)
                } header: { Text(L("A kihívás neve")) }
                if let error { Section { Text(error).foregroundStyle(.red) } }
            }
            .task(id: "\(kind.rawValue)|\(query)") { await search() }
            .onChange(of: kind) { pickedPerson = nil; pickedCollection = nil; people = []; collections = [] }
            .navigationTitle(L("Új kihívás"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(L("Mégse")) { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button(L("Mentés")) { Task { await save() } }
                        .disabled(!picked || busy || (kind == .own && name.trimmingCharacters(in: .whitespaces).isEmpty))
                }
            }
        }
    }

    private func chosen(_ title: String, _ sub: String, _ image: String?, clear: @escaping () -> Void) -> some View {
        HStack {
            JournalFoundRow(title: title, sub: sub, poster: image)
            Button(L("Másik"), action: clear)
        }
    }

    private func search() async {
        let q = query.trimmingCharacters(in: .whitespaces)
        guard kind != .own, !q.isEmpty else { people = []; collections = []; return }
        try? await Task.sleep(for: .milliseconds(350))
        guard !Task.isCancelled else { return }
        do {
            if kind == .series {
                collections = try await model.api { try await $0.journalSearchCollections(q) }
                people = []
            } else {
                let all = try await model.api { try await $0.journalSearchPeople(q) }
                // the directors first for a director's list, the actors first for an actor's
                let want = kind == .director ? "Directing" : "Acting"
                people = all.filter { $0.department == want } + all.filter { $0.department != want }
                collections = []
            }
            error = nil
        } catch { if !Task.isCancelled { self.error = error.localizedDescription } }
    }

    private func save() async {
        let source: JournalChallengeSource
        switch kind {
        case .director: guard let p = pickedPerson else { return }; source = .director(p.id)
        case .actor: guard let p = pickedPerson else { return }; source = .actor(p.id)
        case .series: guard let c = pickedCollection else { return }; source = .collection(c.id)
        case .own: source = .own
        }
        busy = true
        defer { busy = false }
        let title = name.trimmingCharacters(in: .whitespaces)
        do {
            try await model.api { try await $0.addJournalChallenge(title: title, from: source) }
            saved()
            dismiss()
        } catch { self.error = error.localizedDescription }
    }
}

/// One challenge: its titles with what is seen ticked; titles can be added, removed, the list renamed or deleted.
struct JournalChallengeSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    @Environment(\.metrics) private var metrics
    @State var challenge: JournalChallenge
    var canEdit: Bool
    var open: (Item) -> Void
    var changed: () -> Void

    @State private var adding = false
    @State private var renaming = false
    @State private var newName = ""
    @State private var deleting = false
    @State private var error: String?

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    Text(L("{0} / {1} megnézve", challenge.done, challenge.total)).foregroundStyle(Theme.muted)
                    if challenge.items.isEmpty {
                        Text(L("Még nincs benne film.")).foregroundStyle(Theme.muted)
                    }
                    LazyVGrid(columns: [GridItem(.adaptive(minimum: 100), spacing: 14, alignment: .top)], spacing: 18) {
                        ForEach(challenge.items) { t in tile(t) }
                    }
                }
                .padding(.horizontal, metrics.gutter)
                .padding(.vertical, 16)
            }
            .kinoraScreen()
            .navigationTitle(challenge.title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(L("Bezárás")) { dismiss() } }
                if canEdit {
                    ToolbarItem(placement: .primaryAction) {
                        Menu {
                            Button { adding = true } label: { Label(L("Filmek hozzáadása"), systemImage: "plus") }
                            Button { newName = challenge.title; renaming = true } label: { Label(L("Átnevezés"), systemImage: "pencil") }
                            Button(role: .destructive) { deleting = true } label: { Label(L("Kihívás törlése"), systemImage: "trash") }
                        } label: { Label(L("Továbbiak"), systemImage: "ellipsis.circle") }
                    }
                }
            }
            .sheet(isPresented: $adding) {
                JournalPickFilmsSheet(challenge: challenge.id) { Task { await reload() } }
            }
            .alert(L("Átnevezés"), isPresented: $renaming) {
                TextField(L("A kihívás neve"), text: $newName)
                Button(L("Mentés")) { Task { await rename() } }
                Button(L("Mégse"), role: .cancel) {}
            }
            .confirmationDialog(L("Törlöd a kihívást: {0}?", challenge.title), isPresented: $deleting, titleVisibility: .visible) {
                Button(L("Törlés"), role: .destructive) { Task { await delete() } }
            }
            .alert(L("Nem sikerült"), isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
                Button(L("Rendben"), role: .cancel) {}
            } message: { Text(error ?? "") }
        }
    }

    private func tile(_ t: JournalChallenge.Title) -> some View {
        Button {
            guard let id = t.itemId else { return }
            Task { if let d = try? await model.api({ try await $0.item(id) }) { open(d.item) } }
        } label: {
            VStack(alignment: .leading, spacing: 4) {
                RemotePoster(url: t.poster, title: t.title)
                    .opacity(t.watched ? 1 : 0.55)
                    .overlay(alignment: .topTrailing) {
                        if t.watched {
                            Image(systemName: "checkmark.circle.fill").font(.title3).foregroundStyle(Theme.amberInk, Theme.amber).padding(5)
                        }
                    }
                    .clipShape(.rect(cornerRadius: 6))
                Text(t.title).font(.caption.weight(.semibold)).foregroundStyle(Theme.plasterBright).lineLimit(2)
                if let y = t.year, y > 0 { Text(String(y)).font(.caption2).foregroundStyle(Theme.muted) }
            }
        }
        .buttonStyle(.plain)
        .disabled(t.itemId == nil)
        .accessibilityElement(children: .combine)
        .accessibilityValue(t.watched ? L("megnézve") : L("még nem láttad"))
        .contextMenu {
            if canEdit {
                Button(role: .destructive) { Task { await remove(t) } } label: { Label(L("Kivétel a listából"), systemImage: "minus.circle") }
            }
        }
    }

    private func reload() async {
        if let all = try? await model.api({ try await $0.journalChallenges() }), let c = all.first(where: { $0.id == challenge.id }) { challenge = c }
        changed()
    }

    private func remove(_ t: JournalChallenge.Title) async {
        do { try await model.api { try await $0.removeJournalChallengeTitle(challenge.id, t) }; await reload() }
        catch { self.error = error.localizedDescription }
    }

    private func rename() async {
        let t = newName.trimmingCharacters(in: .whitespaces)
        guard !t.isEmpty else { return }
        do { try await model.api { try await $0.renameJournalChallenge(challenge.id, title: t) }; challenge.title = t; changed() }
        catch { self.error = error.localizedDescription }
    }

    private func delete() async {
        do { try await model.api { try await $0.deleteJournalChallenge(challenge.id) }; changed(); dismiss() }
        catch { self.error = error.localizedDescription }
    }
}

/// Films found on TMDB, ticked to go on a challenge's list.
struct JournalPickFilmsSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    var challenge: Int64
    var saved: () -> Void

    @State private var query = ""
    @State private var results: [JournalFound] = []
    @State private var picked: [Int64: JournalFound] = [:]
    @State private var error: String?

    var body: some View {
        NavigationStack {
            List {
                ForEach(results) { f in
                    Button { picked[f.id] = picked[f.id] == nil ? f : nil } label: {
                        HStack {
                            JournalFoundRow(title: f.title, sub: f.year.map { $0 > 0 ? String($0) : "" } ?? "", poster: f.poster)
                            Image(systemName: picked[f.id] != nil ? "checkmark.circle.fill" : "circle").foregroundStyle(Theme.amber)
                        }
                    }
                    .buttonStyle(.plain)
                    .accessibilityAddTraits(picked[f.id] != nil ? .isSelected : [])
                }
                if let error { Text(error).foregroundStyle(.red) }
            }
            .searchable(text: $query, placement: .navigationBarDrawer(displayMode: .always), prompt: L("Film címe"))
            .task(id: query) {
                let q = query.trimmingCharacters(in: .whitespaces)
                guard !q.isEmpty else { results = []; return }
                try? await Task.sleep(for: .milliseconds(350))
                guard !Task.isCancelled else { return }
                do { results = try await model.api { try await $0.journalSearchFilms(q) }; error = nil }
                catch { if !Task.isCancelled { self.error = error.localizedDescription } }
            }
            .navigationTitle(L("Filmek hozzáadása"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(L("Mégse")) { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button(picked.isEmpty ? L("Hozzáadás") : L("Hozzáadás ({0})", picked.count)) {
                        Task {
                            do { try await model.api { try await $0.addJournalChallengeTitles(challenge, Array(picked.values)) }; saved(); dismiss() }
                            catch { self.error = error.localizedDescription }
                        }
                    }
                    .disabled(picked.isEmpty)
                }
            }
        }
    }
}

// MARK: - left unfinished

struct JournalAbandoned: View {
    @Environment(AppModel.self) private var model
    var items: [Item]
    var open: (Item) -> Void
    var changed: () -> Void

    var body: some View {
        if !items.isEmpty {
            VStack(alignment: .leading, spacing: 10) {
                Text(L("Félbehagyva")).font(Theme.display(26)).foregroundStyle(Theme.plasterBright).accessibilityAddTraits(.isHeader)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.bottom, 8)
                    .overlay(alignment: .bottom) { Rectangle().fill(Theme.rule).frame(height: 1) }
                Text(L("Elkezdted, a játékidő ötöde előtt abbahagytad, és két hete nem nyúltál hozzá.")).font(.subheadline).foregroundStyle(Theme.muted)
                ScrollView(.horizontal, showsIndicators: false) {
                    LazyHStack(alignment: .top, spacing: 14) {
                        ForEach(items) { it in
                            Button { open(it) } label: { PosterCard(item: it) }
                                .buttonStyle(.plain)
                                .contextMenu {
                                    Button { Task { await hide(it) } } label: { Label(L("Elrejtés innen"), systemImage: "eye.slash") }
                                }
                                .accessibilityAction(named: L("Elrejtés innen")) { Task { await hide(it) } }
                        }
                    }
                }
            }
        }
    }

    private func hide(_ it: Item) async {
        if (try? await model.api({ try await $0.hideAbandoned(it.id) })) != nil { changed() }
    }
}
