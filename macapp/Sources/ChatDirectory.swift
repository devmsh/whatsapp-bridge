import Foundation

/// Resolves chat display names and mute state for notification banners.
///
/// Name resolution deliberately mirrors the web UI's `buildNameMap` +
/// `chatTitle` (web/src/explorer/format.ts): `chats.name` holds the raw JID for
/// groups and a masked string for many DMs, so real names come from
/// /api/v2/groups and /api/v2/contacts. Getting this wrong is very visible —
/// every group banner reads "Group".
///
/// Mute and archive matter too: a muted or archived chat must not raise a
/// banner, and the bridge already owns that state (including the
/// working-hours auto-mute), so the app honours what the bridge says rather
/// than keeping a second notion of either.
final class ChatDirectory {
    struct Chat {
        let jid: String
        let name: String
        let isMuted: Bool
        let isArchived: Bool
    }

    private var muted: Set<String> = []
    private var archived: Set<String> = []
    private var rawChatName: [String: String] = [:]
    /// JID → display name, from groups and contacts (incl. phone and LID forms).
    private var names: [String: String] = [:]
    private let lock = NSLock()
    private var refreshTimer: Timer?

    /// Chats change slowly; a periodic refresh keeps names and mute state
    /// honest without hammering the API.
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
        return Chat(
            jid: jid, name: resolvedNameLocked(jid),
            isMuted: muted.contains(jid), isArchived: archived.contains(jid))
    }

    /// The contact-book name for a JID, or nil when it is genuinely unknown.
    /// Unlike `chat(for:)` this does not fall back to a phone number, so
    /// callers can prefer a push name over "+9725…".
    func knownName(for jid: String) -> String? {
        lock.lock()
        defer { lock.unlock() }
        if let mapped = names[jid], !mapped.isEmpty { return mapped }
        let raw = rawChatName[jid] ?? ""
        return Self.looksLikeName(raw) ? raw : nil
    }

    /// Same precedence as the UI's chatTitle().
    private func resolvedNameLocked(_ jid: String) -> String {
        if jid.hasSuffix("@broadcast") { return "Status updates" }
        if let mapped = names[jid], !mapped.isEmpty { return mapped }

        let raw = rawChatName[jid] ?? ""
        if Self.looksLikeName(raw) { return raw }

        let user = Self.jidUser(jid)
        if jid.hasSuffix("@g.us") {
            return "Group · " + String(user.suffix(4))
        }
        return "+" + user
    }

    func refresh() {
        fetchJSONArray("chats?limit=1000") { [weak self] rows in
            guard let self else { return }
            var mutedSet: Set<String> = []
            var archivedSet: Set<String> = []
            var raw: [String: String] = [:]
            for row in rows {
                guard let jid = row["jid"] as? String else { continue }
                raw[jid] = (row["name"] as? String) ?? ""
                if (row["is_muted"] as? Bool) == true { mutedSet.insert(jid) }
                if (row["is_archived"] as? Bool) == true { archivedSet.insert(jid) }
            }
            self.lock.lock()
            self.muted = mutedSet
            self.archived = archivedSet
            self.rawChatName = raw
            self.lock.unlock()
        }

        // Groups and contacts together form the name map. Merged into the same
        // dictionary because their JID suffixes never collide.
        fetchJSONArray("groups") { [weak self] rows in
            guard let self else { return }
            var found: [String: String] = [:]
            for row in rows {
                guard let jid = row["jid"] as? String,
                    let name = row["name"] as? String, !name.isEmpty
                else { continue }
                found[jid] = name
            }
            self.mergeNames(found, replacingSuffix: "@g.us")
        }

        fetchJSONArray("contacts?limit=5000") { [weak self] rows in
            guard let self else { return }
            var found: [String: String] = [:]
            for row in rows {
                let name = ["name", "verified_name", "business_name", "push_name"]
                    .compactMap { row[$0] as? String }
                    .first { !$0.trimmingCharacters(in: .whitespaces).isEmpty } ?? ""
                if name.isEmpty { continue }
                if let jid = row["jid"] as? String, !jid.isEmpty { found[jid] = name }
                if let phone = row["phone"] as? String, !phone.isEmpty {
                    found[phone + "@s.whatsapp.net"] = name
                }
                if let lid = row["lid"] as? String, !lid.isEmpty {
                    found[lid + "@lid"] = name
                }
            }
            self.mergeNames(found, replacingSuffix: nil)
        }
    }

    /// Replaces the entries this source owns without dropping the other's.
    private func mergeNames(_ found: [String: String], replacingSuffix suffix: String?) {
        lock.lock()
        defer { lock.unlock() }
        if let suffix {
            names = names.filter { !$0.key.hasSuffix(suffix) }
        } else {
            names = names.filter { $0.key.hasSuffix("@g.us") }
        }
        names.merge(found) { _, new in new }
    }

    private func fetchJSONArray(
        _ path: String, _ handler: @escaping ([[String: Any]]) -> Void
    ) {
        var req = URLRequest(url: Config.api(path))
        req.timeoutInterval = 15
        URLSession.shared.dataTask(with: req) { data, _, err in
            if let err {
                log("directory refresh failed for \(path): \(err.localizedDescription)")
                return
            }
            guard let data,
                let rows = try? JSONSerialization.jsonObject(with: data) as? [[String: Any]]
            else { return }
            handler(rows)
        }.resume()
    }

    // MARK: helpers mirroring web/src/explorer/format.ts

    static func jidUser(_ jid: String) -> String {
        jid.split(separator: "@").first.map(String.init) ?? jid
    }

    /// Rejects JIDs, privacy-masked strings ("+966∙∙∙∙∙29") and bare numbers.
    static func looksLikeName(_ s: String) -> Bool {
        if s.isEmpty { return false }
        if s.contains("@") { return false }
        if s.contains("∙") || s.contains("•") { return false }
        let bare = s.replacingOccurrences(of: " ", with: "")
            .replacingOccurrences(of: "-", with: "")
            .replacingOccurrences(of: "+", with: "")
        if !bare.isEmpty, bare.allSatisfy(\.isNumber) { return false }
        return true
    }

    /// Fallback used when nothing is known about the sender of a message.
    static func prettyJID(_ jid: String) -> String {
        if jid.hasSuffix("@g.us") { return "Group · " + String(jidUser(jid).suffix(4)) }
        return "+" + jidUser(jid)
    }
}
