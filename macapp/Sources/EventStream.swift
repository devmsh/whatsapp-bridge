import Foundation

/// A message as delivered by GET /api/v2/stream.
/// Mirrors the JSON tags on db.Message — only the fields a banner needs.
struct IncomingMessage: Decodable {
    let id: String
    let chatJID: String
    let sender: String
    let senderName: String
    let pushName: String
    let content: String
    let timestamp: Int64
    let isFromMe: Bool
    let isGroup: Bool
    let mediaType: String?
    let mediaCaption: String?
    let isDeleted: Bool

    enum CodingKeys: String, CodingKey {
        case id
        case chatJID = "chat_jid"
        case sender
        case senderName = "sender_name"
        case pushName = "push_name"
        case content
        case timestamp
        case isFromMe = "is_from_me"
        case isGroup = "is_group"
        case mediaType = "media_type"
        case mediaCaption = "media_caption"
        case isDeleted = "is_deleted"
    }

    /// Best available human name for whoever sent this.
    var displaySender: String {
        for candidate in [senderName, pushName] {
            let trimmed = candidate.trimmingCharacters(in: .whitespacesAndNewlines)
            if !trimmed.isEmpty { return trimmed }
        }
        return ChatDirectory.prettyJID(sender)
    }

    /// Human label for a media kind, used for both the `media_type` field and
    /// the bridge's own "[audio]"-style content placeholders.
    private static func mediaLabel(_ kind: String) -> String? {
        switch kind.lowercased() {
        case "image": return "📷 Photo"
        case "video": return "🎥 Video"
        case "audio", "ptt", "voice_note": return "🎤 Voice message"
        case "document": return "📄 Document"
        case "sticker": return "🌟 Sticker"
        case "location": return "📍 Location"
        case "contact", "vcard": return "👤 Contact"
        default: return nil
        }
    }

    /// Body text for a banner: the message, its caption, or a media label.
    var preview: String {
        let text = content.trimmingCharacters(in: .whitespacesAndNewlines)
        if !text.isEmpty {
            // handler_messages.go stores bare placeholders like "[audio]" as
            // the content of a media-only message — show a real label instead.
            if text.hasPrefix("["), text.hasSuffix("]"),
                let label = Self.mediaLabel(String(text.dropFirst().dropLast()))
            {
                return label
            }
            return text
        }

        let caption = (mediaCaption ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        if !caption.isEmpty { return caption }

        return Self.mediaLabel(mediaType ?? "") ?? "New message"
    }
}

/// Long-lived SSE reader for /api/v2/stream, with automatic reconnect.
///
/// The Go handler sends a `: ping` comment every 30s, so a silent connection is
/// a broken one — the 90s idle timeout turns that into a reconnect rather than
/// a listener that is quietly dead for hours.
final class EventStream: NSObject, URLSessionDataDelegate {
    private var session: URLSession?
    private var task: URLSessionDataTask?
    private var buffer = Data()
    private var retryDelay: TimeInterval = 1
    private var stopped = false

    var onMessage: ((IncomingMessage) -> Void)?
    var onConnectionChange: ((Bool) -> Void)?

    func start() {
        stopped = false
        connect()
    }

    func stop() {
        stopped = true
        task?.cancel()
        session?.invalidateAndCancel()
        task = nil
        session = nil
    }

    private func connect() {
        guard !stopped else { return }

        let cfg = URLSessionConfiguration.default
        cfg.timeoutIntervalForRequest = 90  // > the 30s server heartbeat
        cfg.timeoutIntervalForResource = .infinity
        cfg.requestCachePolicy = .reloadIgnoringLocalCacheData

        let session = URLSession(configuration: cfg, delegate: self, delegateQueue: nil)
        self.session = session

        var req = URLRequest(url: Config.api("stream"))
        req.setValue("text/event-stream", forHTTPHeaderField: "Accept")
        req.setValue("no-cache", forHTTPHeaderField: "Cache-Control")

        buffer.removeAll()
        let task = session.dataTask(with: req)
        self.task = task
        task.resume()
        log("stream: connecting")
    }

    private func scheduleReconnect() {
        guard !stopped else { return }
        let delay = retryDelay
        retryDelay = min(retryDelay * 2, 30)  // exponential backoff, capped
        log("stream: reconnecting in \(Int(delay))s")
        DispatchQueue.global().asyncAfter(deadline: .now() + delay) { [weak self] in
            self?.connect()
        }
    }

    // MARK: URLSessionDataDelegate

    func urlSession(
        _ session: URLSession, dataTask: URLSessionDataTask,
        didReceive response: URLResponse,
        completionHandler: @escaping (URLSession.ResponseDisposition) -> Void
    ) {
        if let http = response as? HTTPURLResponse, (200..<300).contains(http.statusCode) {
            log("stream: connected")
            retryDelay = 1  // a good connection resets the backoff
            DispatchQueue.main.async { self.onConnectionChange?(true) }
            completionHandler(.allow)
        } else {
            completionHandler(.cancel)
        }
    }

    func urlSession(
        _ session: URLSession, dataTask: URLSessionDataTask, didReceive data: Data
    ) {
        buffer.append(data)
        // SSE events are separated by a blank line.
        let separator = Data("\n\n".utf8)
        while let range = buffer.range(of: separator) {
            let chunk = buffer.subdata(in: buffer.startIndex..<range.lowerBound)
            buffer.removeSubrange(buffer.startIndex..<range.upperBound)
            handle(chunk)
        }
    }

    func urlSession(
        _ session: URLSession, task: URLSessionTask, didCompleteWithError error: Error?
    ) {
        if let error, (error as NSError).code == NSURLErrorCancelled { return }
        log("stream: disconnected (\(error?.localizedDescription ?? "server closed"))")
        DispatchQueue.main.async { self.onConnectionChange?(false) }
        session.invalidateAndCancel()
        scheduleReconnect()
    }

    private func handle(_ chunk: Data) {
        guard let text = String(data: chunk, encoding: .utf8) else { return }
        for line in text.split(separator: "\n", omittingEmptySubsequences: true) {
            // Comments (": ping", ": connected") are keep-alives, not events.
            guard line.hasPrefix("data:") else { continue }
            let json = line.dropFirst("data:".count).trimmingCharacters(in: .whitespaces)
            guard let data = json.data(using: .utf8) else { continue }
            do {
                let msg = try JSONDecoder().decode(IncomingMessage.self, from: data)
                DispatchQueue.main.async { self.onMessage?(msg) }
            } catch {
                log("stream: undecodable event — \(error)")
            }
        }
    }
}
