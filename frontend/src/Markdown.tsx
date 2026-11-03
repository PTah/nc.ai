// Edited by nc.ai 1.0.5 on new-admin-pc 2026-11-02 09:49:46
import {memo} from 'react'
import ReactMarkdown, {type Components} from 'react-markdown'
import remarkGfm from 'remark-gfm'
import {BrowserOpenURL} from '../wailsjs/runtime/runtime'

const components: Components = {
  a: ({href, children}) => (
    <a
      href={href}
      target="_blank"
      rel="noreferrer noopener"
      onClick={(e) => {
        // В webview ссылка не должна открываться внутри окна — отдаём её
        // системному браузеру, как и остальные внешние ссылки приложения.
        if (!href) return
        e.preventDefault()
        BrowserOpenURL(href)
      }}
    >
      {children}
    </a>
  ),
}

function Markdown({content}: {content: string}) {
  return (
    <div className="nc-markdown">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {content}
      </ReactMarkdown>
    </div>
  )
}

// Мемоизация: ввод в композере перерисовывает App, но markdown-блоки с тем же
// содержимым перерисовывать незачем — это главный источник лагов при наборе.
export default memo(Markdown)
