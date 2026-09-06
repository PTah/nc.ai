import {useEffect, useState} from 'react'

/** Served from frontend/public/brand (stable for Vite/Wails asset pipeline). */
const FRAMES = [1, 2, 3, 4, 5, 6].map((n) => `./brand/frame-${n}.png`)
const FRAME_MS = 280

type Props = {
  size?: number
  title?: string
  className?: string
}

/** In-app animated brand mark from sequential icon frames 1–6. */
export default function BrandMark({
  size = 34,
  title = 'NotCursor.ai',
  className = '',
}: Props) {
  const [idx, setIdx] = useState(0)

  useEffect(() => {
    const id = window.setInterval(() => {
      setIdx((v) => (v + 1) % FRAMES.length)
    }, FRAME_MS)
    return () => window.clearInterval(id)
  }, [])

  return (
    <span
      className={`nc-brand-mark nc-brand-mark-frames ${className}`.trim()}
      title={title}
      style={{width: size, height: size}}
      aria-label={title}
      role="img"
    >
      {FRAMES.map((src, i) => (
        <img
          key={src}
          src={src}
          alt=""
          aria-hidden
          draggable={false}
          className={i === idx ? 'is-active' : undefined}
        />
      ))}
    </span>
  )
}
