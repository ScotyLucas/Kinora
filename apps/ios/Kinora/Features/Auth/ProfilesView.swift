import SwiftUI
import KinoraKit

/// "Ki néz?" – profile picker, with a PIN prompt for protected profiles.
struct ProfilesView: View {
    @Environment(AppModel.self) private var model
    @State private var pinFor: Profile?
    @State private var adding = false
    @State private var error: String?
    @State private var busy = false

    private let columns = [GridItem(.adaptive(minimum: 120, maximum: 160), spacing: 20)]

    var body: some View {
        ScrollView {
            VStack(spacing: 28) {
                BrandMark(size: 32)
                Text(L("Ki néz?")).font(Theme.display(44, weight: .bold))
                if let profiles = model.me?.profiles {
                    LazyVGrid(columns: columns, spacing: 24) {
                        ForEach(profiles) { p in
                            Button { pick(p) } label: { ProfileFace(profile: p) }
                                .buttonStyle(.plain)
                                .disabled(busy)
                        }
                        if model.canAddProfile {
                            Button { adding = true } label: { AddProfileFace() }
                                .buttonStyle(.plain)
                                .disabled(busy)
                        }
                    }
                } else {
                    ProgressView()
                }
                ErrorText(message: error)
                ParkedAccountsList(busy: busy)
                // Profiles belong to one account; another family member signs in with their own.
                // This one stays signed in, so it can come back without a password.
                Button { Task { await model.addAccount() } } label: {
                    Label(L("Másik felhasználó"), systemImage: "person.crop.circle.badge.plus")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(Theme.plaster)
                        .frame(minHeight: 44)
                        .contentShape(.rect)
                }
                .disabled(busy)
                OtherServerButton()
            }
            .frame(maxWidth: 700)
            .padding(24)
            .frame(maxWidth: .infinity)
        }
        .task {
            if model.me == nil { try? await model.guarded { try await model.loadMe() } }
        }
        .sheet(item: $pinFor) { p in
            PinSheet(profile: p)
                .presentationDetents([.medium])
                .presentationBackground(Theme.lacquer)
        }
        .sheet(isPresented: $adding) {
            AddProfileSheet()
                .presentationDetents([.medium])
                .presentationBackground(Theme.lacquer)
        }
    }

    private func pick(_ p: Profile) {
        if p.hasPin { pinFor = p; return }
        busy = true
        error = nil
        Task {
            defer { busy = false }
            do { try await model.guarded { try await model.selectProfile(p, pin: nil) } }
            catch { self.error = error.localizedDescription; Haptic.error.play() }
        }
    }
}

struct ProfileFace: View {
    var profile: Profile
    var size: CGFloat = 104
    var showsName = true

    var body: some View {
        VStack(spacing: 10) {
            ZStack(alignment: .bottomTrailing) {
                Text(profile.name.prefix(1).uppercased())
                    .font(Theme.display(size * 0.5))
                    .foregroundStyle(Theme.black)
                    .frame(width: size, height: size)
                    .background(Color(css: profile.color), in: .rect(cornerRadius: size * 0.18))
                if showsName, profile.hasPin {
                    Image(systemName: "lock.fill")
                        .font(.caption)
                        .padding(6)
                        .background(Theme.black.opacity(0.75), in: .circle)
                        .padding(6)
                }
            }
            if showsName { Text(profile.name).font(.headline).lineLimit(1) }
            if showsName, profile.isKids {
                Text(L("Gyerek")).font(.caption).foregroundStyle(Theme.muted)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

private struct AddProfileFace: View {
    var size: CGFloat = 104

    var body: some View {
        VStack(spacing: 10) {
            Image(systemName: "plus")
                .font(.system(size: size * 0.36, weight: .semibold))
                .foregroundStyle(Theme.plaster)
                .frame(width: size, height: size)
                .overlay {
                    RoundedRectangle(cornerRadius: size * 0.18)
                        .strokeBorder(Theme.muted, style: StrokeStyle(lineWidth: 2, dash: [6, 5]))
                }
            Text(L("Profil hozzáadása")).font(.headline).lineLimit(1)
        }
        .accessibilityElement(children: .combine)
    }
}

/// A new profile: its name and whether it is a kids profile; the rest is under Settings once it is picked.
private struct AddProfileSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @State private var isKids = false
    @State private var error: String?
    @State private var busy = false
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            Text(L("Profil hozzáadása")).font(.title3.bold())
            TextField(L("Név"), text: $name)
                .textInputAutocapitalization(.words)
                .submitLabel(.done)
                .focused($focused)
                .onSubmit(submit)
                .kinoraField()
            Toggle(L("Gyerekprofil"), isOn: $isKids)
                .tint(Theme.amber)
            ErrorText(message: error)
            Button(action: submit) {
                if busy { ProgressView().tint(Theme.amberInk) } else { Text(L("Hozzáadás")) }
            }
            .buttonStyle(PrimaryButtonStyle())
            .disabled(trimmed.isEmpty || busy)
        }
        .padding(24)
        .onAppear { focused = true }
    }

    private var trimmed: String { String(name.trimmingCharacters(in: .whitespacesAndNewlines).prefix(30)) }

    private func submit() {
        guard !busy, !trimmed.isEmpty else { return }
        busy = true
        error = nil
        Task {
            defer { busy = false }
            do {
                try await model.guarded { try await model.createProfile(name: trimmed, isKids: isKids) }
                dismiss()
            } catch {
                self.error = error.localizedDescription
                Haptic.error.play()
            }
        }
    }
}

private struct PinSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var profile: Profile
    @State private var pin = ""
    @State private var error: String?
    @State private var busy = false
    @State private var fieldID = 0
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            Text(L("{0} profilja PIN-nel védett", profile.name)).font(.title3.bold())
            SecureField("PIN", text: $pin)
                .keyboardType(.numberPad)
                .textContentType(.oneTimeCode)
                .focused($focused)
                .onSubmit(submit)
                .kinoraField()
                // A fresh field after a wrong PIN: clearing the binding alone can leave
                // the old digits inside the secure text field.
                .id(fieldID)
            ErrorText(message: error)
            Button(action: submit) {
                if busy { ProgressView().tint(Theme.amberInk) } else { Text(L("Belépés")) }
            }
            .buttonStyle(PrimaryButtonStyle())
            .disabled(digits.isEmpty || busy)
        }
        .padding(24)
        .onAppear { focused = true }
    }

    private var digits: String { String(pin.filter(\.isNumber).prefix(8)) }

    private func submit() {
        guard !busy, !digits.isEmpty else { return }
        let pin = digits
        busy = true
        error = nil
        Task {
            defer { busy = false }
            do {
                try await model.guarded { try await model.selectProfile(profile, pin: pin) }
                dismiss()
            } catch {
                self.error = error.localizedDescription
                Haptic.error.play()
                self.pin = ""
                fieldID += 1
                try? await Task.sleep(for: .milliseconds(150))
                focused = true
            }
        }
    }
}
