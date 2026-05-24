import type { ReactNode } from 'react'

// Card is the centered container used by every onboarding screen.
export function Card({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-full items-center justify-center bg-neutral-950 p-6 text-neutral-100">
      <div className="w-full max-w-md rounded-2xl border border-neutral-800 bg-neutral-900 p-8 shadow-xl">
        <div className="mb-6 flex items-center gap-3">
          <div className="flex h-9 w-9 items-center justify-center rounded-full bg-emerald-500 text-lg font-bold text-neutral-950">
            W
          </div>
          <h1 className="text-lg font-semibold">WhatsApp Bridge</h1>
        </div>
        {children}
      </div>
    </div>
  )
}

export function Spinner() {
  return (
    <div className="h-5 w-5 animate-spin rounded-full border-2 border-neutral-600 border-t-emerald-400" />
  )
}
