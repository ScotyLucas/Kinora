import SwiftUI
import UIKit
import KinoraKit

/// Artwork in memory. Images need the Bearer token, so they are fetched through the
/// client instead of AsyncImage. Items without artwork are remembered and not asked again.
@MainActor
final class ArtworkCache {
    static let shared = ArtworkCache()

    private let images = NSCache<NSString, UIImage>()
    private var missing = Set<String>()
    private var inflight: [String: Task<UIImage?, Never>] = [:]

    private init() { images.countLimit = 400 }

    private func key(_ server: String, _ id: Int, _ kind: ImageKind) -> String { "\(server)/\(id)/\(kind.rawValue)" }

    func cached(server: String, id: Int, kind: ImageKind) -> UIImage? {
        images.object(forKey: key(server, id, kind) as NSString)
    }

    func load(client: KinoraClient, server: String, id: Int, kind: ImageKind) async -> UIImage? {
        let k = key(server, id, kind)
        if let img = images.object(forKey: k as NSString) { return img }
        if missing.contains(k) { return nil }
        if let running = inflight[k] { return await running.value }

        let task = Task<UIImage?, Never> { [weak self] in
            do {
                let data = try await client.image(id, kind)
                guard let img = UIImage(data: data) else { return nil }
                return await img.byPreparingForDisplay() ?? img
            } catch KinoraError.http(404, _) {
                self?.missing.insert(k)
                return nil
            } catch {
                return nil // offline or cancelled: try again next time
            }
        }
        inflight[k] = task
        let img = await task.value
        inflight[k] = nil
        if let img { images.setObject(img, forKey: k as NSString) }
        return img
    }
}

/// Poster, backdrop or episode still with a quiet placeholder while loading or when missing.
struct ArtworkView: View {
    @Environment(AppModel.self) private var model
    var item: Item
    var kind: ImageKind = .poster
    @State private var image: UIImage?

    var body: some View {
        Rectangle()
            .fill(Placeholder.gradient(for: item))
            .overlay {
                if let image {
                    Image(uiImage: image).resizable().scaledToFill().transition(.opacity)
                } else {
                    Text(item.title)
                        .font(Theme.display(kind == .poster ? 18 : 22, weight: .bold))
                        .foregroundStyle(Theme.plaster.opacity(0.85))
                        .multilineTextAlignment(.center)
                        .lineLimit(3)
                        .minimumScaleFactor(0.6)
                        .padding(10)
                }
            }
            .clipped()
            .accessibilityHidden(true)
            .task(id: "\(item.id)/\(kind.rawValue)") { await load() }
    }

    private func load() async {
        guard let client = model.client, let server = model.server?.id else { return }
        if let hit = ArtworkCache.shared.cached(server: server, id: item.id, kind: kind) { image = hit; return }
        let img = await ArtworkCache.shared.load(client: client, server: server, id: item.id, kind: kind)
        withAnimation(.easeOut(duration: 0.2)) { image = img }
    }
}

private enum Placeholder {
    private static let tones: [Color] = [
        Color(hex: 0x3A2E26), Color(hex: 0x2F3433), Color(hex: 0x3B3226), Color(hex: 0x2C2A33), Color(hex: 0x33292B),
    ]

    static func gradient(for item: Item) -> LinearGradient {
        let base = tones[abs(item.seriesId ?? item.id) % tones.count]
        return LinearGradient(colors: [base, Theme.panel], startPoint: .topLeading, endPoint: .bottomTrailing)
    }
}

// MARK: - layout metrics

/// Sizes that follow the window: a phone (compact) or an iPad (regular). In a narrow iPad window
/// the width turns compact, and on a big phone in landscape the height does: the phone sizes
/// come back on their own in both.
struct Metrics: Equatable {
    var regular: Bool

    /// Poster card width in the horizontal rows.
    var poster: CGFloat { regular ? 168 : 120 }
    /// 16:9 card width in the horizontal rows.
    var wide: CGFloat { regular ? 320 : 240 }
    /// Side margin of the pages.
    var gutter: CGFloat { regular ? 32 : 20 }
    /// Gap between cards.
    var spacing: CGFloat { regular ? 20 : 14 }
    /// Section title size.
    var sectionTitle: CGFloat { regular ? 28 : 22 }
    /// Poster grid of the library and search pages.
    var posterGrid: [GridItem] {
        [GridItem(.adaptive(minimum: regular ? 150 : 104, maximum: regular ? 210 : 150), spacing: spacing, alignment: .top)]
    }
    /// Wide hero artwork: on an iPad a 16:10 picture would fill the whole screen.
    func heroRatio(_ phone: CGFloat) -> CGFloat { regular ? max(phone, 2.2) : phone }
    /// Longest comfortable line of running text.
    var readableWidth: CGFloat { 760 }
}

extension EnvironmentValues {
    var metrics: Metrics {
        #if os(iOS)
        Metrics(regular: horizontalSizeClass == .regular && verticalSizeClass != .compact)
        #else
        Metrics(regular: true)
        #endif
    }
}

// MARK: - cards

/// 2:3 poster with title and a second line (year or episode).
struct PosterCard: View {
    var item: Item
    /// In a grid the column sets the width; in a row the card has its own.
    var fillsColumn = false
    @Environment(\.metrics) private var metrics

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            ArtworkView(item: item, kind: .poster)
                .aspectRatio(2 / 3, contentMode: .fit)
                .overlay(alignment: .bottom) { ProgressStripe(item: item) }
                .overlay(alignment: .topTrailing) { WatchedBadge(item: item) }
                .clipShape(.rect(cornerRadius: 8))
                .pointerLift(cornerRadius: 8)
            Text(item.title).font(.subheadline.weight(.semibold)).lineLimit(1)
            Text(Format.subtitle(item)).font(.caption).foregroundStyle(Theme.muted).lineLimit(1)
        }
        .frame(width: fillsColumn ? nil : metrics.poster)
        .accessibilityElement(children: .combine)
    }
}

/// 16:9 card for continue-watching style rows and episodes.
struct WideCard: View {
    var item: Item
    @Environment(\.metrics) private var metrics

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            ArtworkView(item: item, kind: item.kind == .episode ? .thumb : .backdrop)
                .aspectRatio(16 / 9, contentMode: .fit)
                .overlay(alignment: .bottom) { ProgressStripe(item: item) }
                .overlay(alignment: .topTrailing) { WatchedBadge(item: item) }
                .clipShape(.rect(cornerRadius: 8))
                .pointerLift(cornerRadius: 8)
            Text(item.kind == .episode ? (item.seriesTitle ?? item.title) : item.title)
                .font(.subheadline.weight(.semibold)).lineLimit(1)
            Text(item.kind == .episode ? [item.episodeLabel, item.title].compactMap { $0 }.joined(separator: " · ") : Format.subtitle(item))
                .font(.caption).foregroundStyle(Theme.muted).lineLimit(1)
        }
        .frame(width: metrics.wide)
        .accessibilityElement(children: .combine)
    }
}

extension View {
    /// iPad trackpad or mouse: the artwork lifts under the pointer, like the system's own cards.
    func pointerLift(cornerRadius: CGFloat) -> some View {
        #if os(iOS)
        contentShape(.hoverEffect, .rect(cornerRadius: cornerRadius)).hoverEffect(.lift)
        #else
        self
        #endif
    }
}

struct ProgressStripe: View {
    var item: Item
    var body: some View {
        if let raw = item.progress {
            let p = min(max(raw, 0), 1)
            GeometryReader { g in
                ZStack(alignment: .leading) {
                    Rectangle().fill(.black.opacity(0.55))
                    Rectangle().fill(Theme.amber).frame(width: g.size.width * p)
                }
            }
            .frame(height: 4)
            .accessibilityLabel(L("{0} százalék megnézve", Int(p * 100)))
        }
    }
}

struct WatchedBadge: View {
    var item: Item
    var body: some View {
        if item.userData?.played == true {
            Image(systemName: "checkmark")
                .font(.caption2.bold())
                .foregroundStyle(Theme.amberInk)
                .padding(5)
                .background(Theme.amber, in: .circle)
                .padding(6)
                .accessibilityLabel(L("Megnézve"))
        } else if let n = item.unplayedCount, n > 0, item.kind == .series || item.kind == .season {
            Text("\(n)")
                .font(.caption2.bold().monospacedDigit())
                .foregroundStyle(Theme.amberInk)
                .padding(.horizontal, 7).padding(.vertical, 3)
                .background(Theme.amber, in: .capsule)
                .padding(6)
                .accessibilityLabel(L("{0} nem látott rész", n))
        }
    }
}

// MARK: - helpers

enum Format {
    /// "1 ó 32 perc", "45 perc"
    static func runtime(_ seconds: Double?) -> String? {
        guard let s = seconds, s > 0 else { return nil }
        let minutes = max(1, Int((s / 60).rounded()))
        let h = minutes / 60, m = minutes % 60
        if h == 0 { return L("{0} perc", m) }
        return m == 0 ? L("{0} óra", h) : L("{0} ó {1} perc", h, m)
    }

    static func subtitle(_ item: Item) -> String {
        switch item.kind {
        case .series:
            if let n = item.episodeCount { return L("{0} rész", n) }
            return L("Sorozat")
        case .season:
            return item.episodeCount.map { L("{0} rész", $0) } ?? ""
        case .episode:
            return item.episodeLabel ?? ""
        default:
            return item.year.map(String.init) ?? ""
        }
    }

    /// "2019 · 1 ó 32 perc · PG-13 · ★ 7.9"
    /// `rating: false` leaves the star out, when a ratings strip shows the scores.
    static func meta(_ item: Item, rating: Bool = true) -> String {
        var parts: [String] = []
        if let y = item.year { parts.append(String(y)) }
        if item.kind == .movie || item.kind == .episode, let r = runtime(item.runtimeSec) { parts.append(r) }
        if let c = item.contentRating, !c.isEmpty { parts.append(c) }
        if rating, let r = item.communityRating, r > 0 { parts.append("★ " + r.formatted(.number.precision(.fractionLength(1)))) }
        if let w = item.width, let h = item.height { parts.append(resolution(w, h)) }
        return parts.joined(separator: " · ")
    }

    private static func resolution(_ w: Int, _ h: Int) -> String {
        switch max(w, h * 16 / 9) {
        case 3200...: "4K"
        case 1800...: "1080p"
        case 1200...: "720p"
        default: "SD"
        }
    }
}

/// Spinner, error with retry, or the content.
struct LoadingState: View {
    var error: String?
    var retry: () -> Void

    var body: some View {
        VStack(spacing: 14) {
            if let error {
                Image(systemName: "wifi.exclamationmark").font(.largeTitle).foregroundStyle(Theme.muted)
                Text(error).multilineTextAlignment(.center).foregroundStyle(Theme.plaster)
                Button(L("Újra"), action: retry).buttonStyle(.bordered)
            } else {
                ProgressView().controlSize(.large)
            }
        }
        .padding(40)
        .frame(maxWidth: .infinity, minHeight: 300)
    }
}

#if os(iOS)
/// Profile switch / server / logout, in every tab's toolbar.
struct AccountMenu: View {
    @Environment(AppModel.self) private var model
    @State private var confirmLogout = false

    var body: some View {
        Menu {
            if let p = model.me?.profile {
                Text(p.name)
            }
            Button(L("Beállítások"), systemImage: "gearshape") { model.settingsVisible = true }
            if model.me?.profileLocked != true {
                Button(L("Profilváltás"), systemImage: "person.2") { model.switchProfile() }
                Menu(L("Fiókváltás"), systemImage: "person.crop.circle") {
                    ForEach(model.accounts) { a in
                        Button(a.username, systemImage: "person.crop.circle") { Task { await model.switchAccount(to: a) } }
                    }
                    Button(L("Fiók hozzáadása"), systemImage: "person.crop.circle.badge.plus") { Task { await model.addAccount() } }
                }
            }
            Button(L("Másik szerver"), systemImage: "server.rack") { model.showServers() }
            Button(L("Kijelentkezés"), systemImage: "rectangle.portrait.and.arrow.right", role: .destructive) {
                confirmLogout = true
            }
        } label: {
            if let p = model.me?.profile {
                ProfileFace(profile: p, size: 30, showsName: false)
            } else {
                Image(systemName: "person.crop.circle")
            }
        }
        .accessibilityLabel(L("Fiók"))
        .accessibilityValue(model.me?.profile?.name ?? "")
        .confirmationDialog(L("Biztosan kijelentkezel?"), isPresented: $confirmLogout, titleVisibility: .visible) {
            Button(L("Kijelentkezés"), role: .destructive) { Task { await model.logout() } }
        }
    }
}

extension View {
    /// Shared navigation destinations for the browse stacks.
    func browseDestinations() -> some View {
        navigationDestination(for: Item.self) { ItemDetailView(item: $0) }
            .navigationDestination(for: Library.self) { LibraryView(library: $0) }
    }

    func kinoraScreen() -> some View {
        background(Theme.black)
            .toolbarBackground(Theme.black, for: .navigationBar)
    }
}

/// Full width at `ratio`, but never taller than `maxHeight`.
private struct HeroLayout: Layout {
    var ratio: CGFloat
    var maxHeight: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width ?? maxHeight * ratio
        return CGSize(width: width, height: min(width / ratio, maxHeight))
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        for view in subviews { view.place(at: bounds.origin, proposal: ProposedViewSize(bounds.size)) }
    }
}

extension View {
    /// Wide artwork at its own ratio; on a phone in landscape a short strip instead,
    /// so the title and play button stay on screen.
    /// `maxHeight` keeps a wide window's artwork from pushing the title and buttons below the fold:
    /// past it the picture is cropped instead of growing.
    @ViewBuilder func heroFrame(_ ratio: CGFloat, compactHeight: Bool, maxHeight: CGFloat? = nil) -> some View {
        if compactHeight {
            frame(maxWidth: .infinity).frame(height: 170).clipped()
        } else if let maxHeight, maxHeight > 0 {
            HeroLayout(ratio: ratio, maxHeight: maxHeight) { self }.clipped()
        } else {
            aspectRatio(ratio, contentMode: .fit).frame(maxWidth: .infinity)
        }
    }

    /// A bar that runs along the top of a window: in an iPad window its leading end makes room for
    /// the window controls (close, minimize, tile), which would otherwise cover it and take its taps.
    @ViewBuilder func clearOfWindowControls() -> some View {
        if #available(iOS 26.0, *) {
            containerCornerOffset(.leading, sizeToFit: true)
        } else {
            self
        }
    }
}
#endif
