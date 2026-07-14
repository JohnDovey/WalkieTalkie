import SwiftUI

struct WatchContentView: View {
    @EnvironmentObject private var session: WatchTalkSession
    @State private var talking = false

    var body: some View {
        VStack(spacing: 8) {
            Text(session.statusLine)
                .font(.caption2)
                .multilineTextAlignment(.center)
            Text(talking ? "Talking…" : "Hold to talk")
                .font(.headline)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(talking ? Color.green.opacity(0.85) : Color.blue.opacity(0.85))
                .clipShape(Circle())
                .padding(12)
                .gesture(
                    DragGesture(minimumDistance: 0)
                        .onChanged { _ in
                            if !talking {
                                talking = true
                                session.startTalking()
                            }
                        }
                        .onEnded { _ in
                            talking = false
                            session.stopTalking()
                        }
                )
        }
        .padding(4)
    }
}
