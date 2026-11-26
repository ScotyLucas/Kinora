import SwiftUI
import KinoraKit

/// The Napló's recap as a picture to share: the period, the counts, the timeline and the most watched posters.
/// Drawn by ImageRenderer, so everything it shows (the posters too) must be at hand before it is rendered.
struct JournalShareCard: View {
    static let size = CGSize(width: 360, height: 450)

    var summary: JournalSummary
    var lead: String
    var posters: [UIImage]
    /// The most watched titles, shown instead when no poster could be loaded.
    var titles: [String]

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text("Kinora").font(Theme.display(20)).foregroundStyle(Theme.amber)
                Spacer()
                Text(lead).font(.caption.weight(.semibold)).textCase(.uppercase).foregroundStyle(Theme.muted)
            }
            .padding(.bottom, 18)

            (Text(L("{0} filmet", summary.films)).foregroundStyle(Theme.amber) + Text(" " + L("és {0} epizódot láttál.", summary.episodes)))
                .font(Theme.display(30))
                .foregroundStyle(Theme.plasterBright)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.bottom, 16)

            HStack(alignment: .top, spacing: 18) {
                figure(L("Nézési idő"), summary.seconds < 3600 ? L("{0} perc", summary.seconds / 60)
                                                              : L("{0} óra", Int((Double(summary.seconds) / 3600).rounded())))
                if let g = summary.genres.first { figure(L("Kedvenc műfaj"), g.name) }
            }
            .padding(.bottom, 18)

            bars.frame(height: 54)
            Spacer(minLength: 18)

            if posters.isEmpty {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(titles.prefix(3).enumerated()), id: \.offset) { i, t in
                        HStack(spacing: 12) {
                            Text(String(i + 1)).font(Theme.display(18)).foregroundStyle(i == 0 ? Theme.amber : Theme.muted)
                                .frame(width: 16, alignment: .leading)
                            Text(t).font(.callout.weight(.semibold)).foregroundStyle(Theme.plaster).lineLimit(1)
                        }
                        .padding(.vertical, 8)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .overlay(alignment: .top) { Rectangle().fill(Theme.rule).frame(height: 1) }
                    }
                }
            } else {
                HStack(spacing: 10) {
                    ForEach(Array(posters.prefix(4).enumerated()), id: \.offset) { _, img in
                        Image(uiImage: img).resizable().scaledToFill()
                            .frame(width: 72, height: 108)
                            .clipShape(RoundedRectangle(cornerRadius: 5))
                    }
                }
            }
        }
        .padding(24)
        .frame(width: Self.size.width, height: Self.size.height, alignment: .topLeading)
        .background(Theme.black)
        .overlay(alignment: .top) { Rectangle().fill(Theme.amber).frame(height: 4) }
    }

    private func figure(_ label: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label).font(.caption2.weight(.semibold)).textCase(.uppercase).foregroundStyle(Theme.muted)
            Text(value).font(Theme.display(22)).foregroundStyle(Theme.plasterBright).lineLimit(1).minimumScaleFactor(0.6)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// The timeline in miniature; the busiest bar is amber.
    private var bars: some View {
        let counts = summary.buckets.map(\.events.count)
        let top = max(1, counts.max() ?? 1)
        return HStack(alignment: .bottom, spacing: summary.unit == .day ? 2 : 5) {
            ForEach(Array(counts.enumerated()), id: \.offset) { _, n in
                RoundedRectangle(cornerRadius: 1.5)
                    .fill(n == top && n > 0 ? Theme.amber : n == 0 ? Theme.panel : Theme.rule)
                    .frame(maxWidth: .infinity)
                    .frame(height: n == 0 ? 2 : max(4, 54 * CGFloat(n) / CGFloat(top)))
            }
        }
        .frame(maxHeight: .infinity, alignment: .bottom)
    }
}
