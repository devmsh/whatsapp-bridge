import { useEffect } from 'react'

// ShortcutsHelp is the modal that shows every keyboard shortcut the app
// wires. Triggered by Cmd/Ctrl + / (the modern "?" gesture every
// workspace app standardised on) and listed in plain language so the
// user doesn't have to grep the source.
//
// New shortcuts go here as they're added. The list is grouped by
// surface (Anywhere / In a chat / Composer) so the user can scan to
// the slice that matters.
export function ShortcutsHelp({ onClose }: { onClose: () => void }) {
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      window.removeEventListener('keydown', onKey)
      document.body.style.overflow = prev
    }
  }, [onClose])

  return (
    <div
      role="dialog"
      aria-modal="true"
      onClick={onClose}
      className="fixed inset-0 z-[90] flex items-center justify-center bg-black/75 backdrop-blur-sm"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="flex max-h-[85vh] w-[520px] max-w-[94vw] flex-col overflow-hidden rounded-2xl border border-neutral-700 bg-neutral-900 shadow-2xl shadow-black/60"
      >
        <header className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
          <h2 className="text-sm font-semibold text-neutral-100">Keyboard shortcuts</h2>
          <button
            onClick={onClose}
            title="Close (Esc)"
            aria-label="Close"
            className="flex h-7 w-7 items-center justify-center rounded text-neutral-400 transition hover:bg-neutral-800 hover:text-neutral-200"
          >
            ✕
          </button>
        </header>

        <div className="flex-1 overflow-y-auto px-4 py-3">
          <Group title="Anywhere">
            <Row keys={['⌘', 'K']}>Focus the universal search at the top</Row>
            <Row keys={['⌘', '/']}>Open this shortcuts panel</Row>
          </Group>

          <Group title="In a chat">
            <Row keys={['⌘', 'F']}>Find in this chat</Row>
            <Row keys={['↑']} or={['↓']}>Step to the previous / next match (search bar open)</Row>
            <Row keys={['Enter']}>Jump to the next match (search bar open)</Row>
            <Row keys={['Esc']}>Close the search bar / cancel reply / leave select mode</Row>
          </Group>

          <Group title="In the composer">
            <Row keys={['Enter']}>Send the message</Row>
            <Row keys={['⇧', 'Enter']}>New line</Row>
            <Row keys={['⌘', 'B']}>
              Wrap selection in <code className="rounded bg-black/40 px-1">*bold*</code>
            </Row>
            <Row keys={['⌘', 'I']}>
              Wrap selection in <code className="rounded bg-black/40 px-1">_italic_</code>
            </Row>
            <Row keys={['⌘', '⇧', 'X']}>
              Wrap selection in <code className="rounded bg-black/40 px-1">~strike~</code>
            </Row>
            <Row keys={['⌘', 'E']}>
              Wrap selection in <code className="rounded bg-black/40 px-1">`code`</code>
            </Row>
            <Row keys={['@']}>Mention a participant (groups)</Row>
            <Row keys={['Esc']}>Cancel reply or edit, dismiss the @ picker</Row>
          </Group>

          <Group title="In the voice recorder">
            <Row keys={['Esc']}>Discard the current recording</Row>
          </Group>

          <div className="mt-3 text-[11px] text-neutral-500">
            On Windows / Linux, ⌘ is Ctrl.
          </div>
        </div>
      </div>
    </div>
  )
}

function Group({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="mb-4">
      <h3 className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-neutral-500">
        {title}
      </h3>
      <ul className="flex flex-col">{children}</ul>
    </section>
  )
}

function Row({
  keys,
  or,
  children,
}: {
  keys: string[]
  /** Optional alternate binding rendered as "↑ or ↓" — for shortcuts
   *  that have a symmetric counterpart you'd otherwise duplicate. */
  or?: string[]
  children: React.ReactNode
}) {
  return (
    <li className="flex items-center justify-between gap-3 py-1.5 text-sm text-neutral-200">
      <span className="flex-1">{children}</span>
      <span className="flex shrink-0 items-center gap-1">
        {keys.map((k, i) => (
          <Key key={i}>{k}</Key>
        ))}
        {or && (
          <>
            <span className="px-1 text-[10px] text-neutral-500">or</span>
            {or.map((k, i) => (
              <Key key={i}>{k}</Key>
            ))}
          </>
        )}
      </span>
    </li>
  )
}

function Key({ children }: { children: React.ReactNode }) {
  return (
    <kbd className="inline-flex h-6 min-w-[24px] items-center justify-center rounded border border-neutral-700 bg-neutral-800 px-1.5 text-[11px] font-medium text-neutral-200 shadow-[inset_0_-1px_0_rgba(0,0,0,0.4)]">
      {children}
    </kbd>
  )
}
