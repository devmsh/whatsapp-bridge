import { useMemo, useState } from 'react'
import type { Contact, Tag } from '../api'
import { jidUser } from './format'
import { TagChips, TagEditor } from './Tags'
import { ChatAvatar } from './ChatAvatar'

function contactName(c: Contact): string {
  // Display priority matches WA mobile: a WA-verified business name and the
  // plain business name both beat a self-set push name.
  return c.name || c.verified_name || c.business_name || c.push_name || '+' + (c.phone || jidUser(c.jid))
}

// messageCount returns how many messages we have with this contact, checking the
// JIDs a DM might be stored under (jid, phone, lid).
function messageCount(c: Contact, activity: Map<string, number>): number {
  const candidates = [c.jid, c.phone ? c.phone + '@s.whatsapp.net' : '', c.lid || '']
  let max = 0
  for (const j of candidates) {
    if (j) max = Math.max(max, activity.get(j) || 0)
  }
  return max
}

// ContactsPanel lists contacts ordered by how much you talk to them (message
// count), then by name. Clicking one opens their DM.
export function ContactsPanel({
  contacts,
  activity,
  allTags,
  contactTags,
  onTagsChanged,
  onOpen,
}: {
  contacts: Contact[]
  activity: Map<string, number>
  allTags: Tag[]
  contactTags: Record<string, Tag[]>
  onTagsChanged: () => void
  onOpen: (c: Contact) => void
}) {
  const [q, setQ] = useState('')

  const rows = useMemo(() => {
    const enriched = contacts
      .map((c) => ({ c, title: contactName(c), count: messageCount(c, activity) }))
      .sort((a, b) => b.count - a.count || a.title.localeCompare(b.title))
    const needle = q.trim().toLowerCase()
    if (!needle) return enriched
    return enriched.filter(
      (r) =>
        r.title.toLowerCase().includes(needle) ||
        (r.c.phone || '').includes(needle) ||
        r.c.jid.toLowerCase().includes(needle),
    )
  }, [contacts, activity, q])

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="p-2">
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search contacts"
          className="w-full rounded-lg border border-neutral-800 bg-neutral-900 px-3 py-2 text-sm outline-none placeholder:text-neutral-600 focus:border-neutral-600"
        />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="px-3 py-1 text-[11px] text-neutral-600">
          {rows.length} contacts · most contacted first
        </div>
        {rows.map(({ c, title, count }) => {
          const tags = contactTags[c.jid] || []
          return (
            <div
              key={c.jid}
              className="group flex w-full items-center gap-3 px-3 py-2 transition hover:bg-neutral-900"
            >
              <button onClick={() => onOpen(c)} className="flex min-w-0 flex-1 items-center gap-3 text-left">
                <ChatAvatar jid={c.jid} title={title} size={36} />
                <div className="min-w-0 flex-1">
                  <div dir="auto" className="truncate text-sm">
                    {title}
                  </div>
                  <div className="truncate text-xs text-neutral-500">
                    {c.phone ? '+' + c.phone : jidUser(c.jid)}
                    {c.is_business ? ' · Business' : ''}
                  </div>
                  {tags.length > 0 && (
                    <div className="mt-1">
                      <TagChips tags={tags} />
                    </div>
                  )}
                </div>
              </button>
              {count > 0 && (
                <span className="shrink-0 rounded-full bg-neutral-800 px-2 py-0.5 text-[11px] text-neutral-400">
                  {count.toLocaleString()}
                </span>
              )}
              <TagEditor jid={c.jid} tags={tags} allTags={allTags} onChanged={onTagsChanged} />
            </div>
          )
        })}
      </div>
    </div>
  )
}
