import SwiftUI

/// Kinora's "foyer" palette (DESIGN.md): warm blacks, plaster text, one amber marquee bulb.
enum Theme {
    static let black = Color(hex: 0x141210)
    static let lacquer = Color(hex: 0x1A1613)
    static let panel = Color(hex: 0x221D19)
    static let raised = Color(hex: 0x2B2520)
    static let rule = Color(hex: 0x352E28)
    static let plasterBright = Color(hex: 0xEFE7DA)
    static let plaster = Color(hex: 0xCFC5B6)
    static let muted = Color(hex: 0x9D9387)
    static let amber = Color(hex: 0xF0B44C)
    static let amberInk = Color(hex: 0x2B1C00)
    static let danger = Color(hex: 0xE8705F)
    /// Going the right way: a goal ahead of plan, a number up on the period before.
    static let ok = Color(hex: 0x8FC27A)

    /// Condensed display face standing in for Big Shoulders Display.
    static func display(_ size: CGFloat, weight: Font.Weight = .heavy) -> Font {
        .system(size: size, weight: weight).width(.condensed)
    }
}

extension Color {
    init(hex: UInt32) {
        self.init(red: Double((hex >> 16) & 0xFF) / 255, green: Double((hex >> 8) & 0xFF) / 255, blue: Double(hex & 0xFF) / 255)
    }

    /// "#c97c5d" → Color; falls back to the panel color.
    init(css: String) {
        let v = UInt32(css.trimmingCharacters(in: CharacterSet(charactersIn: "#")), radix: 16)
        self = v.map { Color(hex: $0) } ?? Theme.raised
    }
}

/// Amber, full-width primary button.
struct PrimaryButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) private var enabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.headline)
            .padding(.horizontal, 16)
            .frame(maxWidth: .infinity, minHeight: 50)
            .foregroundStyle(Theme.amberInk)
            .background(Theme.amber.opacity(enabled ? (configuration.isPressed ? 0.8 : 1) : 0.4), in: .rect(cornerRadius: 10))
    }
}

/// Outlined secondary button.
struct SecondaryButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) private var enabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.headline)
            .padding(.horizontal, 16)
            .frame(maxWidth: .infinity, minHeight: 50)
            .foregroundStyle(Theme.plasterBright.opacity(enabled ? 1 : 0.45))
            .background(Theme.panel.opacity(configuration.isPressed ? 0.6 : 1), in: .rect(cornerRadius: 10))
            .overlay(RoundedRectangle(cornerRadius: 10).stroke(Theme.rule))
    }
}

/// The input field look of the web client.
struct FieldStyle: ViewModifier {
    func body(content: Content) -> some View {
        content
            .padding(.horizontal, 14)
            .frame(minHeight: 50)
            .background(Theme.lacquer, in: .rect(cornerRadius: 8))
            .overlay(RoundedRectangle(cornerRadius: 8).stroke(Theme.rule))
            .foregroundStyle(Theme.plasterBright)
    }
}

extension View {
    func kinoraField() -> some View { modifier(FieldStyle()) }
}

/// "Kinora" word mark in amber.
struct BrandMark: View {
    var size: CGFloat = 40
    var body: some View {
        Text("Kinora")
            .font(Theme.display(size))
            .foregroundStyle(Theme.amber)
            .accessibilityAddTraits(.isHeader)
    }
}
