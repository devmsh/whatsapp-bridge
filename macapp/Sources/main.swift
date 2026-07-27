import AppKit

// Single instance: a second copy would open a second stream and double every
// notification banner. Hand off to the running one instead.
let running = NSWorkspace.shared.runningApplications.filter {
    $0.bundleIdentifier == Config.bundleID
}
if running.count > 1, let other = running.first(where: { !$0.isActive }) {
    other.activate(options: [.activateAllWindows])
    exit(0)
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.setActivationPolicy(.regular)
app.run()
