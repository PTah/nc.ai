import ReactMarkdown, {type Components} from 'react-markdown'
import remarkGfm from 'remark-gfm'

const components: Components = {
  a: ({href, children}) => (
    <a href={href} target="_blank" rel="noreferrer">
      {children}
    </a>
  ),
}

export default function Markdown({content}: {content: string}) {
  return (
    <div className="nc-markdown">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {content}
      </ReactMarkdown>
    </div>
  )
}
