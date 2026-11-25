import SwiftUI
import CoreImage
import KinoraKit

#if os(iOS)
/// The watch party panel beside the video: code and invite, members, chat, reactions.
struct PartyPanel: View {
    @Bindable var party: PartySession
    var leave: () -> Void
    @State private var message = ""
    @FocusState private var typing: Bool
    @Environment(\.verticalSizeClass) private var vSize
    /// The QR code is shown: by default while nobody else is in the room yet, unless the screen is short
    /// (a phone in landscape), where it would leave no room for the chat.
    @State private var qr: Bool?
    private var showQR: Bool { qr ?? (party.members.count <= 1 && vSize != .compact) }
    /// The chat line waiting for "report?" confirmation, and the person waiting for "block?".
    @State private var reporting: PartySession.Line?
    @State private var blocking: PartySession.Line?

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            header
            roomAndChat
            reactions
            composer
        }
        .padding(14)
        // Never wider than the screen, and never taller: the panel must not push the player off it.
        .frame(maxWidth: 320)
        .frame(maxHeight: .infinity)
        .background(.ultraThinMaterial.opacity(0.95), in: .rect(cornerRadius: 18))
        .environment(\.colorScheme, .dark)
        .padding(.vertical, 8)
        .padding(.trailing, 8)
    }

    private var header: some View {
        HStack(alignment: .firstTextBaseline) {
            VStack(alignment: .leading, spacing: 2) {
                Text(L("Watch party")).font(.caption).foregroundStyle(Theme.muted)
                Text(party.code)
                    .font(.title2.monospaced().weight(.bold))
                    .accessibilityLabel(L("Kód: ") + party.code.map(String.init).joined(separator: " "))
            }
            Spacer()
            SharePlayButton(party: party, title: party.player?.title ?? L("Watch party"))
            Button { Haptic.tap.play(); withAnimation(.easeOut(duration: 0.2)) { qr = !showQR } } label: {
                Image(systemName: "qrcode").frame(width: 36, height: 36).contentShape(.rect)
            }
            .foregroundStyle(showQR ? Theme.amber : .primary)
            .accessibilityLabel(showQR ? L("QR-kód elrejtése") : L("QR-kód megjelenítése"))
            ShareLink(item: inviteText, subject: Text(L("Watch party")), message: Text(inviteText)) {
                Image(systemName: "square.and.arrow.up").frame(width: 36, height: 36).contentShape(.rect)
            }
            .accessibilityLabel(L("Meghívó küldése"))
            Button { party.panelOpen = false } label: {
                Image(systemName: "xmark").frame(width: 36, height: 36).contentShape(.rect)
            }
            .accessibilityLabel(L("Panel bezárása"))
        }
        .buttonStyle(.plain)
    }

    private var qrCard: some View {
        HStack(alignment: .center, spacing: 12) {
            PartyQRCode(url: party.appLink, size: 112)
            Text(L("Mutasd meg a barátodnak: a Kinora appban a kezdőlap watch party gombjával, vagy a telefon kamerájával olvassa be."))
                .font(.caption)
                .foregroundStyle(Theme.muted)
                .fixedSize(horizontal: false, vertical: true)
        }
        .transition(.opacity.combined(with: .move(edge: .top)))
        .accessibilityElement(children: .combine)
        .accessibilityLabel(L("Meghívó QR-kód, kód: ") + party.code.map(String.init).joined(separator: " "))
    }

    /// Code, app link and web link: friends with the app tap the first link, others open the web player.
    private var inviteText: String {
        var lines = [L("Nézzük együtt a Kinorán! Kód: {0}", party.code), party.appLink.absoluteString]
        if let web = party.webLink { lines.append(web.absoluteString) }
        return lines.joined(separator: "\n")
    }

    private var members: some View {
        VStack(alignment: .leading, spacing: 6) {
            ForEach(party.members) { m in
                HStack(spacing: 8) {
                    Circle().fill(Color(partyHex: m.color)).frame(width: 9, height: 9)
                    Text(m.name).lineLimit(1)
                    if m.host { Text(L("házigazda")).font(.caption).foregroundStyle(Theme.muted) }
                    if m.buffering { Text(L("tölt…")).font(.caption).foregroundStyle(Theme.amber) }
                    if let from = m.from, party.blocked.contains(from) {
                        Text(L("letiltva")).font(.caption).foregroundStyle(Theme.muted)
                    }
                }
                .font(.subheadline)
                .accessibilityElement(children: .combine)
                .contextMenu {
                    if let from = m.from, !from.isEmpty, m.id != party.myId {
                        if party.blocked.contains(from) {
                            Button(L("Tiltás feloldása"), systemImage: "person.crop.circle.badge.checkmark") { party.unblock(from) }
                        } else {
                            Button(L("{0} letiltása", m.name), systemImage: "person.crop.circle.badge.xmark", role: .destructive) {
                                blocking = PartySession.Line(name: m.name, text: "", from: from)
                            }
                        }
                    }
                }
            }
            if party.isHost {
                Toggle(L("Mindenki irányíthat"), isOn: Binding(get: { party.everyone }, set: { Haptic.select.play(); party.setEveryone($0) }))
                    .font(.caption)
                    .tint(Theme.amber)
            } else if !party.canControl {
                Text(L("A házigazda irányítja a lejátszást.")).font(.caption).foregroundStyle(Theme.muted)
            }
        }
    }

    private var room: some View {
        VStack(alignment: .leading, spacing: 12) {
            if showQR { qrCard }
            if let s = party.status {
                Text(s).font(.caption).foregroundStyle(Theme.amber)
            }
            members
            Divider().overlay(Theme.rule)
        }
    }

    /// A scroll view that follows the newest chat line.
    private func followingChat<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        let content = content()
        return ScrollViewReader { proxy in
            ScrollView { content }
                .scrollBounceBehavior(.basedOnSize)
                .onChange(of: party.visibleLines.last?.id) { _, id in
                    if let id { withAnimation { proxy.scrollTo(id, anchor: .bottom) } }
                }
        }
    }

    private var chatLines: some View {
        LazyVStack(alignment: .leading, spacing: 6) {
            if party.visibleLines.contains(where: party.isOthers) {
                Text(L("Tarts nyomva egy üzenetet a jelentéshez vagy a küldő letiltásához."))
                    .font(.caption2).foregroundStyle(Theme.muted)
            }
            ForEach(party.visibleLines) { line in
                Group {
                    if let name = line.name {
                        Text("\(Text(name + ": ").bold().foregroundColor(Color(partyHex: line.color ?? ""))))\(line.text)")
                    } else {
                        Text(line.text).italic().foregroundStyle(Theme.muted)
                    }
                }
                .font(.footnote)
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(Rectangle())
                .contextMenu { lineActions(line) }
                .accessibilityActions { lineActions(line) }
                .id(line.id)
            }
        }
    }

    /// QR code, status, members and the chat. When they fit, the chat alone scrolls and takes the room
    /// that is left; on a short screen (a phone in landscape, a low window) all of it scrolls together.
    private var roomAndChat: some View {
        ViewThatFits(in: .vertical) {
            VStack(alignment: .leading, spacing: 12) {
                room
                followingChat { chatLines }
                    .frame(minHeight: 120, idealHeight: 120, maxHeight: .infinity)
            }
            followingChat {
                VStack(alignment: .leading, spacing: 12) {
                    room
                    chatLines
                }
            }
        }
        .frame(maxHeight: .infinity, alignment: .top)
        .confirmationDialog(L("Jelented ezt az üzenetet?"), isPresented: Binding(get: { reporting != nil }, set: { if !$0 { reporting = nil } }),
                            titleVisibility: .visible, presenting: reporting) { line in
            Button(L("Jelentés"), role: .destructive) { Haptic.select.play(); party.report(line) }
            Button(L("Jelentés és letiltás"), role: .destructive) {
                Haptic.select.play()
                party.report(line)
                party.block(line.from)
            }
            Button(L("Mégse"), role: .cancel) {}
        } message: { _ in
            Text(L("Az üzenetet és a küldőjét a szerver adminja kapja meg, aki eltávolíthatja a felhasználót."))
        }
        .confirmationDialog(L("{0} letiltása", blocking?.name ?? ""), isPresented: Binding(get: { blocking != nil }, set: { if !$0 { blocking = nil } }),
                            titleVisibility: .visible, presenting: blocking) { line in
            Button(L("Letiltás"), role: .destructive) { Haptic.select.play(); party.block(line.from) }
            Button(L("Mégse"), role: .cancel) {}
        } message: { _ in
            Text(L("Az üzenetei ezen a szerveren nem jelennek meg többé neked. A tagok listájában feloldhatod."))
        }
    }

    /// Report and block, for someone else's chat line (long press, or VoiceOver actions).
    @ViewBuilder private func lineActions(_ line: PartySession.Line) -> some View {
        if party.isOthers(line) {
            if line.lineId > 0 {
                Button(L("Jelentés"), systemImage: "exclamationmark.bubble") { reporting = line }
            }
            Button(L("{0} letiltása", line.name ?? ""), systemImage: "person.crop.circle.badge.xmark", role: .destructive) { blocking = line }
        }
    }

    private var reactions: some View {
        HStack(spacing: 2) {
            ForEach(party.reactions, id: \.self) { e in
                Button(e) { Haptic.tap.play(); party.react(e) }
                    .font(.title3)
                    .frame(maxWidth: .infinity, minHeight: 36)
                    .accessibilityLabel(L("Reakció: ") + e)
            }
        }
        .buttonStyle(.plain)
    }

    private var composer: some View {
        VStack(spacing: 10) {
            HStack {
                TextField(L("Üzenet"), text: $message)
                    .textFieldStyle(.roundedBorder)
                    .focused($typing)
                    .submitLabel(.send)
                    .onSubmit(send)
                Button(L("Küldés"), action: send).disabled(message.trimmingCharacters(in: .whitespaces).isEmpty)
            }
            Button(role: .destructive) { Haptic.warning.play(); leave() } label: {
                Label(L("Kilépés a watch partyból"), systemImage: "rectangle.portrait.and.arrow.right")
                    .font(.footnote.weight(.semibold))
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.bordered)
            .tint(Theme.danger)
        }
    }

    private func send() {
        guard !message.trimmingCharacters(in: .whitespaces).isEmpty else { return }
        Haptic.tap.play()
        party.chat(message)
        message = ""
        typing = true
    }
}
#endif

/// Reactions float up over the video, the sender's name under the emoji.
struct PartyReactionsLayer: View {
    var party: PartySession

    var body: some View {
        GeometryReader { geo in
            ForEach(party.floating) { f in
                FloatingReaction(item: f)
                    .position(x: geo.size.width * f.x, y: geo.size.height * 0.8)
            }
        }
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }
}

struct FloatingReaction: View {
    var item: PartySession.Floating
    var big = false
    @State private var up = false

    var body: some View {
        VStack(spacing: 2) {
            Text(item.emoji).font(.system(size: big ? 80 : 44))
            Text(item.name).font(big ? .callout : .caption2).foregroundStyle(.white.opacity(0.8))
        }
        .offset(y: up ? (big ? -420 : -260) : 0)
        .opacity(up ? 0 : 1)
        .onAppear { withAnimation(.easeOut(duration: 2.6)) { up = true } }
    }
}

/// A room's invite as a QR code: the app link, which the in-app scanner reads and the iPhone
/// camera opens in the app. Black on white with a quiet zone, so any reader takes it.
struct PartyQRCode: View {
    var url: URL
    var size: CGFloat

    var body: some View {
        Group {
            if let img = Self.image(url.absoluteString) {
                Image(decorative: img, scale: 1).interpolation(.none).resizable()
            } else {
                Color.white
            }
        }
        .frame(width: size, height: size)
        .padding(size * 0.08)
        .background(.white, in: .rect(cornerRadius: size * 0.08))
        .accessibilityHidden(true)
    }

    private static func image(_ text: String) -> CGImage? {
        guard let f = CIFilter(name: "CIQRCodeGenerator") else { return nil }
        f.setValue(Data(text.utf8), forKey: "inputMessage")
        f.setValue("M", forKey: "inputCorrectionLevel")
        guard let out = f.outputImage else { return nil }
        return CIContext().createCGImage(out, from: out.extent)
    }
}
