import Foundation

/// Owns the Go bridge process.
///
/// The bridge normally runs 24/7 as the `com.devmsh.whatsapp-bridge` LaunchAgent,
/// so the usual path is *attach*: probe the port, find it serving, and leave it
/// alone. Only when nothing answers do we spawn the helper bundled in the .app.
/// Two bridges must never run at once — they would fight over the SQLite files.
final class BridgeController {
    enum State: Equatable {
        case probing
        case attached  // someone else (the LaunchAgent) owns the process
        case spawned  // we started it
        case failed(String)
    }

    private(set) var state: State = .probing
    private var child: Process?
    private let queue = DispatchQueue(label: "bridge.controller")

    var onStateChange: ((State) -> Void)?

    /// Probes the port; spawns the bundled helper only if nothing answers.
    /// Calls `completion` once the bridge is reachable, or on giving up.
    func start(completion: @escaping (Bool) -> Void) {
        queue.async {
            if self.probe(timeout: 1.5) {
                log("bridge already running on port \(Config.port) — attaching")
                self.set(.attached)
                completion(true)
                return
            }

            guard let helper = Config.helperURL else {
                let msg =
                    "no bridge on port \(Config.port) and no bundled helper to start"
                log(msg)
                self.set(.failed(msg))
                completion(false)
                return
            }

            log("no bridge on port \(Config.port) — spawning \(helper.path)")
            do {
                try self.spawn(helper)
            } catch {
                let msg = "failed to spawn bridge: \(error)"
                log(msg)
                self.set(.failed(msg))
                completion(false)
                return
            }

            // The bridge opens its listener before connecting to WhatsApp, so
            // this normally succeeds in well under a second.
            if self.waitUntilReachable(deadline: 30) {
                self.set(.spawned)
                completion(true)
            } else {
                let msg = "bridge did not come up within 30s"
                log(msg)
                self.set(.failed(msg))
                completion(false)
            }
        }
    }

    private func set(_ s: State) {
        state = s
        DispatchQueue.main.async { self.onStateChange?(s) }
    }

    private func spawn(_ helper: URL) throws {
        let home = Config.home
        try FileManager.default.createDirectory(
            atPath: home, withIntermediateDirectories: true)

        var env = ProcessInfo.processInfo.environment
        env["PATH"] = Config.helperPATH
        env["BRIDGE_HOME"] = home
        env["BRIDGE_PORT"] = String(Config.port)

        let p = Process()
        p.executableURL = helper
        p.environment = env
        // Belt and braces: the Go side also chdir's to BRIDGE_HOME.
        p.currentDirectoryURL = URL(fileURLWithPath: home)

        let logURL = URL(fileURLWithPath: NSHomeDirectory())
            .appendingPathComponent("Library/Logs/whatsapp-bridge-app.log")
        FileManager.default.createFile(atPath: logURL.path, contents: nil)
        if let handle = try? FileHandle(forWritingTo: logURL) {
            handle.seekToEndOfFile()
            p.standardOutput = handle
            p.standardError = handle
        }

        p.terminationHandler = { [weak self] proc in
            log("bridge exited with status \(proc.terminationStatus)")
            self?.child = nil
        }

        try p.run()
        child = p
    }

    /// GET /api/v2/health — true only on a 2xx.
    func probe(timeout: TimeInterval) -> Bool {
        var req = URLRequest(url: Config.api("health"))
        req.timeoutInterval = timeout
        req.httpMethod = "GET"

        let sem = DispatchSemaphore(value: 0)
        var ok = false
        URLSession.shared.dataTask(with: req) { _, resp, _ in
            if let http = resp as? HTTPURLResponse, (200..<300).contains(http.statusCode) {
                ok = true
            }
            sem.signal()
        }.resume()
        _ = sem.wait(timeout: .now() + timeout + 1)
        return ok
    }

    private func waitUntilReachable(deadline seconds: TimeInterval) -> Bool {
        let end = Date().addingTimeInterval(seconds)
        while Date() < end {
            if probe(timeout: 1) { return true }
            Thread.sleep(forTimeInterval: 0.5)
        }
        return false
    }

    /// Only ever stops a process we started. A LaunchAgent-owned bridge must
    /// keep running after the window closes — that is the whole point of it.
    func stopIfOwned() {
        guard let p = child, p.isRunning else { return }
        log("terminating spawned bridge")
        p.terminate()
    }
}
