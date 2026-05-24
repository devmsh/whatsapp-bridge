import { useState } from 'react'
import { initial } from './format'

// ChatAvatar renders a chat/contact/group profile picture, falling back to a
// colored letter avatar when the image is unavailable. The bridge endpoint
// `/api/v2/avatars/{jid}` returns the cached JPEG (or 404 → letter fallback).
//
// `size` controls width/height in pixels (default 40). `group` chooses the
// fallback color palette.
export function ChatAvatar({
  jid,
  title,
  group,
  size = 40,
  className = '',
}: {
  jid: string
  title: string
  group?: boolean
  size?: number
  className?: string
}) {
  const [failed, setFailed] = useState(false)
  const box =
    'flex shrink-0 items-center justify-center rounded-full overflow-hidden ' +
    (group ? 'bg-sky-600/30 text-sky-300' : 'bg-neutral-700 text-neutral-200') +
    ' ' +
    className

  const style = { width: size, height: size, fontSize: Math.round(size * 0.36) }

  if (failed || !jid) {
    return (
      <div className={box + ' font-semibold'} style={style}>
        {initial(title)}
      </div>
    )
  }
  return (
    <div className={box} style={style}>
      <img
        src={'/api/v2/avatars/' + encodeURIComponent(jid)}
        alt=""
        loading="lazy"
        onError={() => setFailed(true)}
        className="h-full w-full object-cover"
      />
    </div>
  )
}
