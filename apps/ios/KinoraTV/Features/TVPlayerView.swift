import AVKit
import SwiftUI
import KinoraKit

/// Full-screen player for the Siri Remote.
///
/// - click or play/pause: pause and resume; left/right: ±10 s (repeats add up);
///   up/down or a swipe: show the controls; Menu/Back: hide the controls, then leave.
/// - Intro skip and the next episode take the focus when they appear, so a click acts on them.
///
/// The same commands come from an iPhone (Control Center remote, lock screen) through
/// PlayerModel's Now Playing info and remote command handlers.
struct TVPlayerView: View {
    @Bindable var player: PlayerModel

    enum Focus: Hashable { case timeline, skip, next, dismissNext, audio, subtitles, speed, chapters, party, retry, transcode, close }

    @FocusState private var focus: Focus?
    @State private var chrome = true
    /// The timeline's right end shows the time left, or the clock time the film ends (a long press switches).
    @AppStorage("kinora.playerEndTime") private var showEnd = false
    @State private var hideTask: Task<Void, Never>?
    @State private var seekFlash: Double?
    @State private var flashTask: Task<Void, Never>?
    /// With VoiceOver the controls stay up: a hidden control can't be reached.
    @Environment(\.accessibilityVoiceOverEnabled) private var voiceOver

    var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()
            TVPlayerSurface(layer: player.layer).ignoresSafeArea()

            VStack {
                Spacer()
                LinearGradient(colors: [.clear, .black.opacity(0.85)], startPoint: .top, endPoint: .bottom)
                    .frame(height: 560)
            }
            .ignoresSafeArea()
            .opacity(chrome && player.error == nil ? 1 : 0)
            .animation(.easeOut(duration: 0.25), value: chrome)
            .allowsHitTesting(false)

            if let caption = player.caption {
                TVCaption(text: caption, raised: chrome || prompt, style: AppSettings.subtitleStyle)
            }

            if player.loading || player.buffering {
                ProgressView().scaleEffect(1.6)
            }

            if let message = player.error {
                errorPanel(message)
            } else {
                controls
            }

            if let party = player.party {
                TVPartyOverlay(party: party, chrome: chrome)
            }

            if let s = seekFlash {
                Text(s > 0 ? L("+{0} mp", Int(s)) : L("−{0} mp", Int(-s)))
                    .font(.system(size: 44, weight: .bold).monospacedDigit())
                    .padding(.horizontal, 36).padding(.vertical, 18)
                    .background(.black.opacity(0.55), in: .capsule)
                    .transition(.opacity)
            }
        }
        .onPlayPauseCommand { player.togglePlay(); show() }
        .onExitCommand(perform: exit)
        .onAppear {
            player.presented = true
            focus = .timeline
            scheduleHide()
        }
        .onDisappear { player.presented = false }
        .onChange(of: player.isPlaying) { _, playing in
            if playing { scheduleHide() } else { show(autoHide: false) }
        }
        .onChange(of: player.currentSegment?.start) { _, start in
            if start != nil { focus = .skip } else if focus == .skip { focus = .timeline }
        }
        .onChange(of: player.showNext) { _, on in
            if on { show(autoHide: false); focus = .next } else if focus == .next || focus == .dismissNext { focus = .timeline }
        }
        .onChange(of: player.error) { _, e in if e != nil { focus = .retry } }
        .persistentSystemOverlays(.hidden)
    }

    private var prompt: Bool { player.showNext || player.currentSegment != nil }

    // MARK: - controls

    private var controls: some View {
        VStack(alignment: .leading, spacing: 30) {
            Spacer()
            HStack(alignment: .bottom, spacing: 30) {
                if chrome { titleBlock.transition(.opacity) }
                Spacer()
                promptButtons
            }
            TVTimeline(player: player, focused: focus == .timeline, showEnd: showEnd, preview: seekFlash != nil)
                .opacity(chrome ? 1 : 0)
                .focusable()
                .focused($focus, equals: .timeline)
                .onMoveCommand { dir in
                    switch dir {
                    case .left: seek(-10)
                    case .right: seek(10)
                    default: show()
                    }
                }
                .onTapGesture { player.togglePlay(); show() }
                // A long press switches the right end between the time left and the clock time the film ends.
                .onLongPressGesture(minimumDuration: 0.5) { showEnd.toggle(); show() }
                .accessibilityAction(named: L("Vált a hátralévő idő és a befejezés időpontja között.")) { showEnd.toggle() }
            if chrome {
                TVMenuRow(player: player, focus: $focus)
                    .equatable()
                    .transition(.opacity)
                    .onChange(of: focus) { _, f in
                        // Menus and buttons keep the controls up while they have the focus.
                        // An open menu takes the focus away from all of them (nil): no hiding then either.
                        if f == .timeline || f == .skip { scheduleHide() } else { hideTask?.cancel() }
                    }
            }
        }
        .padding(.horizontal, 90)
        .padding(.bottom, 60)
        .animation(.easeOut(duration: 0.25), value: chrome)
    }

    private var titleBlock: some View {
        VStack(alignment: .leading, spacing: 8) {
            if player.item.kind == .episode {
                Text([player.item.seriesTitle, player.item.episodeLabel].compactMap { $0 }.joined(separator: " · "))
                    .font(.callout.weight(.semibold)).foregroundStyle(Theme.amber)
            }
            Text(player.item.title).font(Theme.display(54)).lineLimit(1)
            if let c = player.currentChapter?.title, !c.isEmpty {
                Text(c).font(.callout).foregroundStyle(Theme.plaster)
            }
        }
    }

    @ViewBuilder private var promptButtons: some View {
        if player.showNext, let next = player.next {
            VStack(alignment: .trailing, spacing: 16) {
                Text(L("Következő rész") + (player.nextCountdown.map { L(" · {0} mp", $0) } ?? ""))
                    .font(.callout.weight(.semibold)).foregroundStyle(Theme.amber)
                HStack(spacing: 20) {
                    Button(L("Mégse")) { player.dismissNext(); focus = .timeline }
                        .buttonStyle(TVPillStyle())
                        .focused($focus, equals: .dismissNext)
                    Button { player.playNext() } label: {
                        Label([next.episodeLabel, next.title].compactMap { $0 }.joined(separator: " · "), systemImage: "forward.end.fill")
                            .lineLimit(1)
                    }
                    .buttonStyle(TVPillStyle(prominent: true))
                    .focused($focus, equals: .next)
                }
            }
            .focusSection()
        } else if let seg = player.currentSegment {
            Button { player.skipSegment(); focus = .timeline } label: {
                Label(Self.skipLabel(seg.type), systemImage: "forward.fill")
            }
            .buttonStyle(TVPillStyle(prominent: true))
            .focused($focus, equals: .skip)
        }
    }


    private func errorPanel(_ message: String) -> some View {
        VStack(spacing: 30) {
            Image(systemName: "exclamationmark.triangle").font(.system(size: 70)).foregroundStyle(Theme.amber)
            Text(L("Nem sikerült lejátszani")).font(Theme.display(48, weight: .bold))
            Text(message).font(.title3).multilineTextAlignment(.center).frame(maxWidth: 1100)
            if let reasons = player.info?.reasons, !reasons.isEmpty {
                Text(reasons.map(Language.shared.server).joined(separator: " · ")).font(.callout).foregroundStyle(Theme.muted)
            }
            HStack(spacing: 24) {
                Button { player.retry() } label: { Label(L("Újra"), systemImage: "arrow.clockwise") }
                    .buttonStyle(TVPillStyle(prominent: true))
                    .focused($focus, equals: .retry)
                if !player.triedTranscode && !player.isOffline {
                    Button(L("Lejátszás átkódolással")) { player.retry(transcode: true) }
                        .buttonStyle(TVPillStyle())
                        .focused($focus, equals: .transcode)
                }
                Button(L("Bezárás")) { player.close() }
                    .buttonStyle(TVPillStyle())
                    .focused($focus, equals: .close)
            }
        }
        .padding(60)
        .background(Theme.panel.opacity(0.92), in: .rect(cornerRadius: 30))
    }

    // MARK: - behaviour

    private func exit() {
        if player.error == nil, chrome, player.isPlaying {
            hide()
        } else {
            player.close()
        }
    }

    private func seek(_ delta: Double) {
        player.seek(by: delta)
        let total = (seekFlash.map { ($0 > 0) == (delta > 0) ? $0 : 0 } ?? 0) + delta
        withAnimation(.easeOut(duration: 0.15)) { seekFlash = total }
        flashTask?.cancel()
        flashTask = Task {
            try? await Task.sleep(for: .seconds(0.9))
            guard !Task.isCancelled else { return }
            withAnimation(.easeOut(duration: 0.25)) { seekFlash = nil }
        }
        show()
    }

    private func show(autoHide: Bool = true) {
        chrome = true
        if autoHide { scheduleHide() } else { hideTask?.cancel() }
    }

    private func hide() {
        hideTask?.cancel()
        chrome = false
        if focus != .skip && focus != .next && focus != .dismissNext { focus = .timeline }
    }

    private func scheduleHide() {
        hideTask?.cancel()
        hideTask = Task {
            try? await Task.sleep(for: .seconds(5))
            guard !Task.isCancelled, player.isPlaying, !voiceOver, focus == .timeline || focus == .skip else { return }
            hide()
        }
    }

    // MARK: - labels

    static func clock(_ seconds: Double) -> String {
        let s = max(0, Int(seconds.isFinite ? seconds : 0))
        let h = s / 3600, m = (s % 3600) / 60, sec = s % 60
        return h > 0 ? String(format: "%d:%02d:%02d", h, m, sec) : String(format: "%d:%02d", m, sec)
    }

    static func delayLabel(_ d: Double) -> String {
        d == 0 ? L("eredeti") : String(format: L("%+.1f mp"), d).replacingOccurrences(of: ".", with: Language.shared.decimalSeparator)
    }

    static func skipLabel(_ kind: Segment.Kind) -> String {
        switch kind {
        case .recap: L("Összefoglaló átugrása")
        case .credits: L("Stáblista átugrása")
        default: L("Intró átugrása")
        }
    }
}

/// Subtitle, audio, chapter and speed/quality menus.
///
/// A view of its own, compared by `==`: the player view's body runs on every playback tick
/// (the position changes), and a tvOS `Menu` that is rebuilt while open flickers. This one
/// only redraws when a value it reads changes (track, speed, quality, chapter).
private struct TVMenuRow: View, Equatable {
    @Environment(AppModel.self) private var model
    let player: PlayerModel
    var focus: FocusState<TVPlayerView.Focus?>.Binding
    @State private var composing = false
    @State private var message = ""
    @State private var subSearch = false
    /// Playback was running when the subtitle search opened; it goes on when it closes.
    @State private var resumeAfterSearch = false

    nonisolated static func == (a: Self, b: Self) -> Bool { a.player === b.player }

    var body: some View {
        let audio = player.info?.audioTracks ?? []
        let subs = player.info?.textSubtitles ?? []
        HStack(spacing: 24) {
            if !subs.isEmpty || !player.isOffline {
                Menu {
                    Button { player.selectSubtitle(nil) } label: { check(L("Ki"), player.subtitle == nil) }
                    ForEach(subs) { s in
                        Button { player.selectSubtitle(s) } label: { check(TrackLabel.subtitle(s), player.subtitle == s) }
                    }
                    if player.subtitle != nil {
                        Section(L("Időzítés: {0}", TVPlayerView.delayLabel(player.subtitleDelay))) {
                            Button(L("Korábban (−0,5 mp)"), systemImage: "minus") { player.shiftSubtitles(by: -0.5) }
                            Button(L("Később (+0,5 mp)"), systemImage: "plus") { player.shiftSubtitles(by: 0.5) }
                            if player.subtitleDelay != 0 {
                                Button(L("Visszaállítás"), systemImage: "arrow.counterclockwise") { player.shiftSubtitles(by: 0) }
                            }
                        }
                    }
                    if !player.isOffline {
                        Section {
                            Button(L("Felirat keresése…"), systemImage: "text.magnifyingglass") {
                                // A party keeps going: pausing here would stop the whole room.
                                resumeAfterSearch = player.party == nil && player.isPlaying
                                if resumeAfterSearch { player.pause() }
                                subSearch = true
                            }
                        }
                    }
                } label: {
                    Label(player.subtitle.map { TrackLabel.subtitle($0) } ?? L("Felirat"), systemImage: "captions.bubble")
                }
                .focused(focus, equals: .subtitles)
            }
            if audio.count > 1, !player.isOffline {
                Menu {
                    ForEach(audio, id: \.typeIndex) { a in
                        Button { player.selectAudio(a.typeIndex) } label: { check(TrackLabel.audio(a), a.typeIndex == player.audioIdx) }
                    }
                } label: {
                    Label(audio.first { $0.typeIndex == player.audioIdx }.map { TrackLabel.audio($0) } ?? L("Hang"),
                          systemImage: "speaker.wave.2")
                }
                .focused(focus, equals: .audio)
            }
            if !player.chapters.isEmpty {
                Menu {
                    ForEach(Array(player.chapters.enumerated()), id: \.offset) { i, c in
                        Button { player.seek(to: c.start) } label: {
                            let name = c.title.flatMap { $0.isEmpty ? nil : $0 } ?? L("{0}. fejezet", i + 1)
                            check("\(name) · \(TVPlayerView.clock(c.start))", c == player.currentChapter)
                        }
                    }
                } label: { Label(L("Fejezetek"), systemImage: "list.bullet") }
                .focused(focus, equals: .chapters)
            }
            if let party = player.party {
                Menu {
                    Section(L("Reakció")) {
                        ForEach(party.reactions, id: \.self) { e in Button(e) { party.react(e) } }
                    }
                    Button(L("Üzenet írása…"), systemImage: "text.bubble") { message = ""; composing = true }
                    let reportable = party.visibleLines.filter { party.isOthers($0) && $0.lineId > 0 }.suffix(5)
                    if !reportable.isEmpty {
                        Menu(L("Üzenet jelentése"), systemImage: "exclamationmark.bubble") {
                            ForEach(reportable.reversed()) { line in
                                Button("\(line.name ?? ""): \(line.text)") { party.report(line) }
                            }
                        }
                    }
                    let others = party.members.filter { ($0.from ?? "").isEmpty == false && $0.id != party.myId }
                    if !others.isEmpty {
                        Menu(L("Letiltás"), systemImage: "person.crop.circle.badge.xmark") {
                            ForEach(others) { m in
                                let from = m.from ?? ""
                                if party.blocked.contains(from) {
                                    Button(L("{0}: tiltás feloldása", m.name)) { party.unblock(from) }
                                } else {
                                    Button(L("{0} letiltása", m.name), role: .destructive) { party.block(from) }
                                }
                            }
                        }
                    }
                    if party.isHost {
                        Button { party.setEveryone(!party.everyone) } label: { check(L("Mindenki irányíthat"), party.everyone) }
                    }
                    Button(L("Kilépés a watch partyból"), systemImage: "rectangle.portrait.and.arrow.right", role: .destructive) {
                        player.close()
                    }
                } label: { Label("Party · \(party.code)", systemImage: "person.2.fill") }
                .focused(focus, equals: .party)
                .alert(L("Üzenet a partynak"), isPresented: $composing) {
                    TextField(L("Üzenet"), text: $message)
                    Button(L("Küldés")) { party.chat(message) }
                    Button(L("Mégse"), role: .cancel) {}
                }
            }
            Menu {
                // In a party the room's clock sets the speed.
                if player.party == nil {
                    ForEach(PlaybackSpeeds.all, id: \.self) { r in
                        Button { player.setSpeed(r) } label: { check(PlaybackSpeeds.label(r), r == player.speed) }
                    }
                }
                if player.party == nil, !player.isOffline {
                    Section(L("Watch party")) {
                        Button(L("Watch party indítása"), systemImage: "person.2") { model.startParty(player.item) }
                    }
                }
                if !player.isOffline {
                    Section(L("Minőség")) {
                        Button { player.setQuality(nil) } label: { check(L("Automatikus"), player.qualityCap == nil) }
                        ForEach(SettingChoices.bitrates, id: \.0) { b in
                            Button { player.setQuality(b.0) } label: { check(b.1, player.qualityCap == b.0) }
                        }
                    }
                }
            } label: {
                if player.party == nil {
                    Label(PlaybackSpeeds.label(player.speed), systemImage: "gauge.with.dots.needle.67percent")
                } else {
                    Label(L("Minőség"), systemImage: "dial.medium")
                }
            }
            .focused(focus, equals: .speed)
        }
        .font(.callout)
        .focusSection()
        .sheet(isPresented: $subSearch, onDismiss: { if resumeAfterSearch { player.play() } }) {
            TVSubtitleSearchView(item: player.item) { player.addDownloadedSubtitle($0) }
        }
    }

    @ViewBuilder private func check(_ title: String, _ on: Bool) -> some View {
        if on { Label(title, systemImage: "checkmark") } else { Text(title) }
    }
}

/// Progress bar with elapsed and remaining time; thicker while it has the focus.
private struct TVTimeline: View {
    var player: PlayerModel
    var focused: Bool
    var showEnd: Bool
    /// After a ±10 s press: the picture at the new position above the knob (trickplay).
    var preview = false

    var body: some View {
        let d = player.duration
        let p = d > 0 ? min(1, max(0, player.position / d)) : 0
        VStack(spacing: 14) {
            GeometryReader { g in
                ZStack(alignment: .leading) {
                    Capsule().fill(.white.opacity(0.25))
                    Capsule().fill(Theme.amber).frame(width: g.size.width * p)
                    ForEach(Array(player.chapters.dropFirst().enumerated()), id: \.offset) { _, c in
                        if d > 0 {
                            Rectangle().fill(.black.opacity(0.6)).frame(width: 3)
                                .offset(x: g.size.width * c.start / d)
                        }
                    }
                    Circle().fill(Theme.plasterBright)
                        .frame(width: focused ? 26 : 0, height: focused ? 26 : 0)
                        .offset(x: g.size.width * p - (focused ? 13 : 0))
                }
                .overlay(alignment: .topLeading) {
                    if preview, let img = player.previewImage(at: player.position) {
                        let w: CGFloat = 400, h = w * CGFloat(img.height) / CGFloat(max(img.width, 1))
                        TrickplayPreview(image: img, time: TVPlayerView.clock(player.position), width: w)
                            .offset(x: min(max(0, g.size.width * p - w / 2), max(0, g.size.width - w)), y: -h - 24)
                            .transition(.opacity)
                    }
                }
            }
            .frame(height: focused ? 14 : 8)
            HStack {
                Image(systemName: player.isPlaying ? "play.fill" : "pause.fill").foregroundStyle(Theme.plaster)
                Text(TVPlayerView.clock(player.position))
                Spacer()
                Text(showEnd ? player.endClock(from: player.position) : "−" + TVPlayerView.clock(d - player.position))
            }
            .font(.callout.monospacedDigit())
            .foregroundStyle(Theme.plaster)
        }
        .animation(.easeOut(duration: 0.15), value: focused)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(L("Lejátszási pozíció"))
        .accessibilityValue(showEnd
            ? L("{0}, befejezés: {1}", TVPlayerView.clock(player.position), player.endClock(from: player.position))
            : L("{0}, hátra {1}", TVPlayerView.clock(player.position), TVPlayerView.clock(d - player.position)))
        .accessibilityHint(L("Balra vagy jobbra 10 másodpercet ugrik."))
    }
}

private struct TVCaption: View {
    var text: String
    var raised: Bool
    var style: SubtitleStyle

    var body: some View {
        VStack {
            Spacer()
            Text(text)
                .font(.system(size: style.points * 2, weight: .semibold))
                .foregroundStyle(style.color == .yellow ? Color(red: 1, green: 0.88, blue: 0.3) : .white)
                .multilineTextAlignment(.center)
                .shadow(color: .black.opacity(style.background == .none ? 0 : 0.9), radius: 4, x: 0, y: 2)
                .padding(.horizontal, style.background == .box ? 22 : 0)
                .padding(.vertical, style.background == .box ? 8 : 0)
                .background(style.background == .box ? Color.black.opacity(0.6) : .clear, in: .rect(cornerRadius: 10))
                .padding(.horizontal, 160)
                .padding(.bottom, raised ? 300 : 70)
                .animation(.easeOut(duration: 0.25), value: raised)
        }
        .allowsHitTesting(false)
    }
}

/// Hosts the player's AVPlayerLayer.
private struct TVPlayerSurface: UIViewRepresentable {
    var layer: AVPlayerLayer

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
            guard hosted !== l else { return }
            hosted?.removeFromSuperlayer()
            layer.addSublayer(l)
            hosted = l
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

/// Watch party on the TV: the invite QR, room code and members at the top while the controls show,
/// chat and room events for a few seconds at the top left, reactions floating up.
private struct TVPartyOverlay: View {
    var party: PartySession
    var chrome: Bool

    var body: some View {
        ZStack(alignment: .top) {
            GeometryReader { geo in
                ForEach(party.floating) { f in
                    FloatingReaction(item: f, big: true)
                        .position(x: geo.size.width * f.x, y: geo.size.height * 0.75)
                }
            }
            HStack(alignment: .top) {
                TimelineView(.periodic(from: .now, by: 1)) { ctx in
                    let recent = party.visibleLines.suffix(4).filter { ctx.date.timeIntervalSince($0.at) < 8 }
                    VStack(alignment: .leading, spacing: 8) {
                        ForEach(recent) { line in
                            Group {
                                if let name = line.name {
                                    Text("\(Text(name + ": ").bold().foregroundColor(Color(partyHex: line.color ?? "")))\(line.text)")
                                } else {
                                    Text(line.text).italic().foregroundStyle(Theme.plaster)
                                }
                            }
                            .font(.callout)
                            .padding(.horizontal, 18).padding(.vertical, 10)
                            .background(.black.opacity(0.6), in: .rect(cornerRadius: 14))
                            .transition(.opacity)
                        }
                    }
                    .frame(maxWidth: 900, alignment: .leading)
                    .animation(.easeOut(duration: 0.3), value: recent.map(\.id))
                }
                Spacer()
                if chrome || party.status != nil {
                    VStack(alignment: .trailing, spacing: 10) {
                        if chrome {
                            // Friends join from their phones: the camera opens the app on this room.
                            HStack(alignment: .center, spacing: 18) {
                                Text(L("Csatlakozz telefonról:\nolvasd be a kódot"))
                                    .font(.caption)
                                    .foregroundStyle(Theme.plaster)
                                    .multilineTextAlignment(.trailing)
                                PartyQRCode(url: party.appLink, size: 150)
                            }
                        }
                        Text(L("Watch party · {0}", party.code)).font(.callout.weight(.semibold).monospaced())
                        HStack(spacing: 16) {
                            ForEach(party.members) { m in
                                HStack(spacing: 6) {
                                    Circle().fill(Color(partyHex: m.color)).frame(width: 14, height: 14)
                                    Text(m.name + (m.host ? " ★" : "")).font(.caption)
                                    if m.buffering { ProgressView().scaleEffect(0.6) }
                                }
                            }
                        }
                        if let s = party.status { Text(s).font(.caption).foregroundStyle(Theme.amber) }
                    }
                    .padding(.horizontal, 24).padding(.vertical, 16)
                    .background(.black.opacity(0.6), in: .rect(cornerRadius: 18))
                    .transition(.opacity)
                }
            }
            .padding(.horizontal, 90)
            .padding(.top, 50)
        }
        .allowsHitTesting(false)
        .animation(.easeOut(duration: 0.25), value: chrome)
    }
}
