import Foundation

/// Caches chat display names and mute state so every incoming message does not
/// need its own API round-trip.
///
/// Mute matters: a muted chat must not raise a banner. The bridge already tracks
/// it (including the working-hours auto-mute scheduler), so the app just honours
/// what the bridge says rather than keeping a second notion of "muted".
final class ChatDirectory {
    struct Chat {
        let jid: String
        let name: String
        let isMuted: Bool
    }

    private var byJID: [String: Chat] = [:]
    private let lock = NSLock()
    private var refreshTimer: Timer?

    /// Chats change slowly; a periodic refresh keeps mute state honest without
    /// hammering the API.
    func startRefreshing(every seconds: TimeInterval = 60) {
        refresh()
        refreshTimer = Timer.scheduledTimer(withTimeInterval: seconds, repeats: true) {
            [weak self] _ in
            self?.refresh()
        }
    }

    func stop() {
        refreshTimer?.invalidate()
        refreshTimer = nil
    }

    func chat(for jid: String) -> Chat? {
        lock.lock()
        defer { lock.unlock() }
        return byJID[jid]
    }

    func refresh() {
        var req = URLRequest(url: Config.api("chats?limit=1000"))
        req.timeoutInterval = 10
        URLSession.shared.dataTask(with: req) { [weak self] data, _, err in
            guard let self else { return }
            if let err {
                log("chat refresh failed: \(err.localizedDescription)")
                return
            }
            guard let data,
                let rows = try? JSONSerialization.jsonObject(with: data) as? [[String: Any]]
            else { return }

            var map: [String: Chat] = [:]
            for row in rows {
                guard let jid = row["jid"] as? String else { continue }
                // The API falls back to the JID when a group name has not
                // synced yet; that is not worth showing in a banner.
                let raw = (row["name"] as? String) ?? ""
                let name = (raw.isEmpty || raw == jid) ? Self.prettyJID(jid) : raw
                map[jid] = Chat(
                    jid: jid,
                    name: name,
                    isMuted: (row["is_muted"] as? Bool) ?? false)
            }

            self.lock.lock()
            self.byJID = map
            self.lock.unlock()
        }.resume()
    }

    /// "972592239213@s.whatsapp.net" → "+972592239213"; group JIDs stay opaque.
    static func prettyJID(_ jid: String) -> String {
        let user = jid.split(separator: "@").first.map(String.init) ?? jid
        if jid.hasSuffix("@g.us") { return "Group" }
        let digits = user.split(separator: "-").first.map(String.init) ?? user
        return digits.allSatisfy(\.isNumber) ? "+\(digits)" : digits
    }
}
