import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'
import { installViewportDebug } from './utils/viewportDebug'

// 実機での高さ・スクロールの計測用 (一時的。?debug=viewport で有効になる)
installViewportDebug()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
