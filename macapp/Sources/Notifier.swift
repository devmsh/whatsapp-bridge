import AppKit
import UserNotifications

/// Turns stream events into native notification banners.
///
/// Suppression rules, in order:
///   1. Own messages never notify.
///   2. Deletions never notify.
///   3. Muted chats never notify — the bridge owns mute state.
///   4. The chat you are already looking at, in a focused window, never notifies.
final class Notifier: NSObject, UNUserNotificationCenterDelegate {
    private let directory: ChatDirectory
    private var unreadCount = 0

    /// JID currently open in the UI, or nil. Set by the web view bridge.
    var visibleChatJID: String?
    var isWindowFocused = false

    var onOpenChat: ((String) -> Void)?
    var onUnreadCountChange: ((Int) -> Void)?

    init(directory: ChatDirectory) {
        self.directory = directory
        super.init()
        UNUserNotificationCenter.current().delegate = self
    }

    /// Must run from a bundled, signed app launched through LaunchServices —
    /// a bare exec of the binary is refused with "Notifications are not allowed".
    func requestAuthorization() {
        let center = UNUserNotificationCenter.current()
        center.requestAuthorization(options: [.alert, .sound, .badge]) { granted, err in
            if let err {
                log("notification authorization error: \(err.localizedDescription)")
            } else {
                log("notification authorization granted=\(granted)")
            }
            // The granted flag alone hides "allowed, but banners turned off in
            // System Settings", which looks identical to a broken pipeline.
            center.getNotificationSettings { s in
                log(
                    "notification settings: authorization=\(s.authorizationStatus.rawValue) "
                        + "alert=\(s.alertSetting.rawValue) sound=\(s.soundSetting.rawValue) "
                        + "style=\(s.alertStyle.rawValue)")
            }
        }
    }

    func handle(_ msg: IncomingMessage) {
        guard !msg.isFromMe, !msg.isDeleted else {
            log("suppressed \(msg.id): \(msg.isFromMe ? "own message" : "deleted")")
            return
        }

        let chat = directory.chat(for: msg.chatJID)
        if chat?.isMuted == true {
            log("suppressed \(msg.id): chat muted")
            return
        }

        if isWindowFocused, visibleChatJID == msg.chatJID {
            log("suppressed \(msg.id): chat already on screen")
            return
        }

        let content = UNMutableNotificationContent()
        if msg.isGroup {
            content.title = chat?.name ?? ChatDirectory.prettyJID(msg.chatJID)
            content.subtitle = msg.displaySender
        } else {
            content.title = chat?.name ?? msg.displaySender
        }
        content.body = msg.preview
        content.sound = .default
        content.userInfo = ["chat_jid": msg.chatJID]
        // Collapse a burst from one chat into a single stack.
        content.threadIdentifier = msg.chatJID

        let request = UNNotificationRequest(
            identifier: msg.id.isEmpty ? UUID().uuidString : msg.id,
            content: content,
            trigger: nil)

        UNUserNotificationCenter.current().add(request) { err in
            if let err {
                log("failed to post notification: \(err.localizedDescription)")
            } else {
                log("notified: \(content.title) — \(content.body.prefix(60))")
            }
        }

        unreadCount += 1
        publishUnread()
    }

    func clearUnread() {
        guard unreadCount != 0 else { return }
        unreadCount = 0
        publishUnread()
    }

    private func publishUnread() {
        let count = unreadCount
        DispatchQueue.main.async {
            NSApp.dockTile.badgeLabel = count > 0 ? String(count) : nil
            self.onUnreadCountChange?(count)
        }
    }

    // MARK: UNUserNotificationCenterDelegate

    /// Show banners even when our app is frontmost — the window may be on
    /// another chat or another Space.
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler handler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        handler([.banner, .sound, .list])
    }

    /// Clicking a banner opens that chat.
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler handler: @escaping () -> Void
    ) {
        let jid = response.notification.request.content.userInfo["chat_jid"] as? String
        DispatchQueue.main.async {
            NSApp.activate(ignoringOtherApps: true)
            if let jid { self.onOpenChat?(jid) }
            self.clearUnread()
        }
        handler()
    }
}
