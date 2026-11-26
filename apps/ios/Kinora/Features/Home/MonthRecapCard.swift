import SwiftUI
import KinoraKit

/// In the first week of a month the home page sums up the month before, like the web client: how many films
/// and episodes, and the genre watched most. Tapping it opens the Napló on that month; once tapped or closed
/// it stays away until the next month.
struct MonthRecapCard: View {
    @Environment(AppModel.self) private var model
    @Environment(\.metrics) private var metrics
    var recap: Recap
    /// Called once the card was opened or closed.
    var done: () -> Void

    struct Recap: Equatable {
        var films: Int
        var episodes: Int
        var genre: String?
        var month: Date
        var key: String
        var ym: String
    }

    var body: some View { card(recap) }

    private func card(_ r: Recap) -> some View {
        ZStack(alignment: .topTrailing) {
            Button { open(r) } label: {
                VStack(alignment: .leading, spacing: 6) {
                    Text(L("Napló")).font(.caption.weight(.bold)).textCase(.uppercase).tracking(1).foregroundStyle(Theme.amber)
                    Text(L("{0} a Naplóban", monthName(r.month))).font(Theme.display(24)).foregroundStyle(Theme.plasterBright)
                        .multilineTextAlignment(.leading)
                    (Text(L("{0} filmet", r.films)).foregroundStyle(Theme.amber) + Text(" " + L("és {0} epizódot láttál.", r.episodes)))
                        .font(.subheadline).foregroundStyle(Theme.plaster)
                        .multilineTextAlignment(.leading)
                    if let g = r.genre {
                        Text(L("Leginkább {0}.", g)).font(.subheadline).foregroundStyle(Theme.plaster)
                    }
                    Label(L("Mutasd"), systemImage: "chevron.right")
                        .labelStyle(TrailingIcon())
                        .font(.subheadline.weight(.semibold)).foregroundStyle(Theme.amber)
                        .padding(.top, 4)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(18)
                .padding(.trailing, 28)
                .background(
                    LinearGradient(colors: [Theme.amber.opacity(0.22), Theme.amber.opacity(0.05)], startPoint: .topLeading, endPoint: .bottomTrailing),
                    in: RoundedRectangle(cornerRadius: 14)
                )
                .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(Theme.amber.opacity(0.35)))
            }
            .buttonStyle(.plain)

            Button { dismissCard(r) } label: {
                Image(systemName: "xmark").font(.footnote.weight(.bold)).foregroundStyle(Theme.plaster)
                    .frame(width: 44, height: 44).contentShape(.rect)
            }
            .accessibilityLabel(L("Bezárás"))
        }
        .frame(maxWidth: 720, alignment: .leading)
        .padding(.horizontal, metrics.gutter)
    }

    /// The month before, when it is the first week of a month, it had viewings and its card was not closed yet.
    @MainActor static func load(_ model: AppModel) async -> Recap? {
        let cal = Calendar.current, now = Date()
        guard cal.component(.day, from: now) <= 7,
              let thisMonth = cal.dateInterval(of: .month, for: now)?.start,
              let prev = cal.date(byAdding: .month, value: -1, to: thisMonth),
              let server = model.server?.id, let profile = model.me?.profile?.id else { return nil }
        let c = cal.dateComponents([.year, .month], from: prev)
        let key = "kinora.monthRecap.\(server).\(profile).\(c.year ?? 0)-\(c.month ?? 0)"
        let ym = String(format: "%04d-%02d", c.year ?? 0, c.month ?? 0)
        // the server keeps the closed month for every device; the local key covers an older server
        guard (model.me?.profile?.journalRecapSeen ?? "") < ym, !UserDefaults.standard.bool(forKey: key),
              let evs = try? await model.api({ try await $0.journal(from: prev, to: thisMonth) }), !evs.isEmpty else { return nil }
        let s = JournalSummary(events: evs, period: .month, now: prev)
        return Recap(films: s.films, episodes: s.episodes, genre: s.genres.first?.name, month: prev, key: key, ym: ym)
    }

    private func open(_ r: Recap) {
        let cal = Calendar.current
        let sameYear = cal.component(.year, from: r.month) == cal.component(.year, from: Date())
        model.journalStart = (sameYear ? .year : .lastYear, cal.component(.month, from: r.month))
        model.journalVisible = true
        dismissCard(r)
    }

    private func dismissCard(_ r: Recap) {
        UserDefaults.standard.set(true, forKey: r.key)
        Task { _ = try? await model.api { try await $0.journalRecapSeen(month: r.ym) } }
        withAnimation { done() }
    }

    private func monthName(_ d: Date) -> String {
        let s = d.formatted(.dateTime.month(.wide))
        return s.prefix(1).uppercased() + s.dropFirst()
    }
}

private struct TrailingIcon: LabelStyle {
    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: 4) { configuration.title; configuration.icon.imageScale(.small) }
    }
}
