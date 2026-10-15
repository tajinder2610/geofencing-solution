import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import { AppDataProvider } from './hooks/useAppData'
import { ToastProvider } from './hooks/useToasts'
import './styles.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ToastProvider>
      <AppDataProvider>
        <App />
      </AppDataProvider>
    </ToastProvider>
  </StrictMode>,
)
