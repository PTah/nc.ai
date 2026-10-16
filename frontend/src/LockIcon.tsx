// Created by nc.ai 0.8.7.3 on Papasha 2026-10-16 17:37:10
type Props = {
  /** true — проект на замке (закрыт для записи), false — открыт для редактирования. */
  locked: boolean
}

/**
 * Значок замка проекта. Один силуэт (корпус + дужка), меняется только дужка:
 * закрытая «садится» на корпус, открытая обрывается выше него — так состояние
 * читается на 13 px, в отличие от эмодзи, который выглядит по-разному в каждой ОС.
 * Цвет наследуется (currentColor), поэтому значок идёт за темой.
 */
export default function LockIcon({locked}: Props) {
  return (
    <svg viewBox="0 0 20 20" width="13" height="13" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden focusable="false">
      <rect
        x="4.1"
        y="8.9"
        width="11.8"
        height="7.6"
        rx="1.9"
        stroke="currentColor"
        strokeWidth="1.5"
      />
      <path
        d={locked ? 'M6.9 8.9V6.6a3.1 3.1 0 0 1 6.2 0v2.3' : 'M6.9 8.9V6.6a3.1 3.1 0 0 1 6.2 0v0.7'}
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
      />
      <circle cx="10" cy="12.4" r="0.95" fill="currentColor" />
    </svg>
  )
}
