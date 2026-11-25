import SwiftUI
import KinoraKit

/// Two-column auth page: brand and explanation on the left, the controls on the right.
private struct TVAuthPage<Side: View, Content: View>: View {
    var title: String
    @ViewBuilder var side: Side
    @ViewBuilder var content: Content

    var body: some View {
        HStack(alignment: .top, spacing: 100) {
            VStack(alignment: .leading, spacing: 28) {
                BrandMark(size: 76)
                Text(title).font(Theme.display(64, weight: .bold)).foregroundStyle(Theme.plasterBright)
                side
            }
            .frame(width: 640, alignment: .leading)
            VStack(alignment: .leading, spacing: 26) { content }
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.horizontal, 100)
        .padding(.vertical, 80)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }
}

/// The server the auth screens are about.
private struct TVServerLine: View {
    @Environment(AppModel.self) private var model
    var body: some View {
        if let s = model.server {
            Label("\(s.name) · \(ServerAddress.host(of: s.url))", systemImage: "server.rack")
                .font(.callout).foregroundStyle(Theme.muted)
        }
    }
}

// MARK: - servers

struct TVServersView: View {
    @Environment(AppModel.self) private var model
    @State private var address = ""
    @State private var found: [DiscoveredServer] = []
    @State private var scanning = false
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        TVAuthPage(title: L("Válassz szervert")) {
            Text(L("A Kinora a saját médiaszerveredről játszik le. Ha a szerver ezen a hálózaton fut, lent megjelenik; különben írd be a címét."))
                .font(.title3).foregroundStyle(Theme.plaster)
            TVErrorText(message: error ?? model.notice)
        } content: {
            ScrollView {
                VStack(alignment: .leading, spacing: 22) {
                    if !model.servers.isEmpty {
                        TVHeading(text: L("Mentett szerverek"))
                        ForEach(model.servers) { s in
                            Button { open(s) } label: {
                                row(s.name, ServerAddress.host(of: s.url), icon: "server.rack")
                            }
                            .buttonStyle(TVRowStyle())
                            .contextMenu {
                                Button(L("Elfelejtés"), systemImage: "trash", role: .destructive) { model.forget(s) }
                            }
                        }
                        Text(L("Egy mentett szerver elfelejtéséhez tartsd lenyomva a sorát."))
                            .font(.callout).foregroundStyle(Theme.muted)
                    }
                    HStack {
                        TVHeading(text: L("Ezen a hálózaton"))
                        if scanning { ProgressView().padding(.leading, 12) }
                    }
                    ForEach(found) { f in
                        Button { connect(f.address) } label: {
                            row(f.name, ServerAddress.host(of: f.address) + " · " + f.version, icon: "wifi")
                        }
                        .buttonStyle(TVRowStyle())
                    }
                    if !scanning && found.isEmpty {
                        Text(L("Nem találtam szervert a helyi hálózaton.")).foregroundStyle(Theme.muted)
                    }
                    TVHeading(text: L("Cím megadása")).padding(.top, 10)
                    TextField(L("pl. 192.168.1.20:8096 vagy kinora.pelda.hu"), text: $address)
                        .textContentType(.URL)
                        .keyboardType(.URL)
                        .autocorrectionDisabled()
                        .textInputAutocapitalization(.never)
                        .onSubmit { connect(address) }
                    TVButtonRow {
                        Button { connect(address) } label: { Label(L("Csatlakozás"), systemImage: "arrow.right") }
                            .buttonStyle(TVPillStyle(prominent: true))
                            .disabled(address.trimmingCharacters(in: .whitespaces).isEmpty || busy)
                        Button { scan() } label: { Label(L("Keresés újra"), systemImage: "arrow.clockwise") }
                            .buttonStyle(TVPillStyle())
                            .disabled(scanning)
                        if busy { ProgressView() }
                    }
                }
                .padding(30)
            }
            .scrollClipDisabled()
        }
        .task { scan() }
        .onDisappear { model.notice = nil }
    }

    private func row(_ title: String, _ subtitle: String, icon: String) -> some View {
        HStack(spacing: 24) {
            Image(systemName: icon).font(.title2).frame(width: 50)
            VStack(alignment: .leading, spacing: 4) {
                Text(title).font(.headline)
                Text(subtitle).font(.callout).opacity(0.75)
            }
        }
    }

    private func connect(_ input: String) {
        let input = input.trimmingCharacters(in: .whitespaces)
        guard !busy, !input.isEmpty else { return }
        run { try await model.connect(to: input) }
    }

    private func open(_ s: SavedServer) {
        guard !busy else { return }
        run { try await model.open(s) }
    }

    private func run(_ work: @escaping () async throws -> Void) {
        error = nil
        model.notice = nil
        busy = true
        Task {
            defer { busy = false }
            do { try await work() } catch { self.error = error.localizedDescription }
        }
    }

    private func scan() {
        guard !scanning else { return }
        scanning = true
        found = []
        Task {
            await Discovery.scanLocalNetwork { server in
                await MainActor.run {
                    if !found.contains(where: { $0.id == server.id }) { found.append(server) }
                }
            }
            scanning = false
        }
    }
}

// MARK: - login

struct TVLoginView: View {
    @Environment(AppModel.self) private var model
    @State private var username = ""
    @State private var password = ""
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        TVAuthPage(title: L("Belépés")) {
            TVServerLine()
            Text(L("A legegyszerűbb a kóddal belépés: a tévén megjelenő kódot egy már bejelentkezett telefonon vagy gépen hagyod jóvá."))
                .font(.title3).foregroundStyle(Theme.plaster)
            TVErrorText(message: error ?? model.notice)
        } content: {
            if model.status?.quickconnectEnabled != false {
                Button { model.notice = nil; model.go(.quickConnect) } label: {
                    Label(L("Belépés kóddal"), systemImage: "key")
                }
                .buttonStyle(TVPillStyle(prominent: true))
                .padding(.bottom, 20)
            }
            TVHeading(text: L("Felhasználónév és jelszó"))
            TextField(L("Felhasználónév"), text: $username)
                .textContentType(.username)
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
            SecureField(L("Jelszó"), text: $password)
                .textContentType(.password)
                .onSubmit(login)
            TVButtonRow {
                Button(action: login) { Label(L("Belépés"), systemImage: "person.fill.checkmark") }
                    .buttonStyle(TVPillStyle())
                    .disabled(username.trimmingCharacters(in: .whitespaces).isEmpty || password.isEmpty || busy)
                Button(L("Másik szerver")) { model.showServers() }
                    .buttonStyle(TVPillStyle())
                if busy { ProgressView() }
            }
            if !model.accounts.isEmpty {
                TVHeading(text: L("Bejelentkezve ezen az eszközön")).padding(.top, 20)
                TVParkedAccounts()
            }
        }
        .onAppear { username = model.server?.username ?? "" }
    }

    private func login() {
        guard !busy, !username.trimmingCharacters(in: .whitespaces).isEmpty, !password.isEmpty else { return }
        error = nil
        model.notice = nil
        busy = true
        Task {
            defer { busy = false }
            do {
                try await model.login(username: username.trimmingCharacters(in: .whitespaces), password: password)
            } catch let e as KinoraError where e.status == 401 {
                error = L("Hibás felhasználónév vagy jelszó.")
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}

// MARK: - quick connect

struct TVQuickConnectView: View {
    @Environment(AppModel.self) private var model
    @State private var start: QuickConnectStart?
    @State private var message: String?
    @State private var failed = false
    @State private var attempt = 0

    var body: some View {
        TVAuthPage(title: L("Belépés kóddal")) {
            TVServerLine()
            if let server = model.server {
                Text(L("Egy már bejelentkezett telefonon vagy gépen nyisd meg a Kinorát ({0}), és írd be a kódot.", server.url))
                    .font(.title3).foregroundStyle(Theme.plaster)
            }
            Text(L("iPhone-on: Beállítások → Eszköz csatlakoztatása. A webes felületen: profil menü → Eszköz csatlakoztatása."))
                .font(.callout).foregroundStyle(Theme.muted)
        } content: {
            if let start {
                Text(spaced(start.code))
                    .font(Theme.display(150))
                    .monospacedDigit()
                    .foregroundStyle(Theme.amber)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 40)
                    .background(Theme.panel, in: .rect(cornerRadius: 24))
                    .accessibilityLabel(start.code.map(String.init).joined(separator: " "))
                HStack(spacing: 18) {
                    if !failed { ProgressView() }
                    Text(message ?? L("Várakozás a jóváhagyásra…"))
                        .foregroundStyle(failed ? Theme.danger : Theme.muted)
                }
            } else if failed {
                TVErrorText(message: message)
            } else {
                ProgressView().frame(maxWidth: .infinity).padding(60)
            }
            TVButtonRow {
                if failed {
                    Button { attempt += 1 } label: { Label(L("Új kód"), systemImage: "arrow.clockwise") }
                        .buttonStyle(TVPillStyle(prominent: true))
                }
                Button { model.go(.login) } label: { Label(L("Jelszóval"), systemImage: "person") }
                    .buttonStyle(TVPillStyle())
                Button(L("Másik szerver")) { model.showServers() }
                    .buttonStyle(TVPillStyle())
            }
            .padding(.top, 20)
        }
        .task(id: attempt) { await run() }
    }

    private func spaced(_ code: String) -> String {
        code.count == 6 ? code.prefix(3) + " " + code.suffix(3) : code
    }

    private func run() async {
        start = nil
        failed = false
        message = nil
        do {
            let s = try await model.startQuickConnect()
            start = s
            try await Task.sleep(for: .seconds(2))
            while !Task.isCancelled {
                let st = try await model.pollQuickConnect(s)
                switch st.status {
                case .approved: return
                case .denied: failed = true; message = L("A csatlakozást elutasították."); return
                case .expired: failed = true; message = L("A kód lejárt."); return
                case .pending: break
                }
                try await Task.sleep(for: .seconds(max(1, s.pollInterval ?? 2)))
            }
        } catch is CancellationError {
        } catch {
            failed = true
            message = error.localizedDescription
        }
    }
}

// MARK: - accounts

/// The other accounts of this server signed in on this Apple TV: one click brings one back,
/// without a password.
struct TVParkedAccounts: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        if !model.accounts.isEmpty {
            TVButtonRow {
                ForEach(model.accounts) { a in
                    Button { Task { await model.switchAccount(to: a) } } label: {
                        Label(a.username, systemImage: "person.crop.circle")
                    }
                    .buttonStyle(TVPillStyle())
                    .accessibilityLabel(L("Fiókváltás"))
                    .accessibilityValue(a.username)
                    .contextMenu {
                        Button(L("Elfelejtés"), role: .destructive) { Task { await model.forgetAccount(a) } }
                    }
                }
            }
            .focusSection()
        }
    }
}

// MARK: - profiles

struct TVProfilesView: View {
    @Environment(AppModel.self) private var model
    @State private var pinFor: Profile?
    @State private var adding = false
    @State private var error: String?
    @State private var busy = false

    var body: some View {
        VStack(spacing: 70) {
            BrandMark(size: 60)
            Text(L("Ki néz?")).font(Theme.display(72, weight: .bold))
            if let profiles = model.me?.profiles {
                // Centered when they fit; a scrolling row when there are more than the screen is wide.
                ViewThatFits(in: .horizontal) {
                    profileRow(profiles)
                    ScrollView(.horizontal) {
                        profileRow(profiles).padding(.horizontal, 100)
                    }
                    .scrollClipDisabled()
                }
            } else {
                ProgressView()
            }
            TVErrorText(message: error)
            TVParkedAccounts()
            HStack(spacing: 40) {
                // Profiles belong to one account; another family member signs in with their own.
                // This one stays signed in, so it can come back without a password.
                Button(L("Másik felhasználó")) { Task { await model.addAccount() } }
                    .buttonStyle(TVPillStyle())
                Button(L("Másik szerver")) { model.showServers() }
                    .buttonStyle(TVPillStyle())
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .task { if model.me == nil { try? await model.guarded { try await model.loadMe() } } }
        .sheet(item: $pinFor) { TVPinSheet(profile: $0) }
        .sheet(isPresented: $adding) { TVAddProfileSheet() }
    }

    private func profileRow(_ profiles: [Profile]) -> some View {
        HStack(spacing: 60) {
            ForEach(profiles) { p in
                Button { pick(p) } label: { TVProfileFace(profile: p) }
                    // No .disabled while busy: on tvOS that would throw the focus elsewhere.
                    .buttonStyle(TVCardStyle())
            }
            if model.canAddProfile {
                Button { adding = true } label: { TVAddProfileFace() }
                    .buttonStyle(TVCardStyle())
            }
        }
        .padding(.vertical, 40)
    }

    private func pick(_ p: Profile) {
        guard !busy else { return }
        if p.hasPin { pinFor = p; return }
        error = nil
        busy = true
        Task {
            defer { busy = false }
            do { try await model.guarded { try await model.selectProfile(p, pin: nil) } }
            catch { self.error = error.localizedDescription }
        }
    }
}

struct TVProfileFace: View {
    @Environment(\.tvCardFocused) private var focused
    var profile: Profile
    var size: CGFloat = 220
    /// Off where the name is already written next to the face (Settings).
    var showsName = true

    var body: some View {
        VStack(spacing: 22) {
            ZStack(alignment: .bottomTrailing) {
                Text(profile.name.prefix(1).uppercased())
                    .font(Theme.display(size * 0.5))
                    .foregroundStyle(Theme.black)
                    .frame(width: size, height: size)
                    .background(Color(css: profile.color), in: .rect(cornerRadius: size * 0.18))
                    .overlay {
                        RoundedRectangle(cornerRadius: size * 0.18).stroke(Theme.plasterBright, lineWidth: focused ? 6 : 0)
                    }
                if profile.hasPin {
                    Image(systemName: "lock.fill")
                        .font(.callout)
                        .padding(12)
                        .background(Theme.black.opacity(0.75), in: .circle)
                        .padding(12)
                }
            }
            if showsName {
                Text(profile.name).font(.title3.weight(.semibold))
                    .foregroundStyle(focused ? Theme.plasterBright : Theme.plaster)
            }
            if showsName, profile.isKids {
                Text(L("Gyerek")).font(.callout).foregroundStyle(Theme.muted)
            }
        }
    }
}

private struct TVAddProfileFace: View {
    @Environment(\.tvCardFocused) private var focused
    var size: CGFloat = 220

    var body: some View {
        VStack(spacing: 22) {
            Image(systemName: "plus")
                .font(.system(size: size * 0.32, weight: .semibold))
                .foregroundStyle(focused ? Theme.plasterBright : Theme.plaster)
                .frame(width: size, height: size)
                .overlay {
                    RoundedRectangle(cornerRadius: size * 0.18)
                        .strokeBorder(focused ? Theme.plasterBright : Theme.muted,
                                      style: StrokeStyle(lineWidth: focused ? 6 : 3, dash: focused ? [] : [14, 10]))
                }
            Text(L("Profil hozzáadása")).font(.title3.weight(.semibold))
                .foregroundStyle(focused ? Theme.plasterBright : Theme.plaster)
        }
    }
}

/// A new profile: its name and whether it is a kids profile; the rest is under Settings once it is picked.
private struct TVAddProfileSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @State private var isKids = false
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        VStack(spacing: 40) {
            Text(L("Profil hozzáadása")).font(.title2.bold())
            TextField(L("Név"), text: $name)
                .textInputAutocapitalization(.words)
                .frame(width: 700)
                .onSubmit(submit)
            Button { isKids.toggle() } label: {
                Label(L("Gyerekprofil"), systemImage: isKids ? "checkmark.circle.fill" : "circle")
            }
            .buttonStyle(TVPillStyle())
            TVErrorText(message: error)
            TVButtonRow {
                Button(L("Hozzáadás"), action: submit)
                    .buttonStyle(TVPillStyle(prominent: true))
                    .disabled(trimmed.isEmpty || busy)
                Button(L("Mégse")) { dismiss() }
                    .buttonStyle(TVPillStyle())
            }
        }
        .padding(80)
    }

    private var trimmed: String { String(name.trimmingCharacters(in: .whitespacesAndNewlines).prefix(30)) }

    private func submit() {
        guard !trimmed.isEmpty, !busy else { return }
        busy = true
        error = nil
        Task {
            defer { busy = false }
            do {
                try await model.guarded { try await model.createProfile(name: trimmed, isKids: isKids) }
                dismiss()
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}

private struct TVPinSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var profile: Profile
    @State private var pin = ""
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        VStack(spacing: 40) {
            TVProfileFace(profile: profile, size: 160)
            Text(L("Add meg a PIN-kódot")).font(.title2.bold())
            SecureField("PIN", text: $pin)
                .keyboardType(.numberPad)
                .frame(width: 500)
                .onSubmit(submit)
            TVErrorText(message: error)
            TVButtonRow {
                Button(L("Belépés"), action: submit)
                    .buttonStyle(TVPillStyle(prominent: true))
                    .disabled(pin.isEmpty || busy)
                Button(L("Mégse")) { dismiss() }
                    .buttonStyle(TVPillStyle())
            }
        }
        .padding(80)
    }

    private func submit() {
        guard !pin.isEmpty, !busy else { return }
        busy = true
        error = nil
        Task {
            defer { busy = false }
            do {
                try await model.guarded { try await model.selectProfile(profile, pin: pin) }
                dismiss()
            } catch {
                self.error = error.localizedDescription
                pin = ""
            }
        }
    }
}
