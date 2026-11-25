import KinoraKit
import SwiftUI
import UIKit

/// The picture of a Kinora bubble: the backdrop behind, the poster in its lit case, and a
/// marquee label (WATCH PARTY with the room code, or the recommendation). Messages draws the
/// title and the code under it as the caption, so the picture stays big and quiet.
struct MessageCard: View {
    static let size = CGSize(width: 600, height: 340)

    var invite: KinoraInvite
    var backdrop: UIImage?
    var poster: UIImage?

    var body: some View {
        ZStack(alignment: .bottomLeading) {
            Theme.black
            if let backdrop {
                Image(uiImage: backdrop).resizable().scaledToFill()
                    .frame(width: Self.size.width, height: Self.size.height).clipped()
            }
            LinearGradient(stops: [.init(color: Theme.black.opacity(0.15), location: 0),
                                   .init(color: Theme.black.opacity(0.55), location: 0.5),
                                   .init(color: Theme.black.opacity(0.95), location: 1)],
                           startPoint: .top, endPoint: .bottom)
            HStack(alignment: .bottom, spacing: 22) {
                if let poster {
                    Image(uiImage: poster).resizable().scaledToFill()
                        .frame(width: 128, height: 192).clipped()
                        .clipShape(.rect(cornerRadius: 8))
                        .overlay(RoundedRectangle(cornerRadius: 8).stroke(Theme.plasterBright.opacity(0.25), lineWidth: 1.5))
                        .shadow(color: .black.opacity(0.6), radius: 14, y: 6)
                }
                VStack(alignment: .leading, spacing: 10) {
                    MarqueeLabel(text: invite.partyCode == nil ? L("Ajánlás") : "Watch party")
                    Text(invite.title)
                        .font(Theme.display(44))
                        .foregroundStyle(Theme.plasterBright)
                        .lineLimit(2)
                        .minimumScaleFactor(0.6)
                        .shadow(color: .black.opacity(0.5), radius: 6)
                    if let code = invite.partyCode {
                        HStack(spacing: 10) {
                            Image(systemName: "person.2.fill")
                            Text(code).font(Theme.display(30, weight: .bold)).tracking(4).monospacedDigit()
                        }
                        .foregroundStyle(Theme.amber)
                    } else if !invite.subtitle.isEmpty {
                        Text(invite.subtitle).font(.system(size: 22, weight: .semibold)).foregroundStyle(Theme.plaster).lineLimit(1)
                    }
                }
                Spacer(minLength: 0)
            }
            .padding(28)
            // No logo of its own: Messages badges the bubble with the app icon.
        }
        .frame(width: Self.size.width, height: Self.size.height)
        .environment(\.colorScheme, .dark)
    }

    @MainActor
    static func render(invite: KinoraInvite, backdrop: UIImage?, poster: UIImage?) -> UIImage? {
        let r = ImageRenderer(content: MessageCard(invite: invite, backdrop: backdrop, poster: poster))
        r.scale = 2
        r.isOpaque = true
        return r.uiImage
    }
}

/// An amber pill with uppercase condensed lettering: the marquee sign.
struct MarqueeLabel: View {
    var text: String
    var size: CGFloat = 20

    var body: some View {
        Text(text.uppercased())
            .font(Theme.display(size, weight: .heavy))
            .tracking(size * 0.12)
            .foregroundStyle(Theme.amberInk)
            .padding(.horizontal, size * 0.6)
            .padding(.vertical, size * 0.22)
            .background(Theme.amber, in: .capsule)
    }
}

/// The Kinora K (apps/branding/logo.svg), in a 1×1 box scaled to the frame.
struct KMark: Shape {
    func path(in rect: CGRect) -> Path {
        // Strokes of the 1024 icon: M320 224v576 · M320 512l320-288 · M416 448l256 352, normalized to 320…672 × 224…800.
        func p(_ x: CGFloat, _ y: CGFloat) -> CGPoint {
            CGPoint(x: rect.minX + (x - 320) / 352 * rect.width, y: rect.minY + (y - 224) / 576 * rect.height)
        }
        var path = Path()
        path.move(to: p(320, 224)); path.addLine(to: p(320, 800))
        path.move(to: p(320, 512)); path.addLine(to: p(640, 224))
        path.move(to: p(416, 448)); path.addLine(to: p(672, 800))
        return path
    }
}
