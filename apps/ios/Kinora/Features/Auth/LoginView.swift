import SwiftUI
import KinoraKit

/// Password login, with Quick Connect as the alternative.
struct LoginView: View {
    @Environment(AppModel.self) private var model
    @State private var username = ""
    @State private var password = ""
    @State private var error: String?
    @State private var busy = false
    @FocusState private var field: Field?

    enum Field { case username, password }

    var body: some View {
        AuthPage {
            ServerHeader()
            Text(L("Bejelentkezés")).font(Theme.display(40, weight: .bold))

            VStack(alignment: .leading, spacing: 6) {
                Text(L("Felhasználónév")).font(.subheadline).foregroundStyle(Theme.muted)
                TextField("", text: $username)
                    .textContentType(.username)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .submitLabel(.next)
                    .focused($field, equals: .username)
                    .onSubmit { field = .password }
                    .kinoraField()
                    .accessibilityLabel(L("Felhasználónév"))
            }
            VStack(alignment: .leading, spacing: 6) {
                Text(L("Jelszó")).font(.subheadline).foregroundStyle(Theme.muted)
                SecureField("", text: $password)
                    .textContentType(.password)
                    .submitLabel(.go)
                    .focused($field, equals: .password)
                    .onSubmit(login)
                    .kinoraField()
                    .accessibilityLabel(L("Jelszó"))
            }

            ErrorText(message: error ?? model.notice)

            Button(action: login) {
                if busy { ProgressView().tint(Theme.amberInk) } else { Text(L("Belépés")) }
            }
            .buttonStyle(PrimaryButtonStyle())
            .disabled(busy || username.trimmingCharacters(in: .whitespaces).isEmpty || password.isEmpty)

            if model.status?.quickconnectEnabled != false {
                Button { model.notice = nil; model.go(.quickConnect) } label: {
                    Label(L("Belépés kóddal"), systemImage: "key")
                }
                .buttonStyle(SecondaryButtonStyle())
            }

            ParkedAccountsList(busy: busy)
                .padding(.top, 8)

            OtherServerButton()
        }
        .onAppear {
            username = model.server?.username ?? ""
            field = username.isEmpty ? .username : .password
        }
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
                Haptic.error.play()
            } catch {
                self.error = error.localizedDescription
                Haptic.error.play()
            }
        }
    }
}

/// Small "server name · address" line above auth forms.
struct ServerHeader: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            BrandMark(size: 32)
            if let s = model.server {
                Label("\(s.name) · \(ServerAddress.host(of: s.url))", systemImage: "server.rack")
                    .font(.subheadline)
                    .foregroundStyle(Theme.muted)
            }
        }
    }
}
