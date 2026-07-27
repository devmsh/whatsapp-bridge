import Foundation

/// Runtime settings for the shell.
///
/// Defaults are baked into Info.plist by build.sh; a UserDefaults key of the
/// same name overrides one without a rebuild, e.g.
///
///     defaults write com.devmsh.whatsapp-bridge BridgePort -int 9000
enum Config {
    static let bundleID = "com.devmsh.whatsapp-bridge"

    /// Port the Go bridge serves its API and web UI on.
    static var port: Int {
        if let n = UserDefaults.standard.object(forKey: "BridgePort") as? Int, n > 0 {
            return n
        }
        if let n = Bundle.main.object(forInfoDictionaryKey: "BridgePort") as? Int, n > 0 {
            return n
        }
        return 8082
    }

    /// Working directory for the bridge — where store/, agent/ and .env live.
    /// Passed to the helper as BRIDGE_HOME; the Go side chdir's to it.
    static var home: String {
        if let s = UserDefaults.standard.string(forKey: "BridgeHome"), !s.isEmpty {
            return (s as NSString).expandingTildeInPath
        }
        if let s = Bundle.main.object(forInfoDictionaryKey: "BridgeHome") as? String,
            !s.isEmpty
        {
            return (s as NSString).expandingTildeInPath
        }
        return (("~/Library/Application Support/WhatsAppBridge") as NSString).expandingTildeInPath
    }

    /// launchd gives a minimal PATH, and the bridge shells out to node, codex,
    /// whisper-cli and ffmpeg. Mirror the LaunchAgent's PATH so a spawned
    /// helper can find them too.
    static var helperPATH: String {
        if let s = UserDefaults.standard.string(forKey: "BridgePATH"), !s.isEmpty { return s }
        if let s = Bundle.main.object(forInfoDictionaryKey: "BridgePATH") as? String, !s.isEmpty {
            return s
        }
        return [
            NSHomeDirectory() + "/.local/bin",
            "/opt/homebrew/opt/node@22/bin",
            "/opt/homebrew/bin",
            "/opt/homebrew/sbin",
            "/usr/bin", "/bin", "/usr/sbin", "/sbin",
        ].joined(separator: ":")
    }

    static var baseURL: URL { URL(string: "http://127.0.0.1:\(port)")! }

    static func api(_ path: String) -> URL {
        URL(string: "http://127.0.0.1:\(port)/api/v2/\(path)")!
    }

    /// The Go binary shipped inside the bundle, used only when nothing is
    /// already serving the port.
    static var helperURL: URL? {
        let url = Bundle.main.bundleURL
            .appendingPathComponent("Contents/MacOS/whatsapp-bridge-v2")
        return FileManager.default.isExecutableFile(atPath: url.path) ? url : nil
    }
}

/// Log to a file as well as stderr. When the app is launched through
/// LaunchServices (which it must be, for notifications to work) stderr goes
/// nowhere useful, so the file is the only way to see what happened.
private let logURL = URL(fileURLWithPath: NSHomeDirectory())
    .appendingPathComponent("Library/Logs/whatsapp-bridge-app.log")
private let logQueue = DispatchQueue(label: "bridge.log")

func log(_ message: String) {
    let stamp = ISO8601DateFormatter().string(from: Date())
    let line = "[\(stamp)] \(message)\n"
    FileHandle.standardError.write(line.data(using: .utf8)!)
    logQueue.async {
        guard let data = line.data(using: .utf8) else { return }
        if let handle = try? FileHandle(forWritingTo: logURL) {
            defer { try? handle.close() }
            handle.seekToEndOfFile()
            handle.write(data)
        } else {
            try? data.write(to: logURL)
        }
    }
}
