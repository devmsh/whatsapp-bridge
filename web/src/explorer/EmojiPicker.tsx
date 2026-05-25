import { useEffect, useMemo, useRef, useState } from 'react'

// EmojiPicker is the smiley-button popover the Composer hangs above the
// textarea. Lightweight on purpose — no external library, no full Unicode
// table, no skin-tones (yet). Eight tabs of the most-used emoji match what
// the overwhelming majority of WA users actually pick. Recents persists to
// localStorage so the next session opens with your last 24 right there.
//
// Behavior:
//   - Click tab to switch category; the grid swaps in place.
//   - Click emoji to insert at the textarea's caret (handled by the parent
//     via the onPick callback — we don't reach into the textarea).
//   - Click outside the popover closes it.
//   - Esc closes it.
//
// The popover anchors to the parent's positioned wrapper (the Composer
// renders us inside a `relative` slot), so we never escape the chat column.
export function EmojiPicker({
  onPick,
  onClose,
}: {
  onPick: (emoji: string) => void
  onClose: () => void
}) {
  const wrapRef = useRef<HTMLDivElement>(null)
  const [tab, setTab] = useState<string>(() => (loadRecents().length > 0 ? 'recent' : 'smileys'))
  const [recents, setRecents] = useState<string[]>(loadRecents)

  // Close on outside click + Esc — same pair the rest of our popovers use.
  useEffect(() => {
    function onDown(e: MouseEvent) {
      if (!wrapRef.current?.contains(e.target as Node)) onClose()
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    // mousedown beats click so the textarea doesn't steal focus mid-click.
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [onClose])

  function pick(emoji: string) {
    onPick(emoji)
    setRecents((prev) => {
      const next = [emoji, ...prev.filter((e) => e !== emoji)].slice(0, 24)
      try {
        localStorage.setItem(RECENTS_KEY, JSON.stringify(next))
      } catch {}
      return next
    })
  }

  const visible = useMemo<string[]>(() => {
    if (tab === 'recent') return recents
    return CATEGORIES.find((c) => c.id === tab)?.emoji ?? []
  }, [tab, recents])

  return (
    <div
      ref={wrapRef}
      role="dialog"
      aria-label="Emoji picker"
      className="absolute bottom-full left-0 z-30 mb-2 flex h-72 w-80 flex-col overflow-hidden rounded-2xl border border-neutral-700 bg-neutral-900 shadow-2xl shadow-black/60"
    >
      {/* Grid takes the full height minus the tab bar; native scroll for
          overflow so a tall category (Smileys) doesn't blow up the popover. */}
      <div className="grid flex-1 grid-cols-8 gap-0.5 overflow-y-auto p-2">
        {visible.length === 0 ? (
          <div className="col-span-8 flex items-center justify-center text-xs text-neutral-600">
            {tab === 'recent' ? 'No recents yet — pick one below' : 'Empty'}
          </div>
        ) : (
          visible.map((e, i) => (
            <button
              key={e + i}
              onClick={() => pick(e)}
              className="flex h-8 w-8 items-center justify-center rounded text-xl leading-none transition hover:bg-neutral-800"
              aria-label={`Insert ${e}`}
            >
              {e}
            </button>
          ))
        )}
      </div>
      <div className="flex shrink-0 items-center justify-between gap-0.5 border-t border-neutral-800 px-1 py-1">
        <TabButton id="recent" current={tab} onPick={setTab} disabled={recents.length === 0}>
          🕘
        </TabButton>
        {CATEGORIES.map((c) => (
          <TabButton key={c.id} id={c.id} current={tab} onPick={setTab}>
            {c.tab}
          </TabButton>
        ))}
      </div>
    </div>
  )
}

function TabButton({
  id,
  current,
  onPick,
  disabled,
  children,
}: {
  id: string
  current: string
  onPick: (id: string) => void
  disabled?: boolean
  children: React.ReactNode
}) {
  const active = current === id
  return (
    <button
      onClick={() => onPick(id)}
      disabled={disabled}
      className={
        'flex h-7 w-7 items-center justify-center rounded text-base transition ' +
        (active
          ? 'bg-neutral-800 text-neutral-100'
          : 'text-neutral-400 hover:bg-neutral-800/60 hover:text-neutral-200 disabled:cursor-not-allowed disabled:opacity-30')
      }
    >
      {children}
    </button>
  )
}

const RECENTS_KEY = 'wa.emoji-recents'

function loadRecents(): string[] {
  try {
    const raw = localStorage.getItem(RECENTS_KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw)
    if (Array.isArray(parsed)) return parsed.filter((s) => typeof s === 'string').slice(0, 24)
  } catch {}
  return []
}

// Curated emoji lists per category. Not exhaustive — these are the high-
// frequency picks that cover ~95% of real WA usage. Tab glyphs match WA's
// category icons so the bar reads at a glance.
type Category = { id: string; tab: string; emoji: string[] }
const CATEGORIES: Category[] = [
  {
    id: 'smileys',
    tab: '😀',
    emoji: [
      '😀','😃','😄','😁','😆','😅','😂','🤣','😊','😇',
      '🙂','🙃','😉','😌','😍','🥰','😘','😗','😙','😚',
      '😋','😛','😝','😜','🤪','🤨','🧐','🤓','😎','🤩',
      '🥳','😏','😒','😞','😔','😟','😕','🙁','☹️','😣',
      '😖','😫','😩','🥺','😢','😭','😤','😠','😡','🤬',
      '🤯','😳','🥵','🥶','😱','😨','😰','😥','😓','🤗',
      '🤔','🤭','🤫','🤥','😶','😐','😑','😬','🙄','😯',
      '😦','😧','😮','😲','🥱','😴','🤤','😪','😵','🤐',
      '🥴','🤢','🤮','🤧','😷','🤒','🤕','🤑','🤠','💩',
      '👻','💀','👽','🤖','🎃','😺','😸','😹','😻','😼',
    ],
  },
  {
    id: 'hearts',
    tab: '❤️',
    emoji: [
      '❤️','🧡','💛','💚','💙','💜','🤎','🖤','🤍','💔',
      '❣️','💕','💞','💓','💗','💖','💘','💝','💟','♥️',
    ],
  },
  {
    id: 'hands',
    tab: '👍',
    emoji: [
      '👍','👎','👌','🤌','🤏','✌️','🤞','🤟','🤘','🤙',
      '👈','👉','👆','👇','☝️','👋','🤚','🖐️','✋','🖖',
      '👏','🙌','🤲','🤝','🙏','💪','🦾','🖕','🫶','🫰',
      '🫳','🫴','🫵','🫱','🫲','✊','👊','🤛','🤜','💅',
    ],
  },
  {
    id: 'animals',
    tab: '🐾',
    emoji: [
      '🐶','🐱','🐭','🐹','🐰','🦊','🐻','🐼','🐨','🐯',
      '🦁','🐮','🐷','🐸','🐵','🙈','🙉','🙊','🐒','🐔',
      '🐧','🐦','🐤','🦆','🦅','🦉','🐴','🦄','🐝','🐛',
      '🦋','🐌','🐞','🐢','🐍','🐙','🦑','🦐','🦀','🐠',
      '🐟','🐬','🐳','🐋','🦈','🐊','🐅','🦓','🦒','🦘',
      '🦌','🐕','🐈','🦔','🦝','🐿','🦥','🦦','🦨','🐇',
    ],
  },
  {
    id: 'food',
    tab: '🍔',
    emoji: [
      '🍏','🍎','🍐','🍊','🍋','🍌','🍉','🍇','🍓','🫐',
      '🍒','🍑','🥭','🍍','🥥','🥝','🍅','🥑','🌶','🌽',
      '🥕','🥔','🍠','🥐','🍞','🧀','🥚','🍳','🥞','🥓',
      '🍗','🍖','🌭','🍔','🍟','🍕','🥪','🌮','🌯','🥗',
      '🍝','🍜','🍣','🍱','🍙','🍚','🍦','🍩','🍪','🍰',
      '🎂','🍫','🍬','🍭','🍯','🥛','☕️','🍵','🍺','🍷',
      '🍸','🍹','🍾','🥂','🥃',
    ],
  },
  {
    id: 'activity',
    tab: '⚽',
    emoji: [
      '⚽️','🏀','🏈','⚾️','🥎','🎾','🏐','🏉','🥏','🎱',
      '🏓','🏸','🥅','🏒','🏑','🏏','⛳️','🪁','🏹','🎣',
      '🥊','🥋','⛸','🎿','🏂','🏋️','🤼','🤸','⛹️','🤺',
      '🤾','🏌️','🏇','🧘','🏄','🏊','🚣','🧗','🚵','🚴',
      '🏆','🥇','🥈','🥉','🏅','🎯','🎳','🎮','🎰','🧩',
      '🎲','🎭','🎨','🎬','🎤','🎧','🎼','🎹','🥁','🎷',
      '🎺','🎸','🎻',
    ],
  },
  {
    id: 'travel',
    tab: '🚗',
    emoji: [
      '🚗','🚕','🚙','🚌','🚎','🏎','🚓','🚑','🚒','🚐',
      '🛻','🚚','🚛','🚜','🛴','🚲','🛵','🏍','🚨','🚔',
      '✈️','🛫','🛬','🛩','💺','🚀','🛸','🚁','⛵️','🚤',
      '⛴','🚢','⚓️','⛽️','🚧','🚦','🚥','🗺','🗿','🗽',
      '🗼','🏰','🎡','🎢','⛲️','🏖','🏝','🌋','⛰','🏔',
      '🏕','⛺️','🏠','🏡','🏢','🏥','🏨','🏪','🏫','⛪️',
    ],
  },
  {
    id: 'objects',
    tab: '💡',
    emoji: [
      '💻','⌨️','🖥','🖨','🖱','💾','📀','💿','📷','📹',
      '📼','💡','🔦','📔','📕','📖','📗','📘','📙','📚',
      '📰','🔖','💰','💵','💳','✉️','📧','📦','📨','📤',
      '📥','📝','✏️','✒️','📁','📂','📅','📈','📉','📊',
      '📌','📍','📎','✂️','🔒','🔓','🔑','🔨','⚙️','⚖️',
      '🔗','⛓','🧰','🧲','🪜','🔭','🔬','🧪','🧬','🧫',
      '⚗️','🩺','💉','💊','🚿','🛁','🪑','🛏','🛌','🛋',
      '🎁','🎈','🎉','🎊','🎀','🎗','🏷','🔔','🎵','🎶',
    ],
  },
]
