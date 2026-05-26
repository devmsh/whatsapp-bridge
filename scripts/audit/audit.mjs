#!/usr/bin/env node
/* audit.mjs — drive the WA-Bridge UI via Playwright + grab screenshots.
 *
 * Two connect modes:
 *   --connect   → attach to a Chrome already running on
 *                  --remote-debugging-port=9222 (uses your real session)
 *   default     → launch a fresh chromium (no auth needed for the bridge UI
 *                  on localhost, but you'll need to re-pair WA in it)
 *
 * Targets a list of routes / overlays — each one shot to PNG so the calling
 * agent can review them visually.
 *
 * Usage:
 *   node audit.mjs                       # headed, fresh chromium
 *   node audit.mjs --connect             # attach to your real Chrome
 *   node audit.mjs --connect --only=header,emoji
 *   node audit.mjs --base=https://whatsapp-bridge.test
 */
import { chromium } from 'playwright'
import { mkdir } from 'node:fs/promises'
import { resolve } from 'node:path'

const argv = Object.fromEntries(
  process.argv.slice(2).map((a) => {
    const [k, v] = a.startsWith('--') ? a.slice(2).split('=') : [a, true]
    return [k, v === undefined ? true : v]
  }),
)
const BASE = argv.base || 'https://whatsapp-bridge.test'
const CONNECT = !!argv.connect
const OUT = resolve(process.cwd(), 'audit-out')
const only = typeof argv.only === 'string' ? new Set(argv.only.split(',')) : null

// Each shot describes one surface to capture. `setup` runs after the page is
// at BASE — open a modal, click a chat, etc. Keep them defensive (`?.()`
// optional clicks) so a missing element doesn't crash the whole run.
const shots = [
  {
    id: 'home',
    label: 'App home (chat list + empty thread)',
    async setup() {},
  },
  {
    id: 'filter-pills',
    label: 'Chat-list filter pills (All / Unread / Groups / Mentions / Drafts)',
    async setup(page) {
      // Just home — filters are already visible. Hover the Unread pill so we
      // capture the hover state too.
      await page.locator('button:has-text("Unread")').first().hover().catch(() => {})
    },
  },
  {
    id: 'header-cluster',
    label: 'Top-right header IconButton cluster',
    async setup(page, ctx) {
      ctx.clip = await page
        .locator('header')
        .first()
        .boundingBox()
        .catch(() => null)
    },
  },
  {
    id: 'privacy-modal',
    label: 'Privacy settings modal',
    async setup(page) {
      await page.getByTitle('Privacy', { exact: true }).click({ timeout: 4000 })
      await page.waitForSelector('text=Privacy', { timeout: 4000 }).catch(() => {})
      await page.waitForTimeout(400)
    },
  },
  {
    id: 'self-profile',
    label: 'Your profile modal',
    async setup(page) {
      await page.getByTitle('Your profile').click({ timeout: 4000 })
      await page.waitForTimeout(400)
    },
  },
  {
    id: 'status-panel',
    label: 'Status updates panel',
    async setup(page) {
      await page.getByTitle('Status updates').click({ timeout: 4000 })
      await page.waitForTimeout(400)
    },
  },
  {
    id: 'channels-panel',
    label: 'Channels panel',
    async setup(page) {
      await page.getByTitle('Channels').click({ timeout: 4000 })
      await page.waitForTimeout(400)
    },
  },
  {
    id: 'first-chat',
    label: 'First chat thread open (header + bubbles + composer)',
    async setup(page) {
      // Click the first chat row in the chat list.
      await page.locator('button:has(img), button:has(div[class*="ChatAvatar"])')
        .first()
        .click({ timeout: 4000 })
        .catch(() => {})
      await page.waitForTimeout(600)
    },
  },
  {
    id: 'archived-view',
    label: 'Archived chats view',
    async setup(page) {
      // Click the "Archived" header (lives at the top of the chat list
      // when there's at least one archived chat).
      await page.locator('button:has-text("Archived")').first().click({ timeout: 4000 }).catch(() => {})
      await page.waitForTimeout(400)
    },
  },
  {
    id: 'group-info',
    label: 'Group info modal (first group chat)',
    async setup(page) {
      // Find a chat that looks like a group via the chat-list. Then click
      // the header title to open Group info.
      const group = page.locator('button:has-text("members")').first()
      if (await group.count()) {
        await group.click({ timeout: 4000 }).catch(() => {})
      }
      await page.waitForTimeout(500)
      await page.locator('header button:has-text("Group info"), header [title="Group members + admins"], header button:has(span)').first().click({ timeout: 2000 }).catch(() => {})
      await page.waitForTimeout(500)
    },
  },
]

await mkdir(OUT, { recursive: true })

const browser = CONNECT
  ? await chromium.connectOverCDP('http://localhost:9222')
  : await chromium.launch({ headless: true })

const context = CONNECT ? browser.contexts()[0] : await browser.newContext({ viewport: { width: 1400, height: 900 } })

let page
if (CONNECT) {
  // Try to reuse an existing tab on the bridge; otherwise open one.
  const pages = context.pages()
  page = pages.find((p) => p.url().includes('whatsapp-bridge.test')) ||
         pages.find((p) => p.url().includes('localhost:8082')) ||
         pages[0] ||
         await context.newPage()
} else {
  page = await context.newPage()
}

console.log(`→ ${CONNECT ? 'attached to' : 'launched'} Chrome, page: ${page.url() || '(blank)'}`)

if (!page.url().startsWith(BASE)) {
  console.log(`→ navigating to ${BASE}`)
  await page.goto(BASE, { waitUntil: 'networkidle', timeout: 15000 }).catch((e) => {
    console.warn('  goto warning:', e.message)
  })
} else {
  await page.waitForLoadState('networkidle', { timeout: 5000 }).catch(() => {})
}

await page.waitForTimeout(500)

for (const shot of shots) {
  if (only && !only.has(shot.id)) continue
  const file = resolve(OUT, shot.id + '.png')
  process.stdout.write(`→ ${shot.id.padEnd(20)} `)
  try {
    // Reset to home with a fresh navigation so shots are fully independent.
    // Cheap on localhost; spares us the "did Esc close the overlay" guessing.
    await page.goto(BASE, { waitUntil: 'domcontentloaded', timeout: 15000 })
    await page.waitForLoadState('networkidle', { timeout: 5000 }).catch(() => {})
    await page.waitForTimeout(400)
    const ctx = {}
    await shot.setup(page, ctx)
    await page.waitForTimeout(300)
    await page.screenshot({ path: file, fullPage: false, clip: ctx.clip || undefined })
    console.log('✓', shot.label)
  } catch (e) {
    console.log('✗', e.message)
  }
}

if (!CONNECT) await browser.close()
console.log(`\nDone → ${OUT}`)
