import CryptoKit
import KinoraKit
import Messages
import SwiftUI
import UIKit

/// Kinora's stickers: little marquee signs and ticket stubs for movie night.
/// They are drawn on the device in the app's language (the text comes from L), so a German
/// or English user gets their own words; each drawing is kept as a PNG under Caches.
struct StickerDesign: Identifiable, Hashable {
    enum Shape: Hashable { case marquee, ticket, badge }

    var id: String
    var key: String
    var symbol: String
    var shape: Shape
    var tilt: Double
    var hot = false

    var text: String { key.isEmpty ? "" : L(key) }

    static let all: [StickerDesign] = [
        StickerDesign(id: "movie-night", key: "Mozi este?", symbol: "popcorn.fill", shape: .ticket, tilt: -5),
        StickerDesign(id: "in", key: "Benne vagyok!", symbol: "hand.thumbsup.fill", shape: .badge, tilt: 4),
        StickerDesign(id: "starting", key: "Indul a film!", symbol: "play.fill", shape: .marquee, tilt: -3, hot: true),
        StickerDesign(id: "no-spoilers", key: "Nincs spoiler!", symbol: "hand.raised.fill", shape: .marquee, tilt: 3),
        StickerDesign(id: "one-more", key: "Még egy rész", symbol: "forward.end.fill", shape: .ticket, tilt: 4),
        StickerDesign(id: "pause", key: "Szünet!", symbol: "pause.fill", shape: .badge, tilt: -4),
        StickerDesign(id: "snacks", key: "Hozom a nasit", symbol: "takeoutbag.and.cup.and.straw.fill", shape: .ticket, tilt: -4),
        StickerDesign(id: "where", key: "Hol tartasz?", symbol: "clock.fill", shape: .badge, tilt: 5),
        StickerDesign(id: "loved", key: "Imádtam!", symbol: "heart.fill", shape: .marquee, tilt: -2, hot: true),
        StickerDesign(id: "cried", key: "Sírtam", symbol: "drop.fill", shape: .badge, tilt: 3),
        StickerDesign(id: "asleep", key: "Elaludtam", symbol: "zzz", shape: .ticket, tilt: 3),
        StickerDesign(id: "kinora", key: "", symbol: "", shape: .marquee, tilt: 0, hot: true),
    ]
}

enum StickerStore {
    static let pixels: CGFloat = 408 // Messages' "regular" sticker size

    /// The sticker as a file, drawn on first use; nil if it could not be written.
    @MainActor
    static func sticker(_ design: StickerDesign) -> MSSticker? {
        let text = design.text
        let digest = SHA256.hash(data: Data("\(design.id)|\(text)|v1".utf8)).prefix(8).map { String(format: "%02x", $0) }.joined()
        let dir = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0].appending(path: "Stickers", directoryHint: .isDirectory)
        let file = dir.appending(path: "\(design.id)-\(digest).png")
        if !FileManager.default.fileExists(atPath: file.path) {
            try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            let r = ImageRenderer(content: StickerArt(design: design, text: text))
            r.scale = 1
            r.isOpaque = false
            guard let png = r.uiImage?.pngData(), (try? png.write(to: file, options: .atomic)) != nil else { return nil }
        }
        let label = design.key.isEmpty ? "Kinora" : text
        return try? MSSticker(contentsOfFileURL: file, localizedDescription: label)
    }
}

/// One sticker, 408×408 with a transparent edge: the sign, a plaster die-cut border and a
/// row of amber bulbs just inside it.
struct StickerArt: View {
    var design: StickerDesign
    var text: String

    private var side: CGFloat { StickerStore.pixels }

    var body: some View {
        ZStack {
            sign
                .rotationEffect(.degrees(design.tilt))
                .shadow(color: .black.opacity(0.35), radius: 8, y: 5)
        }
        .frame(width: side, height: side)
        .environment(\.colorScheme, .dark)
    }

    @ViewBuilder private var sign: some View {
        switch design.shape {
        case .marquee: framed(RoundedRectangle(cornerRadius: 46), size: CGSize(width: 360, height: 270))
        case .ticket: framed(TicketShape(), size: CGSize(width: 370, height: 230))
        case .badge: framed(Circle(), size: CGSize(width: 330, height: 330))
        }
    }

    private func framed<S: InsettableShape>(_ shape: S, size: CGSize) -> some View {
        let fill = design.hot
            ? LinearGradient(colors: [Theme.amber, Color(hex: 0xD9962F)], startPoint: .top, endPoint: .bottom)
            : LinearGradient(colors: [Theme.raised, Theme.black], startPoint: .top, endPoint: .bottom)
        return ZStack {
            shape.fill(fill)
            // The bulbs: round dots of a dashed stroke, inset from the edge.
            shape
                .inset(by: 22)
                .stroke(design.hot ? Theme.amberInk.opacity(0.55) : Theme.amber,
                        style: StrokeStyle(lineWidth: 9, lineCap: .round, dash: [0.1, 24]))
            content(size: size)
                .padding(.horizontal, 44)
        }
        .frame(width: size.width, height: size.height)
        // The die-cut: a thick plaster rim, like a sticker peeled off a sheet.
        .overlay(shape.stroke(Theme.plasterBright, lineWidth: 12))
    }

    @ViewBuilder private func content(size: CGSize) -> some View {
        let ink = design.hot ? Theme.amberInk : Theme.plasterBright
        let accent = design.hot ? Theme.amberInk : Theme.amber
        if design.key.isEmpty {
            VStack(spacing: 6) {
                KMark()
                    .stroke(accent, style: StrokeStyle(lineWidth: 18, lineCap: .round, lineJoin: .round))
                    .frame(width: 70, height: 100)
                Text("Kinora").font(Theme.display(62)).foregroundStyle(ink)
            }
        } else {
            VStack(spacing: 10) {
                Image(systemName: design.symbol)
                    .font(.system(size: design.shape == .badge ? 84 : 72, weight: .bold))
                    .foregroundStyle(accent)
                Text(text.uppercased())
                    .font(Theme.display(design.shape == .badge ? 46 : 52))
                    .tracking(1.5)
                    .multilineTextAlignment(.center)
                    .foregroundStyle(ink)
                    .lineLimit(2)
                    .minimumScaleFactor(0.5)
            }
        }
    }
}

/// A cinema ticket: a rounded stub with a half-round notch in each side.
struct TicketShape: InsettableShape {
    var inset: CGFloat = 0

    func path(in rect: CGRect) -> Path {
        let r = rect.insetBy(dx: inset, dy: inset)
        let corner: CGFloat = max(28 - inset, 4)
        let notch: CGFloat = max(30 - inset * 0.5, 8)
        var p = Path()
        p.move(to: CGPoint(x: r.minX + corner, y: r.minY))
        p.addLine(to: CGPoint(x: r.maxX - corner, y: r.minY))
        p.addArc(center: CGPoint(x: r.maxX - corner, y: r.minY + corner), radius: corner, startAngle: .degrees(-90), endAngle: .degrees(0), clockwise: false)
        p.addLine(to: CGPoint(x: r.maxX, y: r.midY - notch))
        p.addArc(center: CGPoint(x: r.maxX, y: r.midY), radius: notch, startAngle: .degrees(-90), endAngle: .degrees(90), clockwise: true)
        p.addLine(to: CGPoint(x: r.maxX, y: r.maxY - corner))
        p.addArc(center: CGPoint(x: r.maxX - corner, y: r.maxY - corner), radius: corner, startAngle: .degrees(0), endAngle: .degrees(90), clockwise: false)
        p.addLine(to: CGPoint(x: r.minX + corner, y: r.maxY))
        p.addArc(center: CGPoint(x: r.minX + corner, y: r.maxY - corner), radius: corner, startAngle: .degrees(90), endAngle: .degrees(180), clockwise: false)
        p.addLine(to: CGPoint(x: r.minX, y: r.midY + notch))
        p.addArc(center: CGPoint(x: r.minX, y: r.midY), radius: notch, startAngle: .degrees(90), endAngle: .degrees(-90), clockwise: true)
        p.addLine(to: CGPoint(x: r.minX, y: r.minY + corner))
        p.addArc(center: CGPoint(x: r.minX + corner, y: r.minY + corner), radius: corner, startAngle: .degrees(180), endAngle: .degrees(270), clockwise: false)
        p.closeSubpath()
        return p
    }

    func inset(by amount: CGFloat) -> TicketShape { TicketShape(inset: inset + amount) }
}

/// Messages' own sticker view: tap to put it in the message field, or peel and drag onto a bubble.
struct StickerCell: UIViewRepresentable {
    var sticker: MSSticker

    func makeUIView(context: Context) -> MSStickerView {
        let v = MSStickerView(frame: .zero, sticker: sticker)
        v.setContentHuggingPriority(.defaultLow, for: .horizontal)
        v.setContentHuggingPriority(.defaultLow, for: .vertical)
        return v
    }

    func updateUIView(_ v: MSStickerView, context: Context) {
        if v.sticker?.imageFileURL != sticker.imageFileURL { v.sticker = sticker }
    }
}
