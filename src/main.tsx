import {StrictMode} from 'react';
import {createRoot} from 'react-dom/client';
import App from './App.tsx';
import {DesktopViewerPage} from './pages/DesktopViewerPage.tsx';
import './index.css';

// Detect if this is a popup viewer route
const path = window.location.pathname;
const viewerMatch = path.match(/^\/sandbox\/([^/]+)\/viewer$/);

if (viewerMatch) {
  // Render the desktop viewer popup
  const sandboxId = viewerMatch[1];
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <DesktopViewerPage sandboxId={sandboxId} />
    </StrictMode>,
  );
} else {
  // Render the main app
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
}
