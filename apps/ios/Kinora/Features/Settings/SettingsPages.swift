import SwiftUI
import KinoraKit

// The pages behind the settings hub. Profile settings (shared by every device of the profile)
// and device settings (only this phone) sit in separate, labelled sections on each page.

extension AppModel {
    /// Kids profiles and profile-locked devices (Quick Connect with a fixed profile) only look.
    var canEditSettings: Bool {
        (me?.profile?.isKids == false) && me?.profileLocked != true
    }

    /// A binding to a profile setting that saves on change; a failed save lands in `error`.
    func profileSetting<V: Equatable>(_ key: KeyPath<Profile, V?>, default fallback: V, error: Binding<String?>,
                                      _ update: @escaping (V) -> ProfileUpdate) -> Binding<V> {
        Binding {
            self.me?.profile?[keyPath: key] ?? fallback
        } set: { value in
            guard value != (self.me?.profile?[keyPath: key] ?? fallback) else { return }
            self.saveProfile(update(value), error: error)
        }
    }

    func saveProfile(_ update: ProfileUpdate, error: Binding<String?>) {
        Task {
            do {
                try await updateProfile(update)
                error.wrappedValue = nil
            } catch is CancellationError {
            } catch let e {
                error.wrappedValue = e.localizedDescription
                Haptic.error.play()
            }
        }
    }
}

extension SettingChoices {
    /// "Automatikus (idegen nyelvű hangnál)" → "Automatikus", for the value column of the hub.
    static func shortLabel<K: Equatable>(_ choices: [(K, String)], _ key: K) -> String {
        let label = choices.first { $0.0 == key }?.1 ?? ""
        return label.components(separatedBy: " (").first ?? label
    }
}

/// The footer of a profile section: whose setting it is, or why it can't be changed here.
private struct ProfileFooter: View {
    @Environment(AppModel.self) private var model
    var text: String

    var body: some View {
        if model.canEditSettings {
            Text(text)
        } else {
            Label(model.me?.profile?.isKids == true
                  ? L("Gyerekprofilon ez nem módosítható.")
                  : L("Ezen az eszközön a profil rögzített, ez nem módosítható."),
                  systemImage: "lock.fill")
        }
    }
}

private extension View {
    func settingsPage(_ title: String) -> some View {
        scrollContentBackground(.hidden)
            .background(Theme.black)
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbarBackground(Theme.black, for: .navigationBar)
    }
}

// MARK: - Lejátszás

struct PlaybackSettingsView: View {
    @Environment(AppModel.self) private var model
    @State private var error: String?
    @State private var cellularMax = AppSettings.cellularMaxBitrate
    @State private var backgroundAudio = AppSettings.backgroundAudio

    var body: some View {
        Form {
            if let error { Section { ErrorText(message: error) }.listRowBackground(Theme.panel) }
            Section {
                Picker(selection: model.profileSetting(\.maxBitrate, default: 0, error: $error) { ProfileUpdate(maxBitrate: $0) }) {
                    ForEach(SettingChoices.bitrates, id: \.0) { Text($0.1).tag($0.0) }
                } label: {
                    Label(L("Maximális minőség"), systemImage: "sparkles.tv")
                }
                Toggle(isOn: model.profileSetting(\.autoplayNext, default: true, error: $error) { ProfileUpdate(autoplayNext: $0) }) {
                    Label(L("Következő rész automatikusan"), systemImage: "forward.end.fill")
                }
                Picker(selection: model.profileSetting(\.skipIntro, default: "manual", error: $error) { ProfileUpdate(skipIntro: $0) }) {
                    ForEach(SettingChoices.skipModes, id: \.0) { Text($0.1).tag($0.0) }
                } label: {
                    Label(L("Intró átugrása"), systemImage: "arrow.uturn.forward")
                }
            } header: {
                Text(L("Profil"))
            } footer: {
                ProfileFooter(text: L("Minden eszközödön érvényes, ahol ezzel a profillal nézel."))
            }
            .disabled(!model.canEditSettings)
            .listRowBackground(Theme.panel)

            Section {
                Picker(selection: $cellularMax) {
                    Text(L("Nincs korlát")).tag(0)
                    ForEach(SettingChoices.bitrates.filter { $0.0 > 0 }, id: \.0) { Text($0.1).tag($0.0) }
                } label: {
                    Label(L("Minőség mobilneten"), systemImage: "antenna.radiowaves.left.and.right")
                }
                Toggle(isOn: $backgroundAudio) {
                    Label(L("Hang a háttérben"), systemImage: "speaker.wave.2.fill")
                }
            } header: {
                Text(L("Ezen az eszközön"))
            } footer: {
                Text(L("Mobilneten és hotspoton a lejátszás legfeljebb ekkora sávszélességet használ; a lejátszóban videónként felülírhatod. A háttérhang akkor szól tovább, ha kilépsz az appból vagy lezárod a telefont, és nem indul el a kép a képben."))
            }
            .listRowBackground(Theme.panel)
        }
        .settingsPage(L("Lejátszás"))
        .onChange(of: cellularMax) { _, v in AppSettings.cellularMaxBitrate = v }
        .onChange(of: backgroundAudio) { _, on in AppSettings.backgroundAudio = on }
    }
}

// MARK: - Feliratok és nyelvek

struct SubtitleSettingsView: View {
    @Environment(AppModel.self) private var model
    @State private var error: String?
    @State private var style = AppSettings.subtitleStyle
    @State private var subLangs = ""
    @State private var audioLangs = ""
    @FocusState private var focused: LangField?

    private enum LangField { case sub, audio }

    var body: some View {
        Form {
            if let error { Section { ErrorText(message: error) }.listRowBackground(Theme.panel) }
            Section {
                SubtitlePreview(style: style)
                    .listRowInsets(EdgeInsets())
                Picker(L("Méret"), selection: $style.size) {
                    ForEach(SubtitleStyle.Size.allCases, id: \.self) { Text(SubtitleStyle.label($0)).tag($0) }
                }
                Picker(L("Háttér"), selection: $style.background) {
                    ForEach(SubtitleStyle.Background.allCases, id: \.self) { Text(SubtitleStyle.label($0)).tag($0) }
                }
                .pickerStyle(.segmented)
                .listRowSeparator(.hidden, edges: .bottom)
                Picker(L("Szín"), selection: $style.color) {
                    ForEach(SubtitleStyle.Color.allCases, id: \.self) { Text(SubtitleStyle.label($0)).tag($0) }
                }
                .pickerStyle(.segmented)
            } header: {
                Text(L("Megjelenés ezen az eszközön"))
            }
            .listRowBackground(Theme.panel)

            Section {
                Picker(L("Feliratok"), selection: model.profileSetting(\.subMode, default: "auto", error: $error) { ProfileUpdate(subMode: $0) }) {
                    ForEach(SettingChoices.subModes, id: \.0) { Text($0.1).tag($0.0) }
                }
                LabeledContent(L("Feliratnyelvek")) {
                    TextField("hun, eng", text: $subLangs)
                        .multilineTextAlignment(.trailing)
                        .focused($focused, equals: .sub)
                        .submitLabel(.next)
                        .onSubmit { focused = .audio }
                }
                LabeledContent(L("Hangsávnyelvek")) {
                    TextField("hun, eng", text: $audioLangs)
                        .multilineTextAlignment(.trailing)
                        .focused($focused, equals: .audio)
                        .submitLabel(.done)
                        .onSubmit { focused = nil }
                }
            } header: {
                Text(L("Profil"))
            } footer: {
                ProfileFooter(text: L("Nyelvkódok vesszővel, fontossági sorrendben (például hun, eng). Ha a filmben van ilyen sáv, ezzel indul."))
            }
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .disabled(!model.canEditSettings)
            .listRowBackground(Theme.panel)
        }
        .settingsPage(L("Feliratok és nyelvek"))
        .toolbar {
            ToolbarItemGroup(placement: .keyboard) {
                Spacer()
                Button(L("Kész")) { focused = nil }
            }
        }
        .onAppear {
            subLangs = model.me?.profile?.subLangs ?? ""
            audioLangs = model.me?.profile?.audioLangs ?? ""
        }
        .onChange(of: focused) { old, _ in if old != nil { saveLanguages() } }
        .onChange(of: style) { _, s in AppSettings.subtitleStyle = s }
        .onDisappear(perform: saveLanguages)
    }

    private func saveLanguages() {
        guard model.canEditSettings, let p = model.me?.profile else { return }
        let sub = subLangs.trimmingCharacters(in: .whitespaces)
        let audio = audioLangs.trimmingCharacters(in: .whitespaces)
        var u = ProfileUpdate()
        if sub != (p.subLangs ?? "") { u.subLangs = sub }
        if audio != (p.audioLangs ?? "") { u.audioLangs = audio }
        if u != ProfileUpdate() { model.saveProfile(u, error: $error) }
    }
}

/// A 16:9 "frame" with the subtitle drawn the way the player will draw it.
private struct SubtitlePreview: View {
    var style: SubtitleStyle

    var body: some View {
        ZStack(alignment: .bottom) {
            LinearGradient(colors: [Color(hex: 0x5A4632), Color(hex: 0x2E3A44), Color(hex: 0x15181C)],
                           startPoint: .topLeading, endPoint: .bottomTrailing)
            Text(L("Így néz ki a felirat a filmben."))
                .font(.system(size: style.points * 0.8, weight: .semibold))
                .multilineTextAlignment(.center)
                .foregroundStyle(style.color == .yellow ? Color(hex: 0xFFE14D) : .white)
                .padding(.horizontal, style.background == .box ? 10 : 0)
                .padding(.vertical, style.background == .box ? 4 : 0)
                .background(style.background == .box ? Color.black.opacity(0.6) : .clear, in: .rect(cornerRadius: 4))
                .shadow(color: .black.opacity(style.background == .shadow ? 1 : 0), radius: 3)
                .padding(.horizontal, 16)
                .padding(.bottom, 14)
                .animation(.snappy, value: style)
        }
        .aspectRatio(16 / 9, contentMode: .fit)
        .frame(maxWidth: .infinity)
        .accessibilityElement()
        .accessibilityLabel(L("Felirat előnézete"))
    }
}

// MARK: - Letöltések

struct DownloadSettingsView: View {
    @State private var downloads = DownloadStore.shared
    @State private var wifiOnly = AppSettings.downloadsWifiOnly
    @State private var deleteWatched = AppSettings.deleteWatchedDownloads
    @State private var limit = AppSettings.downloadLimitGB

    var body: some View {
        Form {
            Section {
                VStack(alignment: .leading, spacing: 8) {
                    HStack(alignment: .firstTextBaseline) {
                        Text(bytes(downloads.usedBytes))
                            .font(Theme.display(28, weight: .bold))
                            .foregroundStyle(Theme.plasterBright)
                        Text(limit > 0 ? "/ \(limit) GB" : L("felhasználva"))
                            .foregroundStyle(Theme.muted)
                    }
                    if limit > 0 {
                        ProgressView(value: min(1, Double(downloads.usedBytes) / Double(limit * 1_000_000_000)))
                            .tint(ratio >= 0.9 ? Theme.danger : Theme.amber)
                    }
                }
                .padding(.vertical, 6)
                .accessibilityElement(children: .combine)
            } header: {
                Text(L("Tárhely"))
            }
            .listRowBackground(Theme.panel)

            Section {
                Picker(selection: $limit) {
                    ForEach([0, 5, 10, 20, 50, 100], id: \.self) { gb in
                        Text(gb == 0 ? L("Nincs") : "\(gb) GB").tag(gb)
                    }
                } label: {
                    Label(L("Tárhelykeret"), systemImage: "internaldrive")
                }
                Toggle(isOn: $wifiOnly) {
                    Label(L("Csak Wi-Fi-n"), systemImage: "wifi")
                }
                Toggle(isOn: $deleteWatched) {
                    Label(L("Megnézettek törlése"), systemImage: "trash")
                }
            } footer: {
                Text(L("A keret elérése után új letöltés nem indul. Csak Wi-Fi-n: mobilneten a letöltés megvárja a Wi-Fi-t. A megnézett rész a végignézés után törlődik az eszközről."))
            }
            .listRowBackground(Theme.panel)
        }
        .settingsPage(L("Letöltések"))
        .onChange(of: wifiOnly) { _, on in AppSettings.downloadsWifiOnly = on }
        .onChange(of: deleteWatched) { _, on in AppSettings.deleteWatchedDownloads = on }
        .onChange(of: limit) { _, gb in AppSettings.downloadLimitGB = gb }
    }

    private var ratio: Double {
        limit > 0 ? Double(downloads.usedBytes) / Double(limit * 1_000_000_000) : 0
    }

    private func bytes(_ n: Int64) -> String {
        n == 0 ? "0 GB" : ByteCountFormatter.string(fromByteCount: n, countStyle: .file)
    }
}

// MARK: - Értesítések

struct NotificationSettingsView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.openURL) private var openURL
    @State private var push = PushManager.shared
    @State private var sent = false

    var body: some View {
        Form {
            Section {
                Toggle(isOn: Binding(get: { push.wanted }, set: { on in
                    Task { on ? await push.enable() : await push.disable() }
                })) {
                    Label(L("Értesítések"), systemImage: "bell.badge")
                }
                .disabled(push.working)
                if push.wanted {
                    HStack {
                        Text(L("Ezen a szerveren"))
                        Spacer()
                        if push.working {
                            ProgressView()
                        } else {
                            Text(stateText).foregroundStyle(Theme.muted)
                        }
                    }
                }
            } footer: {
                Text(L("A szerver titkosítja az üzeneteket, és a nyelvedre fordítja őket; a közvetítő csak a titkosított változatot továbbítja."))
            }
            .listRowBackground(Theme.panel)

            if push.wanted, push.paired, let categories = push.status?.categories, !categories.isEmpty {
                Section {
                    ForEach(categories.filter { $0 != PushCategory.adminRequests || model.me?.user.isAdmin == true }, id: \.self) { cat in
                        Toggle(isOn: Binding(get: { !push.muted.contains(cat) }, set: { on in
                            Task { await push.setMuted(cat, !on) }
                        })) {
                            Label(Self.title(cat), systemImage: Self.icon(cat))
                        }
                    }
                    Toggle(isOn: Binding(get: { push.liveWanted && push.liveAvailable }, set: { on in
                        Task { await push.setLive(on) }
                    })) {
                        Label(L("Élő letöltés a zárolási képernyőn"), systemImage: "tray.and.arrow.down")
                    }
                    .disabled(!push.liveAvailable)
                } header: {
                    Text(L("Miről szóljon"))
                } footer: {
                    Text(push.liveAvailable
                         ? L("Amíg a szerver letölti, amit kértél, a zárolási képernyőn és a Dynamic Islandben látod, hol tart.")
                         : L("Az élő letöltéshez iOS 17.2 kell, és engedélyezni kell az élő tevékenységeket a Kinoránál."))
                }
                .listRowBackground(Theme.panel)
            }

            if push.authorization == .denied {
                Section {
                    Button(L("iOS-beállítások megnyitása")) {
                        if let url = URL(string: UIApplication.openNotificationSettingsURLString) { openURL(url) }
                    }
                } footer: {
                    Text(L("Az értesítések le vannak tiltva. Engedélyezd őket az iOS Beállítások appban a Kinoránál."))
                }
                .listRowBackground(Theme.panel)
            }

            if push.wanted, push.paired {
                Section {
                    Button {
                        Task {
                            await push.test()
                            sent = push.lastError == nil
                        }
                    } label: {
                        Label(L("Teszt értesítés"), systemImage: "paperplane")
                    }
                    .disabled(push.working)
                } footer: {
                    if sent { Text(L("Elküldve. Pár másodpercen belül meg kell érkeznie.")) }
                }
                .listRowBackground(Theme.panel)
            }

            if let error = push.lastError {
                Section {
                    Text(error).foregroundStyle(Theme.danger)
                }
                .listRowBackground(Theme.panel)
            }
        }
        .settingsPage(L("Értesítések"))
        .task {
            await push.refreshAuthorization()
            await push.syncCurrent()
        }
    }

    static func title(_ category: String) -> String {
        switch category {
        case PushCategory.requests: L("Kéréseim")
        case PushCategory.episodes: L("Új részek a követett sorozatokból")
        case PushCategory.adminRequests: L("Új kérések (rendszergazda)")
        default: category
        }
    }

    static func icon(_ category: String) -> String {
        switch category {
        case PushCategory.requests: "film.stack"
        case PushCategory.episodes: "sparkles.tv"
        case PushCategory.adminRequests: "tray.full"
        default: "bell"
        }
    }

    private var stateText: String {
        if push.status?.enabled == false { return L("A rendszergazda nem kapcsolta be") }
        return push.paired ? L("Bekapcsolva") : L("Nincs párosítva")
    }
}

// MARK: - Bejelentkezett eszközök

struct DevicesSettingsView: View {
    @Environment(AppModel.self) private var model
    @Binding var devices: [Device]?
    @State private var error: String?
    @State private var confirmRemoveOthers = false
    @State private var pendingRemoval: Device?

    var body: some View {
        Form {
            if let error, devices != nil { Section { ErrorText(message: error) }.listRowBackground(Theme.panel) }
            if let devices {
                Section {
                    ForEach(devices.sorted { ($0.current ? 0 : 1, -$0.lastSeen) < ($1.current ? 0 : 1, -$1.lastSeen) }) { d in
                        DeviceRow(device: d)
                            .swipeActions {
                                if !d.current && model.canEditSettings {
                                    Button(L("Kiléptetés"), role: .destructive) { remove(d) }
                                }
                            }
                            .contextMenu {
                                if !d.current && model.canEditSettings {
                                    Button(L("Kiléptetés"), systemImage: "rectangle.portrait.and.arrow.right", role: .destructive) { remove(d) }
                                }
                            }
                    }
                } footer: {
                    if model.canEditSettings, devices.count > 1 {
                        Text(L("Egy eszköz kiléptetéséhez húzd balra a sorát, vagy tartsd rajta az ujjad."))
                    }
                }
                .listRowBackground(Theme.panel)

                if model.canEditSettings && devices.count > 1 {
                    Section {
                        Button(role: .destructive) { confirmRemoveOthers = true } label: {
                            Text(L("Kiléptetés minden más eszközről"))
                                .foregroundStyle(Theme.danger)
                                .frame(maxWidth: .infinity)
                        }
                    }
                    .listRowBackground(Theme.panel)
                }
            } else if let error {
                Section {
                    ErrorText(message: error)
                    Button(L("Újra")) { Task { await load() } }
                }
                .listRowBackground(Theme.panel)
            } else {
                Section { ProgressView().frame(maxWidth: .infinity) }
                    .listRowBackground(Theme.panel)
            }
        }
        .settingsPage(L("Eszközök"))
        .task { await load() }
        .refreshable { await load() }
        .confirmationDialog(L("Kiléptetsz minden más eszközt?"), isPresented: $confirmRemoveOthers, titleVisibility: .visible) {
            Button(L("Kiléptetés mindenhonnan"), role: .destructive) { removeOthers() }
        } message: {
            Text(L("Ezen az eszközön bejelentkezve maradsz; a többin újra be kell lépni."))
        }
    }

    private func load() async {
        do {
            devices = try await model.api { try await $0.devices() }
            error = nil
        } catch is CancellationError {
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func remove(_ d: Device) {
        Task {
            do {
                try await model.api { try await $0.removeDevice(d.id) }
                withAnimation { devices?.removeAll { $0.id == d.id } }
                Haptic.success.play()
            } catch {
                self.error = error.localizedDescription
            }
        }
    }

    private func removeOthers() {
        Task {
            do {
                try await model.api { try await $0.removeOtherDevices() }
                await load()
                Haptic.success.play()
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}

private struct DeviceRow: View {
    var device: Device

    var body: some View {
        HStack(spacing: 14) {
            Image(systemName: device.isTV ? "tv" : device.platform == "web" ? "globe" : "iphone")
                .font(.title3)
                .foregroundStyle(device.current ? Theme.amber : Theme.muted)
                .frame(width: 30)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(device.name.isEmpty ? device.platformLabel : device.name)
                        .font(.body.weight(device.current ? .semibold : .regular))
                        .foregroundStyle(Theme.plasterBright)
                    if device.current {
                        Text(L("ez az eszköz"))
                            .font(.caption2.weight(.semibold))
                            .foregroundStyle(Theme.amber)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 1)
                            .background(Theme.amber.opacity(0.14), in: .capsule)
                    }
                }
                Text(detail).font(.caption).foregroundStyle(Theme.muted)
            }
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
    }

    private var detail: String {
        let seen = Int64(Date.now.timeIntervalSince1970) - device.lastSeen
        return [device.platformLabel, device.appVersion.isEmpty ? nil : "v" + device.appVersion,
                device.ip.isEmpty ? nil : device.ip, device.current ? nil : SettingChoices.ago(seen)]
            .compactMap { $0 }.joined(separator: " · ")
    }
}

// MARK: - Szerver

struct ServerSettingsView: View {
    @Environment(AppModel.self) private var model
    @State private var error: String?
    @State private var addingAddress = false
    @State private var newAddress = ""
    @State private var busy = false
    @State private var notice: String?

    var body: some View {
        Form {
            if let error { Section { ErrorText(message: error) }.listRowBackground(Theme.panel) }
            if let server = model.server {
                Section {
                    LabeledContent(L("Név"), value: model.me?.serverName ?? server.name)
                    if let version = model.me?.version {
                        LabeledContent(L("Verzió"), value: version)
                    }
                }
                .listRowBackground(Theme.panel)

                Section {
                    ForEach(server.addresses, id: \.self) { url in
                        HStack {
                            Text(url).font(.callout).lineLimit(2).textSelection(.enabled)
                                .foregroundStyle(Theme.plasterBright)
                            Spacer(minLength: 8)
                            if url == server.url {
                                Image(systemName: "checkmark.circle.fill")
                                    .foregroundStyle(Theme.amber)
                                    .accessibilityLabel(L("Most ezt használja"))
                            }
                        }
                    }
                    Button {
                        addingAddress = true
                    } label: {
                        if busy {
                            HStack(spacing: 10) { ProgressView(); Text(L("Cím ellenőrzése…")) }
                        } else {
                            Label(L("Külső cím hozzáadása"), systemImage: "plus.circle.fill")
                        }
                    }
                    .disabled(busy)
                } header: {
                    Text(L("Címek"))
                } footer: {
                    Text(notice ?? L("A Kinora mindig a pipával jelölt, elérhető címet használja. Adj meg egy külső címet, hogy otthonon kívül is működjön."))
                }
                .listRowBackground(Theme.panel)
            }
        }
        .settingsPage(L("Szerver"))
        .alert(L("Külső cím hozzáadása"), isPresented: $addingAddress) {
            TextField("https://kinora.pelda.hu", text: $newAddress)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .keyboardType(.URL)
            Button(L("Mégse"), role: .cancel) { newAddress = "" }
            Button(L("Hozzáadás")) { addAddress() }
        } message: {
            Text(L("Az a cím, amelyen a szerver otthonon kívül is elérhető."))
        }
    }

    private func addAddress() {
        let input = newAddress.trimmingCharacters(in: .whitespaces)
        newAddress = ""
        guard !input.isEmpty else { return }
        busy = true
        notice = nil
        Task {
            defer { busy = false }
            do {
                try await model.addAddress(input)
                notice = L("Cím hozzáadva.")
                error = nil
                Haptic.success.play()
            } catch {
                self.error = error.localizedDescription
                Haptic.error.play()
            }
        }
    }
}

// MARK: - Nyelv

/// The app's language. The languages come from the server (the same ones the web client has, including those
/// an admin added), and the device keeps every one it downloaded.
struct LanguageSettingsView: View {
    @Environment(AppModel.self) private var model

    static func summary(_ model: AppModel) -> String {
        let name = model.languages.first { $0.code == Language.shared.code }?.name ?? Language.shared.code
        return model.languageChoice.isEmpty ? L("Automatikus ({0})", name) : name
    }

    var body: some View {
        Form {
            Section {
                row(L("Automatikus"), detail: L("A készülék nyelve szerint"), code: "")
                ForEach(model.languages) { l in
                    row(l.name, detail: nil, code: l.code)
                }
            } footer: {
                Text(L("A nyelveket a szerver adja: ha az adminisztrátor a webes felületen új nyelvet ad hozzá, itt is megjelenik. A letöltött nyelvek ezen az eszközön maradnak, a szerver nélkül is."))
            }
            .listRowBackground(Theme.panel)
        }
        .settingsPage(L("Nyelv"))
    }

    private func row(_ title: String, detail: String?, code: String) -> some View {
        Button {
            Haptic.select.play()
            model.languageChoice = code
        } label: {
            HStack {
                VStack(alignment: .leading, spacing: 1) {
                    Text(title).foregroundStyle(Theme.plasterBright)
                    if let detail { Text(detail).font(.caption).foregroundStyle(Theme.muted) }
                }
                Spacer()
                if model.languageLoading && model.languageChoice == code && Language.shared.code != code {
                    ProgressView()
                } else if model.languageChoice == code {
                    Image(systemName: "checkmark").foregroundStyle(Theme.amber).accessibilityHidden(true)
                }
            }
            .contentShape(.rect)
        }
        .accessibilityAddTraits(model.languageChoice == code ? .isSelected : [])
    }
}
