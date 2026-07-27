import AppKit
import WebKit

/// The app window: a WKWebView showing the bridge's existing React UI.
///
/// Closing the window hides it rather than quitting. The bridge keeps running
/// (usually as the LaunchAgent) and notifications keep arriving — the same
/// behaviour as Mail or Messages.
final class MainWindowController: NSObject, NSWindowDelegate, WKNavigationDelegate,
    WKScriptMessageHandler
{
    private(set) var window: NSWindow!
    private var webView: WKWebView!
    private var retryTimer: Timer?

    /// Reported by the web UI so notifications for the chat on screen can be
    /// suppressed. nil when no chat is open.
    var onVisibleChatChange: ((String?) -> Void)?
    var onFocusChange: ((Bool) -> Void)?

    override init() {
        super.init()
        buildWindow()
    }

    private func buildWindow() {
        let config = WKWebViewConfiguration()
        config.websiteDataStore = .default()
        // Lets the React app tell us which chat is on screen.
        config.userContentController.add(self, name: "app")

        // Tells the web UI it is running inside the native shell, so it stops
        // offering its own web-notification prompt (see useDesktopNotifications).
        config.userContentController.addUserScript(
            WKUserScript(
                source: "window.__WA_NATIVE_SHELL__ = true;",
                injectionTime: .atDocumentStart,
                forMainFrameOnly: true))

        webView = WKWebView(frame: .zero, configuration: config)
        webView.navigationDelegate = self
        webView.allowsBackForwardNavigationGestures = false
        webView.setValue(false, forKey: "drawsBackground")

        let frame = NSRect(x: 0, y: 0, width: 1280, height: 860)
        // A normal title bar, not fullSizeContentView: the web UI draws its own
        // header row at the very top, which the traffic lights would sit on top of.
        window = NSWindow(
            contentRect: frame,
            styleMask: [.titled, .closable, .miniaturizable, .resizable],
            backing: .buffered,
            defer: false)
        window.title = "WhatsApp Bridge"
        window.delegate = self
        window.contentView = webView
        window.minSize = NSSize(width: 900, height: 600)
        window.setFrameAutosaveName("WhatsAppBridgeMain")
        window.isReleasedWhenClosed = false
        window.center()
    }

    func show() {
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    func load() {
        webView.load(URLRequest(url: Config.baseURL))
    }

    /// Ask the React app to switch to a chat. Handled by the listener added in
    /// web/src/main.tsx; harmless if that listener is not present.
    func openChat(_ jid: String) {
        show()
        let escaped =
            jid.data(using: .utf8)?.base64EncodedString() ?? ""
        let js = """
            window.dispatchEvent(new CustomEvent('wa-open-chat', {
              detail: decodeURIComponent(escape(atob('\(escaped)')))
            }));
            """
        webView.evaluateJavaScript(js) { _, err in
            if let err { log("openChat failed: \(err.localizedDescription)") }
        }
    }

    func reload() {
        webView.reload()
    }

    // MARK: WKNavigationDelegate

    func webView(
        _ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!,
        withError error: Error
    ) {
        // Most likely the bridge is still starting. Keep retrying quietly.
        log("web view load failed: \(error.localizedDescription) — retrying")
        retryTimer?.invalidate()
        retryTimer = Timer.scheduledTimer(withTimeInterval: 2, repeats: false) { [weak self] _ in
            self?.load()
        }
    }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        retryTimer?.invalidate()
        retryTimer = nil
    }

    // MARK: WKScriptMessageHandler

    func userContentMessage(_ body: Any) {
        guard let dict = body as? [String: Any] else { return }
        if dict["type"] as? String == "visible-chat" {
            onVisibleChatChange?(dict["jid"] as? String)
        }
    }

    func userContentController(
        _ controller: WKUserContentController, didReceive message: WKScriptMessage
    ) {
        userContentMessage(message.body)
    }

    // MARK: NSWindowDelegate

    /// Hide instead of close — the bridge and its notifications stay alive.
    func windowShouldClose(_ sender: NSWindow) -> Bool {
        window.orderOut(nil)
        NSApp.setActivationPolicy(.accessory)  // drop the dock icon while hidden
        return false
    }

    func windowDidBecomeKey(_ notification: Notification) {
        onFocusChange?(true)
    }

    func windowDidResignKey(_ notification: Notification) {
        onFocusChange?(false)
    }
}
