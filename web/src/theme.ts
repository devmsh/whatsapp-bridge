// Theme switching.
//
// The palette itself lives in index.css: one block of colour tokens under
// :root[data-theme='dark']. This file only decides which value that attribute
// carries.
//
// "System" is resolved here rather than in CSS. A prefers-color-scheme copy of
// the dark block would work, but then two places decide the theme and they can
// disagree — an explicit light choice on a dark OS needs a :not() guard, and
// every future token has to be written twice. Instead the OS preference is
// watched in JS and turned into a plain "light" or "dark" stamp, so the CSS
// only ever sees one selector.

export type ThemeChoice = 'light' | 'dark' | 'system'

const KEY = 'wa.theme'
const DEFAULT: ThemeChoice = 'light'

let choice: ThemeChoice = DEFAULT
let media: MediaQueryList | null = null

function prefersDark(): boolean {
  return !!media?.matches
}

// stamp writes the resolved theme onto <html>, which is what the CSS matches.
function stamp() {
  const resolved = choice === 'system' ? (prefersDark() ? 'dark' : 'light') : choice
  document.documentElement.dataset.theme = resolved
}

// readTheme returns the saved choice. Storage can throw in a private window,
// so a failure just means "use the default" rather than breaking boot.
export function readTheme(): ThemeChoice {
  try {
    const v = localStorage.getItem(KEY)
    if (v === 'light' || v === 'dark' || v === 'system') return v
  } catch {
    /* storage blocked — fall through to the default */
  }
  return DEFAULT
}

export function setTheme(next: ThemeChoice) {
  choice = next
  try {
    localStorage.setItem(KEY, next)
  } catch {
    /* not fatal: the theme still applies for this session */
  }
  stamp()
}

// initTheme runs once at boot, before React renders, so the first paint is
// already the right colour instead of flashing light then correcting.
export function initTheme() {
  choice = readTheme()
  if (typeof window !== 'undefined' && window.matchMedia) {
    media = window.matchMedia('(prefers-color-scheme: dark)')
    // Only matters while the choice is "system", but staying subscribed is
    // cheaper than adding and removing the listener on every change.
    media.addEventListener('change', () => {
      if (choice === 'system') stamp()
    })
  }
  stamp()
}
