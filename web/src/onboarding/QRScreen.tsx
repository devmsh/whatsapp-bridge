import { useEffect, useState } from 'react'
import { QRCodeSVG } from 'qrcode.react'
import { api, HISTORY_LABELS } from '../api'
import { Card } from './Card'

// QRScreen renders the WhatsApp linking QR code plus a history-period chooser.
// The period is sent to WhatsApp at pairing, so picking it here (before the
// scan) is the right moment. Changing it regenerates the QR.
export function QRScreen({ qrCode }: { qrCode: string }) {
  return (
    <Card>
      <h2 className="mb-1 text-base font-medium">Link your phone</h2>
      <p className="mb-6 text-sm text-neutral-400">
        Open WhatsApp on your phone, go to{' '}
        <span className="text-neutral-200">Settings → Linked Devices → Link a Device</span>, then
        scan this code.
      </p>

      <div className="mx-auto w-fit rounded-xl bg-white p-4">
        <QRCodeSVG value={qrCode} size={232} level="M" />
      </div>

      <HistoryChooser />

      <p className="mt-4 text-center text-xs text-neutral-500">
        The code refreshes automatically. Keep this page open.
      </p>
    </Card>
  )
}

function HistoryChooser() {
  const [period, setPeriod] = useState<string>('3months')
  const [options, setOptions] = useState<string[]>(['3months', '1year', 'everything'])
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api.historySettings().then((s) => {
      setPeriod(s.period)
      if (s.options?.length) setOptions(s.options)
    }).catch(() => {})
  }, [])

  async function choose(p: string) {
    if (p === period || busy) return
    setBusy(true)
    setPeriod(p)
    try {
      // This regenerates the QR on the server; the auth stream pushes the new code.
      await api.setHistory(p)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="mt-6">
      <div className="mb-2 text-xs text-neutral-400">How much history to sync?</div>
      <div className="grid grid-cols-3 gap-2">
        {options.map((opt) => (
          <button
            key={opt}
            disabled={busy}
            onClick={() => choose(opt)}
            className={
              'rounded-lg border px-2 py-2 text-xs transition disabled:opacity-50 ' +
              (opt === period
                ? 'border-emerald-500 bg-emerald-500/10 text-emerald-300'
                : 'border-neutral-700 text-neutral-300 hover:bg-neutral-800')
            }
          >
            {HISTORY_LABELS[opt] ?? opt}
          </button>
        ))}
      </div>
      <div className="mt-2 text-[11px] text-neutral-600">
        More history takes longer to download.
      </div>
    </div>
  )
}
