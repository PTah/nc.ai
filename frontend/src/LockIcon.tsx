// Edited by nc.ai 0.8.7.6 on Papasha 2026-10-16 17:48:28
type Props = {
  /** true — проект на замке (закрыт для записи), false — открыт для редактирования. */
  locked: boolean
}

/**
 * Значок замка проекта. Цветной и намеренно заметный: закрытый — красный, открытый —
 * зелёный, чтобы состояние читалось без чтения подсказки. Силуэт один (корпус +
 * дужка), меняется только дужка: закрытая «садится» на корпус, открытая обрывается
 * выше него. Плоские заливки, без градиентов: <defs> с id при нескольких значках
 * на странице конфликтуют, а градиент всё равно неразличим на 14 px.
 *
 * Анимацию на наведение вешает CSS по классам nc-lock-* (см. App.css): открытый
 * замок качает дужкой, закрытый подрагивает корпусом.
 */
export default function LockIcon({locked}: Props) {
  const body = locked ? '#ef4444' : '#22c55e'
  const shackle = locked ? '#b91c1c' : '#15803d'
  const keyhole = locked ? '#7f1d1d' : '#14532d'

  return (
    <svg
      viewBox="0 0 20 20"
      width="14"
      height="14"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      aria-hidden
      focusable="false"
      className={locked ? 'nc-lock nc-lock-locked' : 'nc-lock nc-lock-open'}
    >
      <g className="nc-lock-body">
        <rect x="4.2" y="8.9" width="11.6" height="7.5" rx="1.9" fill={body} />
        {/* блик сверху — чтобы корпус не выглядел плоской заливкой */}
        <rect x="5.4" y="9.7" width="9.2" height="1.4" rx="0.7" fill="#ffffff" opacity="0.35" />
        <circle cx="10" cy="13.3" r="1.05" fill={keyhole} />
      </g>
      <path
        className="nc-lock-shackle"
        d={locked ? 'M6.9 8.9V6.6a3.1 3.1 0 0 1 6.2 0v2.3' : 'M6.9 8.9V6.6a3.1 3.1 0 0 1 6.2 0v0.7'}
        stroke={shackle}
        strokeWidth="1.7"
        strokeLinecap="round"
      />
    </svg>
  )
}
