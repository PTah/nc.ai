/**
 * WebView2 on Windows can leave a blank (white) surface after the window was
 * minimised / occluded for a long time: the Go host stays alive, the HWND frame
 * stays alive, but the compositor stops painting. Kick the surface on resume;
 * after a long idle, soft-reload the frontend (WindowReloadApp) without killing
 * the Go process or chat drafts on disk.
 */

import {WindowGetSize, WindowReloadApp, WindowSetSize} from '../wailsjs/runtime/runtime'

const BRIEF_MS = 5_000
const KICK_MS = 30_000
const RELOAD_MS = 2 * 60_000
const RELOAD_COOLDOWN_MS = 60_000
const RELOAD_FLAG = 'nc.webviewRecover.reloadAt'

function kickCompositor(): void {
  const root = document.documentElement
  void root.offsetHeight
  const prev = root.style.transform
  root.style.transform = 'translateZ(0)'
  void root.offsetHeight
  window.requestAnimationFrame(() => {
    root.style.transform = prev
    window.dispatchEvent(new Event('resize'))
  })
}

async function nudgeNativeWindow(): Promise<void> {
  try {
    const size = await WindowGetSize()
    const w = Number((size as {w?: number})?.w || 0)
    const h = Number((size as {h?: number})?.h || 0)
    if (w < 100 || h < 100) return
    WindowSetSize(w, h + 1)
    WindowSetSize(w, h)
  } catch {
    /* browser / missing bindings */
  }
}

function canSoftReload(): boolean {
  try {
    const raw = sessionStorage.getItem(RELOAD_FLAG)
    const prev = raw ? Number(raw) : 0
    if (prev && Date.now() - prev < RELOAD_COOLDOWN_MS) return false
  } catch {
    /* ignore */
  }
  return true
}

function softReloadUI(): void {
  if (!canSoftReload()) return
  try {
    sessionStorage.setItem(RELOAD_FLAG, String(Date.now()))
  } catch {
    /* ignore */
  }
  try {
    WindowReloadApp()
    return
  } catch {
    window.location.reload()
  }
}

/** True when React never mounted or #root looks empty. */
function looksBlank(): boolean {
  const root = document.getElementById('root')
  if (!root) return true
  if (root.childElementCount === 0) return true
  if (!root.querySelector('.nc-root') && !root.querySelector('.nc-crash')) return true
  return false
}

export function installWebviewRecover(): void {
  let hiddenAt = document.hidden ? Date.now() : 0
  let busy = false

  const markHidden = () => {
    if (document.hidden) hiddenAt = Date.now()
  }

  const onResume = async () => {
    if (document.hidden || busy) return
    const hiddenFor = hiddenAt > 0 ? Date.now() - hiddenAt : 0
    if (hiddenFor > 0) hiddenAt = 0
    if (hiddenFor < BRIEF_MS) return

    busy = true
    try {
      kickCompositor()
      if (hiddenFor >= KICK_MS) {
        await nudgeNativeWindow()
      }
      if (hiddenFor >= RELOAD_MS) {
        await new Promise<void>((r) => window.setTimeout(r, 350))
        if (!document.hidden) {
          // After a long occlusion the surface is often white while the DOM is
          // still “fine” in memory — soft-reload the frontend once.
          softReloadUI()
        }
      } else if (looksBlank()) {
        softReloadUI()
      }
    } finally {
      busy = false
    }
  }

  document.addEventListener('visibilitychange', () => {
    if (document.hidden) markHidden()
    else void onResume()
  })
  window.addEventListener('focus', () => {
    void onResume()
  })
}
