// Единые форматы даты для интерфейса (локальное время машины):
//   дата            — «ДД-ММ-ГГГГ»  (например 24-09-2026)
//   дата со временем — «ДД.ММ.ГГГГ ЧЧ:мм:сс»  (например 24.09.2026 09:32:16)
// Раньше в разных местах было по-разному, а новости показывались через
// toLocaleString() — то есть зависели от локали системы.

function toDate(value: unknown): Date | null {
  if (value === null || value === undefined || value === '') return null
  if (value instanceof Date) return Number.isNaN(value.getTime()) ? null : value
  if (typeof value === 'number') {
    const d = new Date(value)
    return Number.isNaN(d.getTime()) ? null : d
  }
  const s = String(value).trim()
  if (!s) return null
  // «2026-09-24» без времени: new Date() в части движков считает такие строки
  // UTC — из-за этого дата могла показываться на день раньше.
  const dateOnly = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s)
  if (dateOnly) {
    return new Date(Number(dateOnly[1]), Number(dateOnly[2]) - 1, Number(dateOnly[3]))
  }
  const d = new Date(s)
  return Number.isNaN(d.getTime()) ? null : d
}

function pad(n: number): string {
  return String(n).padStart(2, '0')
}

/** Дата в формате РФ: «24-09-2026». Непонятное значение отдаём как есть. */
export function fmtDate(value: unknown): string {
  const d = toDate(value)
  if (!d) return typeof value === 'string' ? value.trim() : ''
  return `${pad(d.getDate())}-${pad(d.getMonth() + 1)}-${d.getFullYear()}`
}

/** Дата и время в формате РФ: «24.09.2026 09:32:16». */
export function fmtDateTime(value: unknown): string {
  const d = toDate(value)
  if (!d) return typeof value === 'string' ? value.trim() : ''
  return `${pad(d.getDate())}.${pad(d.getMonth() + 1)}.${d.getFullYear()} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}
