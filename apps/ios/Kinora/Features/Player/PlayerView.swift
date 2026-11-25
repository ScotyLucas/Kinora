import AVKit
import SwiftUI
import KinoraKit

/// Full-screen player: video, controls that hide while playing, skip and next-episode prompts.
struct PlayerView: View {
    @Environment(AppModel.self) private var model
    let player: PlayerModel
    @State private var chrome = true
    @State private var scrubbing = false
    @State private var hideTask: Task<Void, Never>?
    /// Double-tap seeking: which side and how far, shown briefly.
    @State private var seekFlash: SeekFlash?
    @State private var flashTask: Task<Void, Never>?
    /// The subtitle search sheet is open.
    @State private var subSearch = false
    /// With VoiceOver the controls stay up: a hidden control can't be found by swiping.
    @Environment(\.accessibilityVoiceOverEnabled) private var voiceOver

    var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()
            GeometryReader { geo in
                PlayerSurface(layer: player.layer)
                    .contentShape(.rect)
                    .gesture(
                        SpatialTapGesture(count: 2)
                            .onEnded { doubleTap(at: $0.location.x, width: geo.size.width) }
                            .exclusively(before: TapGesture().onEnded { toggleChrome() })
                    )
            }
            .ignoresSafeArea()
            .accessibilityHidden(true)

            if let seekFlash { SeekFlashView(flash: seekFlash).allowsHitTesting(false) }

            if player.error == nil, player.loading || player.buffering {
                ProgressView().controlSize(.large).tint(.white).allowsHitTesting(false)
            }

            if player.error == nil {
                CaptionLayer(player: player, raised: chrome)
            }

            if let error = player.error {
                errorPanel(error)
            } else {
                if chrome { controls.transition(.opacity) }
                PromptsView(player: player)
            }

            if let party = player.party {
                PartyReactionsLayer(party: party)
                if party.panelOpen {
                    HStack {
                        Spacer()
                        PartyPanel(party: party) { player.close() }
                    }
                    .transition(.move(edge: .trailing).combined(with: .opacity))
                }
            }
        }
        .animation(.easeOut(duration: 0.2), value: player.party?.panelOpen)
        .alert(L("Watch party"), isPresented: Binding(get: { model.partyError != nil && model.playerVisible },
                                                   set: { if !$0 { model.partyError = nil } })) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(model.partyError ?? "")
        }
        .sheet(isPresented: $subSearch, onDismiss: { scheduleHide() }) {
            SubtitleSearchView(item: player.item) { player.addDownloadedSubtitle($0) }
                .foregroundStyle(.primary)
        }
        .background { keyboardShortcuts }
        .foregroundStyle(.white)
        .statusBarHidden(!chrome)
        .persistentSystemOverlays(chrome ? .automatic : .hidden)
        .preferredColorScheme(.dark)
        .onAppear { player.presented = true; scheduleHide() }
        .onDisappear {
            player.presented = false
            // Swiped away or closed; in picture in picture it keeps playing.
            if !player.pipActive { player.close() }
        }
        .onChange(of: player.menuOpen) { _, open in
            if open { hideTask?.cancel() } else { scheduleHide() }
        }
        .onChange(of: voiceOver) { _, on in if on { show() } else { scheduleHide() } }
        .onChange(of: player.isPlaying) { _, playing in
            if playing { scheduleHide() } else { show() }
        }
    }

    // MARK: - controls

    /// Hardware keyboard (iPad Magic Keyboard and the like): space plays or pauses, the arrows
    /// seek 10 s, Esc closes. Invisible buttons, so they work while the controls are hidden;
    /// holding ⌘ lists them.
    private var keyboardShortcuts: some View {
        Group {
            Button(L("Lejátszás / szünet")) { Haptic.tap.play(); player.togglePlay() }
                .keyboardShortcut(.space, modifiers: [])
            Button(L("Vissza 10 másodpercet")) { seekWithFlash(forward: false) }
                .keyboardShortcut(.leftArrow, modifiers: [])
            Button(L("Előre 10 másodpercet")) { seekWithFlash(forward: true) }
                .keyboardShortcut(.rightArrow, modifiers: [])
            Button(L("Bezárás")) { player.close() }
                .keyboardShortcut(.cancelAction)
        }
        .opacity(0)
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }

    private var controls: some View {
        VStack(spacing: 0) {
            HStack(spacing: 6) {
                RoundIcon("xmark", label: L("Bezárás")) { player.close() }
                Text(player.title)
                    .font(.headline)
                    .lineLimit(1)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.leading, 6)
                if let party = player.party {
                    Button { Haptic.tap.play(); party.panelOpen.toggle() } label: {
                        Image(systemName: "person.2.fill")
                            .font(.system(size: 18, weight: .semibold))
                            .frame(width: 44, height: 44)
                            .overlay(alignment: .topTrailing) {
                                if party.unread > 0 {
                                    Text("\(min(party.unread, 99))")
                                        .font(.caption2.weight(.bold)).foregroundStyle(Theme.amberInk)
                                        .padding(.horizontal, 5).padding(.vertical, 1)
                                        .background(Theme.amber, in: .capsule)
                                        .offset(x: -2, y: 4)
                                }
                            }
                            .contentShape(.circle)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel(party.unread > 0 ? L("Watch party, {0} új üzenet", party.unread) : L("Watch party"))
                }
                ChaptersMenu(player: player)
                OptionsMenu(player: player, subSearch: $subSearch)
                AirPlayButton().frame(width: 44, height: 44)
                if player.pipSupported {
                    RoundIcon("pip.enter", label: L("Kép a képben")) { player.startPiP() }
                }
            }
            .padding(.horizontal, 12)
            .padding(.top, 8)
            .clearOfWindowControls()

            Spacer()

            HStack(spacing: 48) {
                RoundIcon("gobackward.10", size: 30, label: L("Vissza 10 másodpercet")) { Haptic.tap.play(); player.seek(by: -10); scheduleHide() }
                RoundIcon(player.isPlaying ? "pause.fill" : "play.fill", size: 44,
                          label: player.isPlaying ? L("Szünet") : L("Lejátszás")) { Haptic.tap.play(); player.togglePlay() }
                RoundIcon("goforward.10", size: 30, label: L("Előre 10 másodpercet")) { Haptic.tap.play(); player.seek(by: 10); scheduleHide() }
            }

            Spacer()

            Scrubber(player: player, scrubbing: $scrubbing,
                     began: { hideTask?.cancel() }, ended: { scheduleHide() })
            .padding(.horizontal, 20)
            .padding(.bottom, 12)
        }
        .background {
            LinearGradient(stops: [.init(color: .black.opacity(0.7), location: 0),
                                   .init(color: .clear, location: 0.25),
                                   .init(color: .clear, location: 0.7),
                                   .init(color: .black.opacity(0.8), location: 1)],
                           startPoint: .top, endPoint: .bottom)
                .ignoresSafeArea()
                .allowsHitTesting(false)
        }
    }

    private func errorPanel(_ message: String) -> some View {
        VStack(spacing: 14) {
            Image(systemName: "exclamationmark.triangle").font(.largeTitle).foregroundStyle(Theme.amber)
            Text(L("Nem sikerült lejátszani")).font(Theme.display(24, weight: .bold))
            Text(message).multilineTextAlignment(.center).foregroundStyle(Theme.plaster)
            if let reasons = player.info?.reasons, !reasons.isEmpty {
                Text(reasons.map(Language.shared.server).joined(separator: " · ")).font(.caption).foregroundStyle(Theme.muted)
                    .multilineTextAlignment(.center)
            }
            HStack {
                Button(L("Újra")) { player.retry() }.buttonStyle(PrimaryButtonStyle())
                if !player.triedTranscode {
                    Button(L("Átkódolással")) { player.retry(transcode: true) }.buttonStyle(SecondaryButtonStyle())
                }
                Button(L("Bezárás")) { player.close() }.buttonStyle(SecondaryButtonStyle())
            }
            .fixedSize()
        }
        .padding(32)
        .frame(maxWidth: 520)
    }

    // MARK: - tracks, speed, quality

    static func delayLabel(_ d: Double) -> String {
        if d == 0 { return L("Eredeti") }
        let v = String(format: L("%+.1f mp"), d).replacingOccurrences(of: ".", with: Language.shared.decimalSeparator)
        return v
    }

    static func qualityLabel(_ cap: Int?, effective: Int?) -> String {
        if let cap { return SettingChoices.bitrates.first { $0.0 == cap }?.1 ?? "\(cap) kbps" }
        if let effective { return L("Automatikus (mobilnet: {0} Mbps)", effective / 1000) }
        return L("Automatikus")
    }

    // MARK: - double tap

    /// Left or right third: ±10 s (taps in a row add up); the middle switches fill and fit.
    private func doubleTap(at x: CGFloat, width: CGFloat) {
        guard width > 0, player.error == nil else { return }
        let third = x / width
        guard third < 1 / 3 || third > 2 / 3 else {
            withAnimation { player.fill.toggle() }
            return
        }
        seekWithFlash(forward: third > 2 / 3)
    }

    /// ±10 s with the flash on the side; seeks in a row add up.
    private func seekWithFlash(forward: Bool) {
        guard player.error == nil else { return }
        Haptic.tap.play()
        player.seek(by: forward ? 10 : -10)
        let total = (seekFlash?.forward == forward ? seekFlash?.seconds ?? 0 : 0) + 10
        withAnimation(.easeOut(duration: 0.15)) { seekFlash = SeekFlash(forward: forward, seconds: total) }
        flashTask?.cancel()
        flashTask = Task {
            try? await Task.sleep(for: .seconds(0.8))
            guard !Task.isCancelled else { return }
            withAnimation(.easeIn(duration: 0.25)) { seekFlash = nil }
        }
    }

    // MARK: - chrome timing

    private func toggleChrome() {
        withAnimation(.easeInOut(duration: 0.2)) { chrome.toggle() }
        if chrome { scheduleHide() }
    }

    private func show() {
        hideTask?.cancel()
        withAnimation(.easeInOut(duration: 0.2)) { chrome = true }
    }

    private func scheduleHide() {
        hideTask?.cancel()
        hideTask = Task {
            try? await Task.sleep(for: .seconds(3.5))
            guard !Task.isCancelled, player.isPlaying, !scrubbing, !player.menuOpen, !voiceOver else { return }
            withAnimation(.easeInOut(duration: 0.3)) { chrome = false }
        }
    }

    // MARK: - text

    static func clock(_ seconds: Double) -> String {
        let s = Int(max(0, seconds.isFinite ? seconds : 0))
        let h = s / 3600, m = (s % 3600) / 60, sec = s % 60
        return h > 0 ? String(format: "%d:%02d:%02d", h, m, sec) : String(format: "%d:%02d", m, sec)
    }

    static func skipLabel(_ kind: Segment.Kind) -> String {
        switch kind {
        case .recap: L("Összefoglaló átugrása")
        case .credits: L("Stáblista átugrása")
        default: L("Intró átugrása")
        }
    }
}

/// Skip intro and next episode sit above the controls, also when those are hidden.
/// Its own view: it follows the position, which must not re-render the menus.
private struct PromptsView: View {
    var player: PlayerModel

    var body: some View {
        VStack {
            Spacer()
            HStack {
                Spacer()
                if player.showNext, let next = player.next {
                    NextCard(next: next, countdown: player.nextCountdown,
                             play: { Haptic.firm.play(); player.playNext() }, dismiss: { player.dismissNext() })
                } else if let seg = player.currentSegment {
                    Button { Haptic.firm.play(); player.skipSegment() } label: {
                        Label(PlayerView.skipLabel(seg.type), systemImage: "forward.end.fill")
                            .font(.subheadline.weight(.semibold))
                    }
                    .buttonStyle(PrimaryButtonStyle())
                    .fixedSize()
                }
            }
            .padding(.horizontal, 20)
            // A fixed spot above the scrubber: a button that moves when the controls hide
            // is easy to miss.
            .padding(.bottom, 84)
        }
        .animation(.default, value: player.showNext)
        .animation(.default, value: player.currentSegment)
    }
}

/// Scrubber with chapter marks and times; its own view so the position ticks
/// don't re-render the top bar (an open menu would close).
private struct Scrubber: View {
    var player: PlayerModel
    @Binding var scrubbing: Bool
    var began: () -> Void
    var ended: () -> Void
    @State private var scrub: Double?
    @State private var commitTask: Task<Void, Never>?

    var body: some View {
        let pos = scrub ?? player.position
        VStack(spacing: 4) {
            Slider(value: Binding(get: { pos }, set: { t in
                       // A tick at every chapter mark the thumb passes.
                       if chapterIndex(scrub ?? player.position) != chapterIndex(t) { Haptic.select.play() }
                       scrub = t
                       // iOS doesn't always report the end of an edit (a track tap, VoiceOver's
                       // adjust, and sometimes a plain drag on iOS 26), and then nothing would
                       // seek: once the value rests, seek anyway; sooner when no drag is going on.
                       commitTask?.cancel()
                       commitTask = Task {
                           try? await Task.sleep(for: .milliseconds(scrubbing ? 900 : 350))
                           if !Task.isCancelled { commit() }
                       }
                   }),
                   in: 0...max(player.duration, 1)) { editing in
                if editing {
                    scrubbing = true
                    began()
                } else {
                    commit()
                }
            }
            .tint(Theme.amber)
            .overlay { ChapterTicks(chapters: player.chapters, duration: player.duration) }
            .accessibilityLabel(L("Lejátszási pozíció"))
            .accessibilityValue(PlayerView.clock(pos))
            HStack {
                Text(PlayerView.clock(pos))
                if let c = player.chapters.current(at: pos), let t = c.title, !t.isEmpty {
                    Spacer()
                    Text(t).lineLimit(1)
                }
                Spacer()
                Text("−" + PlayerView.clock(max(0, player.duration - pos)))
                    .accessibilityLabel(L("Hátralévő idő: {0}", PlayerView.clock(max(0, player.duration - pos))))
            }
            .font(.caption.monospacedDigit())
            .foregroundStyle(.white.opacity(0.8))
        }
    }

    /// Seeks to where the thumb was let go.
    private func commit() {
        commitTask?.cancel()
        commitTask = nil
        scrubbing = false
        guard let t = scrub else { return }
        Haptic.tap.play()
        player.seek(to: t)
        scrub = nil
        ended()
    }

    /// Which chapter a time falls in; nil without chapters (then scrubbing is silent).
    private func chapterIndex(_ t: Double) -> Int? {
        let starts = player.chapters.map(\.start)
        guard starts.count > 1 else { return nil }
        return starts.lastIndex { $0 <= t } ?? 0
    }
}

private struct NextCard: View {
    var next: Item
    var countdown: Int?
    var play: () -> Void
    var dismiss: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(L("Következő rész")).font(.caption.weight(.semibold)).foregroundStyle(Theme.amber)
            Text(next.title).font(.headline).lineLimit(2)
            if let label = next.episodeLabel {
                Text(label).font(.caption).foregroundStyle(Theme.plaster)
            }
            HStack(spacing: 10) {
                Button(action: play) {
                    Label(countdown.map { L("Lejátszás ({0})", $0) } ?? L("Lejátszás"), systemImage: "play.fill")
                        .font(.subheadline.weight(.semibold))
                        .contentTransition(.numericText())
                }
                .buttonStyle(PrimaryButtonStyle())
                Button(L("Mégse"), action: dismiss).buttonStyle(SecondaryButtonStyle())
            }
            .fixedSize()
        }
        .padding(16)
        .frame(maxWidth: 320, alignment: .leading)
        .background(.ultraThinMaterial, in: .rect(cornerRadius: 14))
    }
}

private struct RoundIcon: View {
    var name: String
    var size: CGFloat
    var label: String
    var action: () -> Void

    init(_ name: String, size: CGFloat = 20, label: String, action: @escaping () -> Void) {
        self.name = name
        self.size = size
        self.label = label
        self.action = action
    }

    var body: some View {
        Button(action: action) {
            Image(systemName: name)
                .font(.system(size: size, weight: .semibold))
                .frame(width: max(44, size * 1.6), height: max(44, size * 1.6))
                .contentShape(.circle)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
    }
}

/// The caption, kept clear of the scrubber and of a skip or next-episode prompt.
/// Its own view: the prompt check follows the position.
private struct CaptionLayer: View {
    var player: PlayerModel
    var raised: Bool

    var body: some View {
        if let caption = player.caption {
            // The prompt sits 84 pt above the bottom and is about 50 pt tall.
            let prompt = player.showNext || player.currentSegment != nil
            CaptionView(text: caption, bottom: raised ? (prompt ? 146 : 110) : 28,
                        style: AppSettings.subtitleStyle)
                .animation(.easeInOut(duration: 0.2), value: raised)
                .animation(.easeInOut(duration: 0.2), value: prompt)
        }
    }
}

private struct CaptionView: View {
    var text: String
    var bottom: CGFloat
    var style: SubtitleStyle

    var body: some View {
        VStack {
            Spacer()
            Text(text)
                .font(.system(size: style.points, weight: .semibold))
                .foregroundStyle(style.color == .yellow ? Color(red: 1, green: 0.88, blue: 0.3) : .white)
                .multilineTextAlignment(.center)
                .padding(.horizontal, style.background == .box ? 10 : 0)
                .padding(.vertical, style.background == .box ? 4 : 0)
                .background(style.background == .box ? Color.black.opacity(0.6) : .clear, in: .rect(cornerRadius: 6))
                .shadow(color: .black, radius: style.background == .none ? 0 : 2)
                .shadow(color: .black.opacity(style.background == .shadow ? 1 : 0), radius: 4)
                .padding(.horizontal, 24)
                .padding(.bottom, bottom)
        }
        .allowsHitTesting(false)
        .accessibilityLabel(text)
    }
}

private struct OptionsMenu: View {
    @Environment(AppModel.self) private var model
    var player: PlayerModel
    @Binding var subSearch: Bool

    var body: some View {
        let audio = player.info?.audioTracks ?? []
        let subs = player.info?.textSubtitles ?? []
        return Menu {
            Group {
            if audio.count > 1, !player.isOffline {
                Picker(selection: Binding(get: { player.audioIdx ?? -1 }, set: { Haptic.select.play(); player.selectAudio($0) })) {
                    ForEach(audio, id: \.typeIndex) { Text(TrackLabel.audio($0)).tag($0.typeIndex) }
                } label: {
                    Label(L("Hang"), systemImage: "speaker.wave.2")
                    Text(audio.first { $0.typeIndex == player.audioIdx }.map { TrackLabel.audio($0) } ?? "")
                }
                .pickerStyle(.menu)
            }
            if !subs.isEmpty {
                Picker(selection: Binding(get: { player.subtitle?.idx ?? -1 },
                                          set: { i in Haptic.select.play(); player.selectSubtitle(subs.first { $0.idx == i }) })) {
                    Text(L("Ki")).tag(-1)
                    ForEach(subs) { Text(TrackLabel.subtitle($0)).tag($0.idx) }
                } label: {
                    Label(L("Felirat"), systemImage: "captions.bubble")
                    Text(player.subtitle.map { TrackLabel.subtitle($0) } ?? L("Ki"))
                }
                .pickerStyle(.menu)
            }
            if player.subtitle != nil {
                Menu {
                    Button(L("Korábban (−0,5 mp)"), systemImage: "minus") { Haptic.select.play(); player.shiftSubtitles(by: -0.5) }
                    Button(L("Később (+0,5 mp)"), systemImage: "plus") { Haptic.select.play(); player.shiftSubtitles(by: 0.5) }
                    if player.subtitleDelay != 0 {
                        Button(L("Visszaállítás"), systemImage: "arrow.counterclockwise") { Haptic.select.play(); player.shiftSubtitles(by: 0) }
                    }
                } label: {
                    Label(L("Felirat időzítése"), systemImage: "timer")
                    Text(PlayerView.delayLabel(player.subtitleDelay))
                }
                .menuActionDismissBehavior(.disabled)
            }
            if !player.isOffline {
                Button(L("Felirat keresése…"), systemImage: "text.magnifyingglass") {
                    Haptic.select.play()
                    subSearch = true
                }
            }
            // In a party the room's clock sets the speed.
            if player.party == nil {
                Picker(selection: Binding(get: { player.speed }, set: { Haptic.select.play(); player.setSpeed($0) })) {
                    ForEach(PlaybackSpeeds.all, id: \.self) { Text(PlaybackSpeeds.label($0)).tag($0) }
                } label: {
                    Label(L("Sebesség"), systemImage: "gauge.with.dots.needle.67percent")
                    Text(PlaybackSpeeds.label(player.speed))
                }
                .pickerStyle(.menu)
            }
            if !player.isOffline {
                Picker(selection: Binding(get: { player.qualityCap ?? -1 }, set: { Haptic.select.play(); player.setQuality($0 < 0 ? nil : $0) })) {
                    Text(L("Automatikus")).tag(-1)
                    ForEach(SettingChoices.bitrates, id: \.0) { Text($0.1).tag($0.0) }
                } label: {
                    Label(L("Minőség"), systemImage: "dial.medium")
                    Text(PlayerView.qualityLabel(player.qualityCap, effective: player.effectiveMaxBitrate))
                }
                .pickerStyle(.menu)
            }
            if player.party == nil, !player.isOffline {
                Button(L("Watch party indítása"), systemImage: "person.2") { model.startParty(player.item) }
            }
            }
            .modifier(MenuPresence(player: player))
        } label: {
            Image(systemName: "ellipsis.circle")
                .font(.system(size: 20, weight: .semibold))
                .frame(width: 44, height: 44)
                .contentShape(.circle)
        }
        .accessibilityLabel(L("Hang, felirat, sebesség, minőség és watch party"))
    }

}

private struct ChaptersMenu: View {
    var player: PlayerModel

    var body: some View {
        let chapters = player.chapters
        if chapters.count > 1 {
            Menu {
                Group {
                ForEach(Array(chapters.enumerated()), id: \.offset) { i, c in
                    Button {
                        Haptic.select.play()
                        player.seek(to: c.start)
                    } label: {
                        let name = c.title.flatMap { $0.isEmpty ? nil : $0 } ?? L("{0}. fejezet", i + 1)
                        Text("\(name) · \(PlayerView.clock(c.start))")
                    }
                }
                }
                .modifier(MenuPresence(player: player))
            } label: {
                Image(systemName: "list.bullet")
                    .font(.system(size: 20, weight: .semibold))
                    .frame(width: 44, height: 44)
                    .contentShape(.circle)
            }
            .accessibilityLabel(L("Fejezetek"))
            }
    }
}

/// Tracks whether a menu's content is on screen.
private struct MenuPresence: ViewModifier {
    var player: PlayerModel

    func body(content: Content) -> some View {
        content
            .onAppear { player.menuOpen = true }
            .onDisappear { player.menuOpen = false }
    }
}

/// Small marks on the scrubber where chapters start.
private struct ChapterTicks: View {
    var chapters: [Chapter]
    var duration: Double

    var body: some View {
        GeometryReader { g in
            // The slider's track is inset by about the thumb radius on both ends.
            let inset: CGFloat = 14
            let w = max(0, g.size.width - inset * 2)
            ForEach(chapters.filter { $0.start > 1 && $0.start < duration - 1 }, id: \.start) { c in
                Capsule()
                    .fill(.white.opacity(0.85))
                    .frame(width: 2, height: 8)
                    .position(x: inset + w * CGFloat(c.start / max(duration, 1)), y: g.size.height / 2)
            }
        }
        .allowsHitTesting(false)
        .opacity(duration > 0 && chapters.count > 1 ? 1 : 0)
    }
}

/// Hosts the player's own layer, so the video survives the view (picture in picture).
private struct PlayerSurface: UIViewRepresentable {
    let layer: AVPlayerLayer

    func makeUIView(context: Context) -> SurfaceView {
        let v = SurfaceView()
        v.backgroundColor = .black
        v.host(layer)
        return v
    }

    func updateUIView(_ view: SurfaceView, context: Context) { view.host(layer) }

    final class SurfaceView: UIView {
        private weak var hosted: AVPlayerLayer?

        func host(_ l: AVPlayerLayer) {
            guard hosted !== l || l.superlayer !== layer else { return }
            hosted = l
            layer.addSublayer(l)
            setNeedsLayout()
        }

        override func layoutSubviews() {
            super.layoutSubviews()
            CATransaction.begin()
            CATransaction.setDisableActions(true)
            hosted?.frame = bounds
            CATransaction.commit()
        }
    }
}

private struct AirPlayButton: UIViewRepresentable {
    func makeUIView(context: Context) -> AVRoutePickerView {
        let v = AVRoutePickerView()
        v.tintColor = .white
        v.activeTintColor = UIColor(Theme.amber)
        v.prioritizesVideoDevices = true
        return v
    }

    func updateUIView(_ uiView: AVRoutePickerView, context: Context) {}
}

struct SeekFlash: Equatable {
    var forward: Bool
    var seconds: Int
}

/// "−20 mp" on the side that was double-tapped.
private struct SeekFlashView: View {
    var flash: SeekFlash

    var body: some View {
        HStack {
            if flash.forward { Spacer() }
            VStack(spacing: 6) {
                Image(systemName: flash.forward ? "goforward" : "gobackward").font(.title)
                Text(L("{0}{1} mp", flash.forward ? "+" : "−", flash.seconds)).font(.subheadline.weight(.semibold).monospacedDigit())
            }
            .padding(22)
            .background(.black.opacity(0.45), in: .circle)
            .padding(.horizontal, 48)
            if !flash.forward { Spacer() }
        }
        .transition(.opacity)
        .accessibilityHidden(true)
    }
}
