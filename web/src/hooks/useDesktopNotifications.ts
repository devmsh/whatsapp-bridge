import { useCallback, useEffect, useRef, useState } from 'react'
import type { Chat, Message } from '../api'
import { chatTitle, isGroup, senderTitle } from '../explorer/format'

// useDesktopNotifications wires the browser's native Notification API
// into the live-message stream. The hook holds three pieces of state
// that survive across renders:
//
//   - permission: the current Notification.permission, mirrored so
//     React renders react to "default → granted → denied" transitions.
//   - enabled: a user preference (localStorage) — even with permission
//     granted, the user can mute desktop alerts without re-prompting.
//   - dismissed: whether the user closed the "Enable notifications"
//     banner. Persists so the banner doesn't keep nagging.
//
// fire(msg) is the one-shot helper Explorer calls from its SSE
// onmessage. It applies WA-style gating before showing anything:
//   • own messages are silent (you already know)
//   • muted chats are silent
//   • already-focused chat is silent (you're reading it live)
//   • window-visible but a different chat is open: still fires —
//     the user has lots of chats; a ping on a different one is the
//     whole point of notifications.
const ENABLED_KEY = 'wa.notifications.enabled'
const SOUND_KEY = 'wa.notifications.sound'
const DISMISSED_KEY = 'wa.notifications.banner-dismissed'
const DND_UNTIL_KEY = 'wa.notifications.dnd-until'

export type Permission = 'default' | 'granted' | 'denied' | 'unsupported'

export function useDesktopNotifications({
  chats,
  nameMap,
  selectedJid,
  onOpenChat,
}: {
  chats: Chat[]
  nameMap: Map<string, string>
  /** The chat currently open in the main pane. Null = no chat open. */
  selectedJid: string | null
  /** Called when the user clicks a notification — Explorer wires this to
   *  focus the window + open the chat the notification was for. */
  onOpenChat: (jid: string) => void
}) {
  const [permission, setPermission] = useState<Permission>(currentPermission)
  const [enabled, setEnabledState] = useState<boolean>(() => readEnabled())
  const [dismissed, setDismissed] = useState<boolean>(() => readDismissed())
  // DND end time in epoch seconds. 0 = off (default). When > Date.now()/1000
  // both visual notifications and the audio ding are suppressed. Re-evaluated
  // on every fire() so a timer ticking past the deadline silently restores
  // notifications without any explicit re-render.
  const [dndUntil, setDndUntilState] = useState<number>(() => readDndUntil())

  // Keep refs in sync so the SSE callback (created outside React's commit
  // phase) always sees the latest values without re-binding.
  const chatsRef = useRef(chats)
  const nameMapRef = useRef(nameMap)
  const selectedRef = useRef<string | null>(selectedJid)
  const onOpenRef = useRef(onOpenChat)
  useEffect(() => { chatsRef.current = chats }, [chats])
  useEffect(() => { nameMapRef.current = nameMap }, [nameMap])
  useEffect(() => { selectedRef.current = selectedJid }, [selectedJid])
  useEffect(() => { onOpenRef.current = onOpenChat }, [onOpenChat])

  const request = useCallback(async () => {
    if (typeof Notification === 'undefined') return
    try {
      const p = await Notification.requestPermission()
      setPermission(p as Permission)
      if (p === 'granted') setEnabled(true)
    } catch {
      // Some browsers throw if permission was already decided — re-read.
      setPermission(currentPermission())
    }
  }, [])

  const setEnabled = useCallback((v: boolean) => {
    setEnabledState(v)
    try { localStorage.setItem(ENABLED_KEY, v ? 'true' : 'false') } catch {}
  }, [])

  const dismissBanner = useCallback(() => {
    setDismissed(true)
    try { localStorage.setItem(DISMISSED_KEY, 'true') } catch {}
  }, [])

  // Set DND end time (epoch seconds). 0 = off. The hook tick at the bottom
  // of this file polls the deadline so the badge clears the moment time
  // ticks past it, without needing the caller to refresh.
  const setDndUntil = useCallback((endTs: number) => {
    setDndUntilState(endTs)
    try {
      if (endTs > 0) localStorage.setItem(DND_UNTIL_KEY, String(endTs))
      else localStorage.removeItem(DND_UNTIL_KEY)
    } catch {}
  }, [])

  const fire = useCallback((m: Message) => {
    if (typeof Notification === 'undefined') return
    if (Notification.permission !== 'granted') return
    if (!readEnabled()) return // re-read in case another tab toggled
    // Do Not Disturb: silent until the deadline passes.
    if (Math.floor(Date.now() / 1000) < readDndUntil()) return
    // Own messages: silent. You already know.
    if (m.is_from_me) return
    // Muted: silent. Same as WA.
    const chat = chatsRef.current.find((c) => c.jid === m.chat_jid)
    if (chat?.is_muted) return
    // The chat is open and the user is looking at it: skip — they're
    // reading it live. Otherwise (different chat OR window hidden) fire.
    const isOpenAndFocused =
      selectedRef.current === m.chat_jid &&
      typeof document !== 'undefined' &&
      document.visibilityState === 'visible'
    if (isOpenAndFocused) return

    const groupChat = isGroup(m.chat_jid)
    const sender = senderTitle(m.sender, m.sender_name, m.push_name, nameMapRef.current)
    const title = chat ? chatTitle(chat, nameMapRef.current) : sender || 'New message'
    const body = previewBody(m, groupChat ? sender : undefined)
    const icon = '/api/v2/avatars/' + encodeURIComponent(m.chat_jid)
    try {
      const n = new Notification(title, {
        body,
        icon,
        // tag dedupes — a flurry of messages from the same chat collapses
        // to one entry in the OS tray instead of stacking.
        tag: 'wa-chat:' + m.chat_jid,
      })
      n.onclick = () => {
        try { window.focus() } catch {}
        onOpenRef.current(m.chat_jid)
        n.close()
      }
    } catch {
      // Some browsers throw if too many notifications stack — fail silent.
    }
    // Soft ding when the window is hidden (the user can't see the
    // notification flash on-screen, so an audio cue helps). When the
    // window is visible we stay silent — the OS notification itself is
    // enough, and audio while you're already at the keyboard is jarring.
    if (readSound() && typeof document !== 'undefined' && document.visibilityState !== 'visible') {
      playDing()
    }
  }, [])

  // External changes (other tabs) sync into our state via storage event.
  useEffect(() => {
    function onStorage(e: StorageEvent) {
      if (e.key === ENABLED_KEY) setEnabledState(readEnabled())
      if (e.key === DISMISSED_KEY) setDismissed(readDismissed())
      if (e.key === DND_UNTIL_KEY) setDndUntilState(readDndUntil())
    }
    window.addEventListener('storage', onStorage)
    return () => window.removeEventListener('storage', onStorage)
  }, [])

  // Tick once a minute so the badge clears when the DND deadline passes
  // (and the chrome around it reflects the new state). Cheap; we only run
  // it while dndUntil > 0 so dormant sessions don't pay.
  useEffect(() => {
    if (dndUntil <= 0) return
    const id = window.setInterval(() => {
      const now = Math.floor(Date.now() / 1000)
      if (now >= dndUntil) setDndUntil(0)
    }, 30_000)
    return () => window.clearInterval(id)
  }, [dndUntil, setDndUntil])

  return {
    permission,
    enabled,
    setEnabled,
    request,
    dismissed,
    dismissBanner,
    dndUntil,
    setDndUntil,
    fire,
  }
}

function readDndUntil(): number {
  try {
    const v = localStorage.getItem(DND_UNTIL_KEY)
    const n = v ? parseInt(v, 10) : 0
    if (!isFinite(n) || n <= 0) return 0
    // Clamp out values that have already expired so callers don't see
    // a stale "DND on" badge after sleeping past the deadline.
    return n > Math.floor(Date.now() / 1000) ? n : 0
  } catch {
    return 0
  }
}

function currentPermission(): Permission {
  if (typeof Notification === 'undefined') return 'unsupported'
  return Notification.permission as Permission
}

function readEnabled(): boolean {
  try {
    const v = localStorage.getItem(ENABLED_KEY)
    return v === null ? true : v === 'true'
  } catch {
    return true
  }
}

function readSound(): boolean {
  try {
    const v = localStorage.getItem(SOUND_KEY)
    return v === null ? true : v === 'true'
  } catch {
    return true
  }
}

// playDing synthesises a brief two-tone chime via WebAudio — no asset to
// ship, no autoplay block (we're already past a user-gesture by the time
// permission was granted, and modern browsers allow synthesis-only nodes).
// Soft, short envelope so it never overlaps with itself in busy chats.
let _audioCtx: AudioContext | null = null
function playDing() {
  if (typeof window === 'undefined') return
  try {
    const AC: typeof AudioContext =
      window.AudioContext ||
      (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext
    if (!_audioCtx) _audioCtx = new AC()
    const ac = _audioCtx
    const now = ac.currentTime
    // Two notes ~ a perfect fifth (E5 → B5) — the WA "delivered" feel.
    const notes = [
      { freq: 659.25, start: 0, dur: 0.18 }, // E5
      { freq: 987.77, start: 0.07, dur: 0.18 }, // B5
    ]
    for (const n of notes) {
      const osc = ac.createOscillator()
      const gain = ac.createGain()
      osc.type = 'sine'
      osc.frequency.value = n.freq
      osc.connect(gain)
      gain.connect(ac.destination)
      // Quick attack + exponential decay → a pleasant short ping with no
      // pops at start/end.
      gain.gain.setValueAtTime(0, now + n.start)
      gain.gain.linearRampToValueAtTime(0.18, now + n.start + 0.01)
      gain.gain.exponentialRampToValueAtTime(0.0001, now + n.start + n.dur)
      osc.start(now + n.start)
      osc.stop(now + n.start + n.dur)
    }
  } catch {
    // If the browser blocks audio creation (Safari pre-gesture, etc.),
    // we just skip the ding — the visual notification is enough.
  }
}

function readDismissed(): boolean {
  try {
    return localStorage.getItem(DISMISSED_KEY) === 'true'
  } catch {
    return false
  }
}

// previewBody trims the message to a one-line notification body. For
// groups we prefix the sender name so a notification reads as
// "Alice: hey can you check this".
function previewBody(m: Message, senderName?: string): string {
  let text =
    m.content ||
    m.media_caption ||
    (m.media_type === 'image'
      ? '📷 Photo'
      : m.media_type === 'video'
        ? '🎥 Video'
        : m.media_type === 'voice_note'
          ? '🎤 Voice message'
          : m.media_type === 'audio'
            ? '🎵 Audio'
            : m.media_type === 'document'
              ? '📄 Document'
              : m.media_type === 'sticker'
                ? '🌟 Sticker'
                : 'New message')
  if (text.length > 140) text = text.slice(0, 137) + '…'
  return senderName ? `${senderName}: ${text}` : text
}
