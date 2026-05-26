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
const DISMISSED_KEY = 'wa.notifications.banner-dismissed'

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

  const fire = useCallback((m: Message) => {
    if (typeof Notification === 'undefined') return
    if (Notification.permission !== 'granted') return
    if (!readEnabled()) return // re-read in case another tab toggled
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
      // tag dedupes — a flurry of messages from the same chat collapses
      // to one notification instead of stacking the OS tray.
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
  }, [])

  // External changes (other tabs) sync into our state via storage event.
  useEffect(() => {
    function onStorage(e: StorageEvent) {
      if (e.key === ENABLED_KEY) setEnabledState(readEnabled())
      if (e.key === DISMISSED_KEY) setDismissed(readDismissed())
    }
    window.addEventListener('storage', onStorage)
    return () => window.removeEventListener('storage', onStorage)
  }, [])

  return {
    permission,
    enabled,
    setEnabled,
    request,
    dismissed,
    dismissBanner,
    fire,
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
