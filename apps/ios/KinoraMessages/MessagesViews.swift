import KinoraKit
import Messages
import SwiftUI

struct MessagesRoot: View {
    @Bindable var model: MessagesModel

    var body: some View {
        ZStack {
            Theme.black.ignoresSafeArea()
            if let received = model.received, model.style != .compact {
                ReceivedView(model: model, received: received)
            } else if let pick = model.pick, model.style != .compact {
                PickView(model: model, pick: pick)
            } else if let sent = model.sent {
                SentView(model: model, invite: sent)
            } else {
                ComposeView(model: model)
            }
        }
        .tint(Theme.amber)
        .environment(\.colorScheme, .dark)
        .animation(.snappy(duration: 0.25), value: model.tab)
    }
}

// MARK: - compose (compact and expanded)

struct ComposeView: View {
    @Bindable var model: MessagesModel

    var body: some View {
        VStack(spacing: 0) {
            TabStrip(tab: $model.tab)
                .padding(.horizontal, 16)
                .padding(.top, 10)
                .padding(.bottom, 8)
            Group {
                switch model.tab {
                case .party: PartyTab(model: model)
                case .recommend: RecommendTab(model: model)
                case .stickers: StickersGrid()
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }
}

/// Three pills, the live one lit amber.
struct TabStrip: View {
    @Binding var tab: MessagesTab

    var body: some View {
        HStack(spacing: 6) {
            item(.party, "Watch party", "person.2.fill")
            item(.recommend, L("Ajánlás"), "hand.point.right.fill")
            item(.stickers, L("Matricák"), "face.smiling.inverse")
        }
        .padding(4)
        .background(Theme.lacquer, in: .capsule)
        .overlay(Capsule().stroke(Theme.rule))
    }

    private func item(_ t: MessagesTab, _ title: String, _ symbol: String) -> some View {
        let on = tab == t
        return Button {
            tab = t
            Haptics.tap()
        } label: {
            Label(title, systemImage: symbol)
                .font(.subheadline.weight(.semibold))
                .labelStyle(.titleAndIcon)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
                .frame(maxWidth: .infinity, minHeight: 34)
                .foregroundStyle(on ? Theme.amberInk : Theme.plaster)
                .background(on ? Theme.amber : .clear, in: .capsule)
                .contentShape(.capsule)
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(on ? .isSelected : [])
    }
}

struct PartyTab: View {
    @Bindable var model: MessagesModel

    var body: some View {
        if model.phase == .signedOut {
            SignedOutView(model: model)
        } else if model.phase == .loading {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if model.phase == .offline {
            StatusView(symbol: "wifi.exclamationmark", title: L("Nem érem el a szervert."),
                       text: L("A meghívóhoz a Kinora-szerverednek elérhetőnek kell lennie."),
                       button: L("Újra"), action: model.reload)
        } else if model.continueItems.isEmpty && model.recentItems.isEmpty {
            StatusView(symbol: "film.stack", title: L("Még nincs mit nézni"),
                       text: L("Ha a könyvtáradban lesznek címek, itt választhatsz közülük."))
        } else {
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    Text(L("Válassz címet, és hívd meg a beszélgetés tagjait közös nézésre."))
                        .font(.footnote)
                        .foregroundStyle(Theme.muted)
                        .padding(.horizontal, 16)
                    if !model.continueItems.isEmpty {
                        Shelf(title: L("Folytatás")) {
                            ForEach(model.continueItems, id: \.id) { item in
                                WideCard(model: model, item: item) { model.choose(item, mode: .party) }
                            }
                        }
                    }
                    if !model.recentItems.isEmpty {
                        Shelf(title: L("Új a könyvtárban")) {
                            ForEach(model.recentItems, id: \.id) { item in
                                PosterCard(model: model, item: item, width: 92) { model.choose(item, mode: .party) }
                            }
                        }
                    }
                }
                .padding(.bottom, 16)
            }
            .scrollIndicators(.hidden)
        }
    }
}

struct RecommendTab: View {
    @Bindable var model: MessagesModel
    @FocusState private var focused: Bool

    private let columns = [GridItem(.adaptive(minimum: 96, maximum: 140), spacing: 12)]

    var body: some View {
        VStack(spacing: 10) {
            HStack(spacing: 8) {
                Image(systemName: "magnifyingglass").foregroundStyle(Theme.muted)
                TextField(L("Keresés a könyvtárban"), text: $model.query)
                    .focused($focused)
                    .submitLabel(.search)
                    .autocorrectionDisabled()
                if model.searching { ProgressView().controlSize(.small) }
                if !model.query.isEmpty {
                    Button { model.query = "" } label: { Image(systemName: "xmark.circle.fill").foregroundStyle(Theme.muted) }
                        .accessibilityLabel(L("Törlés"))
                }
            }
            .padding(.horizontal, 12)
            .frame(minHeight: 42)
            .background(Theme.lacquer, in: .rect(cornerRadius: 10))
            .overlay(RoundedRectangle(cornerRadius: 10).stroke(focused ? Theme.amber : Theme.rule))
            .padding(.horizontal, 16)
            .onChange(of: focused) { _, on in if on { model.actions?.expand() } }

            let list = model.query.trimmingCharacters(in: .whitespaces).count >= 2 ? model.results : model.recentItems
            if model.phase == .signedOut {
                SignedOutView(model: model)
            } else if model.phase == .offline {
                StatusView(symbol: "wifi.exclamationmark", title: L("Nem érem el a szervert."),
                           text: nil, button: L("Újra"), action: model.reload)
            } else if list.isEmpty && !model.searching && !model.query.isEmpty {
                StatusView(symbol: "magnifyingglass", title: L("Nincs találat"), text: nil)
            } else {
                ScrollView {
                    LazyVGrid(columns: columns, spacing: 14) {
                        ForEach(list, id: \.id) { item in
                            PosterCard(model: model, item: item, width: nil) {
                                focused = false
                                model.choose(item, mode: .recommend)
                            }
                        }
                    }
                    .padding(.horizontal, 16)
                    .padding(.bottom, 16)
                }
                .scrollIndicators(.hidden)
                .scrollDismissesKeyboard(.immediately)
            }
        }
    }
}

struct SignedOutView: View {
    var model: MessagesModel

    var body: some View {
        StatusView(symbol: "person.crop.circle.badge.exclamationmark",
                   title: L("Jelentkezz be a Kinorába"),
                   text: L("A meghívókhoz és az ajánlásokhoz be kell lépned az appban. A matricák így is működnek."),
                   button: L("Kinora megnyitása"), action: model.openApp,
                   secondary: L("Matricák")) { model.tab = .stickers }
    }
}

struct StickersGrid: View {
    @State private var stickers: [(StickerDesign, MSSticker)] = []
    private let columns = [GridItem(.adaptive(minimum: 96, maximum: 130), spacing: 8)]

    var body: some View {
        ScrollView {
            LazyVGrid(columns: columns, spacing: 8) {
                ForEach(stickers, id: \.0.id) { design, sticker in
                    StickerCell(sticker: sticker)
                        .aspectRatio(1, contentMode: .fit)
                        .accessibilityLabel(design.key.isEmpty ? "Kinora" : design.text)
                }
            }
            .padding(.horizontal, 12)
            .padding(.bottom, 16)
            Text(L("Koppints egy matricára, vagy tartsd lenyomva, és húzd rá egy üzenetre."))
                .font(.footnote)
                .foregroundStyle(Theme.muted)
                .multilineTextAlignment(.center)
                .padding(.horizontal, 24)
                .padding(.bottom, 20)
        }
        .scrollIndicators(.hidden)
        .task {
            guard stickers.isEmpty else { return }
            stickers = StickerDesign.all.compactMap { d in StickerStore.sticker(d).map { (d, $0) } }
        }
    }
}

// MARK: - pieces

struct Shelf<Content: View>: View {
    var title: String
    @ViewBuilder var content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title.uppercased())
                .font(Theme.display(17, weight: .bold))
                .tracking(1.5)
                .foregroundStyle(Theme.plaster)
                .padding(.horizontal, 16)
            ScrollView(.horizontal) {
                LazyHStack(alignment: .top, spacing: 12) { content }
                    .padding(.horizontal, 16)
            }
            .scrollIndicators(.hidden)
        }
    }
}

/// A poster loaded from the server, with a plain panel while it loads. A title without art
/// gets its name on the panel, like in the app.
struct Artwork: View {
    var model: MessagesModel
    var item: Item
    var kind: ImageKind
    var showsTitle = true
    var onLoad: ((Bool) -> Void)? = nil
    @State private var image: UIImage?
    @State private var missing = false

    var body: some View {
        ZStack {
            LinearGradient(colors: [Theme.raised, Theme.panel], startPoint: .top, endPoint: .bottom)
            if let image {
                Image(uiImage: image).resizable().scaledToFill().transition(.opacity)
            } else if missing && showsTitle {
                Text(MessagesModel.title(of: item))
                    .font(Theme.display(kind == .poster ? 17 : 22))
                    .foregroundStyle(Theme.plaster)
                    .multilineTextAlignment(.center)
                    .lineLimit(4)
                    .minimumScaleFactor(0.6)
                    .padding(10)
            } else if !missing {
                Image(systemName: "film").font(.title2).foregroundStyle(Theme.rule)
            }
        }
        .clipped()
        .task(id: item.id) {
            var img = await model.image(item, kind)
            if img == nil, kind != .poster { img = await model.image(item, .poster) }
            withAnimation(.easeOut(duration: 0.2)) {
                image = img
                missing = img == nil
            }
            onLoad?(img != nil)
        }
    }
}

struct PosterCard: View {
    var model: MessagesModel
    var item: Item
    var width: CGFloat?
    var action: () -> Void

    var body: some View {
        Button(action: action) {
            VStack(alignment: .leading, spacing: 6) {
                Artwork(model: model, item: item, kind: .poster)
                    .aspectRatio(2 / 3, contentMode: .fit)
                    .frame(width: width)
                    .clipShape(.rect(cornerRadius: 8))
                    .overlay(RoundedRectangle(cornerRadius: 8).stroke(Theme.rule))
                Text(MessagesModel.title(of: item))
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(Theme.plasterBright)
                    .lineLimit(1)
                    .frame(width: width, alignment: .leading)
            }
        }
        .buttonStyle(PressStyle())
        .accessibilityLabel(MessagesModel.title(of: item))
    }
}

struct WideCard: View {
    var model: MessagesModel
    var item: Item
    var action: () -> Void

    var body: some View {
        Button(action: action) {
            VStack(alignment: .leading, spacing: 6) {
                ZStack(alignment: .bottomLeading) {
                    Artwork(model: model, item: item, kind: item.kind == .episode ? .thumb : .backdrop)
                        .frame(width: 200, height: 112)
                    if let p = item.progress, p > 0 {
                        GeometryReader { g in
                            ZStack(alignment: .leading) {
                                Rectangle().fill(Theme.black.opacity(0.6))
                                Rectangle().fill(Theme.amber).frame(width: g.size.width * min(p, 1))
                            }
                        }
                        .frame(height: 4)
                    }
                }
                .clipShape(.rect(cornerRadius: 8))
                .overlay(RoundedRectangle(cornerRadius: 8).stroke(Theme.rule))
                Text(MessagesModel.title(of: item))
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(Theme.plasterBright)
                    .lineLimit(1)
                let sub = MessagesModel.subtitle(of: item)
                if !sub.isEmpty {
                    Text(sub).font(.caption2).foregroundStyle(Theme.muted).lineLimit(1)
                }
            }
            .frame(width: 200, alignment: .leading)
        }
        .buttonStyle(PressStyle())
        .accessibilityLabel(MessagesModel.title(of: item))
    }
}

struct PressStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed ? 0.96 : 1)
            .animation(.snappy(duration: 0.15), value: configuration.isPressed)
    }
}

struct StatusView: View {
    var symbol: String
    var title: String
    var text: String?
    var button: String?
    var action: (() -> Void)?
    var secondary: String?
    var secondaryAction: (() -> Void)?

    init(symbol: String, title: String, text: String?, button: String? = nil, action: (() -> Void)? = nil,
         secondary: String? = nil, secondaryAction: (() -> Void)? = nil) {
        self.symbol = symbol; self.title = title; self.text = text; self.button = button; self.action = action
        self.secondary = secondary; self.secondaryAction = secondaryAction
    }

    var body: some View {
        VStack(spacing: 10) {
            Image(systemName: symbol).font(.system(size: 30, weight: .semibold)).foregroundStyle(Theme.amber)
            Text(title).font(.headline).foregroundStyle(Theme.plasterBright).multilineTextAlignment(.center)
            if let text {
                Text(text).font(.footnote).foregroundStyle(Theme.muted).multilineTextAlignment(.center)
            }
            HStack(spacing: 10) {
                if let button, let action {
                    Button(button, action: action).buttonStyle(PrimaryButtonStyle()).frame(maxWidth: 220)
                }
                if let secondary, let secondaryAction {
                    Button(secondary, action: secondaryAction).buttonStyle(SecondaryButtonStyle()).frame(maxWidth: 160)
                }
            }
            .padding(.top, 4)
        }
        .padding(.horizontal, 28)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

// MARK: - confirm (expanded)

struct PickView: View {
    @Bindable var model: MessagesModel
    var pick: Pick
    @State private var posterMissing = false

    private var party: Bool { pick.mode == .party }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                ZStack(alignment: .bottomLeading) {
                    Artwork(model: model, item: pick.item, kind: pick.item.kind == .episode ? .thumb : .backdrop, showsTitle: false)
                        .frame(height: 210)
                        .frame(maxWidth: .infinity)
                    LinearGradient(colors: [.clear, Theme.black], startPoint: .center, endPoint: .bottom)
                    HStack(alignment: .bottom, spacing: 14) {
                        if !posterMissing {
                            Artwork(model: model, item: pick.item, kind: .poster, onLoad: { found in
                                if !found { withAnimation { posterMissing = true } }
                            })
                            .frame(width: 84, height: 126)
                            .clipShape(.rect(cornerRadius: 8))
                            .overlay(RoundedRectangle(cornerRadius: 8).stroke(Theme.plasterBright.opacity(0.2)))
                            .shadow(color: .black.opacity(0.5), radius: 10, y: 4)
                        }
                        VStack(alignment: .leading, spacing: 6) {
                            MarqueeLabel(text: party ? "Watch party" : L("Ajánlás"), size: 13)
                            Text(MessagesModel.title(of: pick.item))
                                .font(Theme.display(30))
                                .foregroundStyle(Theme.plasterBright)
                                .lineLimit(2)
                                .minimumScaleFactor(0.7)
                            let sub = MessagesModel.subtitle(of: pick.item)
                            if !sub.isEmpty { Text(sub).font(.subheadline).foregroundStyle(Theme.plaster).lineLimit(1) }
                        }
                    }
                    .padding(16)
                }
                .clipShape(.rect(cornerRadius: 14))
                .overlay(RoundedRectangle(cornerRadius: 14).stroke(Theme.rule))

                if let overview = pick.item.overview, !overview.isEmpty {
                    Text(overview).font(.subheadline).foregroundStyle(Theme.plaster).lineLimit(4)
                }

                if party {
                    VStack(alignment: .leading, spacing: 0) {
                        Toggle(isOn: $model.everyone) {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(L("Mindenki irányíthat")).foregroundStyle(Theme.plasterBright)
                                Text(L("Bárki megállíthatja és tekerheti a filmet, nem csak te."))
                                    .font(.caption).foregroundStyle(Theme.muted)
                            }
                        }
                        .tint(Theme.amber)
                        .padding(14)
                    }
                    .background(Theme.panel, in: .rect(cornerRadius: 12))
                    .overlay(RoundedRectangle(cornerRadius: 12).stroke(Theme.rule))

                    Label(L("Elküldés után nyisd meg a Kinorában: elsőként belépve te leszel a házigazda. A szoba 10 percig vár rád."),
                          systemImage: "info.circle")
                        .font(.footnote)
                        .foregroundStyle(Theme.muted)
                }

                if let error = model.error {
                    Label(error, systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(Theme.danger)
                }

                VStack(spacing: 10) {
                    Button {
                        Task { await model.send() }
                    } label: {
                        HStack(spacing: 8) {
                            if model.sending { ProgressView().tint(Theme.amberInk) }
                            Text(party ? L("Meghívó küldése") : L("Ajánlás küldése"))
                        }
                    }
                    .buttonStyle(PrimaryButtonStyle())
                    .disabled(model.sending)
                    Button(L("Mégse"), action: model.cancelPick)
                        .buttonStyle(SecondaryButtonStyle())
                }
            }
            .padding(16)
        }
        .scrollIndicators(.hidden)
    }
}

// MARK: - after sending

struct SentView: View {
    var model: MessagesModel
    var invite: KinoraInvite

    var body: some View {
        VStack(spacing: 12) {
            HStack(spacing: 12) {
                Image(systemName: "checkmark.circle.fill").font(.system(size: 30)).foregroundStyle(Theme.amber)
                VStack(alignment: .leading, spacing: 2) {
                    Text(L("A meghívó az üzenetmezőben van")).font(.headline).foregroundStyle(Theme.plasterBright)
                    Text(L("Küldd el, aztán indítsd el a szobát: elsőként belépve te irányítod."))
                        .font(.footnote).foregroundStyle(Theme.muted)
                }
                Spacer(minLength: 0)
            }
            HStack(spacing: 10) {
                Text(invite.partyCode ?? "")
                    .font(Theme.display(30, weight: .bold))
                    .tracking(5)
                    .foregroundStyle(Theme.amber)
                    .padding(.horizontal, 16)
                    .frame(minHeight: 50)
                    .background(Theme.lacquer, in: .rect(cornerRadius: 10))
                    .overlay(RoundedRectangle(cornerRadius: 10).stroke(Theme.rule))
                    .accessibilityLabel(L("Kód: {0}", invite.partyCode ?? ""))
                Button {
                    model.startSent()
                } label: {
                    Label(L("Indítás a Kinorában"), systemImage: "play.fill")
                }
                .buttonStyle(PrimaryButtonStyle())
            }
            Button(L("Kész")) { model.sent = nil }
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(Theme.plaster)
        }
        .padding(16)
        .frame(maxHeight: .infinity)
    }
}

// MARK: - an opened bubble

struct ReceivedView: View {
    var model: MessagesModel
    var received: Received

    private var invite: KinoraInvite { received.invite }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                if let image = received.image {
                    Image(uiImage: image).resizable().scaledToFit()
                        .clipShape(.rect(cornerRadius: 14))
                        .overlay(RoundedRectangle(cornerRadius: 14).stroke(Theme.rule))
                }
                VStack(alignment: .leading, spacing: 6) {
                    Text(invite.title).font(Theme.display(32)).foregroundStyle(Theme.plasterBright)
                    if !invite.subtitle.isEmpty {
                        Text(invite.subtitle).font(.subheadline).foregroundStyle(Theme.plaster)
                    }
                    Text(headline).font(.subheadline).foregroundStyle(Theme.muted)
                }

                if let code = invite.partyCode {
                    HStack {
                        Text(L("Szobakód")).font(.subheadline).foregroundStyle(Theme.muted)
                        Spacer()
                        Text(code).font(Theme.display(26, weight: .bold)).tracking(4).foregroundStyle(Theme.amber)
                    }
                    .padding(14)
                    .background(Theme.panel, in: .rect(cornerRadius: 12))
                    .overlay(RoundedRectangle(cornerRadius: 12).stroke(Theme.rule))
                }

                if !received.known {
                    Label(L("Ez a meghívó egy másik Kinora-szerverre szól, amelyre ezen az eszközön nem vagy bejelentkezve. A böngészőben megnyithatod, ha van ott fiókod."),
                          systemImage: "server.rack")
                        .font(.footnote)
                        .foregroundStyle(Theme.muted)
                }

                VStack(spacing: 10) {
                    if received.known {
                        Button(action: model.openReceived) {
                            Label(primary, systemImage: invite.partyCode == nil ? "film" : "play.fill")
                        }
                        .buttonStyle(PrimaryButtonStyle())
                    }
                    if invite.webLink != nil {
                        Button(action: model.openReceivedInBrowser) {
                            Label(L("Megnyitás böngészőben"), systemImage: "safari")
                        }
                        .buttonStyle(received.known ? AnyButtonStyle(SecondaryButtonStyle()) : AnyButtonStyle(PrimaryButtonStyle()))
                    }
                }
            }
            .padding(16)
        }
        .scrollIndicators(.hidden)
    }

    private var headline: String {
        switch (invite.partyCode != nil, received.mine) {
        case (true, true): L("A te watch partyd. Indítsd el, a többiek csatlakoznak.")
        case (true, false): L("Meghívtak egy közös nézésre.")
        case (false, true): L("Ezt ajánlottad.")
        case (false, false): L("Ezt ajánlották neked.")
        }
    }

    private var primary: String {
        switch (invite.partyCode != nil, received.mine) {
        case (true, true): L("Indítás a Kinorában")
        case (true, false): L("Csatlakozás a Kinorában")
        default: L("Megnyitás a Kinorában")
        }
    }
}

struct AnyButtonStyle: ButtonStyle {
    private let make: (Configuration) -> AnyView
    init<S: ButtonStyle>(_ style: S) { make = { AnyView(style.makeBody(configuration: $0)) } }
    func makeBody(configuration: Configuration) -> some View { make(configuration) }
}
