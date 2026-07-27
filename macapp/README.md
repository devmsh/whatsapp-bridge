# WhatsApp Bridge — macOS app

A native macOS app around the existing Go bridge and its React UI. Not a PWA:
a real `.app` bundle with native notification banners, a dock icon, a menu bar
item, and launch-at-login.

```
./macapp/build.sh            # build + install to ~/Applications
./macapp/build.sh --web      # rebuild web/dist first
./macapp/build.sh --no-install
```

Needs only the Command Line Tools (`swiftc`, `codesign`, `iconutil`). No Xcode.

## How it fits together

```
  WhatsApp Bridge.app
  ├─ WhatsAppBridge          Swift shell  ──┐
  └─ whatsapp-bridge-v2      Go helper      │  (fallback only)
                                            │
   ┌────────────────────────────────────────┘
   │  attach-or-spawn on 127.0.0.1:8082
   ▼
  Go bridge  ← normally already running as the LaunchAgent
   ├─ /                 React UI      → WKWebView
   └─ /api/v2/stream    SSE           → UNUserNotification banners
```

The shell does not reimplement anything. It shows the same web UI in a
`WKWebView` and reads the same SSE stream the UI reads, turning each message
into a native banner.

## Design decisions

**Attach, don't spawn.** The bridge already runs 24/7 as the
`com.devmsh.whatsapp-bridge` LaunchAgent. On launch the app probes
`/api/v2/health`; if something answers, it attaches and never starts a second
copy — two bridges would fight over the SQLite files. The bundled
`whatsapp-bridge-v2` is a fallback for when nothing is serving the port.
Quitting the app never stops a LaunchAgent-owned bridge.

**Notifications come from the native side.** WKWebView does not give web pages
a working Notification API, so the banners cannot come from the React app. The
shell keeps its own SSE connection and posts `UNUserNotification`s. The web
UI's own "Get notified" prompt is suppressed via `window.__WA_NATIVE_SHELL__`
(see `web/src/hooks/useDesktopNotifications.ts`) so there is one notification
path, not two.

**Suppression rules**, in order: own messages, deleted messages, muted chats
(the bridge owns mute state, including the working-hours auto-mute), and the
chat already open in a focused window.

**Closing the window hides it.** The app keeps running in the menu bar so
notifications keep arriving — like Mail or Messages. Quit from the menu bar
item or ⌘Q.

## Gotchas worth remembering

**Launch it as an app, never by exec'ing the binary.** Running
`WhatsApp Bridge.app/Contents/MacOS/WhatsAppBridge` directly gets
`UNErrorDomain Code=1 "Notifications are not allowed for this application"`,
because the process is not registered with LaunchServices. Use Finder or
`open -a`. This was verified, not assumed.

**The bundle must be signed.** Ad-hoc (`codesign --sign -`) is enough on this
Mac. Distributing to another machine needs a Developer ID and notarization.

**App Sandbox is deliberately off.** The bridge shells out to `node`, `codex`,
`whisper-cli` and `ffmpeg`. The bundle also passes an explicit `PATH`
(`BridgePATH` in Info.plist), mirroring the LaunchAgent, because a GUI-launched
process does not inherit a login shell's PATH.

**Paths.** A bundled process starts with `/` as its working directory, while
the codebase uses relative paths (`store/`, `agent/`, `./models/`). The shell
passes `BRIDGE_HOME`, and `internal/config` chdir's to it on startup — one
anchor point instead of rewriting every call site.

## Settings

Baked into Info.plist by `build.sh`, overridable per-machine without a rebuild:

```sh
defaults write com.devmsh.whatsapp-bridge BridgeHome -string /path/to/repo
defaults write com.devmsh.whatsapp-bridge BridgePort -int 8082
defaults write com.devmsh.whatsapp-bridge BridgePATH -string "/usr/local/bin:..."
```

## Logs

- Spawned-bridge output: `~/Library/Logs/whatsapp-bridge-app.log`
- LaunchAgent bridge output: `~/Library/Logs/whatsapp-bridge.log`
