import './index.css'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { createBrowserRouter } from 'react-router'
import { App } from './App'
import { createQueryClient } from './lib/queryClient'
import { UpdatePrompt } from './pwa/UpdatePrompt'
import { routes } from './routes'

const root = document.getElementById('root')
if (!root) {
  throw new Error('#root is missing from index.html')
}

createRoot(root).render(
  <StrictMode>
    <App router={createBrowserRouter(routes)} queryClient={createQueryClient()}>
      <UpdatePrompt />
    </App>
  </StrictMode>,
)
