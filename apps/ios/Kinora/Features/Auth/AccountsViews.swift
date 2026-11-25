import SwiftUI
import KinoraKit

/// The other accounts of this server signed in on this device, on the profile picker and the
/// login form: one tap brings one back, without a password.
struct ParkedAccountsList: View {
    @Environment(AppModel.self) private var model
    var busy = false

    var body: some View {
        if !model.accounts.isEmpty {
            VStack(alignment: .leading, spacing: 10) {
                Text(L("Bejelentkezve ezen az eszközön"))
                    .font(.subheadline)
                    .foregroundStyle(Theme.muted)
                ForEach(model.accounts) { a in
                    Button { Task { await model.switchAccount(to: a) } } label: {
                        Label(a.username, systemImage: "person.crop.circle")
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .buttonStyle(SecondaryButtonStyle())
                    .disabled(busy)
                    .accessibilityLabel(L("Fiókváltás"))
                    .accessibilityValue(a.username)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

/// Settings → Fiókok: the active account, the parked ones, and adding another.
struct AccountsSection: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        if model.canSwitchAccount, let me = model.me {
            Section {
                SettingsRow(me.user.username, icon: "person.crop.circle.fill", detail: L("Aktív"))
                ForEach(model.accounts) { a in
                    Button { Task { await model.switchAccount(to: a) } } label: {
                        SettingsRow(a.username, icon: "person.crop.circle")
                    }
                    .tint(Theme.plasterBright)
                    .swipeActions {
                        Button(L("Elfelejtés"), role: .destructive) { Task { await model.forgetAccount(a) } }
                    }
                    .accessibilityLabel(L("Fiókváltás"))
                    .accessibilityValue(a.username)
                }
                Button { Task { await model.addAccount() } } label: {
                    SettingsRow(L("Fiók hozzáadása"), icon: "person.crop.circle.badge.plus")
                }
                .tint(Theme.plasterBright)
            } header: {
                Text(L("Fiókok"))
            } footer: {
                Text(L("A többi fiók bejelentkezve marad ezen az eszközön, így jelszó nélkül válthatsz köztük. Egy fiók elfelejtéséhez húzd balra a sorát."))
            }
            .listRowBackground(Theme.panel)
        }
    }
}
