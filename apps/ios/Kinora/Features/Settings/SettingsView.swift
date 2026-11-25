import SwiftUI
import KinoraKit

/// The settings hub: the profile up top, then one row per topic with its current value,
/// like the system Settings app. Each topic opens its own page (SettingsPages.swift).
struct SettingsView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss

    @State private var devices: [Device]?
    @State private var confirmLogout = false
    @State private var downloads = DownloadStore.shared

    private var profile: Profile? { model.me?.profile }

    var body: some View {
        NavigationStack {
            Form {
                profileHeader
                Section {
                    NavigationLink { PlaybackSettingsView() } label: {
                        SettingsRow(L("Lejátszás"), icon: "play.rectangle.fill", value: qualitySummary)
                    }
                    NavigationLink { SubtitleSettingsView() } label: {
                        SettingsRow(L("Feliratok és nyelvek"), icon: "captions.bubble.fill", value: subtitleSummary)
                    }
                    NavigationLink { DownloadSettingsView() } label: {
                        SettingsRow(L("Letöltések"), icon: "arrow.down.circle.fill", value: downloadSummary)
                    }
                } header: {
                    Text(L("Nézés"))
                }
                .listRowBackground(Theme.panel)

                Section {
                    if model.canEditSettings {
                        NavigationLink { ConnectDeviceView() } label: {
                            SettingsRow(L("Eszköz csatlakoztatása"), icon: "tv.and.mediabox.fill")
                        }
                    }
                    NavigationLink { DevicesSettingsView(devices: $devices) } label: {
                        SettingsRow(L("Bejelentkezett eszközök"), icon: "laptopcomputer.and.iphone",
                                    value: devices.map { "\($0.count)" })
                    }
                    NavigationLink { NotificationSettingsView() } label: {
                        SettingsRow(L("Értesítések"), icon: "bell.badge.fill")
                    }
                    NavigationLink { HomeScenesView() } label: {
                        SettingsRow(L("Otthon-jelenetek"), icon: "lightbulb.max.fill")
                    }
                    NavigationLink { LanguageSettingsView() } label: {
                        SettingsRow(L("Nyelv"), icon: "globe", value: LanguageSettingsView.summary(model))
                    }
                } header: {
                    Text(L("Eszközök és otthon"))
                }
                .listRowBackground(Theme.panel)

                AccountsSection()

                Section {
                    NavigationLink { ServerSettingsView() } label: {
                        SettingsRow(model.me?.serverName ?? model.server?.name ?? L("Szerver"), icon: "server.rack",
                                    detail: model.server.map { host($0.url) })
                    }
                    Button { model.showServers() } label: {
                        SettingsRow(L("Másik szerver"), icon: "arrow.left.arrow.right")
                    }
                    .tint(Theme.plasterBright)
                } header: {
                    Text(L("Szerver"))
                }
                .listRowBackground(Theme.panel)

                Section {
                    Button(role: .destructive) { confirmLogout = true } label: {
                        Text(L("Kijelentkezés"))
                            .font(.body.weight(.semibold))
                            .foregroundStyle(Theme.danger)
                            .frame(maxWidth: .infinity)
                    }
                } footer: {
                    Text("Kinora \(AppModel.appVersion) · iOS")
                        .frame(maxWidth: .infinity)
                        .padding(.top, 12)
                }
                .listRowBackground(Theme.panel)
            }
            .scrollContentBackground(.hidden)
            .background(Theme.black)
            .navigationTitle(L("Beállítások"))
            .navigationBarTitleDisplayMode(.large)
            .toolbarBackground(Theme.black, for: .navigationBar)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button(L("Kész")) { dismiss() }.fontWeight(.semibold)
                }
            }
            .task { await loadDevices() }
            .confirmationDialog(L("Biztosan kijelentkezel?"), isPresented: $confirmLogout, titleVisibility: .visible) {
                Button(L("Kijelentkezés"), role: .destructive) { Task { await model.logout() } }
            } message: {
                Text(L("A letöltéseid megmaradnak ezen az eszközön."))
            }
        }
        .tint(Theme.amber)
        .preferredColorScheme(.dark)
    }

    // MARK: - profile

    @ViewBuilder private var profileHeader: some View {
        if let p = profile, let me = model.me {
            Section {
                HStack(spacing: 16) {
                    ProfileFace(profile: p, size: 60, showsName: false)
                    VStack(alignment: .leading, spacing: 4) {
                        Text(p.name).font(.title3.weight(.semibold)).foregroundStyle(Theme.plasterBright)
                        Text(me.user.username).font(.subheadline).foregroundStyle(Theme.muted)
                        if p.isKids || me.profileLocked == true {
                            Text(p.isKids ? L("Gyerekprofil") : L("Rögzített profil"))
                                .font(.caption.weight(.semibold))
                                .foregroundStyle(Theme.amber)
                                .padding(.horizontal, 8)
                                .padding(.vertical, 2)
                                .background(Theme.amber.opacity(0.14), in: .capsule)
                        }
                    }
                    Spacer(minLength: 8)
                    if me.profileLocked != true {
                        Button(L("Váltás")) { model.switchProfile() }
                            .buttonStyle(.bordered)
                            .buttonBorderShape(.capsule)
                            .accessibilityLabel(L("Profilváltás"))
                    }
                }
                .padding(.vertical, 6)
            }
            .listRowBackground(Theme.panel)
        }
    }

    // MARK: - summaries

    private var qualitySummary: String {
        let bitrate = profile?.maxBitrate ?? 0
        return SettingChoices.bitrates.first { $0.0 == bitrate }?.1 ?? L("Eredeti")
    }

    private var subtitleSummary: String {
        let mode = profile?.subMode ?? "auto"
        return SettingChoices.shortLabel(SettingChoices.subModes, mode)
    }

    private var downloadSummary: String? {
        let used = downloads.usedBytes
        return used > 0 ? ByteCountFormatter.string(fromByteCount: used, countStyle: .file) : nil
    }

    private func host(_ url: String) -> String {
        URL(string: url)?.host() ?? url
    }

    private func loadDevices() async {
        do {
            devices = try await model.api { try await $0.devices() }
        } catch {
            // The devices page shows the error and a retry button.
        }
    }
}

/// A settings row: an amber icon tile, the title (with an optional second line) and the current value.
struct SettingsRow: View {
    var title: String
    var icon: String
    var detail: String?
    var value: String?

    init(_ title: String, icon: String, detail: String? = nil, value: String? = nil) {
        self.title = title; self.icon = icon; self.detail = detail; self.value = value
    }

    var body: some View {
        HStack(spacing: 14) {
            Image(systemName: icon)
                .font(.system(size: 15, weight: .semibold))
                .foregroundStyle(Theme.amber)
                .frame(width: 30, height: 30)
                .background(Theme.amber.opacity(0.14), in: .rect(cornerRadius: 7))
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 1) {
                Text(title).foregroundStyle(Theme.plasterBright)
                if let detail {
                    Text(detail).font(.caption).foregroundStyle(Theme.muted).lineLimit(1)
                }
            }
            Spacer(minLength: 8)
            if let value {
                Text(value).foregroundStyle(Theme.muted).lineLimit(1)
            }
        }
        .accessibilityElement(children: .combine)
    }
}
