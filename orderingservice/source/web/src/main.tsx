import { StrictMode, Component, type ReactNode, type ErrorInfo } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'

class ErrorBoundary extends Component<{ children: ReactNode }, { error: string | null }> {
  constructor(props: { children: ReactNode }) {
    super(props)
    this.state = { error: null }
  }
  static getDerivedStateFromError(e: unknown) {
    return { error: String(e) }
  }
  componentDidCatch(e: Error, info: ErrorInfo) {
    console.error('[ErrorBoundary]', e, info)
  }
  render() {
    if (this.state.error) {
      return (
        <div style={{ padding: 24, fontFamily: 'monospace', background: '#1a0000', color: '#ff6b6b', minHeight: '100vh' }}>
          <h2>React render error</h2>
          <pre style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{this.state.error}</pre>
          <p style={{ color: '#9CA3AF' }}>Check browser console (F12) for full stack trace.</p>
        </div>
      )
    }
    return this.props.children
  }
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ErrorBoundary>
      <App />
    </ErrorBoundary>
  </StrictMode>
)
