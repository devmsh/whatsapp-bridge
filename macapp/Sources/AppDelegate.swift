import AppKit
import UserNotifications

final class AppDelegate: NSObject, NSApplicationDelegate {
    private let bridge = BridgeController()
    private let directory = ChatDirectory()
    private let stream = EventStream()
    private lazy var notifier = Notifier(directory: directory)
    private lazy var windowController = MainWindowController()
    private lazy var statusItem = StatusItemController()

    private var connected = false
    private var unread = 0

    func applicationDidFinishLaunching(_ notification: Notification) {
        buildMainMenu()

        notifier.requestAuthorization()
        notifier.onOpenChat = { [weak self] jid in
            self?.windowController.openChat(jid)
        }
        notifier.onUnreadCountChange = { [weak self] count in
            self?.unread = count
            self?.refreshStatusItem()
        }

        windowController.onVisibleChatChange = { [weak self] jid in
            self?.notifier.visibleChatJID = jid
        }
        windowController.onFocusChange = { [weak self] focused in
            self?.notifier.isWindowFocused = focused
            if focused { self?.notifier.clearUnread() }
        }

        statusItem.onOpen = { [weak self] in self?.showWindow() }
        statusItem.onReload = { [weak self] in self?.windowController.reload() }
        statusItem.onQuit = { NSApp.terminate(nil) }
        statusItem.refreshLoginItemState()

        stream.onMessage = { [weak self] msg in self?.notifier.handle(msg) }
        stream.onConnectionChange = { [weak self] up in
            self?.connected = up
            self?.refreshStatusItem()
        }

        showWindow()

        bridge.start { [weak self] ok in
            DispatchQueue.main.async {
                guard let self else { return }
                if ok {
                    self.windowController.load()
                    self.directory.startRefreshing()
                    self.stream.start()
                } else {
                    self.presentBridgeFailure()
                }
            }
        }
    }

    private func showWindow() {
        NSApp.setActivationPolicy(.regular)
        windowController.show()
        notifier.clearUnread()
    }

    private func refreshStatusItem() {
        statusItem.update(connected: connected, unread: unread)
    }

    private func presentBridgeFailure() {
        let alert = NSAlert()
        alert.messageText = "Could not reach the WhatsApp bridge"
        alert.informativeText = """
            Nothing is serving port \(Config.port), and the bundled bridge did not start.

            Check ~/Library/Logs/whatsapp-bridge-app.log, or confirm the \
            com.devmsh.whatsapp-bridge LaunchAgent is loaded.
            """
        alert.alertStyle = .critical
        alert.addButton(withTitle: "Quit")
        alert.addButton(withTitle: "Retry")
        if alert.runModal() == .alertSecondButtonReturn {
            bridge.start { [weak self] ok in
                DispatchQueue.main.async {
                    guard ok, let self else { return }
                    self.windowController.load()
                    self.directory.startRefreshing()
                    self.stream.start()
                }
            }
        } else {
            NSApp.terminate(nil)
        }
    }

    /// Clicking the dock icon reopens the hidden window.
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows: Bool) -> Bool {
        showWindow()
        return true
    }

    /// The menu bar item is the app's home while the window is closed, so the
    /// app outlives its last window.
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        false
    }

    func applicationWillTerminate(_ notification: Notification) {
        stream.stop()
        directory.stop()
        // Only stops a bridge this app spawned; a LaunchAgent-owned one keeps
        // running so background sync and the MCP server stay up.
        bridge.stopIfOwned()
    }

    /// A minimal menu so the standard shortcuts (⌘Q, ⌘W, ⌘C/⌘V, ⌘R) work.
    private func buildMainMenu() {
        let main = NSMenu()

        let appItem = NSMenuItem()
        let appMenu = NSMenu()
        appMenu.addItem(
            withTitle: "About WhatsApp Bridge",
            action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: "")
        appMenu.addItem(.separator())
        appMenu.addItem(
            withTitle: "Hide", action: #selector(NSApplication.hide(_:)), keyEquivalent: "h")
        appMenu.addItem(
            withTitle: "Quit", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        appItem.submenu = appMenu
        main.addItem(appItem)

        let editItem = NSMenuItem()
        let editMenu = NSMenu(title: "Edit")
        editMenu.addItem(withTitle: "Undo", action: Selector(("undo:")), keyEquivalent: "z")
        editMenu.addItem(withTitle: "Redo", action: Selector(("redo:")), keyEquivalent: "Z")
        editMenu.addItem(.separator())
        editMenu.addItem(withTitle: "Cut", action: #selector(NSText.cut(_:)), keyEquivalent: "x")
        editMenu.addItem(withTitle: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        editMenu.addItem(
            withTitle: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        editMenu.addItem(
            withTitle: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        editItem.submenu = editMenu
        main.addItem(editItem)

        let viewItem = NSMenuItem()
        let viewMenu = NSMenu(title: "View")
        let reload = NSMenuItem(
            title: "Reload", action: #selector(reloadFromMenu), keyEquivalent: "r")
        reload.target = self
        viewMenu.addItem(reload)
        viewItem.submenu = viewMenu
        main.addItem(viewItem)

        let windowItem = NSMenuItem()
        let windowMenu = NSMenu(title: "Window")
        windowMenu.addItem(
            withTitle: "Close", action: #selector(NSWindow.performClose(_:)), keyEquivalent: "w")
        windowMenu.addItem(
            withTitle: "Minimize", action: #selector(NSWindow.performMiniaturize(_:)),
            keyEquivalent: "m")
        windowItem.submenu = windowMenu
        main.addItem(windowItem)
        NSApp.windowsMenu = windowMenu

        NSApp.mainMenu = main
    }

    @objc private func reloadFromMenu() {
        windowController.reload()
    }
}
