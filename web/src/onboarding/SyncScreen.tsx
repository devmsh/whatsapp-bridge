import type { DeviceInfo, SyncPhase } from '../api'
import { useSyncProgress } from '../hooks/useSyncProgress'
import { Card, Spinner } from './Card'

// SyncScreen shows live history-sync activity after linking. WhatsApp sends
// history in bursts, so we show a stage + growing counts instead of a fake
// percent. The user can enter the app at any time; sync keeps running.
export function SyncScreen({
  device,
  onContinue,
}: {
  device?: DeviceInfo
  onContinue: () => void
}) {
  const status = useSyncProgress(true)
  const phase: SyncPhase = status?.phase ?? 'starting'
  const counts = status?.counts
  const receiving = !!status?.receiving

  return (
    <Card>
      <h2 className="mb-1 text-base font-medium">
        {device?.push_name ? `Welcome, ${device.push_name}` : 'Linked!'}
      </h2>
      <p className="mb-6 text-sm text-neutral-400">
        WhatsApp is sending your chat history. It arrives in bursts and can take a few minutes.
      </p>

      <div className="mb-6 flex items-center gap-3 rounded-lg border border-neutral-800 bg-neutral-950 p-3">
        {receiving || phase === 'starting' ? <Spinner /> : <CheckDot />}
        <div className="text-sm">
          <div className="text-neutral-200">{phaseTitle(phase)}</div>
          <div className="text-xs text-neutral-500">{phaseHint(phase)}</div>
        </div>
      </div>

      {/* Indeterminate bar while receiving — no misleading percent. */}
      <div className="mb-6 h-1.5 w-full overflow-hidden rounded-full bg-neutral-800">
        {receiving || phase === 'starting' ? (
          <div className="h-full w-1/3 animate-pulse rounded-full bg-emerald-500" />
        ) : (
          <div className="h-full w-full rounded-full bg-emerald-600/40" />
        )}
      </div>

      <div className="mb-6 grid grid-cols-3 gap-3">
        <Stat label="Messages" value={counts?.messages} />
        <Stat label="Chats" value={counts?.chats} />
        <Stat label="Contacts" value={counts?.contacts} />
      </div>

      <button
        onClick={onContinue}
        className="w-full rounded-lg bg-emerald-500 px-4 py-2 text-sm font-medium text-neutral-950 transition hover:bg-emerald-400"
      >
        Open WhatsApp Bridge
      </button>

      <p className="mt-4 text-center text-xs text-neutral-500">
        You can open the app now — sync continues in the background.
      </p>
    </Card>
  )
}

function phaseTitle(phase: SyncPhase): string {
  switch (phase) {
    case 'starting':
      return 'Starting sync…'
    case 'receiving':
      return 'Receiving messages…'
    case 'offline':
      return 'Reconnecting…'
    default:
      return 'Up to date for now'
  }
}

function phaseHint(phase: SyncPhase): string {
  switch (phase) {
    case 'receiving':
      return 'New history is arriving right now.'
    case 'idle':
      return 'No new history in the last few seconds. More may still arrive.'
    case 'offline':
      return 'Waiting for the WhatsApp connection.'
    default:
      return 'Waiting for the first batch from your phone.'
  }
}

function CheckDot() {
  return (
    <div className="flex h-5 w-5 items-center justify-center rounded-full bg-emerald-500 text-xs font-bold text-neutral-950">
      ✓
    </div>
  )
}

function Stat({ label, value }: { label: string; value?: number }) {
  return (
    <div className="rounded-lg border border-neutral-800 bg-neutral-950 p-3 text-center">
      <div className="text-xl font-semibold tabular-nums">{(value ?? 0).toLocaleString()}</div>
      <div className="text-xs text-neutral-500">{label}</div>
    </div>
  )
}
