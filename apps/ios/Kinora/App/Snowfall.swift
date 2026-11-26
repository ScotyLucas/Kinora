import SwiftUI
import KinoraKit

extension AppModel {
    /// Snowfall is a profile setting, on by default; it never falls while a film or episode
    /// plays (full screen or picture in picture) or under Reduce Motion.
    var showsSnow: Bool {
        screen == .home && player == nil && (me?.profile?.snow ?? true)
    }
}

/// Small white flakes drifting down over the app. Each flake's place is worked out from the
/// time alone, so there is no state to update per frame; touches and focus pass through.
struct SnowfallView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private struct Flake {
        var x = Double.random(in: 0...1), y = Double.random(in: 0...1)
        var speed = Double.random(in: 25...70), radius = Double.random(in: 1...3.2)
        var phase = Double.random(in: 0...(2 * .pi)), alpha = Double.random(in: 0.45...0.95)
    }

    #if os(tvOS)
    private static let scale = 2.0 // a 1080p screen seen from the sofa
    #else
    private static let scale = 1.0
    #endif

    @State private var flakes = (0..<70).map { _ in Flake() }

    var body: some View {
        if !reduceMotion {
            TimelineView(.animation) { timeline in
                Canvas { ctx, size in
                    let t = timeline.date.timeIntervalSinceReferenceDate
                    let height = size.height + 12
                    for f in flakes {
                        let r = f.radius * Self.scale
                        let y = (f.y * height + f.speed * Self.scale * t).truncatingRemainder(dividingBy: height) - 6
                        let x = f.x * size.width + sin(t * 0.8 + f.phase) * 14 * Self.scale
                        ctx.opacity = f.alpha
                        ctx.fill(Path(ellipseIn: CGRect(x: x - r, y: y - r, width: r * 2, height: r * 2)), with: .color(.white))
                    }
                }
            }
            .ignoresSafeArea()
            .allowsHitTesting(false)
            .accessibilityHidden(true)
        }
    }
}
