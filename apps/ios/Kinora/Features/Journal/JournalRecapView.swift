import SwiftUI
import KinoraKit

/// The year in the Napló told page by page, like a story: tap the right side for the next page, the left side
/// for the previous one. Pages without anything to say (no genres, no marathon day…) are left out.
struct JournalRecapView: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    var summary: JournalSummary
    var year: String
    var card: Image?
    /// The yearly goal shown with the totals (0: none, or not this year's recap).
    var goal: Int = 0

    enum Page: Hashable { case intro, totals, genre, month, titles, marathon, habits, decade, outro }

    @State private var index = 0

    private var pages: [Page] {
        var p: [Page] = [.intro, .totals]
        if !summary.genres.isEmpty { p.append(.genre) }
        if summary.busiestBucket != nil { p.append(.month) }
        if !summary.topTitles.isEmpty { p.append(.titles) }
        if (summary.biggestDay?.count ?? 0) >= 2 { p.append(.marathon) }
        p.append(.habits)
        if !summary.decades.isEmpty { p.append(.decade) }
        p.append(.outro)
        return p
    }

    var body: some View {
        let pages = pages
        let page = pages[min(index, pages.count - 1)]
        ZStack(alignment: .top) {
            Theme.black.ignoresSafeArea()
            RadialGradient(colors: [Theme.amber.opacity(0.18), .clear], center: .topTrailing, startRadius: 0, endRadius: 520)
                .ignoresSafeArea()
                .accessibilityHidden(true)

            content(page)
                .id(page)
                .transition(reduceMotion ? .opacity : .asymmetric(insertion: .move(edge: .trailing).combined(with: .opacity), removal: .opacity))
                .frame(maxWidth: 560, maxHeight: .infinity, alignment: .leading)
                .padding(.horizontal, 28)
                .padding(.top, 70)
                .padding(.bottom, 40)
                .frame(maxWidth: .infinity)

            // Tap zones under the page's own buttons.
            if page != .outro {
                HStack(spacing: 0) {
                    Color.clear.contentShape(Rectangle()).onTapGesture { go(-1) }
                        .frame(width: 110).accessibilityHidden(true)
                    Color.clear.contentShape(Rectangle()).onTapGesture { go(1) }
                        .accessibilityHidden(true)
                }
            }

            header(count: pages.count)
        }
        .preferredColorScheme(.dark)
        .accessibilityAction(named: L("Következő")) { go(1) }
        .accessibilityAction(named: L("Előző")) { go(-1) }
    }

    private func go(_ step: Int) {
        let next = index + step
        guard next >= 0, next < pages.count else { return }
        withAnimation(reduceMotion ? nil : .easeOut(duration: 0.35)) { index = next }
    }

    private func header(count: Int) -> some View {
        VStack(spacing: 12) {
            HStack(spacing: 4) {
                ForEach(0..<count, id: \.self) { i in
                    Capsule().fill(i <= index ? Theme.amber : Theme.plaster.opacity(0.25)).frame(height: 3)
                }
            }
            .accessibilityElement()
            .accessibilityLabel(L("{0}. oldal / {1}", index + 1, count))
            HStack {
                Text("Kinora · \(year)").font(.footnote.weight(.semibold)).foregroundStyle(Theme.plaster)
                Spacer()
                Button { dismiss() } label: {
                    Image(systemName: "xmark").font(.headline).foregroundStyle(Theme.plasterBright).padding(8)
                }
                .accessibilityLabel(L("Bezárás"))
            }
        }
        .padding(.horizontal, 16)
        .padding(.top, 8)
    }

    // MARK: - pages

    @ViewBuilder private func content(_ page: Page) -> some View {
        switch page {
        case .intro:
            VStack(alignment: .leading, spacing: 18) {
                Spacer()
                Text(year).font(Theme.display(120)).foregroundStyle(Theme.amber).minimumScaleFactor(0.5).lineLimit(1)
                big(L("Ilyen éved volt a Kinorán."))
                small(L("Koppints a folytatáshoz."))
                Spacer()
            }
        case .totals:
            VStack(alignment: .leading, spacing: 18) {
                Spacer()
                kicker(L("Összesen"))
                (Text(L("{0} filmet", summary.films)).foregroundStyle(Theme.amber) + Text(" " + L("és {0} epizódot láttál.", summary.episodes)))
                    .font(Theme.display(44)).foregroundStyle(Theme.plasterBright).fixedSize(horizontal: false, vertical: true)
                small(L("Ez {0} a képernyő előtt.", hours))
                if summary.seconds >= 86400 {
                    small(L("≈ {0} teljes nap", (Double(summary.seconds) / 86400).formatted(.number.precision(.fractionLength(1)))))
                }
                if goal > 0 { small(L("Éves cél") + ": " + L("{0} / {1} film", summary.films, goal)) }
                Spacer()
            }
        case .genre:
            let g = summary.genres
            VStack(alignment: .leading, spacing: 18) {
                Spacer()
                kicker(L("Kedvenc műfaj"))
                Text(g[0].name).font(Theme.display(72)).foregroundStyle(Theme.amber).minimumScaleFactor(0.4).lineLimit(2)
                if let share = summary.topGenreShare { big(L("a nézéseid {0}%-a", share)) }
                if g.count > 1 {
                    VStack(alignment: .leading, spacing: 6) {
                        ForEach(Array(g.dropFirst().prefix(3).enumerated()), id: \.offset) { i, x in
                            Text("\(i + 2). \(x.name)").font(.title3.weight(.semibold)).foregroundStyle(Theme.plaster)
                        }
                    }
                    .padding(.top, 8)
                }
                Spacer()
            }
        case .month:
            let b = summary.busiestBucket!
            let top = max(1, summary.buckets.map(\.events.count).max() ?? 1)
            VStack(alignment: .leading, spacing: 18) {
                Spacer()
                kicker(L("A legerősebb hónapod"))
                Text(capitalized(b.start.formatted(.dateTime.month(.wide)))).font(Theme.display(72)).foregroundStyle(Theme.amber)
                    .minimumScaleFactor(0.4).lineLimit(1)
                big(L("{0} nézés", b.events.count))
                HStack(alignment: .bottom, spacing: 6) {
                    ForEach(summary.buckets) { x in
                        VStack(spacing: 6) {
                            RoundedRectangle(cornerRadius: 2)
                                .fill(x.key == b.key ? Theme.amber : Theme.plaster.opacity(x.events.isEmpty ? 0.12 : 0.35))
                                .frame(height: x.events.isEmpty ? 3 : max(6, 120 * CGFloat(x.events.count) / CGFloat(top)))
                            Text(String(x.start.formatted(.dateTime.month(.narrow)))).font(.caption2).foregroundStyle(Theme.muted)
                        }
                        .frame(maxWidth: .infinity)
                    }
                }
                .frame(height: 150, alignment: .bottom)
                .accessibilityHidden(true)
                Spacer()
            }
        case .titles:
            VStack(alignment: .leading, spacing: 16) {
                Spacer()
                kicker(L("Amit a legtöbbet néztél"))
                ForEach(Array(summary.topTitles.prefix(5).enumerated()), id: \.offset) { i, t in
                    HStack(spacing: 14) {
                        Text(String(i + 1)).font(Theme.display(i == 0 ? 40 : 28)).foregroundStyle(i == 0 ? Theme.amber : Theme.muted)
                            .frame(width: 34, alignment: .leading)
                        JournalArtwork(imageId: t.imageId, title: t.title)
                            .frame(width: i == 0 ? 64 : 44, height: i == 0 ? 96 : 66)
                            .clipShape(RoundedRectangle(cornerRadius: 5))
                        VStack(alignment: .leading, spacing: 2) {
                            Text(t.title).font(i == 0 ? Theme.display(26) : .headline).foregroundStyle(Theme.plasterBright).lineLimit(2)
                            Text(t.isSeries ? L("{0} epizód", t.count) : L("{0}× láttad", t.count))
                                .font(.subheadline).foregroundStyle(Theme.muted)
                        }
                    }
                    .accessibilityElement(children: .combine)
                }
                Spacer()
            }
        case .marathon:
            let d = summary.biggestDay!
            VStack(alignment: .leading, spacing: 18) {
                Spacer()
                kicker(L("A nagy maraton"))
                Text(d.date.formatted(.dateTime.month(.wide).day())).font(Theme.display(64)).foregroundStyle(Theme.amber)
                    .minimumScaleFactor(0.4).lineLimit(1)
                big(L("{0} nézés egyetlen nap alatt.", d.count))
                Spacer()
            }
        case .habits:
            let nightOwl = summary.afterMidnight * 100 >= max(1, summary.films + summary.episodes) * 15
            VStack(alignment: .leading, spacing: 18) {
                Spacer()
                kicker(nightOwl ? L("Éjszakai bagoly") : L("Mikor nézel?"))
                Text(L("{0} óra körül", summary.peakHour)).font(Theme.display(64)).foregroundStyle(Theme.amber).lineLimit(1).minimumScaleFactor(0.4)
                big(L("Kedvenc napod: {0}", JournalView.weekdayName(summary.busiestWeekday)))
                if summary.afterMidnight > 0 { small(L("{0} nézés éjfél után", summary.afterMidnight)) }
                Spacer()
            }
        case .decade:
            let d = summary.decades[0]
            VStack(alignment: .leading, spacing: 18) {
                Spacer()
                kicker(L("Kedvenc évtizeded"))
                Text("\(String(d.decade))–\(String(d.decade + 9).suffix(2))").font(Theme.display(80)).foregroundStyle(Theme.amber)
                    .minimumScaleFactor(0.4).lineLimit(1)
                big(L("{0} nézés", d.count))
                Spacer()
            }
        case .outro:
            VStack(alignment: .leading, spacing: 20) {
                Spacer(minLength: 0)
                big(L("Ennyi volt az éved."))
                if let card {
                    card.resizable().scaledToFit()
                        .clipShape(RoundedRectangle(cornerRadius: 10))
                        .frame(maxHeight: 380)
                        .frame(maxWidth: .infinity)
                        .accessibilityHidden(true)
                }
                HStack(spacing: 12) {
                    if let card {
                        ShareLink(item: card, preview: SharePreview(L("Napló"), image: card)) {
                            Label(L("Megosztás"), systemImage: "square.and.arrow.up").frame(maxWidth: .infinity)
                        }
                        .buttonStyle(.borderedProminent).tint(Theme.amber).foregroundStyle(Theme.amberInk)
                    }
                    Button { withAnimation { index = 0 } } label: {
                        Label(L("Elölről"), systemImage: "arrow.counterclockwise").frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.bordered).tint(Theme.plaster)
                }
                .controlSize(.large)
                Spacer(minLength: 0)
            }
        }
    }

    private var hours: String {
        summary.seconds < 3600 ? L("{0} perc", summary.seconds / 60) : L("{0} óra", Int((Double(summary.seconds) / 3600).rounded()))
    }

    private func kicker(_ t: String) -> some View {
        Text(t).font(.subheadline.weight(.bold)).textCase(.uppercase).tracking(1).foregroundStyle(Theme.plaster)
    }

    private func big(_ t: String) -> some View {
        Text(t).font(Theme.display(30)).foregroundStyle(Theme.plasterBright).fixedSize(horizontal: false, vertical: true)
    }

    private func small(_ t: String) -> some View {
        Text(t).font(.title3).foregroundStyle(Theme.plaster).fixedSize(horizontal: false, vertical: true)
    }

    private func capitalized(_ s: String) -> String { s.prefix(1).uppercased() + s.dropFirst() }
}
