import React, { useState } from 'react';
import { Globe, ChevronUp, ChevronDown, Eye, Loader, Send } from 'lucide-react';
import { useWebBrowser, LabeledElement } from '../hooks/useWebBrowser';

interface WebBrowserPanelProps {
  onClose: () => void;
}

export function WebBrowserPanel({ onClose }: WebBrowserPanelProps) {
  const browser = useWebBrowser();
  const [urlInput, setUrlInput] = useState('https://');
  const [typeText, setTypeText] = useState('');
  const [selectedElement, setSelectedElement] = useState<LabeledElement | null>(null);
  const [autoSubmit, setAutoSubmit] = useState(false);

  const handleNavigate = (e: React.FormEvent) => {
    e.preventDefault();
    if (urlInput && urlInput !== 'https://') {
      browser.navigate(urlInput);
    }
  };

  const handleElementClick = (el: LabeledElement) => {
    setSelectedElement(el);
    const isInput = ['input', 'textarea', 'select'].includes(el.tag);
    if (!isInput) {
      browser.click(el.id);
      setSelectedElement(null);
    }
  };

  const handleType = () => {
    if (selectedElement && typeText) {
      browser.type(selectedElement.id, typeText, autoSubmit);
      setTypeText('');
      setSelectedElement(null);
    }
  };

  return (
    <div className="border-t border-zinc-700 bg-zinc-900 flex flex-col" style={{ height: '400px' }}>
      {/* Header */}
      <div className="flex items-center justify-between px-3 py-2 bg-zinc-800 border-b border-zinc-700">
        <div className="flex items-center gap-2">
          <Globe size={14} className="text-blue-400" />
          <span className="text-xs font-bold text-zinc-300">Web Browser</span>
          {browser.loading && <Loader size={12} className="animate-spin text-blue-400" />}
        </div>
        <div className="flex items-center gap-1">
          <button
            onClick={browser.describe}
            className="px-2 py-0.5 text-[10px] bg-zinc-700 hover:bg-zinc-600 rounded text-zinc-300 flex items-center gap-1"
            title="Describe page with vision model"
          >
            <Eye size={10} /> Describe
          </button>
          <button
            onClick={onClose}
            className="px-2 py-0.5 text-[10px] bg-zinc-700 hover:bg-zinc-600 rounded text-zinc-300"
          >
            Close
          </button>
        </div>
      </div>

      {/* URL Bar */}
      <form onSubmit={handleNavigate} className="flex items-center gap-2 px-3 py-1.5 bg-zinc-850 border-b border-zinc-700">
        <input
          type="text"
          value={urlInput}
          onChange={e => setUrlInput(e.target.value)}
          placeholder="Enter URL..."
          className="flex-1 bg-zinc-800 border border-zinc-600 rounded px-2 py-1 text-xs text-white placeholder-zinc-500 focus:outline-none focus:border-blue-500"
        />
        <button
          type="submit"
          className="px-3 py-1 bg-blue-600 hover:bg-blue-700 rounded text-xs text-white"
        >
          Go
        </button>
      </form>

      {/* Main Content: Screenshot + Elements */}
      <div className="flex-1 flex overflow-hidden">
        {/* Screenshot Area */}
        <div className="flex-1 flex items-center justify-center bg-zinc-950 overflow-auto">
          {browser.screenshot ? (
            <img
              src={`data:image/png;base64,${browser.screenshot}`}
              alt="Browser screenshot"
              className="max-w-full max-h-full object-contain"
            />
          ) : (
            <div className="text-center text-zinc-500">
              <Globe size={32} className="mx-auto mb-2 opacity-30" />
              <p className="text-xs">Enter a URL to browse</p>
            </div>
          )}
        </div>

        {/* Element List Sidebar */}
        <div className="w-64 border-l border-zinc-700 flex flex-col bg-zinc-900">
          <div className="px-2 py-1.5 border-b border-zinc-700 text-[10px] text-zinc-400 font-medium">
            Elements ({browser.elements.length})
          </div>
          <div className="flex-1 overflow-y-auto">
            {browser.elements.length === 0 ? (
              <div className="p-3 text-[10px] text-zinc-500 text-center">
                Navigate to a page to see elements
              </div>
            ) : (
              browser.elements.map(el => (
                <button
                  key={el.id}
                  onClick={() => handleElementClick(el)}
                  className={`w-full text-left px-2 py-1 border-b border-zinc-800 hover:bg-zinc-800 transition-colors ${
                    selectedElement?.id === el.id ? 'bg-blue-900/30 border-l-2 border-l-blue-500' : ''
                  }`}
                >
                  <div className="flex items-center gap-1.5">
                    <span className="text-[9px] bg-red-600 text-white px-1 rounded font-mono min-w-[16px] text-center">
                      {el.id}
                    </span>
                    <span className="text-[10px] text-zinc-400 font-mono">{el.tag}</span>
                    {el.role && (
                      <span className="text-[9px] text-zinc-500">[{el.role}]</span>
                    )}
                  </div>
                  {el.text && (
                    <div className="text-[10px] text-zinc-300 mt-0.5 truncate pl-5">
                      {el.text}
                    </div>
                  )}
                </button>
              ))
            )}
          </div>

          {/* Scroll Controls */}
          {browser.screenshot && (
            <div className="flex items-center justify-center gap-2 py-1 border-t border-zinc-700">
              <button
                onClick={() => browser.scroll('up')}
                className="p-1 hover:bg-zinc-800 rounded"
                title="Scroll up"
              >
                <ChevronUp size={14} className="text-zinc-400" />
              </button>
              <button
                onClick={() => browser.scroll('down')}
                className="p-1 hover:bg-zinc-800 rounded"
                title="Scroll down"
              >
                <ChevronDown size={14} className="text-zinc-400" />
              </button>
            </div>
          )}
        </div>
      </div>

      {/* Type Input Bar (shown when input element is selected) */}
      {selectedElement && (
        <div className="flex items-center gap-2 px-3 py-1.5 bg-zinc-800 border-t border-zinc-700">
          <span className="text-[10px] text-zinc-400">
            Type into <span className="text-red-400">[{selectedElement.id}]</span> {selectedElement.tag}:
          </span>
          <input
            type="text"
            value={typeText}
            onChange={e => setTypeText(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && handleType()}
            placeholder="Enter text..."
            className="flex-1 bg-zinc-900 border border-zinc-600 rounded px-2 py-0.5 text-xs text-white focus:outline-none focus:border-blue-500"
            autoFocus
          />
          <label className="flex items-center gap-1 text-[10px] text-zinc-400">
            <input
              type="checkbox"
              checked={autoSubmit}
              onChange={e => setAutoSubmit(e.target.checked)}
              className="rounded"
            />
            Submit
          </label>
          <button
            onClick={handleType}
            className="p-1 bg-blue-600 hover:bg-blue-700 rounded"
          >
            <Send size={12} className="text-white" />
          </button>
        </div>
      )}

      {/* Vision Description */}
      {browser.description && (
        <div className="px-3 py-2 bg-zinc-800 border-t border-zinc-700 max-h-24 overflow-y-auto">
          <div className="text-[10px] text-zinc-400 font-medium mb-1 flex items-center gap-1">
            <Eye size={10} /> Vision Description
          </div>
          <p className="text-[10px] text-zinc-300 leading-relaxed">
            {browser.description}
          </p>
        </div>
      )}

      {/* Error */}
      {browser.error && (
        <div className="px-3 py-1 bg-red-900/30 border-t border-red-700/50 text-[10px] text-red-400">
          {browser.error}
        </div>
      )}
    </div>
  );
}
