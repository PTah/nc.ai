import {useEffect, useState} from 'react'

/** Served from frontend/public/brand (stable for Vite/Wails asset pipeline). */
const FRAMES = [1, 2, 3, 4, 5, 6].map((n) => `./brand/frame-${n}.png`)
const FRAME_MS = 560

type Props = {
  size?: number
  title?: string
  className?: string
  /** Показывать анимацию кадров. Без активных процессов иконка статична. */
  animated?: boolean
}

/**
 * In-app brand mark: кадры 1–6 крутятся, пока идёт работа агента. Когда процессов
 * нет — показываем первый кадр статично (анимация не жжёт CPU в фоне).
 */
export default function BrandMark({
  size = 34,
  title = 'NotCursor.ai',
  className = '',
  animated = false,
}: Props) {
  const [idx, setIdx] = useState(0)

  useEffect(() => {
    if (!animated) {
      setIdx(0)
      return
    }
    const id = window.setInterval(() => {
      setIdx((v) => (v + 1) % FRAMES.length)
    }, FRAME_MS)
    return () => window.clearInterval(id)
  }, [animated])

  const shown = animated ? idx : 0

  return (
    <span
      className={`nc-brand-mark nc-brand-mark-frames ${animated ? '' : 'is-static'} ${className}`.trim()}
      title={animated ? `${title} — идёт работа` : title}
      style={{width: size, height: size}}
      aria-label={animated ? `${title} — идёт работа` : title}
      role="img"
    >
      {FRAMES.map((src, i) => (
        <img
          key={src}
          src={src}
          alt=""
          aria-hidden
          draggable={false}
          className={i === shown ? 'is-active' : undefined}
        />
      ))}
    </span>
  )
}
