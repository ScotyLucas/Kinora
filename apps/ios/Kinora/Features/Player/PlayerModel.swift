import AVKit
import MediaPlayer
import KinoraKit

/// One playback: asks the server how to play, drives AVPlayer, reports progress,
/// offers intro skip and the next episode, and keeps going in picture in picture.
///
/// Remuxed and transcoded streams are HLS playlists that grow while the server works and
/// start at `offset` in the file. A seek past what is ready starts a new session there.
@MainActor @Observable
final class PlayerModel: NSObject {
    let player = AVPlayer()
    let layer: AVPlayerLayer

    private(set) var item: Item
    private(set) var info: PlaybackInfo?
    private(set) var error: String?
    private(set) var loading = true { didSet { party?.buffering(waiting) } }
    private(set) var isPlaying = false
    private(set) var buffering = false { didSet { party?.buffering(waiting) } }
    /// Seconds from the start of the file.
    private(set) var position: Double = 0
    private(set) var next: Item?
    private(set) var nextCountdown: Int?
    private(set) var pipActive = false
    private(set) var triedTranscode = false
    var nextDismissed = false
    /// A controls menu is open (set by the view); the controls stay while it is.
    var menuOpen = false
    /// Audio track (type index) the server is playing.
    private(set) var audioIdx: Int?
    private(set) var subtitle: SubtitleTrack?
    /// The subtitle line showing now.
    private(set) var caption: String?
    /// Seconds the subtitles are shifted; positive shows them later.
    private(set) var subtitleDelay: Double = 0
    private(set) var speed: Float = 1
    /// kbps cap picked in the player; nil follows the network (see `effectiveMaxBitrate`).
    private(set) var qualityCap: Int?
    /// Playing a downloaded file from the device.
    let isOffline: Bool
    var fill = false { didSet { layer.videoGravity = fill ? .resizeAspectFill : .resizeAspect } }

    /// The watch party this player follows; nil when watching alone.
    var party: PartySession?

    /// The full-screen player is on screen (set by the view).
    var presented = true
    var onClose: (() -> Void)?
    var onPiPStart: (() -> Void)?
    var onPiPRestore: (() -> Void)?

    private let client: KinoraClient
    private let skipMode: String
    private let autoplayNext: Bool
    private let firstStart: Double
    private let subMode: String?
    private let subLangs: String?
    private var cues: [Cue] = []
    private var subtitlesChosen = false
    private var subtitleLoad: Task<Void, Never>?
    private let localInfo: PlaybackInfo?
    /// The series' remembered languages were applied to this item.
    private var languagesApplied = false

    private var pip: AVPictureInPictureController?
    private var offset: Double = 0
    private var session: String?
    private var force: String?
    private var pendingSeek: Double?
    /// Counts `start` calls: a response that a newer one overtook is dropped.
    private var startGeneration = 0
    /// A session request is in flight; the player still holds the previous item.
    private var requesting = false
    /// Where a stream that ended early was last picked up again (once per spot).
    private var resumedAt: Double?
    private var ended = false
    private var closed = false
    private var restoring = false
    private var skipped = Set<Double>()
    private var lastReport = Date.distantPast
    private var countdown: Task<Void, Never>?
    private var timeObserver: Any?
    private var endObserver: NSObjectProtocol?
    private var statusObservation: NSKeyValueObservation?
    private var appObservers: [NSObjectProtocol] = []
    private var nowPlayingArtwork: (id: Int, art: MPMediaItemArtwork)?

    init(item: Item, start: Double, client: KinoraClient, profile: Profile?, local: PlaybackInfo? = nil) {
        self.item = item
        self.localInfo = local
        self.isOffline = local != nil
        self.firstStart = start
        self.client = client
        skipMode = profile?.skipIntro ?? "manual"
        autoplayNext = profile?.autoplayNext ?? true
        subMode = profile?.subMode
        subLangs = profile?.subLangs
        layer = AVPlayerLayer(player: player)
        layer.videoGravity = .resizeAspect
        super.init()
        #if os(iOS)
        player.allowsExternalPlayback = true
        player.usesExternalPlaybackWhileExternalScreenIsActive = true
        if AVPictureInPictureController.isPictureInPictureSupported(),
           let pip = AVPictureInPictureController(playerLayer: layer) {
            pip.delegate = self
            pip.canStartPictureInPictureAutomaticallyFromInline = true
            self.pip = pip
        }
        #endif
    }

    // MARK: - derived

    var duration: Double { info?.duration ?? item.runtimeSec ?? 0 }

    /// The clock time playback ends when it runs on from `position` at the current speed, e.g. "22:38".
    func endClock(from position: Double) -> String {
        let left = max(0, duration - position) / Double(max(speed, 0.1))
        return Date().addingTimeInterval(left)
            .formatted(Date.FormatStyle(date: .omitted, time: .shortened).locale(Language.shared.locale))
    }

    /// Loading or stalled: a watch party waits for this viewer.
    var waiting: Bool { loading || buffering }

    /// The position now, not as of the last tick (the party sync measures drift with it).
    var livePosition: Double {
        let now = player.currentTime().seconds
        return now.isFinite && player.currentItem != nil && !loading ? offset + now : position
    }

    /// In a party only those who may control play and skip; the others follow.
    var canControl: Bool { party?.canControl ?? true }
    var pipSupported: Bool { pip != nil }

    var title: String {
        guard item.kind == .episode else { return item.title }
        return [item.seriesTitle, item.episodeLabel, item.title].compactMap { $0 }.joined(separator: " · ")
    }

    /// The intro / recap / credits part being watched, unless the profile turned skipping off.
    var currentSegment: Segment? {
        guard skipMode != "off", canControl, let segs = info?.segments else { return nil }
        if showNext { return nil } // the next-episode card takes over at the credits
        return segs.first { [.intro, .recap, .credits].contains($0.type) && position >= $0.start && position < $0.end - 1 }
    }

    /// From the credits (or near the end) the next episode is offered.
    var showNext: Bool {
        guard next != nil, !nextDismissed, duration > 0, canControl else { return false }
        if ended { return true }
        let at = info?.creditsAt ?? max(duration - 30, duration * 0.85)
        return position >= at
    }

    /// At the credits or close to the end, whether or not there is a next episode.
    private var showNextReached: Bool {
        guard duration > 0 else { return false }
        return position >= (info?.creditsAt ?? max(duration - 30, duration * 0.85))
    }

    // MARK: - lifecycle

    func begin() async {
        try? AVAudioSession.sharedInstance().setCategory(.playback, mode: .moviePlayback)
        try? AVAudioSession.sharedInstance().setActive(true)
        observe()
        setUpRemoteCommands()
        await start(at: firstStart)
    }

    /// Stops everything and tells the server where we got to.
    func close() {
        guard !closed else { return }
        closed = true
        report(ended: ended)
        stopSession()
        HomeScenes.shared.stopped()
        #if os(iOS)
        Handoff.stop()
        LiveActivities.endParty()
        if let code = party?.code { SharePlay.shared.left(room: code) }
        #endif
        countdown?.cancel()
        subtitleLoad?.cancel()
        player.pause()
        player.replaceCurrentItem(with: nil)
        if let timeObserver { player.removeTimeObserver(timeObserver) }
        if let endObserver { NotificationCenter.default.removeObserver(endObserver) }
        appObservers.forEach { NotificationCenter.default.removeObserver($0) }
        appObservers = []
        statusObservation = nil
        pip?.stopPictureInPicture()
        party?.leave()
        tearDownRemoteCommands()
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
        onClose?()
    }

    func retry(transcode: Bool = false) {
        if transcode { force = "transcode"; triedTranscode = true }
        Task { await start(at: position) }
    }

    private func start(at t: Double) async {
        startGeneration += 1
        let generation = startGeneration
        requesting = true
        loading = true
        error = nil
        let prev = session
        session = nil
        do {
            let info: PlaybackInfo
            if let localInfo {
                info = localInfo
            } else {
                info = try await client.playback(item.id, PlaybackRequest(start: t, audioIdx: audioIdx, maxBitrate: effectiveMaxBitrate,
                                                                          force: force, prevSession: prev))
            }
            guard !closed, generation == startGeneration else {
                // A later seek asked for another session meanwhile: this one is not needed.
                if let s = info.session, localInfo == nil { let c = client; Task { try? await c.stopStream(s) } }
                return
            }
            guard let url = info.url.hasPrefix("file:") ? URL(string: info.url) : await client.url(info.url)
            else { throw KinoraError.network }
            self.info = info
            audioIdx = info.audioIdx.flatMap { $0 >= 0 ? $0 : nil }
            let remembered = item.seriesId.flatMap { AppSettings.languages(series: $0) }
            // A newer server keeps the film's or series' languages per profile and already applied them
            // (audio_idx, sub_idx). A series remembered only on this device moves to the server once.
            let serverChose = info.titleLangs.map { !$0.audioLang.isEmpty || !$0.subLang.isEmpty } ?? false
            let useServer = localInfo == nil && info.subIdx != nil && (serverChose || remembered == nil)
            if !languagesApplied, localInfo == nil, info.subIdx != nil, !serverChose, let r = remembered {
                let c = client, id = item.id
                Task { try? await c.setTitleLangs(id, audio: r.audio, subtitle: r.subtitle.map { $0.isEmpty ? TitleLangs.off : $0 }) }
            }
            if !languagesApplied, localInfo == nil {
                languagesApplied = true
                // The series was last watched in another audio language: ask for that track instead.
                if !useServer, let a = remembered?.audioTrack(in: info.audioTracks), a.typeIndex != audioIdx {
                    audioIdx = a.typeIndex
                    if let s = info.session { session = s }
                    await start(at: t)
                    return
                }
            }
            if !subtitlesChosen {
                subtitlesChosen = true
                if useServer, let s = info.subIdx {
                    selectSubtitle(s >= 0 ? info.textSubtitles.first(where: { $0.idx == s }) : nil, remember: false)
                } else if let pick = remembered?.subtitleTrack(in: info.textSubtitles) {
                    selectSubtitle(pick, remember: false)
                } else {
                    selectSubtitle(defaultSubtitle(info.textSubtitles, mode: subMode, langs: subLangs), remember: false)
                }
            }
            session = info.session
            offset = info.mode == .direct ? 0 : (info.startOffset ?? 0)
            position = t
            syncChapter()
            pendingSeek = info.mode == .direct && t > 0 ? t : nil
            let playerItem = AVPlayerItem(url: url)
            watch(playerItem)
            player.replaceCurrentItem(with: playerItem)
            requesting = false
            player.defaultRate = speed
            // In a party the room says whether to play (the sync starts it when it does).
            if party?.state.map({ $0.playing }) ?? true { player.play() }
            if item.kind == .episode, next == nil {
                next = try? await client.nextEpisode(after: item.id)
            }
            updateNowPlaying()
            loadNowPlayingArtwork()
        } catch is CancellationError {
            if generation == startGeneration { requesting = false }
        } catch {
            guard generation == startGeneration else { return }
            requesting = false
            self.error = error.localizedDescription
            loading = false
        }
    }

    // MARK: - tracks


    /// Switching audio needs a new stream from the server (another track is remuxed in).
    func selectAudio(_ idx: Int) {
        guard idx != audioIdx, !isOffline else { return }
        audioIdx = idx
        rememberLanguages()
        Task { await start(at: position) }
    }

    /// Subtitles are fetched as WebVTT and drawn over the video, so they work in every mode.
    func selectSubtitle(_ track: SubtitleTrack?, remember: Bool = true) {
        subtitleLoad?.cancel()
        defer { if remember { rememberLanguages() } }
        subtitle = track
        cues = []
        caption = nil
        guard let track else { return }
        subtitleLoad = Task {
            do {
                guard let url = track.url.hasPrefix("file:") ? URL(string: track.url) : await client.url(track.url)
                else { throw KinoraError.network }
                let (data, resp) = try await URLSession.shared.data(from: url)
                // A downloaded title's file has no HTTP response.
                if let http = resp as? HTTPURLResponse, http.statusCode != 200 { throw KinoraError.network }
                let parsed = WebVTT.parse(String(decoding: data, as: UTF8.self))
                guard !Task.isCancelled, subtitle == track else { return }
                cues = parsed
                updateCaption()
            } catch is CancellationError {
            } catch {
                guard subtitle == track else { return }
                subtitle = nil
            }
        }
    }

    /// A subtitle just downloaded on the server joins the tracks and shows at once.
    func addDownloadedSubtitle(_ s: MediaStream) {
        guard var info, !isOffline else { return }
        let path = "/api/items/\(item.id)/subtitles/\(s.typeIndex)"
        let url = info.streamToken.map { path + "?st=" + ($0.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? $0) } ?? path
        let track = SubtitleTrack(idx: s.typeIndex, lang: s.lang, title: s.title, forced: s.forced, textBased: true, url: url)
        var subs = info.subtitles ?? []
        subs.removeAll { $0.idx == track.idx }
        subs.append(track)
        info.subtitles = subs
        self.info = info
        selectSubtitle(track)
    }

    /// The film or series starts with these languages next time: kept on the server for this profile (every
    /// device), and for a series on this device too (an older server, offline playback).
    private func rememberLanguages() {
        let audio = info?.audioTracks.first { $0.typeIndex == audioIdx }?.lang
        if let series = item.seriesId {
            AppSettings.setLanguages(LanguageChoice(audio: audio, subtitle: subtitle?.lang ?? (subtitle == nil ? "" : nil),
                                                    subtitleForced: subtitle?.forced), series: series)
        }
        guard !isOffline else { return }
        let sub = subtitle == nil ? TitleLangs.off : subtitle?.lang
        let c = client, id = item.id
        Task { try? await c.setTitleLangs(id, audio: audio, subtitle: sub) }
    }

    func shiftSubtitles(by delta: Double) {
        subtitleDelay = delta == 0 ? 0 : ((subtitleDelay + delta) * 10).rounded() / 10
        updateCaption()
    }

    func setSpeed(_ rate: Float) {
        guard party == nil else { return } // the party sync owns the rate
        speed = rate
        player.defaultRate = rate
        if player.timeControlStatus != .paused { player.rate = rate }
        updateNowPlaying()
    }

    // MARK: - quality and chapters

    /// nil: automatic (the mobile data limit applies on metered connections).
    func setQuality(_ kbps: Int?) {
        guard kbps != qualityCap, !isOffline else { return }
        qualityCap = kbps
        Task { await start(at: position) }
    }

    var effectiveMaxBitrate: Int? {
        if let qualityCap { return qualityCap > 0 ? qualityCap : nil }
        let cell = AppSettings.cellularMaxBitrate
        return NetworkStatus.shared.isExpensive && cell > 0 ? cell : nil
    }

    var chapters: [Chapter] { (info?.chapters ?? []).sorted { $0.start < $1.start } }
    /// Stored, not computed from `position`: views that show it (an open tvOS menu) only redraw
    /// when the chapter changes, not on every tick.
    private(set) var currentChapter: Chapter?

    private func syncChapter() {
        let now = chapters.current(at: position)
        if now != currentChapter { currentChapter = now }
    }

    private func stopSession() {
        guard let s = session else { return }
        session = nil
        let c = client
        Task { try? await c.stopStream(s) }
    }

    // MARK: - controls

    func togglePlay() {
        if player.timeControlStatus == .paused { play() } else { pause() }
    }

    func play() {
        if ended { ended = false; seek(to: 0) }
        party?.played()
        player.play()
        updateNowPlaying()
    }

    func pause() {
        party?.paused(at: livePosition)
        player.pause()
        report()
        updateNowPlaying()
    }

    func seek(by delta: Double) { seek(to: position + delta) }

    /// Seconds from the start of the file; a party follows.
    func seek(to target: Double) {
        guard let t = move(to: target) else { return }
        party?.seeked(to: t)
    }

    /// Seeks without telling the party (its own corrections, skips it hears about otherwise).
    @discardableResult
    private func move(to target: Double) -> Double? {
        guard let info else { return nil }
        let t = max(0, duration > 2 ? min(target, duration - 2) : target)
        ended = false
        position = t
        syncChapter()
        if requesting {
            // A session is still being asked for (an earlier seek, a track change): the
            // current item is the old one, so ask again from here; the newer request wins.
            Task { await start(at: t) }
        } else if info.mode == .direct {
            // Before the item is ready its resume seek is still pending: replace it, or it
            // would land after (and undo) this one.
            if pendingSeek != nil { pendingSeek = t }
            player.seek(to: CMTime(seconds: t, preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero)
        } else {
            let rel = t - offset
            if rel >= 0, let range = player.currentItem?.seekableTimeRanges.last?.timeRangeValue,
               rel <= range.end.seconds {
                player.seek(to: CMTime(seconds: rel, preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero)
            } else {
                // Not transcoded yet (or behind the start of this session): new session from there.
                report()
                Task { await start(at: t) }
            }
        }
        updateNowPlaying()
        return t
    }

    func skipSegment() {
        guard let s = currentSegment else { return }
        skipped.insert(s.start)
        if let t = move(to: s.end) { party?.skipped(to: t, kind: s.type) }
    }

    func dismissNext() {
        nextDismissed = true
        countdown?.cancel()
        nextCountdown = nil
    }

    func playNext() {
        guard let n = next, canControl else { return }
        party?.nextItem(n.id)
        load(n, at: n.progress != nil ? (n.userData?.position ?? 0) : 0)
    }

    /// Another title in the same player: the next episode, or the one a party moved to.
    private func load(_ n: Item, at start: Double) {
        countdown?.cancel()
        nextCountdown = nil
        // Only finished if it got to the credits (a party can move on mid-film).
        report(ended: ended || showNextReached)
        item = n
        next = nil
        info = nil
        nextDismissed = false
        ended = false
        skipped = []
        position = 0
        syncChapter()
        audioIdx = nil
        subtitlesChosen = false
        languagesApplied = false
        subtitleDelay = 0
        selectSubtitle(nil, remember: false)
        Task { await self.start(at: start) }
    }

    // MARK: - watch party

    private var switching: Int?

    /// The room moved to another title (the host started the next episode).
    func partySwitch(to id: Int) {
        guard switching != id, !closed else { return }
        switching = id
        let c = client
        Task {
            defer { switching = nil }
            guard let detail = try? await c.item(id), !closed, item.id != id else { return }
            next = nil
            load(detail.item, at: party?.state.map { $0.itemId == id ? $0.pos : 0 } ?? 0)
        }
    }

    /// The room's clock: play or pause, jump when far off, run a little faster or slower when near.
    func partyApply(playing: Bool, seek target: Double?, rate: Float) {
        if let target { move(to: target) }
        if playing {
            if ended { return }
            if player.rate != rate { player.rate = rate }
        } else if player.timeControlStatus != .paused {
            player.pause()
            report()
        }
        if speed != 1 { speed = 1 }
        player.defaultRate = 1
        updateNowPlaying()
    }

    func startPiP() { pip?.startPictureInPicture() }

    // MARK: - observing

    private func observe() {
        timeObserver = player.addPeriodicTimeObserver(forInterval: CMTime(value: 1, timescale: 4), queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.tick() }
        }
        endObserver = NotificationCenter.default.addObserver(forName: AVPlayerItem.didPlayToEndTimeNotification, object: nil, queue: .main) { [weak self] note in
            let finished = (note.object as AnyObject?).map(ObjectIdentifier.init)
            MainActor.assumeIsolated {
                guard let self, let current = self.player.currentItem, finished == ObjectIdentifier(current) else { return }
                self.didEnd()
            }
        }
        #if os(iOS)
        // Settings → "Hang folytatása a háttérben": without a layer AVPlayer keeps the sound
        // going in the background. Picture in picture, when it started, takes precedence.
        let center = NotificationCenter.default
        appObservers = [
            center.addObserver(forName: UIApplication.didEnterBackgroundNotification, object: nil, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated {
                    guard let self, !self.pipActive, AppSettings.backgroundAudio else { return }
                    let wasPlaying = self.player.rate > 0
                    self.layer.player = nil
                    if wasPlaying { self.player.play() }
                }
            },
            center.addObserver(forName: UIApplication.willEnterForegroundNotification, object: nil, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated {
                    guard let self, self.layer.player == nil else { return }
                    self.layer.player = self.player
                }
            },
        ]
        #endif
    }

    private func watch(_ playerItem: AVPlayerItem) {
        statusObservation = playerItem.observe(\.status, options: [.new]) { [weak self] observed, _ in
            let status = observed.status
            let message = observed.error?.localizedDescription
            Task { @MainActor in self?.statusChanged(status, message) }
        }
    }

    private func statusChanged(_ status: AVPlayerItem.Status, _ message: String?) {
        switch status {
        case .readyToPlay:
            loading = false
            if let t = pendingSeek {
                pendingSeek = nil
                player.seek(to: CMTime(seconds: t, preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero)
            } else if info?.mode != .direct, player.currentTime().seconds > 1 {
                // A growing (EVENT) playlist may open at its live edge: start from the top.
                player.seek(to: .zero)
            }
        case .failed:
            loading = false
            // A server stream that was already playing broke off (its session expired or
            // the server restarted): pick it up from here once with a new session.
            if !isOffline, !closed, info?.mode != .direct, position > 1,
               resumedAt.map({ abs($0 - position) > 5 }) ?? true {
                resumedAt = position
                Task { await start(at: position) }
                return
            }
            error = message ?? L("A videó nem játszható le.")
        default:
            break
        }
    }

    /// The cue showing now; also called while paused, when the periodic observer is quiet.
    private func updateCaption() {
        let line = cues.isEmpty ? nil : WebVTT.text(at: position - subtitleDelay, in: cues)
        if line != caption { caption = line }
    }

    private func tick() {
        guard !closed else { return }
        let now = player.currentTime().seconds
        if now.isFinite, player.currentItem != nil, !loading { position = offset + now }
        syncChapter()
        updateCaption()
        let wasPlaying = isPlaying
        isPlaying = player.timeControlStatus == .playing
        buffering = player.timeControlStatus == .waitingToPlayAtSpecifiedRate
        // Rate and elapsed time keep the lock screen's clock right (and an iPhone's, for the Apple TV).
        if wasPlaying != isPlaying {
            updateNowPlaying()
            if isPlaying { HomeScenes.shared.playing() } else { HomeScenes.shared.paused() }
        }
        if isPlaying, Date.now.timeIntervalSince(lastReport) > 10 { report() }

        if skipMode == "auto", party == nil, let s = currentSegment, s.type != .credits, !skipped.contains(s.start) {
            skipped.insert(s.start)
            seek(to: s.end)
        }
        updateCountdown()
    }

    private func didEnd() {
        // A transcode that broke off closes its playlist early, and the player "finishes"
        // mid-film. That is not the end: pick it up from here once, then say it broke.
        if duration > 0, duration - position > max(60, duration * 0.05) {
            isPlaying = false
            report()
            if !isOffline, info?.mode != .direct, resumedAt.map({ abs($0 - position) > 5 }) ?? true {
                resumedAt = position
                Task { await start(at: position) }
            } else {
                player.pause()
                error = L("A lejátszás megszakadt.")
            }
            return
        }
        ended = true
        isPlaying = false
        report(ended: true)
        // A party stays together at the end: the host starts the next episode or leaves.
        if party != nil { return }
        if next != nil, !nextDismissed {
            if autoplayNext { playNext() }
        } else {
            close()
        }
    }

    private func updateCountdown() {
        if showNext, autoplayNext, party == nil, countdown == nil {
            nextCountdown = 10
            countdown = Task { [weak self] in
                while let self, let n = self.nextCountdown, n > 0 {
                    try? await Task.sleep(for: .seconds(1))
                    if Task.isCancelled { return }
                    self.nextCountdown = n - 1
                }
                guard !Task.isCancelled else { return }
                self?.playNext()
            }
        } else if !showNext, let c = countdown {
            c.cancel()
            countdown = nil
            nextCountdown = nil
        }
    }

    /// Position report: every 10 s while playing, on pause, seek and close.
    private func report(ended: Bool = false) {
        lastReport = .now
        let p = position, id = item.id, c = client
        guard p >= 1 || ended else { return }
        #if os(iOS)
        if !closed, !ended { Handoff.watching(id, title: title, at: p, party: party?.code) }
        #endif
        Task {
            await ProgressQueue.report(id, position: p, ended: ended, client: c)
            #if os(iOS)
            if ended { DownloadStore.shared.watched(id) }
            #endif
        }
    }

    // MARK: - lock screen and Control Center

    private func updateNowPlaying() {
        var info: [String: Any] = [
            MPMediaItemPropertyTitle: item.title,
            MPMediaItemPropertyPlaybackDuration: duration,
            MPNowPlayingInfoPropertyElapsedPlaybackTime: position,
            MPNowPlayingInfoPropertyPlaybackRate: player.rate,
            MPNowPlayingInfoPropertyMediaType: MPNowPlayingInfoMediaType.video.rawValue,
        ]
        if item.kind == .episode {
            info[MPMediaItemPropertyArtist] = [item.seriesTitle, item.episodeLabel].compactMap { $0 }.joined(separator: " · ")
        }
        if let nowPlayingArtwork, nowPlayingArtwork.id == item.id { info[MPMediaItemPropertyArtwork] = nowPlayingArtwork.art }
        MPNowPlayingInfoCenter.default().nowPlayingInfo = info
        refreshPartyActivity()
    }

    /// The room on the lock screen and in the Dynamic Island: title, people, where it is.
    func refreshPartyActivity() {
        #if os(iOS)
        guard let party, !closed else { return }
        LiveActivities.partyChanged(code: party.code, .init(
            title: title, members: max(1, party.members.count), playing: player.rate > 0,
            position: position, duration: duration, at: .now))
        #endif
    }

    /// Still or backdrop of the title on the lock screen and in Control Center.
    private func loadNowPlayingArtwork() {
        let id = item.id, kind: ImageKind = item.kind == .episode ? .thumb : .backdrop, c = client
        guard nowPlayingArtwork?.id != id else { return }
        Task {
            guard let img = await ArtworkCache.shared.load(client: c, server: "nowplaying", id: id, kind: kind),
                  !closed, item.id == id else { return }
            nowPlayingArtwork = (id, Self.artwork(img))
            updateNowPlaying()
        }
    }

    /// MediaPlayer asks for the image on its own queue, so the handler must not be main-actor isolated.
    private nonisolated static func artwork(_ image: UIImage) -> MPMediaItemArtwork {
        MPMediaItemArtwork(boundsSize: image.size) { @Sendable _ in image }
    }

    private func setUpRemoteCommands() {
        let center = MPRemoteCommandCenter.shared()
        center.playCommand.addTarget { [weak self] _ in
            MainActor.assumeIsolated { self?.play() }
            return .success
        }
        center.pauseCommand.addTarget { [weak self] _ in
            MainActor.assumeIsolated { self?.pause() }
            return .success
        }
        center.togglePlayPauseCommand.addTarget { [weak self] _ in
            MainActor.assumeIsolated { self?.togglePlay() }
            return .success
        }
        center.skipForwardCommand.preferredIntervals = [10]
        center.skipForwardCommand.addTarget { [weak self] _ in
            MainActor.assumeIsolated { self?.seek(by: 10) }
            return .success
        }
        center.skipBackwardCommand.preferredIntervals = [10]
        center.skipBackwardCommand.addTarget { [weak self] _ in
            MainActor.assumeIsolated { self?.seek(by: -10) }
            return .success
        }
        center.changePlaybackPositionCommand.addTarget { [weak self] event in
            guard let e = event as? MPChangePlaybackPositionCommandEvent else { return .commandFailed }
            let t = e.positionTime
            MainActor.assumeIsolated { self?.seek(to: t) }
            return .success
        }
    }

    private func tearDownRemoteCommands() {
        let center = MPRemoteCommandCenter.shared()
        for cmd in [center.playCommand, center.pauseCommand, center.togglePlayPauseCommand,
                    center.skipForwardCommand, center.skipBackwardCommand, center.changePlaybackPositionCommand] {
            cmd.removeTarget(nil)
        }
        MPNowPlayingInfoCenter.default().nowPlayingInfo = nil
    }
}

// MARK: - picture in picture

extension PlayerModel: AVPictureInPictureControllerDelegate {
    nonisolated func pictureInPictureControllerWillStartPictureInPicture(_ controller: AVPictureInPictureController) {
        MainActor.assumeIsolated {
            pipActive = true
            onPiPStart?()
        }
    }

    nonisolated func pictureInPictureController(_ controller: AVPictureInPictureController,
                                                restoreUserInterfaceForPictureInPictureStopWithCompletionHandler completionHandler: @escaping (Bool) -> Void) {
        MainActor.assumeIsolated {
            restoring = true
            onPiPRestore?()
        }
        completionHandler(true)
    }

    nonisolated func pictureInPictureControllerDidStopPictureInPicture(_ controller: AVPictureInPictureController) {
        MainActor.assumeIsolated {
            pipActive = false
            // Closed from the PiP window (not brought back): playback ends.
            if !presented && !restoring { close() }
            restoring = false
        }
    }
}
