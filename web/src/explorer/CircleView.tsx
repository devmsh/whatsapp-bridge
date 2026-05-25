import { useEffect, useMemo, useState } from 'react'
import {
  api,
  type Chat,
  type Circle,
  type CircleContact,
  type CircleDetail,
  type CircleMember,
  type Contact,
  type Group,
  type MemberSuggestion,
  type MemberType,
  type Tag,
} from '../api'
import { initial, jidUser, previewText } from './format'
import { CIRCLE_COLORS, pickColor } from './colors'
import { CircleSettings } from './CircleSettings'
import { TagChips, TagEditor } from './Tags'
import { ProfileCard } from './ProfileCard'
import { ExtractionsModal } from './Extractions'

// Kind is the add-members picker mode; "all" searches groups + contacts together.
type Kind = 'all' | MemberType
type Item = { ref: string; label: string; sub: string; type: MemberType }

// CircleView shows one circle's members (nested circles, groups, contacts) with
// management: rename, delete, add and remove members.
export function CircleView({
  circleId,
  circles,
  chats,
  contacts,
  groups,
  nameMap,
  allTags,
  onTagsChanged,
  onOpenChat,
  onOpenCircle,
  onOpenTasks,
  onChanged,
  onDeleted,
}: {
  circleId: number
  circles: Circle[]
  chats: Chat[]
  contacts: Contact[]
  groups: Group[]
  nameMap: Map<string, string>
  allTags: Tag[]
  onTagsChanged: () => void
  onOpenChat: (jid: string) => void
  onOpenCircle: (id: number) => void
  onOpenTasks: (id: number) => void
  onChanged: () => void
  onDeleted: () => void
}) {
  const [detail, setDetail] = useState<CircleDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [renaming, setRenaming] = useState(false)
  const [nameDraft, setNameDraft] = useState('')
  const [adding, setAdding] = useState(false)
  const [showColors, setShowColors] = useState(false)
  const [showSettings, setShowSettings] = useState(false)
  const [suggestions, setSuggestions] = useState<MemberSuggestion[]>([])
  const [suggContext, setSuggContext] = useState('')
  const [dismissedSugg, setDismissedSugg] = useState<Set<string>>(new Set())
  const [creatingSub, setCreatingSub] = useState(false)
  const [subName, setSubName] = useState('')
  const [subBusy, setSubBusy] = useState(false)
  const [contactsEnriched, setContactsEnriched] = useState<CircleContact[]>([])
  const [expand, setExpand] = useState({ circles: false, groups: false, contacts: false })
  const [extracting, setExtracting] = useState(false)
  const [showHistory, setShowHistory] = useState(false)
  const [liveRunId, setLiveRunId] = useState<string | null>(null)

  function reload() {
    return Promise.all([
      api.getCircle(circleId).then((d) => setDetail(d)),
      api.circleContacts(circleId).then((cc) => setContactsEnriched(cc || [])).catch(() => {}),
      api
        .circleSuggestions(circleId)
        .then((r) => {
          setSuggestions(r.suggestions || [])
          setSuggContext(r.context || '')
        })
        .catch(() => {}),
    ])
  }

  useEffect(() => {
    setLoading(true)
    setDismissedSugg(new Set())
    reload().finally(() => setLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [circleId])

  const circle = detail?.circle
  const members = detail?.members || []

  function memberLabel(m: CircleMember): string {
    if (m.member_type === 'circle') {
      return circles.find((c) => String(c.id) === m.member_ref)?.name || `Circle ${m.member_ref}`
    }
    return nameMap.get(m.member_ref) || '+' + jidUser(m.member_ref)
  }

  async function remove(m: CircleMember) {
    await api.removeCircleMember(circleId, m.member_type, m.member_ref)
    await reload()
    onChanged()
  }

  async function removeContact(jid: string) {
    await api.removeCircleMember(circleId, 'contact', jid)
    await reload()
    onChanged()
  }

  async function addSuggestion(sg: MemberSuggestion) {
    await api.addCircleMember(circleId, sg.type, sg.ref)
    await reload()
    onChanged()
  }

  async function createSub() {
    const n = subName.trim()
    if (!n || subBusy) return
    setSubBusy(true)
    try {
      const sub = await api.createCircle(n, pickColor(circles))
      await api.addCircleMember(circleId, 'circle', String(sub.id))
      setSubName('')
      setCreatingSub(false)
      await reload()
      onChanged()
    } finally {
      setSubBusy(false)
    }
  }

  async function saveName() {
    const n = nameDraft.trim()
    if (!n || !circle) return
    await api.updateCircle(circleId, {
      name: n,
      color: circle.color,
      notes: circle.notes,
      keywords: circle.keywords || [],
    })
    setRenaming(false)
    await reload()
    onChanged()
  }

  async function saveColor(col: string) {
    if (!circle) return
    setShowColors(false)
    await api.updateCircle(circleId, {
      name: circle.name,
      color: col,
      notes: circle.notes,
      keywords: circle.keywords || [],
    })
    await reload()
    onChanged()
  }

  async function deleteCircle() {
    if (!circle) return
    if (!confirm(`Delete circle "${circle.name}"? Members are not affected.`)) return
    await api.deleteCircle(circleId)
    onChanged()
    onDeleted()
  }

  if (loading) {
    return <div className="flex h-full items-center justify-center text-sm text-neutral-600">Loading…</div>
  }
  if (!circle) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-neutral-600">
        This circle was deleted.
      </div>
    )
  }

  const nested = members.filter((m) => m.member_type === 'circle')
  const memberGroups = members.filter((m) => m.member_type === 'group')
  const visibleSugg = suggestions.filter((sg) => !dismissedSugg.has(sg.type + ':' + sg.ref))

  // Recency: order groups and (non-admin) contacts by their last message, so
  // active ones surface first.
  const lastAt = (jid: string) => chats.find((ch) => ch.jid === jid)?.last_message_at || 0
  const sortedGroups = [...memberGroups].sort((a, b) => lastAt(b.member_ref) - lastAt(a.member_ref))
  const sortedContacts = [...contactsEnriched].sort((a, b) => {
    if (a.is_admin !== b.is_admin) return a.is_admin ? -1 : 1
    return lastAt(b.jid) - lastAt(a.jid)
  })
  const LIMIT = 5

  return (
    <div className="flex h-full flex-col">
      <header className="flex items-center gap-3 border-b border-neutral-800 px-4 py-3">
        <div className="relative shrink-0">
          <button
            onClick={() => setShowColors((s) => !s)}
            className="block h-4 w-4 rounded-full transition hover:ring-2 hover:ring-neutral-500"
            style={{ backgroundColor: circle.color || '#737373' }}
            title="Change color"
          />
          {showColors && (
            <>
              <div className="fixed inset-0 z-10" onClick={() => setShowColors(false)} />
              <div className="absolute left-0 top-6 z-20 grid w-44 grid-cols-6 gap-1.5 rounded-lg border border-neutral-700 bg-neutral-900 p-2 shadow-xl">
                {CIRCLE_COLORS.map((col) => (
                  <button
                    key={col}
                    onClick={() => saveColor(col)}
                    className={
                      'h-5 w-5 rounded-full transition hover:scale-110 ' +
                      (circle.color === col ? 'ring-2 ring-white ring-offset-2 ring-offset-neutral-900' : '')
                    }
                    style={{ backgroundColor: col }}
                  />
                ))}
              </div>
            </>
          )}
        </div>
        {renaming ? (
          <input
            autoFocus
            value={nameDraft}
            onChange={(e) => setNameDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') saveName()
              if (e.key === 'Escape') setRenaming(false)
            }}
            onBlur={saveName}
            className="flex-1 rounded-md border border-neutral-700 bg-neutral-900 px-2 py-1 text-sm outline-none"
          />
        ) : (
          <button
            onClick={() => {
              setNameDraft(circle.name)
              setRenaming(true)
            }}
            dir="auto"
            className="flex-1 truncate text-start text-sm font-semibold hover:text-emerald-300"
            title="Rename"
          >
            {circle.name}
          </button>
        )}
        <span className="shrink-0 text-xs text-neutral-500">{members.length} members</span>
        <button
          onClick={() => setAdding(true)}
          className="shrink-0 rounded-lg bg-emerald-500 px-3 py-1 text-xs font-medium text-neutral-950 hover:bg-emerald-400"
        >
          + Add
        </button>
        <button
          onClick={async () => {
            if (extracting) return
            setExtracting(true)
            try {
              const r = await api.extractCircleTasks(circleId, circle.name)
              setLiveRunId(r.run_id)
              setShowHistory(true)
            } catch (e) {
              alert('Circle extraction failed to start: ' + (e as Error).message)
            } finally {
              setExtracting(false)
            }
          }}
          disabled={extracting}
          className="shrink-0 rounded-lg bg-emerald-500/15 px-2 py-1 text-xs font-medium text-emerald-300 hover:bg-emerald-500/25 disabled:opacity-60"
          title="Extract tasks across all chats in this circle (AI)"
        >
          {extracting ? 'Extracting…' : '✨ Extract'}
        </button>
        <button
          onClick={() => setShowHistory(true)}
          className="shrink-0 rounded-lg border border-neutral-700 px-2 py-1 text-xs text-neutral-300 hover:bg-neutral-800"
          title="Past circle extraction runs and what the agent did"
        >
          🕘
        </button>
        <button
          onClick={async () => {
            const r = await api.clusterCircleTasks(circleId)
            onOpenTasks(circleId)
            alert(
              `Clustered: ${r.new_parents} new parents, ${r.reused_parents} existing reused, ` +
                `${r.children_linked} children linked` +
                (r.skipped ? `, ${r.skipped} skipped` : '') +
                '.',
            )
          }}
          className="shrink-0 rounded-lg border border-neutral-700 px-2 py-1 text-xs text-neutral-300 hover:bg-neutral-800"
          title="Group related tasks under parent tasks (AI)"
        >
          🌳 Cluster
        </button>
        <button
          onClick={() => onOpenTasks(circleId)}
          className="shrink-0 rounded-lg border border-neutral-700 px-2 py-1 text-xs text-neutral-300 hover:bg-neutral-800"
          title="Tasks in this circle"
        >
          ✓ Tasks
        </button>
        <button
          onClick={() => setShowSettings(true)}
          className="shrink-0 rounded-lg border border-neutral-700 px-2 py-1 text-xs text-neutral-300 hover:bg-neutral-800"
          title="Circle settings"
        >
          ⚙ Settings
        </button>
        <button
          onClick={deleteCircle}
          className="shrink-0 rounded-lg border border-neutral-700 px-2 py-1 text-xs text-neutral-400 hover:bg-neutral-800"
          title="Delete circle"
        >
          🗑
        </button>
      </header>

      <ProfileCard type="circle" ref_={String(circleId)} defaultOpen />

      <div className="min-h-0 flex-1 overflow-y-auto p-4">
        {visibleSugg.length === 0 && suggContext && (
          <div className="mb-4 rounded-lg border border-neutral-800 bg-neutral-900/60 px-3 py-2 text-xs text-neutral-500">
            No matches for <span className="text-neutral-300">{suggContext}</span> yet. Add a keyword
            in ⚙ Settings that appears in the group names (e.g. a brand or product word).
          </div>
        )}

        {members.length === 0 && visibleSugg.length === 0 && (
          <div className="py-8 text-center text-sm text-neutral-600">
            Empty circle. Click “+ Add”, or set keywords in ⚙ Settings to get suggestions.
          </div>
        )}

        {visibleSugg.length > 0 && (
          <div className="mb-5 rounded-xl border border-emerald-700/40 bg-emerald-500/5 p-3">
            <div className="mb-2 flex items-center justify-between">
              <span className="text-[11px] uppercase tracking-wide text-emerald-300/80">
                Suggested · {visibleSugg.length}
              </span>
              {suggContext && (
                <span dir="auto" className="text-[11px] text-neutral-500">
                  matching {suggContext}
                </span>
              )}
            </div>
            <div className="space-y-1">
              {visibleSugg.map((sg) => (
                <div
                  key={sg.type + ':' + sg.ref}
                  className="group flex items-center gap-3 rounded-lg px-2 py-1.5 hover:bg-neutral-900"
                >
                  <span
                    className={
                      'flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-sm font-semibold text-neutral-950 ' +
                      (sg.type === 'group' ? 'bg-sky-400' : 'bg-neutral-400')
                    }
                  >
                    {initial(sg.label)}
                  </span>
                  <button
                    onClick={() => sg.type === 'group' || sg.type === 'contact' ? onOpenChat(sg.ref) : undefined}
                    className="min-w-0 flex-1 text-left"
                  >
                    <span dir="auto" className="block truncate text-sm">
                      {sg.label}
                    </span>
                    <span className="block truncate text-xs text-neutral-500">
                      {sg.type === 'group' ? 'Group' : 'Contact'} · matched “{sg.keyword}”
                    </span>
                  </button>
                  <button
                    onClick={() => addSuggestion(sg)}
                    className="shrink-0 rounded-md bg-emerald-500 px-2 py-1 text-xs font-medium text-neutral-950 hover:bg-emerald-400"
                  >
                    ＋ Add
                  </button>
                  <button
                    onClick={() =>
                      setDismissedSugg((s) => new Set(s).add(sg.type + ':' + sg.ref))
                    }
                    className="shrink-0 rounded-md px-1.5 py-1 text-xs text-neutral-500 opacity-0 transition hover:text-red-400 group-hover:opacity-100"
                    title="Dismiss"
                  >
                    ✕
                  </button>
                </div>
              ))}
            </div>
          </div>
        )}

        <div className="mb-5">
          <div className="mb-1 flex items-center justify-between px-1">
            <span className="text-[11px] uppercase tracking-wide text-neutral-500">
              Circles · {nested.length}
            </span>
            <button
              onClick={() => setCreatingSub(true)}
              className="text-xs font-medium text-emerald-400 hover:text-emerald-300"
            >
              + New sub-circle
            </button>
          </div>
          {creatingSub && (
            <div className="mb-2 flex gap-2 px-1">
              <input
                autoFocus
                value={subName}
                onChange={(e) => setSubName(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') createSub()
                  if (e.key === 'Escape') {
                    setCreatingSub(false)
                    setSubName('')
                  }
                }}
                placeholder="Sub-circle name"
                className="min-w-0 flex-1 rounded-lg border border-neutral-700 bg-neutral-950 px-3 py-1.5 text-sm outline-none focus:border-neutral-500"
              />
              <button
                onClick={createSub}
                disabled={subBusy}
                className="rounded-lg bg-emerald-500 px-3 text-sm font-medium text-neutral-950 disabled:opacity-50"
              >
                Add
              </button>
            </div>
          )}
          <div className="space-y-1">
            {(expand.circles ? nested : nested.slice(0, LIMIT)).map((m) => (
              <Row
                key={'c' + m.member_ref}
                label={memberLabel(m)}
                sub="Circle"
                color="#a855f7"
                onClick={() => onOpenCircle(Number(m.member_ref))}
                onRemove={() => remove(m)}
              />
            ))}
          </div>
          <MoreToggle
            total={nested.length}
            open={expand.circles}
            onToggle={() => setExpand((e) => ({ ...e, circles: !e.circles }))}
          />
        </div>

        <SectionHeader title="Groups" total={memberGroups.length} />
        <div className="mb-1 space-y-1">
          {(expand.groups ? sortedGroups : sortedGroups.slice(0, LIMIT)).map((m) => {
            const chat = chats.find((ch) => ch.jid === m.member_ref)
            return (
              <div
                key={'g' + m.member_ref}
                className="group flex items-center gap-3 rounded-lg px-2 py-2 hover:bg-neutral-900"
              >
                <button
                  onClick={() => onOpenChat(m.member_ref)}
                  className="flex min-w-0 flex-1 items-center gap-3 text-left"
                >
                  <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-sky-600/30 text-sm font-semibold text-sky-300">
                    {initial(memberLabel(m))}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span dir="auto" className="block truncate text-sm">
                      {memberLabel(m)}
                    </span>
                    <span dir="auto" className="block truncate text-xs text-neutral-500">
                      {chat?.last_message ? previewText(chat.last_message, nameMap) : 'Group'}
                    </span>
                  </span>
                </button>
                {chat && chat.unread_count > 0 && (
                  <span className="shrink-0 rounded-full bg-emerald-500 px-1.5 py-0.5 text-[11px] font-semibold text-neutral-950">
                    {chat.unread_count}
                  </span>
                )}
                <button
                  onClick={() => remove(m)}
                  className="shrink-0 rounded-md px-2 py-1 text-xs text-neutral-500 opacity-0 transition hover:text-red-400 group-hover:opacity-100"
                  title="Remove from circle"
                >
                  ✕
                </button>
              </div>
            )
          })}
        </div>
        <MoreToggle
          total={memberGroups.length}
          open={expand.groups}
          onToggle={() => setExpand((e) => ({ ...e, groups: !e.groups }))}
        />

        <SectionHeader title="Contacts" total={contactsEnriched.length} />
        <div className="mb-1 space-y-1">
          {(expand.contacts ? sortedContacts : sortedContacts.slice(0, LIMIT)).map((cc) => {
            const label = nameMap.get(cc.jid) || '+' + jidUser(cc.jid)
            return (
              <div
                key={'p' + cc.jid}
                className="group flex items-center gap-3 rounded-lg px-2 py-2 hover:bg-neutral-900"
              >
                <button
                  onClick={() => onOpenChat(cc.jid)}
                  className="flex min-w-0 flex-1 items-center gap-3 text-left"
                >
                  <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-neutral-700 text-sm font-semibold text-neutral-200">
                    {initial(label)}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="flex items-center gap-2">
                      <span dir="auto" className="truncate text-sm">
                        {label}
                      </span>
                      {cc.is_admin && (
                        <span className="shrink-0 rounded bg-amber-500/20 px-1.5 text-[10px] font-medium text-amber-300">
                          admin
                        </span>
                      )}
                    </span>
                    <span className="block truncate text-xs text-neutral-500">
                      {cc.group_count > 0
                        ? `in ${cc.group_count} ${cc.group_count === 1 ? 'group' : 'groups'}`
                        : '+' + jidUser(cc.jid)}
                    </span>
                    {cc.tags.length > 0 && (
                      <div className="mt-1">
                        <TagChips tags={cc.tags} />
                      </div>
                    )}
                  </span>
                </button>
                <TagEditor
                  jid={cc.jid}
                  tags={cc.tags}
                  allTags={allTags}
                  onChanged={async () => {
                    await reload()
                    onTagsChanged()
                  }}
                />
                <button
                  onClick={() => removeContact(cc.jid)}
                  className="shrink-0 rounded-md px-2 py-1 text-xs text-neutral-500 opacity-0 transition hover:text-red-400 group-hover:opacity-100"
                  title="Remove from circle"
                >
                  ✕
                </button>
              </div>
            )
          })}
        </div>
        <MoreToggle
          total={contactsEnriched.length}
          open={expand.contacts}
          onToggle={() => setExpand((e) => ({ ...e, contacts: !e.contacts }))}
        />
      </div>

      {adding && (
        <AddMembers
          circleId={circleId}
          existing={members}
          circles={circles}
          contacts={contacts}
          groups={groups}
          nameMap={nameMap}
          onClose={() => setAdding(false)}
          onAdded={async () => {
            await reload()
            onChanged()
          }}
        />
      )}

      {showSettings && (
        <CircleSettings
          circle={circle}
          onClose={() => setShowSettings(false)}
          onChanged={async () => {
            await reload()
            onChanged()
          }}
        />
      )}
      {showHistory && (
        <ExtractionsModal
          title={'Circle: ' + circle.name}
          fetchRuns={() => api.listCircleExtractions(circleId)}
          liveRunId={liveRunId}
          onClose={() => {
            setShowHistory(false)
            if (liveRunId) {
              onOpenTasks(circleId)
            }
            setLiveRunId(null)
          }}
        />
      )}
    </div>
  )
}

function SectionHeader({ title, total }: { title: string; total: number }) {
  if (total === 0) return null
  return (
    <div className="mb-1 mt-4 px-1 text-[11px] uppercase tracking-wide text-neutral-500">
      {title} · {total}
    </div>
  )
}

function MoreToggle({ total, open, onToggle }: { total: number; open: boolean; onToggle: () => void }) {
  if (total <= 5) return null
  return (
    <button
      onClick={onToggle}
      className="mb-3 px-1 text-xs font-medium text-neutral-500 hover:text-neutral-300"
    >
      {open ? 'Show less' : `Show all ${total}`}
    </button>
  )
}

function Row({
  label,
  sub,
  color,
  onClick,
  onRemove,
}: {
  label: string
  sub: string
  color: string
  onClick: () => void
  onRemove: () => void
}) {
  return (
    <div className="group flex items-center gap-3 rounded-lg px-2 py-2 hover:bg-neutral-900">
      <button onClick={onClick} className="flex min-w-0 flex-1 items-center gap-3 text-left">
        <span
          className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-sm font-semibold text-neutral-950"
          style={{ backgroundColor: color }}
        >
          {initial(label)}
        </span>
        <span className="min-w-0">
          <span dir="auto" className="block truncate text-sm">
            {label}
          </span>
          <span className="block truncate text-xs text-neutral-500">{sub}</span>
        </span>
      </button>
      <button
        onClick={onRemove}
        className="shrink-0 rounded-md px-2 py-1 text-xs text-neutral-500 opacity-0 transition hover:bg-neutral-800 hover:text-red-400 group-hover:opacity-100"
        title="Remove from circle"
      >
        ✕
      </button>
    </div>
  )
}

// AddMembers is a modal to add groups/contacts/circles to a circle.
function AddMembers({
  circleId,
  existing,
  circles,
  contacts,
  groups,
  nameMap,
  onClose,
  onAdded,
}: {
  circleId: number
  existing: CircleMember[]
  circles: Circle[]
  contacts: Contact[]
  groups: Group[]
  nameMap: Map<string, string>
  onClose: () => void
  onAdded: () => void
}) {
  const [kind, setKind] = useState<Kind>('all')
  const [q, setQ] = useState('')
  const [pending, setPending] = useState<Set<string>>(new Set())

  const has = useMemo(() => {
    const s = new Set<string>()
    for (const m of existing) s.add(m.member_type + ':' + m.member_ref)
    return s
  }, [existing])

  const items = useMemo(() => {
    const needle = q.trim().toLowerCase()
    const groupItems = (): Item[] =>
      groups.map((g) => ({ ref: g.jid, label: g.name || g.jid, sub: 'Group', type: 'group' }))
    const contactItems = (): Item[] =>
      contacts.map((c) => ({
        ref: c.jid,
        label: nameMap.get(c.jid) || c.name || c.push_name || '+' + (c.phone || jidUser(c.jid)),
        sub: '+' + (c.phone || jidUser(c.jid)),
        type: 'contact',
      }))
    const circleItems = (): Item[] =>
      circles
        .filter((c) => c.id !== circleId)
        .map((c) => ({ ref: String(c.id), label: c.name, sub: 'Circle', type: 'circle' }))

    let list: Item[] = []
    if (kind === 'all') list = [...groupItems(), ...contactItems()]
    else if (kind === 'group') list = groupItems()
    else if (kind === 'contact') list = contactItems()
    else list = circleItems()

    return list
      .filter((it) => !has.has(it.type + ':' + it.ref))
      .filter(
        (it) =>
          !needle || it.label.toLowerCase().includes(needle) || it.ref.toLowerCase().includes(needle),
      )
      .slice(0, 100)
  }, [kind, q, groups, contacts, circles, circleId, has, nameMap])

  async function add(it: Item) {
    const key = it.type + ':' + it.ref
    setPending((p) => new Set(p).add(key))
    try {
      await api.addCircleMember(circleId, it.type, it.ref)
      onAdded()
    } catch (e) {
      alert((e as Error).message)
    } finally {
      setPending((p) => {
        const n = new Set(p)
        n.delete(key)
        return n
      })
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={onClose}>
      <div
        className="flex h-[70vh] w-full max-w-md flex-col rounded-2xl border border-neutral-800 bg-neutral-900"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between border-b border-neutral-800 px-4 py-3">
          <h2 className="text-sm font-semibold">Add to circle</h2>
          <button onClick={onClose} className="text-neutral-500 hover:text-neutral-200">
            ✕
          </button>
        </div>

        <div className="flex gap-1 px-3 pt-3 text-xs">
          {(['all', 'group', 'contact', 'circle'] as Kind[]).map((k) => (
            <button
              key={k}
              onClick={() => setKind(k)}
              className={
                'rounded-full px-3 py-1 capitalize transition ' +
                (kind === k ? 'bg-emerald-500 text-neutral-950' : 'bg-neutral-800 text-neutral-300')
              }
            >
              {k === 'all' ? 'All' : k + 's'}
            </button>
          ))}
        </div>

        <div className="p-3">
          <input
            autoFocus
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder={kind === 'all' ? 'Search groups & contacts' : `Search ${kind}s`}
            className="w-full rounded-lg border border-neutral-700 bg-neutral-950 px-3 py-2 text-sm outline-none focus:border-neutral-500"
          />
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-2">
          {items.length === 0 && (
            <div className="p-4 text-center text-xs text-neutral-600">Nothing to add</div>
          )}
          {items.map((it) => (
            <button
              key={it.type + ':' + it.ref}
              onClick={() => add(it)}
              disabled={pending.has(it.type + ':' + it.ref)}
              className="flex w-full items-center gap-3 rounded-lg px-2 py-2 text-left transition hover:bg-neutral-800 disabled:opacity-40"
            >
              <span
                className={
                  'flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-xs font-semibold ' +
                  (it.type === 'group'
                    ? 'bg-sky-600/30 text-sky-300'
                    : it.type === 'circle'
                      ? 'bg-purple-600/30 text-purple-300'
                      : 'bg-neutral-700 text-neutral-200')
                }
              >
                {initial(it.label)}
              </span>
              <span className="min-w-0 flex-1">
                <span dir="auto" className="block truncate text-sm">
                  {it.label}
                </span>
                <span className="block truncate text-xs text-neutral-500">{it.sub}</span>
              </span>
              <span className="shrink-0 text-emerald-400">+</span>
            </button>
          ))}
        </div>
      </div>
    </div>
  )
}
