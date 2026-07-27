import AppKit
import ServiceManagement

/// Menu bar item: quick access while the window is hidden, plus a visible
/// signal for whether the notification stream is actually connected.
final class StatusItemController {
    private let item: NSStatusItem
    private let connectionItem = NSMenuItem(title: "Connecting…", action: nil, keyEquivalent: "")
    private let loginItem = NSMenuItem(title: "Start at Login", action: nil, keyEquivalent: "")

    var onOpen: (() -> Void)?
    var onReload: (() -> Void)?
    var onQuit: (() -> Void)?

    init() {
        item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        if let button = item.button {
            button.image = NSImage(
                systemSymbolName: "bubble.left.and.bubble.right",
                accessibilityDescription: "WhatsApp Bridge")
            button.image?.isTemplate = true
        }
        item.menu = buildMenu()
    }

    private func buildMenu() -> NSMenu {
        let menu = NSMenu()

        connectionItem.isEnabled = false
        menu.addItem(connectionItem)
        menu.addItem(.separator())

        let open = NSMenuItem(
            title: "Open WhatsApp Bridge", action: #selector(openAction), keyEquivalent: "o")
        open.target = self
        menu.addItem(open)

        let reload = NSMenuItem(title: "Reload", action: #selector(reloadAction), keyEquivalent: "r")
        reload.target = self
        menu.addItem(reload)

        menu.addItem(.separator())

        loginItem.target = self
        loginItem.action = #selector(toggleLoginItem)
        menu.addItem(loginItem)

        menu.addItem(.separator())

        let quit = NSMenuItem(title: "Quit", action: #selector(quitAction), keyEquivalent: "q")
        quit.target = self
        menu.addItem(quit)

        return menu
    }

    /// Reflects both stream health and unread count in the menu bar.
    func update(connected: Bool, unread: Int) {
        connectionItem.title = connected ? "Connected" : "Disconnected — retrying…"
        if let button = item.button {
            button.title = unread > 0 ? " \(unread)" : ""
        }
    }

    @objc private func openAction() { onOpen?() }
    @objc private func reloadAction() { onReload?() }
    @objc private func quitAction() { onQuit?() }

    // MARK: launch at login

    /// Notifications only arrive while this app is running, so starting at
    /// login is what makes them dependable. The bridge itself is separate — it
    /// stays up via its own LaunchAgent.
    func refreshLoginItemState() {
        loginItem.state = SMAppService.mainApp.status == .enabled ? .on : .off
    }

    @objc private func toggleLoginItem() {
        do {
            if SMAppService.mainApp.status == .enabled {
                try SMAppService.mainApp.unregister()
            } else {
                try SMAppService.mainApp.register()
            }
        } catch {
            log("could not change login item: \(error.localizedDescription)")
            let alert = NSAlert()
            alert.messageText = "Could not change the login item"
            alert.informativeText = error.localizedDescription
            alert.runModal()
        }
        refreshLoginItemState()
    }
}
