import React, {Component, ReactNode} from 'react'
import {createRoot} from 'react-dom/client'
import './style.css'
import App from './App'

class ErrorBoundary extends Component<{children: ReactNode}, {error: Error | null}> {
  state = {error: null as Error | null}

  static getDerivedStateFromError(error: Error) {
    return {error}
  }

  render() {
    if (this.state.error) {
      return (
        <pre className="nc-crash">
          {this.state.error.stack || this.state.error.message}
        </pre>
      )
    }
    return this.props.children
  }
}

const container = document.getElementById('root')
if (!container) {
  throw new Error('#root not found')
}

const root = createRoot(container, {
  onUncaughtError: (error) => {
    console.error(error)
  },
})

root.render(
  <React.StrictMode>
    <ErrorBoundary>
      <App />
    </ErrorBoundary>
  </React.StrictMode>,
)
