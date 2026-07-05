/**
 * MU Panel - Media Utopia Custom Home Assistant Panel
 * Uses Lit via CDN for web components.
 */

import { LitElement, html, css } from 'https://cdn.jsdelivr.net/npm/lit@3/+esm';

const styles = css`
  :host {
    display: flex;
    flex-direction: column;
    height: 100%;
    background: var(--primary-background-color, #1c1c1c);
    color: var(--primary-text-color, #fff);
    font-family: var(--paper-font-body1_-_font-family, 'Roboto', sans-serif);
    --mu-accent: var(--primary-color, #03a9f4);
    --mu-card-bg: var(--card-background-color, #2c2c2c);
    --mu-border: var(--divider-color, rgba(255,255,255,0.12));
    --mu-secondary: var(--secondary-text-color, #999);
  }

  .panel-container { display: flex; flex: 1; overflow: hidden; }

  .browser-pane {
    display: flex;
    flex-direction: column;
    border-right: 1px solid var(--mu-border);
    min-width: 280px;
    overflow: hidden;
  }

  .resize-handle { width: 5px; background: transparent; cursor: col-resize; flex-shrink: 0; }
  .resize-handle:hover, .resize-handle.dragging { background: var(--mu-accent); }

  .queue-pane { flex: 1; display: flex; flex-direction: column; min-width: 320px; overflow: hidden; }

  .pane-header {
    display: flex;
    align-items: center;
    padding: 8px 10px;
    gap: 8px;
    border-bottom: 1px solid var(--mu-border);
    background: var(--mu-card-bg);
    flex-shrink: 0;
  }

  .pane-title { font-size: 16px; font-weight: 500; flex: 1; }


  .renderer-select {
    background: var(--primary-background-color);
    color: var(--primary-text-color);
    border: 1px solid var(--mu-border);
    border-radius: 4px;
    padding: 6px 8px;
    font-size: 14px;
    max-width: 160px;
  }

  .lease-btn {
    background: var(--mu-accent);
    border: none;
    color: white;
    cursor: pointer;
    padding: 4px 10px;
    border-radius: 4px;
    font-size: 12px;
    font-weight: 500;
  }

  .lease-btn.owned {
    background: #4caf50;
  }

  .now-playing {
    display: flex;
    gap: 10px;
    padding: 10px;
    background: var(--mu-card-bg);
    border-bottom: 1px solid var(--mu-border);
    flex-shrink: 0;
  }

  .now-playing-art {
    width: 56px;
    height: 56px;
    border-radius: 6px;
    background: linear-gradient(135deg, rgba(3,169,244,0.15) 0%, rgba(3,169,244,0.05) 100%);
    object-fit: cover;
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .now-playing-art svg { width: 24px; height: 24px; opacity: 0.4; }

  .now-playing-info { flex: 1; display: flex; flex-direction: column; justify-content: center; min-width: 0; }

  .now-playing-title {
    font-size: 16px;
    font-weight: 500;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .now-playing-artist { font-size: 14px; color: var(--mu-secondary); }

  .transport {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 2px;
    padding: 10px 10px 6px;
    border-bottom: 1px solid var(--mu-border);
    flex-shrink: 0;
  }

  .transport-btn {
    background: transparent;
    border: none;
    color: var(--primary-text-color);
    cursor: pointer;
    width: 32px;
    height: 32px;
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .transport-btn svg { width: 16px; height: 16px; }
  .transport-btn:hover:not(:disabled) { background: rgba(255,255,255,0.1); }
  .transport-btn:disabled { opacity: 0.3; cursor: not-allowed; }
  .transport-btn.primary { background: var(--mu-accent); color: white; width: 40px; height: 40px; }
  .transport-btn.primary svg { width: 20px; height: 20px; }
  .transport-btn.active { color: var(--mu-accent); }

  .progress-section {
    padding: 4px 10px 8px;
    border-bottom: 1px solid var(--mu-border);
    flex-shrink: 0;
  }

  .progress-bar-container { display: flex; align-items: center; gap: 6px; }

  .progress-bar-container span { font-size: 12px; color: var(--mu-secondary); min-width: 32px; text-align: center; }

  .progress-track {
    flex: 1;
    height: 4px;
    background: rgba(255,255,255,0.15);
    border-radius: 2px;
    cursor: pointer;
    position: relative;
  }

  .progress-fill { height: 100%; background: var(--mu-accent); border-radius: 2px; position: relative; }

  .progress-track:hover { height: 6px; margin-top: -1px; }
  .progress-track:hover .progress-thumb { width: 12px; height: 12px; right: -6px; top: -3px; }

  .progress-thumb {
    position: absolute;
    right: -5px;
    top: -3px;
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--mu-accent);
    box-shadow: 0 1px 2px rgba(0,0,0,0.3);
  }

  .volume-row { display: flex; align-items: center; gap: 6px; margin-top: 6px; }
  .volume-row svg { width: 14px; height: 14px; opacity: 0.6; }
  .volume-track { width: 80px; height: 4px; background: rgba(255,255,255,0.15); border-radius: 2px; cursor: pointer; position: relative; }
  .volume-fill { height: 100%; background: var(--mu-accent); border-radius: 2px; position: relative; }
  .volume-thumb { position: absolute; right: -5px; top: -3px; width: 10px; height: 10px; border-radius: 50%; background: var(--mu-accent); box-shadow: 0 1px 2px rgba(0,0,0,0.3); }
  .volume-row span { font-size: 12px; color: var(--mu-secondary); }

  .queue-list, .browser-list { flex: 1; overflow-y: auto; padding: 4px; }

  .queue-item, .browser-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 6px;
    border-radius: 4px;
    cursor: pointer;
    transition: background 0.1s;
  }

  .queue-item:hover, .browser-item:hover { background: rgba(255,255,255,0.03); }
  .queue-item.playing { background: rgba(3,169,244,0.08); }

  .queue-item-art, .browser-item-art {
    width: 32px;
    height: 32px;
    border-radius: 3px;
    background: var(--mu-border);
    object-fit: cover;
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .queue-item-art svg, .browser-item-art svg, .browser-item-icon svg { width: 16px; height: 16px; opacity: 0.4; }
  .browser-item-art { width: 36px; height: 36px; }
  .browser-item-icon { width: 36px; height: 36px; display: flex; align-items: center; justify-content: center; flex-shrink: 0; color: var(--mu-secondary); }

  .queue-item-info, .browser-item-info { flex: 1; min-width: 0; overflow: hidden; }

  .queue-item-title, .browser-item-title {
    font-size: 14px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    line-height: 1.3;
  }

  .queue-item-artist, .browser-item-subtitle {
    font-size: 12px;
    color: var(--mu-secondary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    line-height: 1.3;
  }

  .queue-item-actions, .browser-item-actions {
    display: flex;
    gap: 2px;
    opacity: 0;
    transition: opacity 0.15s;
    flex-shrink: 0;
  }

  .queue-item:hover .queue-item-actions, .browser-item:hover .browser-item-actions { opacity: 1; }

  .queue-drag-handle {
    display: flex;
    align-items: center;
    justify-content: center;
    min-width: 32px;
    min-height: 32px;
    color: var(--mu-secondary, #888);
    cursor: grab;
  }
  .queue-drag-handle:active { cursor: grabbing; }
  .queue-drag-handle svg { width: 18px; height: 18px; pointer-events: none; }

  .icon-btn {
    background: transparent;
    border: none;
    color: var(--mu-secondary);
    cursor: pointer;
    width: 20px;
    height: 20px;
    border-radius: 3px;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .icon-btn svg { width: 12px; height: 12px; }
  .icon-btn:hover { background: rgba(255,255,255,0.1); color: var(--primary-text-color); }

  .action-btn {
    background: var(--mu-accent);
    border: none;
    color: white;
    cursor: pointer;
    padding: 3px 5px;
    border-radius: 3px;
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    flex-shrink: 0;
  }

  .action-btn.secondary { background: rgba(255,255,255,0.12); color: var(--primary-text-color); }

  .browser-with-index { display: flex; flex: 1; overflow: hidden; position: relative; }
  .browser-list-wrapper { flex: 1; overflow-y: auto; }

  .folder-actions {
    display: flex;
    gap: 6px;
    padding: 8px 10px;
    border-bottom: 1px solid var(--mu-border);
    background: var(--mu-card-bg);
    flex-shrink: 0;
  }
  
  .main-header {
    display: flex;
    align-items: center;
    height: 56px;
    padding: 0 4px;
    background: var(--app-header-background-color, var(--primary-color));
    color: var(--app-header-text-color, var(--text-primary-color, #fff));
    box-shadow: 0 2px 4px rgba(0,0,0,0.16);
    z-index: 10;
    flex-shrink: 0;
  }
  
  .main-header-title {
    font-size: 20px;
    font-weight: 500;
    margin-left: 16px;
    flex: 1;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .mobile-toggle {
    display: none;
    background: transparent;
    border: 1px solid var(--mu-border);
    color: var(--primary-text-color);
    padding: 4px 12px;
    border-radius: 4px;
    cursor: pointer;
    font-size: 12px;
  }

  .letter-index {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 4px 2px;
    background: var(--mu-card-bg);
    border-left: 1px solid var(--mu-border);
    flex-shrink: 0;
    overflow-y: auto;
  }

  .letter-index button {
    background: transparent;
    border: none;
    color: var(--mu-secondary);
    cursor: pointer;
    padding: 2px 6px;
    font-size: 12px;
    font-weight: 500;
    border-radius: 3px;
    min-width: 20px;
  }

  .letter-index button:hover { background: rgba(255,255,255,0.1); color: var(--mu-accent); }





  .zone-item {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 14px 0;
    border-bottom: 1px solid var(--mu-border);
  }

  .zone-item:last-child {
    border-bottom: none;
  }

  .zone-item-name {
    font-size: 14px;
    font-weight: 500;
    color: var(--primary-text-color);
  }

  .zone-item-name.disconnected {
    opacity: 0.5;
  }

  .zone-controls {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .zone-volume-slider {
    flex: 2;
    height: 6px;
    background: var(--mu-border);
    border-radius: 3px;
    cursor: pointer;
    position: relative;
  }

  .zone-volume-fill {
    height: 100%;
    background: var(--mu-accent);
    border-radius: 3px;
    position: relative;
  }

  .zone-volume-fill::after {
    content: '';
    position: absolute;
    right: -6px;
    top: 50%;
    transform: translateY(-50%);
    width: 12px;
    height: 12px;
    background: var(--mu-accent);
    border-radius: 50%;
    box-shadow: 0 1px 3px rgba(0,0,0,0.3);
  }

  .zone-mute-btn {
    background: transparent;
    border: none;
    color: var(--mu-secondary);
    cursor: pointer;
    width: 24px;
    height: 24px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 4px;
  }

  .zone-mute-btn:hover { background: rgba(255,255,255,0.1); }
  .zone-mute-btn.muted { color: #f44336; }

  .zone-source-select {
    background: var(--primary-background-color);
    color: var(--primary-text-color);
    border: 1px solid var(--mu-border);
    border-radius: 3px;
    padding: 4px 6px;
    font-size: 14px;
    max-width: 120px;
  }

  .breadcrumbs {
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 5px 8px;
    font-size: 13px;
    color: var(--mu-secondary);
    flex-shrink: 0;
    overflow-x: auto;
    flex-wrap: nowrap;
  }

  .breadcrumb {
    background: transparent;
    border: none;
    color: var(--mu-secondary);
    cursor: pointer;
    padding: 2px 4px;
    border-radius: 3px;
    font-size: 13px;
    white-space: nowrap;
  }

  .breadcrumb:hover { background: rgba(255,255,255,0.08); color: var(--primary-text-color); }
  .breadcrumb svg { width: 12px; height: 12px; }
  .breadcrumb-sep { opacity: 0.4; }

  .tabs { display: flex; border-bottom: 1px solid var(--mu-border); flex-shrink: 0; }

  .tab {
    flex: 1;
    background: transparent;
    border: none;
    color: var(--mu-secondary);
    padding: 8px;
    cursor: pointer;
    font-size: 13px;
    font-weight: 500;
    text-transform: uppercase;
    border-bottom: 2px solid transparent;
  }

  .tab:hover { color: var(--primary-text-color); }
  .tab.active { color: var(--mu-accent); border-bottom-color: var(--mu-accent); }

  .loading, .empty {
    display: flex;
    align-items: center;
    justify-content: center;
    flex: 1;
    color: var(--mu-secondary);
    font-size: 11px;
  }

  .spinner {
    width: 18px;
    height: 18px;
    border: 2px solid var(--mu-border);
    border-top-color: var(--mu-accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  @keyframes spin { to { transform: rotate(360deg); } }

  .render-error {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 12px;
    height: 100%;
    padding: 24px;
    color: var(--mu-text, #eee);
  }
  .render-error pre {
    max-width: 80%;
    max-height: 200px;
    overflow: auto;
    font-size: 10px;
    color: var(--mu-secondary, #999);
  }
  .render-error button {
    background: var(--mu-accent, #03a9f4);
    color: #fff;
    border: none;
    border-radius: 4px;
    padding: 8px 16px;
    cursor: pointer;
  }

  .toast {
    position: fixed;
    bottom: 24px;
    left: 50%;
    transform: translateX(-50%);
    background: var(--mu-card-bg);
    color: var(--primary-text-color);
    padding: 8px 20px;
    border-radius: 8px;
    font-size: 14px;
    box-shadow: 0 4px 12px rgba(0,0,0,0.3);
    z-index: 100;
    animation: toast-in 0.2s ease-out;
  }

  .toast.error {
    background: #b71c1c;
    color: #fff;
  }

  @keyframes toast-in {
    from { opacity: 0; transform: translateX(-50%) translateY(10px); }
    to { opacity: 1; transform: translateX(-50%) translateY(0); }
  }

  .queue-item.dragging { opacity: 0.4; }
  .queue-item.drag-over { border-top: 2px solid var(--mu-accent); margin-top: -2px; }

  /* Mobile View Toggle */
  .mobile-view-toggle {
    display: none;
    background: var(--mu-card-bg);
    border-bottom: 1px solid var(--mu-border);
    padding: 8px;
    gap: 4px;
  }

  .mobile-view-toggle button {
    flex: 1;
    background: transparent;
    border: 1px solid var(--mu-border);
    color: var(--mu-secondary);
    padding: 10px;
    border-radius: 4px;
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
  }

  .mobile-view-toggle button.active {
    background: var(--mu-accent);
    color: white;
    border-color: var(--mu-accent);
  }

  /* Responsive: Tablet (768px and below) */
  @media (max-width: 768px) {
    .mobile-toggle { display: block; }
    .browser-pane.hidden, .queue-pane.hidden { display: none !important; }

    .browser-pane { min-width: 240px; }
    .queue-pane { min-width: 280px; }
    .resize-handle { display: none; }

    .browser-item, .queue-item { padding: 10px 8px; }
    .browser-item-art, .queue-item-art { width: 44px; height: 44px; }

    .action-btn { padding: 6px 10px; font-size: 11px; }
    .transport-btn { width: 44px; height: 44px; }

    .zone-item { padding: 10px 0; }
    .zone-mute-btn { width: 36px; height: 36px; }
    .zone-volume-slider { height: 8px; }

    .letter-index button { padding: 6px 8px; font-size: 12px; }

    .search-bar { padding: 8px; }
    .search-input { padding: 8px; }
    .search-library-select { max-width: 100px; }
  }

  /* Responsive: Mobile (600px and below) */
  @media (max-width: 600px) {
    :host { padding-top: 0; }
    
    .mobile-view-toggle {
      display: flex;
      background: var(--mu-card-bg);
      border-bottom: 1px solid var(--mu-border);
    }
    
    .panel-container {
      flex-direction: column;
      background: var(--primary-background-color, #1c1c1c);
    }
    
    .browser-pane {
      width: 100% !important;
      border-right: none;
      border-bottom: 1px solid var(--mu-border);
      min-width: unset;
      flex: 1;
      min-height: 0;
    }
    
    .browser-pane.hidden { display: none; }
    .queue-pane.hidden { display: none; }
    
    .queue-pane {
      min-width: unset;
      flex: 1;
      min-height: 0;
    }
    
    .pane-header { padding: 10px 12px; }
    .renderer-select { max-width: none; flex: 1; font-size: 14px; padding: 8px; }
    .lease-btn { padding: 8px 12px; font-size: 12px; }
    
    .now-playing { padding: 12px; gap: 12px; }
    .now-playing-art { width: 64px; height: 64px; }
    .now-playing-title { font-size: 15px; }
    .now-playing-artist { font-size: 13px; }
    
    .transport { padding: 12px; gap: 8px; }
    .transport-btn { width: 48px; height: 48px; }
    .transport-btn.main { width: 56px; height: 56px; }
    
    .seek-bar, .volume-bar { height: 10px; }
    
    .browser-item { padding: 12px 10px; user-select: none; }
    .queue-item { padding: 12px 10px 12px 0; user-select: none; touch-action: pan-y; } /* 0 padding left for handle */
    
    .browser-item-title, .queue-item-title { font-size: 14px; margin-left: 8px; }
    .browser-item-subtitle, .queue-item-subtitle { font-size: 12px; margin-left: 8px; }
    
    .queue-drag-handle {
      padding: 0 4px;
      cursor: grab;
      touch-action: none;
      color: var(--mu-secondary, #888);
      display: flex;
      align-items: center;
      justify-content: center;
      min-width: 48px;
      min-height: 48px;
    }
    .queue-drag-handle:active { cursor: grabbing; }
    .queue-drag-handle svg {
      width: 24px;
      height: 24px;
      pointer-events: none; /* Pass touches to container */
    }
    
    .action-btn { padding: 8px 12px; font-size: 12px; }
    .icon-btn { width: 32px; height: 32px; }
    .icon-btn svg { width: 16px; height: 16px; }
    
    .tabs { gap: 0; }
    .tab { padding: 12px 8px; font-size: 11px; }
    

    .zone-item { padding: 12px 0; gap: 10px; }
    .zone-item-name { font-size: 13px; }
    .zone-item-name { font-size: 13px; }
    /* Touch target container (invisible shim) */
    .zone-volume-slider { 
      height: 10px; 
      position: relative;
      touch-action: none;
      user-select: none;
      cursor: pointer;
      margin: 0 4px;
      /* Ensure visible bar is same as before */
      background: var(--mu-border);
      border-radius: 5px;
    }
    /* Increase touch target with pseudo-element */
    .zone-volume-slider::after {
      content: '';
      position: absolute;
      top: -15px; bottom: -15px; left: 0; right: 0;
      z-index: 10;
    }
    .zone-volume-fill {
      height: 100%;
      background: var(--mu-accent);
      border-radius: 5px;
      pointer-events: none; /* Let clicks pass to slider */
    }
    .zone-mute-btn { width: 40px; height: 40px; }
    .zone-source-select { padding: 6px; font-size: 12px; max-width: none; }
    
    .breadcrumbs { padding: 8px 10px; }
    .breadcrumb { padding: 6px 8px; font-size: 12px; }
    
    .letter-index {
      padding: 6px 4px;
      justify-content: flex-start;
      overflow-y: auto;
    }
    .letter-index button { padding: 8px 10px; font-size: 13px; min-width: 24px; }
    
    .toast { bottom: 60px; padding: 10px 18px; font-size: 13px; }
    
    /* Always show action buttons on touch devices */
    .browser-item-actions, .queue-item-actions { opacity: 1; }

    .search-bar { padding: 8px; gap: 6px; }
    .search-input { font-size: 14px; padding: 10px 8px; }
    .search-library-select { font-size: 14px; padding: 10px 8px; max-width: 100px; }
    .search-bar .action-btn { padding: 8px 12px; }
  }

  .search-bar {
    display: flex;
    gap: 6px;
    padding: 6px 8px;
    border-bottom: 1px solid var(--mu-border);
    background: var(--mu-card-bg);
    flex-shrink: 0;
    align-items: center;
  }

  .search-type-toggle {
    display: flex;
    gap: 4px;
    padding: 4px 8px;
    border-bottom: 1px solid var(--mu-border);
    background: var(--mu-card-bg);
    flex-shrink: 0;
  }
  .search-type-toggle button {
    background: transparent;
    border: 1px solid var(--mu-border);
    border-radius: 10px;
    color: var(--mu-secondary);
    font-size: 10px;
    padding: 2px 10px;
    cursor: pointer;
  }
  .search-type-toggle button.active {
    background: var(--mu-accent);
    border-color: var(--mu-accent);
    color: #fff;
  }

  .search-input {
    flex: 1;
    background: var(--primary-background-color);
    color: var(--primary-text-color);
    border: 1px solid var(--mu-border);
    border-radius: 4px;
    padding: 6px 8px;
    font-size: 14px;
    font-family: inherit;
    min-width: 0;
  }

  .search-input::placeholder { color: var(--mu-secondary); }
  .search-input:focus { outline: none; border-color: var(--mu-accent); }

  .search-input-wrapper {
    flex: 1;
    position: relative;
    display: flex;
    align-items: center;
    min-width: 0;
  }

  .search-input-wrapper .search-input {
    flex: 1;
    padding-right: 28px;
  }

  .search-clear {
    position: absolute;
    right: 4px;
    top: 50%;
    transform: translateY(-50%);
    background: transparent;
    border: none;
    color: var(--mu-secondary);
    cursor: pointer;
    padding: 2px 4px;
    font-size: 14px;
  }

  .search-clear:hover {
    color: var(--primary-text-color);
  }

  .search-library-select {
    background: var(--primary-background-color);
    color: var(--primary-text-color);
    border: 1px solid var(--mu-border);
    border-radius: 4px;
    padding: 6px 8px;
    font-size: 14px;
    max-width: 120px;
  }

  .search-bar .action-btn svg { width: 14px; height: 14px; vertical-align: middle; }

  .eq-bars {
    display: flex;
    gap: 1px;
    align-items: flex-end;
    height: 14px;
    width: 14px;
    flex-shrink: 0;
  }

  .eq-bar {
    width: 3px;
    background: var(--mu-accent);
    border-radius: 1px;
    animation: eq 0.8s ease-in-out infinite alternate;
  }

  .eq-bar:nth-child(1) { height: 60%; animation-delay: -0.4s; }
  .eq-bar:nth-child(2) { height: 100%; animation-delay: -0.2s; }
  .eq-bar:nth-child(3) { height: 40%; animation-delay: -0.6s; }

  .paused .eq-bar { animation-play-state: paused; }

  @keyframes eq {
    from { height: 20%; }
    to { height: 100%; }
  }

  .volume-overlay {
    position: fixed;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    background: rgba(0,0,0,0.8);
    color: white;
    padding: 16px 24px;
    border-radius: 12px;
    font-size: 24px;
    font-weight: 500;
    z-index: 200;
    pointer-events: none;
  }
`;

const icons = {
  music: html`<svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 3v10.55c-.59-.34-1.27-.55-2-.55-2.21 0-4 1.79-4 4s1.79 4 4 4 4-1.79 4-4V7h4V3h-6z"/></svg>`,
  folder: html`<svg viewBox="0 0 24 24" fill="currentColor"><path d="M10 4H4c-1.1 0-1.99.9-1.99 2L2 18c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2h-8l-2-2z"/></svg>`,
  library: html`<svg viewBox="0 0 24 24" fill="currentColor"><path d="M4 6H2v14c0 1.1.9 2 2 2h14v-2H4V6zm16-4H8c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2z"/></svg>`,
  playlist: html`<svg viewBox="0 0 24 24" fill="currentColor"><path d="M15 6H3v2h12V6zm0 4H3v2h12v-2zM3 16h8v-2H3v2zM17 6v8.18c-.31-.11-.65-.18-1-.18-1.66 0-3 1.34-3 3s1.34 3 3 3 3-1.34 3-3V8h3V6h-5z"/></svg>`,
  camera: html`<svg viewBox="0 0 24 24" fill="currentColor"><path d="M9 2L7.17 4H4c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V6c0-1.1-.9-2-2-2h-3.17L15 2H9z"/></svg>`,
  prev: html`<svg viewBox="0 0 24 24" fill="currentColor"><path d="M6 6h2v12H6zm3.5 6l8.5 6V6z"/></svg>`,
  next: html`<svg viewBox="0 0 24 24" fill="currentColor"><path d="M6 18l8.5-6L6 6v12zM16 6v12h2V6h-2z"/></svg>`,
  drag: html`<svg viewBox="0 0 24 24" fill="currentColor"><path d="M3,6H21V8H3V6M3,11H21V13H3V11M3,16H21V18H3V16Z"/></svg>`,
  play: html`<svg viewBox="0 0 24 24" fill="currentColor"><path d="M8 5v14l11-7z"/></svg>`,
  pause: html`<svg viewBox="0 0 24 24" fill="currentColor"><path d="M6 19h4V5H6v14zm8-14v14h4V5h-4z"/></svg>`,
  stop: html`<svg viewBox="0 0 24 24" fill="currentColor"><path d="M6 6h12v12H6z"/></svg>`,
  volume: html`<svg viewBox="0 0 24 24" fill="currentColor"><path d="M3 9v6h4l5 5V4L7 9H3zm13.5 3c0-1.77-1.02-3.29-2.5-4.03v8.05c1.48-.73 2.5-2.25 2.5-4.02z"/></svg>`,
  close: html`<svg viewBox="0 0 24 24" fill="currentColor"><path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z"/></svg>`,
  home: html`<svg viewBox="0 0 24 24" fill="currentColor"><path d="M10 20v-6h4v6h5v-8h3L12 3 2 12h3v8z"/></svg>`,
  shuffle: html`<svg viewBox="0 0 24 24" fill="currentColor"><path d="M10.59 9.17L5.41 4 4 5.41l5.17 5.17 1.42-1.41zM14.5 4l2.04 2.04L4 18.59 5.41 20 17.96 7.46 20 9.5V4h-5.5zm.33 9.41l-1.41 1.41 3.13 3.13L14.5 20H20v-5.5l-2.04 2.04-3.13-3.13z"/></svg>`,
  repeat: html`<svg viewBox="0 0 24 24" fill="currentColor"><path d="M7 7h10v3l4-4-4-4v3H5v6h2V7zm10 10H7v-3l-4 4 4 4v-3h12v-6h-2v4z"/></svg>`,
  repeatOne: html`<svg viewBox="0 0 24 24" fill="currentColor"><path d="M7 7h10v3l4-4-4-4v3H5v6h2V7zm10 10H7v-3l-4 4 4 4v-3h12v-6h-2v4zm-4-2V9h-1l-2 1v1h1.5v4H13z"/></svg>`,
  add: html`<svg viewBox="0 0 24 24" fill="currentColor"><path d="M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z"/></svg>`,
  search: html`<svg viewBox="0 0 24 24" fill="currentColor"><path d="M15.5 14h-.79l-.28-.27C15.41 12.59 16 11.11 16 9.5 16 5.91 13.09 3 9.5 3S3 5.91 3 9.5 5.91 16 9.5 16c1.61 0 3.09-.59 4.23-1.57l.27.28v.79l5 4.99L20.49 19l-4.99-5zm-6 0C7.01 14 5 11.99 5 9.5S7.01 5 9.5 5 14 7.01 14 9.5 11.99 14 9.5 14z"/></svg>`,
  clear: html`<svg viewBox="0 0 24 24" fill="currentColor"><path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z"/></svg>`,
};

function formatTime(ms) {
  if (!ms || ms < 0) return '0:00';
  const s = Math.floor(ms / 1000);
  return `${Math.floor(s / 60)}:${(s % 60).toString().padStart(2, '0')}`;
}

class MuPanel extends LitElement {
  static styles = styles;

  static properties = {
    hass: { type: Object },
    narrow: { type: Boolean },
    renderers: { state: true },
    selectedRenderer: { state: true },
    rendererState: { state: true },
    queue: { state: true },
    leaseOwned: { state: true },
    browserTab: { state: true },
    libraries: { state: true },
    browserPath: { state: true },
    browserItems: { state: true },
    browserLoading: { state: true },
    playlists: { state: true },
    snapshots: { state: true },
    loading: { state: true },
    toast: { state: true },
    dragIndex: { state: true },
    dragOverIndex: { state: true },
    browserWidth: { state: true },
    resizing: { state: true },
    browserTotal: { state: true },
    zoneControllers: { state: true },
    zones: { state: true },
    mobileView: { state: true },
    searchQuery: { state: true },
    searchResults: { state: true },
    searchLoading: { state: true },
    searchTotal: { state: true },
    searchType: { state: true },
    searchLibraryId: { state: true },
    _snapshotSaving: { state: true },
    _creatingPlaylist: { state: true },
    _createPlaylistSnapshotId: { state: true },
    _createPlaylistName: { state: true },
    queueLoading: { state: true },
    volumeOverlay: { state: true },
    _failedArtUrls: { state: true },
    _renamingPlaylistId: { state: true },
    _renamePlaylistName: { state: true },
    _pendingDelete: { state: true },
  };

  constructor() {
    super();
    this.renderers = [];
    this.selectedRenderer = null;
    this.rendererState = null;
    this.queue = [];
    this.leaseOwned = false;
    this.browserTab = 'browse';
    this.libraries = [];
    this.browserPath = [];
    this.browserItems = [];
    this.browserLoading = false;
    this.browserTotal = 0;
    this.playlists = [];
    this.playlistServers = [];
    this.selectedPlaylistServer = null;
    this.snapshots = [];
    this.loading = true;
    this.toast = null;
    this.dragIndex = null;
    this.dragOverIndex = null;
    this.browserWidth = 360;
    this.resizing = false;
    this.zoneControllers = [];
    this.zones = [];
    this.mobileView = 'player'; // 'browser' or 'player'
    this.searchQuery = '';
    this.searchResults = null;
    this.searchLoading = false;
    this.searchTotal = 0;
    this.searchLibraryId = '';
    // What kind of results to search for: 'albums' | 'songs' | 'artists'.
    // Albums by default — restored from the last-used preference.
    this.searchType = this._prefGet('searchType') || 'albums';
    this._snapshotSaving = false;
    this._creatingPlaylist = false;
    this._createPlaylistSnapshotId = null;
    this._createPlaylistName = '';
    this.queueLoading = false;
    this.volumeOverlay = null;
    this._renamingPlaylistId = null;
    this._renamePlaylistName = '';
    this._pendingDelete = null;
    this._volumeOverlayTimer = null;
    this._refreshInterval = null;
    this._localSeekPos = null;
    this._localVolume = null;
    this._stateUnsubscribe = null;
    this._searchDebounce = null;
    this._positionTimer = null;
    this._interpolatedPos = null;
    this._lastPushAt = 0;
    this._lastPosUpdate = 0;
    this._failedArtUrls = new Set();
    this._toastTimer = null;
    this._lastWSError = null;
    this._initGeneration = 0;
    this._initLoadFailed = false;
    this._subscribeToken = 0;
    this._searchToken = 0;
    this._browseToken = 0;
  }

  connectedCallback() {
    super.connectedCallback();
    this._init().catch(err => console.error('[MU] init failed:', err));
    this._boundMouseMove = this._onMouseMove.bind(this);
    this._boundMouseUp = this._onMouseUp.bind(this);
    this._boundSeekMove = this._onSeekMove.bind(this);
    this._boundSeekEnd = this._onSeekEnd.bind(this);
    this._boundVolMove = this._onVolMove.bind(this);
    this._boundVolEnd = this._onVolEnd.bind(this);
    this._boundZoneVolMove = this._onZoneVolMove.bind(this);
    this._boundZoneVolEnd = this._onZoneVolEnd.bind(this);
    this._boundKeydown = this._onKeydown.bind(this);
    document.addEventListener('keydown', this._boundKeydown);
    // Refresh when the tab/panel becomes visible again — after a long
    // background period timers were throttled and pushes may have been
    // missed; without this the panel can come back stale or blank.
    this._boundVisibility = this._onVisibilityChange.bind(this);
    document.addEventListener('visibilitychange', this._boundVisibility);
  }

  _onVisibilityChange() {
    if (document.visibilityState !== 'visible' || !this.isConnected) return;
    this.requestUpdate();
    if (this.selectedRenderer) {
      this._loadRendererState().catch(() => {});
      this._loadQueue().catch(() => {});
    }
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    // Invalidate any in-flight _init / subscribe so they don't recreate
    // timers or subscriptions after cleanup.
    this._initGeneration++;
    this._subscribeToken++;
    if (this._refreshInterval) { clearInterval(this._refreshInterval); this._refreshInterval = null; }
    if (this._positionTimer) { clearInterval(this._positionTimer); this._positionTimer = null; }
    if (this._stateUnsubscribe) {
      this._stateUnsubscribe();
      this._stateUnsubscribe = null;
    }
    if (this._searchDebounce) clearTimeout(this._searchDebounce);
    if (this._seekDebounce) clearTimeout(this._seekDebounce);
    if (this._volDebounce) clearTimeout(this._volDebounce);
    if (this._zoneVolDebounce) clearTimeout(this._zoneVolDebounce);
    if (this._volumeOverlayTimer) clearTimeout(this._volumeOverlayTimer);
    if (this._toastTimer) { clearTimeout(this._toastTimer); this._toastTimer = null; }
    this._removeTouchListeners();
    document.removeEventListener('keydown', this._boundKeydown);
    document.removeEventListener('visibilitychange', this._boundVisibility);
    document.removeEventListener('mousemove', this._boundMouseMove);
    document.removeEventListener('mouseup', this._boundMouseUp);
    document.removeEventListener('mousemove', this._boundSeekMove);
    document.removeEventListener('mouseup', this._boundSeekEnd);
    document.removeEventListener('mousemove', this._boundVolMove);
    document.removeEventListener('mouseup', this._boundVolEnd);
    document.removeEventListener('mousemove', this._boundZoneVolMove);
    document.removeEventListener('mouseup', this._boundZoneVolEnd);
  }

  async _init() {
    const gen = ++this._initGeneration;
    const stale = () => gen !== this._initGeneration || !this.isConnected;
    await this._loadInitialData();
    if (stale()) return;
    this.loading = false;
    // Backend may legitimately return not_ready/[] during HA startup — retry
    // with a short backoff so renderers appearing later still populate the panel.
    for (const delayMs of [2000, 5000]) {
      if (this.renderers.length && !this._initLoadFailed) break;
      await new Promise(resolve => setTimeout(resolve, delayMs));
      if (stale()) return;
      await this._loadInitialData();
      if (stale()) return;
    }
    this._subscribeRendererState();
    if (stale()) return;
    // Keep a slower poll as fallback (every 10s) in case subscription misses
    this._refreshInterval = setInterval(
      () => this._refreshState().catch(err => console.error('[MU] refresh failed:', err)),
      10000
    );
    // Client-side position interpolation (1s tick) avoids needing server push for progress bar
    this._positionTimer = setInterval(() => this._interpolatePosition(), 1000);
  }

  async _loadInitialData() {
    const results = [];
    results.push(await this._loadRenderers());
    results.push(await this._loadLibraries());
    results.push(await this._loadPlaylistServers());
    results.push(await this._loadPlaylists());
    results.push(await this._loadSnapshots());
    this._initLoadFailed = results.some(r => r === null);
  }

  _interpolatePosition() {
    const pb = this.rendererState?.playback;
    if (!pb || pb.status !== 'playing' || this._localSeekPos !== null) return;
    const now = Date.now();
    if (this._lastPosUpdate === 0) {
      this._lastPosUpdate = now;
      this._interpolatedPos = pb.position_ms || 0;
      return;
    }
    const elapsed = now - this._lastPosUpdate;
    this._lastPosUpdate = now;
    this._interpolatedPos = Math.min(
      (this._interpolatedPos || 0) + elapsed,
      pb.duration_ms || 0
    );
    this.requestUpdate();
  }

  async _callWS(type, data = {}) {
    try {
      this._lastWSError = null;
      return await this.hass.callWS({ type, ...data });
    }
    catch (err) { console.error(`WS ${type}:`, err); this._lastWSError = err; return null; }
  }

  // Commands may fail as a swallowed WS error (null) or a structured
  // { success: false, error: {...} } reply — treat both as failure.
  _wsOk(r) { return r !== null && r !== undefined && r.success !== false; }

  _showToast(msg, { error = false } = {}) {
    if (this._toastTimer) clearTimeout(this._toastTimer);
    this.toast = { msg, error };
    this._toastTimer = setTimeout(() => { this.toast = null; this._toastTimer = null; }, error ? 3500 : 2000);
  }

  shouldUpdate(changed) {
    // hass is reassigned by HA on every state change house-wide; the panel
    // only uses it for callWS/connection, so skip renders it would trigger.
    if (changed.size === 1 && changed.has('hass')) return false;
    return super.shouldUpdate(changed);
  }

  // Track art URLs that failed to load and render a placeholder instead.
  // Declarative (keyed on item data, not DOM state) so Lit's positional
  // node reuse can't leak a hidden <img> onto a different item.
  _onArtError(url) {
    if (!url || this._failedArtUrls.has(url)) return;
    const next = new Set(this._failedArtUrls);
    next.add(url);
    this._failedArtUrls = next;
  }

  _renderArt(url, cls, fallbackIcon) {
    return url && !this._failedArtUrls.has(url)
      ? html`<img class="${cls}" src="${url}" @error=${() => this._onArtError(url)}/>`
      : html`<div class="${cls}">${fallbackIcon}</div>`;
  }

  _onResizeStart(e) {
    this.resizing = true;
    document.addEventListener('mousemove', this._boundMouseMove);
    document.addEventListener('mouseup', this._boundMouseUp);
    e.preventDefault();
  }

  _onMouseMove(e) {
    if (!this.resizing) return;
    const c = this.shadowRoot.querySelector('.panel-container');
    if (c) { const r = c.getBoundingClientRect(); this.browserWidth = Math.max(280, Math.min(e.clientX - r.left, r.width - 320)); }
  }

  _onMouseUp() {
    this.resizing = false;
    document.removeEventListener('mousemove', this._boundMouseMove);
    document.removeEventListener('mouseup', this._boundMouseUp);
  }

  async _loadRenderers() {
    const r = await this._callWS('mu/renderers');
    if (r) {
      r.sort((a, b) => (a.name || '').localeCompare(b.name || ''));
      this.renderers = r;
      if (!this.selectedRenderer && r.length) {
        // Prefer the last-used renderer over the first in the list.
        const stored = this._prefGet('renderer');
        const match = stored && r.find(x => x.rendererId === stored);
        this.selectedRenderer = match ? match.rendererId : r[0].rendererId;
        await this._loadRendererState();
        await this._loadQueue();
      }
    }
    return r;
  }

  async _loadRendererState() {
    if (!this.selectedRenderer) return;
    const requestedAt = Date.now();
    const r = await this._callWS('mu/renderer_state', { renderer_id: this.selectedRenderer });
    // A push event that arrived while this poll was in flight is fresher
    // than the polled snapshot (the bridge's state can lag right after a
    // command, e.g. a queue jump) — applying the stale poll here would
    // drag the progress bar back to the previous track's position.
    if (this._lastPushAt > requestedAt) return;
    if (r) {
      const oldRev = this.rendererState?.queue?.revision;
      this.rendererState = r;
      this.leaseOwned = r.session?.owned || false;
      // Sync interpolation to server value
      this._interpolatedPos = r.playback?.position_ms || 0;
      this._lastPosUpdate = Date.now();
      if (r.queue?.revision !== oldRev) {
        await this._loadQueue();
      }
    }
  }

  async _loadQueue() {
    if (!this.selectedRenderer) return;
    const r = await this._callWS('mu/queue_get', { renderer_id: this.selectedRenderer });
    if (r) this.queue = r.entries || [];
  }

  async _loadLibraries() {
    const r = await this._callWS('mu/libraries_list');
    if (r) {
      this.libraries = r;
      // Restore the last-used search library (falling back to the first)
      // so search doesn't default to whatever happens to sort first.
      if (!this.searchLibraryId) {
        const stored = this._prefGet('searchLibrary');
        const match = stored && r.find(lib => lib.libraryId === stored);
        this.searchLibraryId = match ? match.libraryId : (r[0] && r[0].libraryId) || '';
      }
      // Only reset the browse view at the root — background retries must
      // not blow away in-progress navigation.
      if (!this.browserPath.length) {
        this.browserItems = r.map(lib => ({ itemId: lib.libraryId, title: lib.name, isContainer: true, isLibrary: true }));
      }
    }
    return r;
  }

  async _loadPlaylists() { const r = await this._callWS('mu/playlists_list'); if (r) this.playlists = r; return r; }
  async _loadSnapshots() { const r = await this._callWS('mu/snapshots_list'); if (r) this.snapshots = r; return r; }
  async _loadPlaylistServers() {
    const r = await this._callWS('mu/playlist_servers_list');
    if (r) {
      this.playlistServers = r;
      const selected = r.find(s => s.selected);
      this.selectedPlaylistServer = selected?.nodeId || null;
    }
    return r;
  }
  async _selectPlaylistServer(e) {
    const nodeId = e.target.value;
    if (nodeId === this.selectedPlaylistServer) return;
    const r = await this._callWS('mu/playlist_server_select', { node_id: nodeId });
    if (r?.success) {
      this.selectedPlaylistServer = nodeId;
      await this._loadPlaylists();
      this._showToast('Server switched');
    }
  }
  async _refreshState() {
    // Backend may come up after us (HA startup): if we have no renderers yet
    // or the last initial load failed, retry the full load on each tick so
    // renderers appearing later populate the panel without a page reload.
    if (!this.renderers.length || this._initLoadFailed) {
      const hadRenderer = !!this.selectedRenderer;
      await this._loadInitialData();
      if (!this.isConnected) return;
      if (this.selectedRenderer && (!hadRenderer || !this._stateUnsubscribe)) this._subscribeRendererState();
      return;
    }
    await this._loadRendererState();
  }

  async _subscribeRendererState() {
    if (!this.selectedRenderer || !this.hass) return;
    // Single-flight guard: token invalidates any older in-flight subscribe
    // so it can't leak or overwrite state for a newly selected renderer.
    const token = ++this._subscribeToken;
    // Unsubscribe from previous
    if (this._stateUnsubscribe) {
      this._stateUnsubscribe();
      this._stateUnsubscribe = null;
    }
    try {
      const unsubscribe = await this.hass.connection.subscribeMessage(
        (event) => this._onRendererStateEvent(event),
        { type: 'mu/subscribe_renderer_state', renderer_id: this.selectedRenderer }
      );
      if (token !== this._subscribeToken || !this.isConnected) {
        unsubscribe();
        return;
      }
      this._stateUnsubscribe = unsubscribe;
    } catch (err) {
      console.warn('State subscription failed, using poll fallback:', err);
    }
  }

  _onRendererStateEvent(event) {
    // Never throw out of a WS subscription callback — an exception here
    // can break the connection's event dispatch and silently kill every
    // subscription on the socket.
    try {
      this._applyRendererStateEvent(event);
    } catch (err) {
      console.error('[MU] state event failed:', err);
    }
  }

  _applyRendererStateEvent(event) {
    if (!event) return;
    const prev = this.rendererState;
    // Skip if nothing meaningful changed (avoid flicker from frequent state publishes)
    if (prev
        && prev.playback?.status === event.playback?.status
        && prev.playback?.position_ms === event.playback?.position_ms
        && prev.current?.title === event.current?.title
        && prev.current?.artist === event.current?.artist
        && prev.queue?.revision === event.queue?.revision
        && prev.queue?.index === event.queue?.index
        && prev.playback?.volume === event.playback?.volume
        && prev.playback?.mute === event.playback?.mute
        && prev.session?.owned === event.session?.owned) {
      return; // No meaningful change, skip re-render
    }
    const oldRev = prev?.queue?.revision;
    this.rendererState = event;
    this.leaseOwned = event.session?.owned || false;
    this._lastPushAt = Date.now();
    // Reset position interpolation to server value
    this._interpolatedPos = event.playback?.position_ms || 0;
    this._lastPosUpdate = Date.now();
    if (event.queue?.revision !== oldRev) {
      this._loadQueue();
    }
  }

  _onKeydown(e) {
    // Don't intercept when typing in inputs (check both target and composedPath for shadow DOM)
    const target = e.composedPath?.()?.[0] || e.target;
    const tag = target?.tagName;
    if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;
    if (target?.isContentEditable) return;
    if (!this.selectedRenderer) return;

    switch (e.key) {
      case ' ':
        // Leave Space alone for focused buttons/focusable elements (it activates them)
        if (tag === 'BUTTON' || target?.hasAttribute?.('tabindex')) return;
        e.preventDefault();
        this._transport(this.rendererState?.playback?.status === 'playing' ? 'pause' : 'play');
        break;
      case 'ArrowRight':
        e.preventDefault();
        if (e.shiftKey) { this._transport('next'); }
        else { this._seekRelative(10000); } // +10s
        break;
      case 'ArrowLeft':
        e.preventDefault();
        if (e.shiftKey) { this._transport('prev'); }
        else { this._seekRelative(-10000); } // -10s
        break;
      case 'ArrowUp':
        e.preventDefault();
        this._adjustVolume(0.05);
        break;
      case 'ArrowDown':
        e.preventDefault();
        this._adjustVolume(-0.05);
        break;
      case 'm':
        this._toggleMute();
        break;
    }
  }

  async _seekRelative(deltaMs) {
    if (!this.rendererState?.playback) return;
    const pos = (this._interpolatedPos ?? this.rendererState.playback.position_ms ?? 0) + deltaMs;
    const clamped = Math.max(0, Math.min(pos, this.rendererState.playback.duration_ms || 0));
    this._interpolatedPos = clamped;
    this._lastPosUpdate = Date.now();
    this.requestUpdate();
    await this._callWS('mu/seek', { renderer_id: this.selectedRenderer, position_ms: clamped });
  }

  async _adjustVolume(delta) {
    if (!this.rendererState?.playback) return;
    // Build on the optimistic local volume so rapid keypresses accumulate
    const base = this._localVolume !== null ? this._localVolume : (this.rendererState.playback.volume || 0);
    const vol = Math.max(0, Math.min(1, base + delta));
    this._localVolume = vol;
    this.volumeOverlay = Math.round(vol * 100);
    if (this._volumeOverlayTimer) clearTimeout(this._volumeOverlayTimer);
    this._volumeOverlayTimer = setTimeout(() => { this.volumeOverlay = null; this._localVolume = null; this.requestUpdate(); }, 1000);
    this.requestUpdate();
    this._callWS('mu/volume', { renderer_id: this.selectedRenderer, volume: vol });
  }

  async _toggleMute() {
    if (!this.rendererState?.playback) return;
    const muted = !this.rendererState.playback.mute;
    await this._callWS('mu/volume', { renderer_id: this.selectedRenderer, mute: muted });
  }

  async _selectRenderer(e) {
    // Unsubscribe from old renderer FIRST to stop stale state events
    if (this._stateUnsubscribe) {
      this._stateUnsubscribe();
      this._stateUnsubscribe = null;
    }
    this.selectedRenderer = e.target.value;
    this._prefSet('renderer', this.selectedRenderer);
    // Clear stale state immediately so UI shows the new renderer's loading state
    this.rendererState = null;
    this.queue = [];
    await this._loadRendererState();
    await this._loadQueue();
    this._subscribeRendererState();
  }

  async _acquireLease() {
    if (!this.selectedRenderer) return;
    const r = await this._callWS('mu/lease_acquire', { renderer_id: this.selectedRenderer });
    if (r?.success) { this.leaseOwned = true; this._showToast('Acquired'); await this._loadRendererState(); }
    else { this._showToast('Could not take control', { error: true }); }
  }

  async _releaseLease() {
    if (!this.selectedRenderer) return;
    const r = await this._callWS('mu/lease_release', { renderer_id: this.selectedRenderer });
    if (r?.success) { this.leaseOwned = false; this._showToast('Released'); await this._loadRendererState(); }
  }

  async _transport(action) {
    if (!this.selectedRenderer) return;
    if (!this.leaseOwned) {
      this._showToast('Acquire control first');
      return;
    }
    await this._callWS('mu/transport', { renderer_id: this.selectedRenderer, action });
    await this._loadRendererState();
  }

  // Slider drag handling
  _onSeekStart(e) {
    if (!this.leaseOwned || !this.selectedRenderer) return;
    this._seekDragging = true;
    this._seekTrack = e.currentTarget;
    this._updateSeekFromEvent(e);

    if (e.type === 'touchstart') {
      document.addEventListener('touchmove', this._boundSeekMove, { passive: false });
      document.addEventListener('touchend', this._boundSeekEnd);
      document.addEventListener('touchcancel', this._boundSeekEnd);
    } else {
      document.addEventListener('mousemove', this._boundSeekMove);
      document.addEventListener('mouseup', this._boundSeekEnd);
    }
    if (e.cancelable) e.preventDefault();
  }

  _onSeekMove(e) {
    if (!this._seekDragging) return;
    if (e.type === 'touchmove') e.preventDefault();
    this._updateSeekFromEvent(e);
  }

  _onSeekEnd() {
    if (!this._seekDragging) return;
    this._seekDragging = false;
    document.removeEventListener('mousemove', this._boundSeekMove);
    document.removeEventListener('mouseup', this._boundSeekEnd);
    document.removeEventListener('touchmove', this._boundSeekMove);
    document.removeEventListener('touchend', this._boundSeekEnd);
    document.removeEventListener('touchcancel', this._boundSeekEnd);
    // Optimistically update interpolation to seek target, then clear local override
    if (this._localSeekPos !== null) {
      this._interpolatedPos = this._localSeekPos;
      this._lastPosUpdate = Date.now();
    }
    setTimeout(() => { this._localSeekPos = null; this.requestUpdate(); }, 300);
  }

  _updateSeekFromEvent(e) {
    if (!this._seekTrack) return;
    let clientX;
    if (e.touches && e.touches.length > 0) clientX = e.touches[0].clientX;
    else clientX = e.clientX;

    const rect = this._seekTrack.getBoundingClientRect();
    const pct = Math.max(0, Math.min(1, (clientX - rect.left) / rect.width));
    const dur = this.rendererState?.playback?.duration_ms || 0;
    // Update UI immediately
    this._localSeekPos = Math.round(pct * dur);
    this.requestUpdate();
    // Debounce API call
    clearTimeout(this._seekDebounce);
    this._seekDebounce = setTimeout(() => {
      this._callWS('mu/seek', { renderer_id: this.selectedRenderer, position_ms: this._localSeekPos });
    }, 50);
  }

  _onVolumeStart(e) {
    if (!this.leaseOwned || !this.selectedRenderer) return;
    this._volDragging = true;
    this._volTrack = e.currentTarget;
    this._updateVolumeFromEvent(e);

    if (e.type === 'touchstart') {
      document.addEventListener('touchmove', this._boundVolMove, { passive: false });
      document.addEventListener('touchend', this._boundVolEnd);
      document.addEventListener('touchcancel', this._boundVolEnd);
    } else {
      document.addEventListener('mousemove', this._boundVolMove);
      document.addEventListener('mouseup', this._boundVolEnd);
    }
    if (e.cancelable) e.preventDefault();
  }

  _onVolMove(e) {
    if (!this._volDragging) return;
    if (e.type === 'touchmove') e.preventDefault();
    this._updateVolumeFromEvent(e);
  }

  _onVolEnd() {
    if (!this._volDragging) return;
    this._volDragging = false;
    document.removeEventListener('mousemove', this._boundVolMove);
    document.removeEventListener('mouseup', this._boundVolEnd);
    document.removeEventListener('touchmove', this._boundVolMove);
    document.removeEventListener('touchend', this._boundVolEnd);
    document.removeEventListener('touchcancel', this._boundVolEnd);
    // Reset local value
    setTimeout(() => { this._localVolume = null; this.requestUpdate(); }, 300);
  }

  _updateVolumeFromEvent(e) {
    if (!this._volTrack) return;
    let clientX;
    if (e.touches && e.touches.length > 0) clientX = e.touches[0].clientX;
    else clientX = e.clientX;

    const rect = this._volTrack.getBoundingClientRect();
    const vol = Math.max(0, Math.min(1, (clientX - rect.left) / rect.width));
    // Update UI immediately
    this._localVolume = vol;
    this.requestUpdate();
    // Debounce API call
    clearTimeout(this._volDebounce);
    this._volDebounce = setTimeout(() => {
      this._callWS('mu/volume', { renderer_id: this.selectedRenderer, volume: this._localVolume });
    }, 50);
  }

  async _toggleShuffle() {
    if (!this.selectedRenderer || !this.leaseOwned) return;
    const current = this.rendererState?.queue?.shuffle || false;
    await this._callWS('mu/shuffle', { renderer_id: this.selectedRenderer, shuffle: !current });
    await this._loadRendererState();
  }

  async _cycleRepeat() {
    if (!this.selectedRenderer || !this.leaseOwned) return;
    const current = this.rendererState?.queue?.repeatMode || 'off';
    const next = current === 'off' ? 'all' : current === 'all' ? 'one' : 'off';
    await this._callWS('mu/repeat_mode', { renderer_id: this.selectedRenderer, mode: next });
    await this._loadRendererState();
  }

  async _shuffleQueue() {
    if (!this.selectedRenderer) return;
    this.queueLoading = true;
    try {
      await this._callWS('mu/queue_shuffle', { renderer_id: this.selectedRenderer });
      await this._loadQueue();
      this._showToast('Queue shuffled');
    } finally {
      this.queueLoading = false;
    }
  }

  async _queueJump(idx) {
    // Single click on a queue row: jump and start playing. Pull renderer
    // state explicitly after the command completes — relying on the push
    // subscription alone was unreliable (events sometimes don't arrive,
    // or arrive after a noticeable delay). Both calls are fire-and-forget
    // so the UI stays responsive.
    if (!this.selectedRenderer) return;
    // Optimistically snap the progress bar to 0:00 — the new track starts
    // from the beginning; the next state push confirms the real position.
    this._localSeekPos = null;
    this._interpolatedPos = 0;
    this._lastPosUpdate = Date.now();
    this.requestUpdate();
    this._callWS('mu/queue_jump', { renderer_id: this.selectedRenderer, index: idx })
      .then(r => {
        if (!this._wsOk(r)) { this._showToast('Jump failed', { error: true }); return; }
        return this._loadRendererState();
      })
      .catch(err => {
        console.warn('[MU] queue_jump failed:', err);
        this._showToast(err?.message || 'Jump failed', { error: true });
      });
  }

  async _queueRemove(id) {
    if (!this.selectedRenderer) return;
    if (!this.leaseOwned) {
      this._showToast('Acquire control first');
      return;
    }
    this._callWS('mu/queue_remove', { renderer_id: this.selectedRenderer, queue_entry_id: id })
      .then(r => {
        if (!this._wsOk(r)) { this._showToast('Remove failed', { error: true }); return; }
        return this._loadRendererState();
      })
      .catch(err => {
        console.warn('[MU] queue_remove failed:', err);
        this._showToast(err?.message || 'Remove failed', { error: true });
      });
  }

  async _queueClear() {
    if (!this.selectedRenderer) return;
    if (!this.leaseOwned) {
      this._showToast('Acquire control first');
      return;
    }
    this._callWS('mu/queue_clear', { renderer_id: this.selectedRenderer })
      .then(r => {
        if (!this._wsOk(r)) { this._showToast('Clear failed', { error: true }); return; }
        this._showToast('Cleared');
        return this._loadRendererState();
      })
      .catch(err => {
        console.warn('[MU] queue_clear failed:', err);
        this._showToast(err?.message || 'Clear failed', { error: true });
      });
  }

  async _saveQueueToPlaylist(e) {
    const playlistId = e.target.value;
    if (!playlistId || !this.selectedRenderer) return;
    // Reset the dropdown
    e.target.value = '';
    const r = await this._callWS('mu/playlist_save_from_queue', {
      renderer_id: this.selectedRenderer,
      playlist_id: playlistId,
    });
    if (r?.success) {
      const playlist = this.playlists.find(p => p.playlistId === playlistId);
      this._showToast(`Saved to ${playlist?.name || 'playlist'} (${r.itemCount} items)`);
      await this._loadPlaylists();
    } else {
      this._showToast('Save failed');
    }
  }

  async _browseContainer(item) {
    if (item.isLibrary) {
      this.browserPath = [{ id: item.itemId, title: item.title, isLibrary: true }];
      this.browserLoading = true;
      this.browserItems = [];
      await this._loadBrowserItems(item.itemId, '');
    } else if (item.isContainer) {
      const libId = this.browserPath[0]?.id;
      if (!libId) return;
      this.browserPath = [...this.browserPath, { id: item.itemId, title: item.title }];
      this.browserLoading = true;
      this.browserItems = [];
      await this._loadBrowserItems(libId, item.itemId);
    }
  }

  async _loadBrowserItems(libId, containerId, append = false) {
    // Token guards against out-of-order responses when navigating quickly
    const token = ++this._browseToken;
    this.browserLoading = true;
    const start = append ? this.browserItems.length : 0;
    const r = await this._callWS('mu/library_browse', { library_id: libId, container_id: containerId, start, count: 100 });
    if (token !== this._browseToken) return;
    this.browserLoading = false;
    if (r) {
      if (append) {
        this.browserItems = [...this.browserItems, ...(r.items || [])];
      } else {
        this.browserItems = r.items || [];
      }
      this.browserTotal = r.totalCount || 0;
    }
  }

  async _loadMoreBrowserItems() {
    const libId = this.browserPath[0]?.id;
    if (!libId) return;
    const containerId = this.browserPath.length > 1 ? this.browserPath[this.browserPath.length - 1].id : '';
    await this._loadBrowserItems(libId, containerId, true);
  }

  async _navigateBreadcrumb(idx) {
    if (idx < 0) {
      // Invalidate any in-flight browse so a stale response can't overwrite the root view
      this._browseToken++;
      this.browserLoading = false;
      this.browserPath = [];
      this.browserItems = this.libraries.map(lib => ({ itemId: lib.libraryId, title: lib.name, isContainer: true, isLibrary: true }));
    } else {
      const libId = this.browserPath[0]?.id;
      const target = this.browserPath[idx];
      this.browserPath = this.browserPath.slice(0, idx + 1);
      await this._loadBrowserItems(libId, idx === 0 ? '' : target.id);
    }
  }

  async _addFolderToQueue(mode) {
    if (!this.selectedRenderer || !this.browserItems.length) return;
    const libId = this.browserPath[0]?.id;
    if (!libId) return;

    // Filter for playable items only
    const items = this.browserItems
      .filter(item => !item.isContainer && !item.isLibrary && item.itemId)
      .map(item => ({ kind: 'libraryItem', libraryId: libId, itemId: item.itemId }));

    if (!items.length) {
      this._showToast('No playable items');
      return;
    }

    const r = await this._callWS('mu/queue_add', { renderer_id: this.selectedRenderer, mode, items });
    if (!this._wsOk(r)) { this._showToast('Add failed', { error: true }); return; }
    this._showToast(mode === 'replace' ? `Playing folder (${items.length})` : `Added folder (${items.length})`);
    this._loadRendererState();
  }

  async _addToQueue(item, mode) {
    if (!this.selectedRenderer || !item.itemId) return;
    const libId = this.browserPath[0]?.id;
    if (!libId) return;
    // Prevent double-click from firing twice during the in-flight WS calls.
    if (this._addInProgress) { console.warn('[MU] _addToQueue blocked: already in progress'); return; }
    this._addInProgress = true;
    try {
      if (item.isContainer) {
        // Containers (albums, etc.) can't be resolved directly — browse to get children
        const r = await this._callWS('mu/library_browse', {
          library_id: libId, container_id: item.itemId, start: 0, count: 10000
        });
        const children = (r?.items || [])
          .filter(child => child.itemId && !child.isContainer && !child.isLibrary)
          .map(child => ({ kind: 'libraryItem', libraryId: libId, itemId: child.itemId }));
        if (!children.length) { this._showToast('No playable items'); return; }
        const rc = await this._callWS('mu/queue_add', { renderer_id: this.selectedRenderer, mode, items: children });
        if (!this._wsOk(rc)) { this._showToast('Add failed', { error: true }); return; }
        this._showToast(mode === 'replace' ? `Playing (${children.length})` : mode === 'next' ? `Next (${children.length})` : `Added (${children.length})`);
        this._loadRendererState();
        return;
      }

      const rs = await this._callWS('mu/queue_add', {
        renderer_id: this.selectedRenderer,
        mode,
        items: [{ kind: 'libraryItem', libraryId: libId, itemId: item.itemId }],
      });
      if (!this._wsOk(rs)) { this._showToast('Add failed', { error: true }); return; }
      this._showToast(mode === 'replace' ? 'Playing' : mode === 'next' ? 'Next' : 'Added');
      this._loadRendererState();
    } finally {
      this._addInProgress = false;
    }
  }

  _selectSearchLibrary(libraryId) {
    this.searchLibraryId = libraryId;
    this._prefSet('searchLibrary', libraryId);
    if (this.searchQuery.trim()) this._performSearch();
  }

  _selectSearchType(type) {
    if (this.searchType === type) return;
    this.searchType = type;
    this._prefSet('searchType', type);
    if (this.searchQuery.trim()) this._performSearch();
  }

  // Protocol item kinds for the selected search type.
  _searchTypes() {
    switch (this.searchType) {
      case 'songs': return ['audio'];
      case 'artists': return ['musicartist'];
      default: return ['musicalbum'];
    }
  }

  async _performSearch() {
    const query = this.searchQuery.trim();
    if (!query) return;
    const libId = this.searchLibraryId || (this.libraries[0] && this.libraries[0].libraryId) || '';
    if (!libId) return;
    // Token guards against out-of-order responses overwriting newer results
    const token = ++this._searchToken;
    this.searchLibraryId = libId;
    this.searchLoading = true;
    this.searchResults = [];
    const r = await this._callWS('mu/library_search', {
      library_id: libId, query, start: 0, count: 50, types: this._searchTypes(),
    });
    if (token !== this._searchToken) return;
    this.searchLoading = false;
    if (r) {
      this.searchResults = r.items || [];
      this.searchTotal = r.totalCount || 0;
    } else {
      this.searchResults = [];
      this.searchTotal = 0;
    }
  }

  async _loadMoreSearchResults() {
    if (!this.searchResults || !this.searchLibraryId || !this.searchQuery.trim()) return;
    const token = ++this._searchToken;
    this.searchLoading = true;
    const r = await this._callWS('mu/library_search', {
      library_id: this.searchLibraryId, query: this.searchQuery.trim(),
      start: this.searchResults.length, count: 50, types: this._searchTypes(),
    });
    if (token !== this._searchToken) return;
    this.searchLoading = false;
    if (r && r.items) {
      this.searchResults = [...this.searchResults, ...r.items];
      this.searchTotal = r.totalCount || this.searchTotal;
    }
  }

  _clearSearch() {
    // Invalidate in-flight search requests and pending debounce
    this._searchToken++;
    if (this._searchDebounce) { clearTimeout(this._searchDebounce); this._searchDebounce = null; }
    this.searchQuery = '';
    this.searchResults = null;
    this.searchLoading = false;
    this.searchTotal = 0;
  }

  async _addSearchResultToQueue(item, mode) {
    if (!this.selectedRenderer || !item.itemId || !this.searchLibraryId) return;
    const libId = this.searchLibraryId;
    try {
      if (item.isContainer) {
        const r = await this._callWS('mu/library_browse', {
          library_id: libId, container_id: item.itemId, start: 0, count: 10000
        });
        const children = (r?.items || [])
          .filter(child => child.itemId && !child.isContainer && !child.isLibrary)
          .map(child => ({ kind: 'libraryItem', libraryId: libId, itemId: child.itemId }));
        if (!children.length) { this._showToast('No playable items'); return; }
        const rc = await this._callWS('mu/queue_add', { renderer_id: this.selectedRenderer, mode, items: children });
        if (!this._wsOk(rc)) { this._showToast('Add failed', { error: true }); return; }
        this._showToast(mode === 'replace' ? `Playing (${children.length})` : mode === 'next' ? `Next (${children.length})` : `Added (${children.length})`);
        this._loadRendererState();
        return;
      }

      const rs = await this._callWS('mu/queue_add', {
        renderer_id: this.selectedRenderer,
        mode,
        items: [{ kind: 'libraryItem', libraryId: libId, itemId: item.itemId }],
      });
      if (!this._wsOk(rs)) { this._showToast('Add failed', { error: true }); return; }
      this._showToast(mode === 'replace' ? 'Playing' : mode === 'next' ? 'Next' : 'Added');
      this._loadRendererState();
    } catch (err) {
      console.warn('[MU] add search result failed:', err);
      this._showToast(err?.message || 'Add failed', { error: true });
    }
  }

  async _addAllSearchResultsToQueue(mode) {
    if (!this.selectedRenderer || !this.searchResults || !this.searchLibraryId) return;
    const libId = this.searchLibraryId;
    const items = this.searchResults
      .filter(item => item.isPlayable && item.itemId)
      .map(item => ({ kind: 'libraryItem', libraryId: libId, itemId: item.itemId }));
    if (!items.length) { this._showToast('No playable items'); return; }
    const r = await this._callWS('mu/queue_add', { renderer_id: this.selectedRenderer, mode, items });
    if (!this._wsOk(r)) { this._showToast('Add failed', { error: true }); return; }
    this._showToast(mode === 'replace' ? `Playing (${items.length})` : `Added (${items.length})`);
    this._loadRendererState();
  }

  _browseFromSearch(item) {
    if (!item.isContainer) return;
    const libId = this.searchLibraryId || (this.libraries[0] && this.libraries[0].libraryId) || '';
    if (!libId) return;
    const libName = (this.libraries.find(l => l.libraryId === libId) || {}).name || libId;
    this._clearSearch();
    this.browserPath = [{ id: libId, title: libName, isLibrary: true }, { id: item.itemId, title: item.title }];
    this._loadBrowserItems(libId, item.itemId);
  }

  _onSearchInput(e) {
    this.searchQuery = e.target.value;
    if (this._searchDebounce) clearTimeout(this._searchDebounce);
    if (this.searchQuery.trim().length >= 2) {
      this._searchDebounce = setTimeout(() => this._performSearch(), 500);
    }
  }

  _onSearchKeydown(e) {
    if (e.key === 'Enter') {
      if (this._searchDebounce) clearTimeout(this._searchDebounce);
      this._performSearch();
    }
  }

  async _switchTab(tab) {
    this.browserTab = tab;
    if (tab === 'playlists') await this._loadPlaylists();
    else if (tab === 'snapshots') await this._loadSnapshots();
    else if (tab === 'zones') await this._loadZones();
  }

  async _loadPlaylistToQueue(id, mode = 'replace') {
    if (!this.selectedRenderer) return;
    const r = await this._callWS('mu/queue_load_playlist', { renderer_id: this.selectedRenderer, playlist_id: id, mode });
    if (!this._wsOk(r)) { this._showToast('Load failed', { error: true }); return; }
    await this._loadQueue();
    this._showToast('Loaded');
  }

  async _loadSnapshotToQueue(id) {
    if (!this.selectedRenderer) return;
    const r = await this._callWS('mu/queue_load_snapshot', { renderer_id: this.selectedRenderer, snapshot_id: id, mode: 'replace' });
    if (!this._wsOk(r)) { this._showToast('Restore failed', { error: true }); return; }
    await this._loadQueue();
    this._showToast('Restored');
  }

  async _saveSnapshot() {
    if (!this.selectedRenderer) return;
    this._snapshotSaving = true;
    this.requestUpdate();
  }

  async _confirmSaveSnapshot() {
    const input = this.shadowRoot.querySelector('.snapshot-name-input');
    const name = input?.value?.trim();
    if (!name) { this._showToast('Enter a name'); return; }
    this._snapshotSaving = false;
    const r = await this._callWS('mu/snapshot_save', { renderer_id: this.selectedRenderer, name });
    if (r?.success) {
      this._showToast('Saved');
      await this._loadSnapshots();
    } else {
      this._showToast('Failed to save snapshot');
    }
  }

  _cancelSaveSnapshot() {
    this._snapshotSaving = false;
    this.requestUpdate();
  }

  async _deleteSnapshot(snapshotId) {
    const r = await this._callWS('mu/snapshot_delete', { snapshot_id: snapshotId });
    if (r?.success) {
      this._showToast('Snapshot deleted');
      await this._loadSnapshots();
    } else {
      this._showToast('Failed to delete');
    }
  }

  async _createPlaylist() {
    this._creatingPlaylist = true;
    this.requestUpdate();
  }

  async _confirmCreatePlaylist() {
    const input = this.shadowRoot.querySelector('.new-playlist-name-input');
    const name = input?.value?.trim();
    if (!name) { this._showToast('Enter a name'); return; }
    this._creatingPlaylist = false;
    const r = await this._callWS('mu/playlist_create', { name });
    if (r?.playlistId) {
      this._showToast('Playlist created');
      await this._loadPlaylists();
    } else {
      this._showToast('Failed to create playlist');
    }
  }

  _cancelCreatePlaylist() {
    this._creatingPlaylist = false;
    this.requestUpdate();
  }

  _startCreatePlaylistFromSnapshot(snapshotId, snapshotName) {
    this._createPlaylistSnapshotId = snapshotId;
    this._createPlaylistName = snapshotName || '';
    this.requestUpdate();
  }

  async _confirmCreatePlaylistFromSnapshot() {
    const name = this._createPlaylistName?.trim();
    if (!name) { this._showToast('Enter a name'); return; }
    const snapshotId = this._createPlaylistSnapshotId;
    this._createPlaylistSnapshotId = null;
    this._createPlaylistName = '';
    const r = await this._callWS('mu/playlist_from_snapshot', { snapshot_id: snapshotId, name });
    if (r?.success) {
      this._showToast('Playlist created');
      await this._loadPlaylists();
    } else {
      this._showToast('Failed to create playlist');
    }
  }

  _cancelCreatePlaylistFromSnapshot() {
    this._createPlaylistSnapshotId = null;
    this._createPlaylistName = '';
    this.requestUpdate();
  }

  _onDragStart(e, i) { this.dragIndex = i; e.dataTransfer.effectAllowed = 'move'; }
  _onDragOver(e, i) { e.preventDefault(); this.dragOverIndex = i; }
  _onDragLeave() { this.dragOverIndex = null; }
  async _onDrop(e, to) {
    e.preventDefault();
    const from = this.dragIndex; this.dragIndex = null; this.dragOverIndex = null;
    if (from === null || from === to) return;
    await this._callWS('mu/queue_move', { renderer_id: this.selectedRenderer, from_index: from, to_index: to });
    await this._loadQueue();
  }

  render() {
    // Error boundary: a throw inside a Lit render wedges the component's
    // update cycle permanently — the panel goes black until it is
    // re-created by navigating away and back. Catch, show a recoverable
    // error view, and keep the update pipeline alive.
    try {
      return this._renderPanel();
    } catch (err) {
      console.error('[MU] render failed:', err);
      return html`
        <div class="render-error">
          <div>The MU panel hit a rendering error.</div>
          <pre>${err?.stack || err}</pre>
          <button @click=${() => { this._recoverFromRenderError(); }}>Retry</button>
          <button @click=${() => location.reload()}>Reload page</button>
        </div>`;
    }
  }

  // --- Persisted UI preferences (last-used renderer / search library / search type) ---

  _prefGet(key) {
    try { return localStorage.getItem(`mu-panel:${key}`); } catch { return null; }
  }

  _prefSet(key, value) {
    try { localStorage.setItem(`mu-panel:${key}`, value); } catch { /* private mode etc. */ }
  }

  _recoverFromRenderError() {
    // Reset the state most likely to have caused a render throw, then
    // reload data from the backend.
    this.loading = false;
    this.rendererState = null;
    this.queue = [];
    this.browserItems = [];
    this._loadInitialData().catch(() => {});
    this.requestUpdate();
  }

  _renderPanel() {
    if (this.loading) return html`<div class="loading"><div class="spinner"></div></div>`;
    return html`
      <div class="main-header">
        <ha-menu-button .hass=${this.hass} .narrow=${this.narrow}></ha-menu-button>
        <div class="main-header-title">Media Utopia</div>
        <button class="mobile-toggle" @click=${() => { this.mobileView = this.mobileView === 'browser' ? 'player' : 'browser'; }}>
          ${this.mobileView === 'browser' ? 'Player \u25B8' : '\u25C2 Browse'}
        </button>
        <ha-icon-button icon="mdi:dots-vertical"></ha-icon-button>
      </div>
      <div class="mobile-view-toggle">
        <button class="${this.mobileView === 'browser' ? 'active' : ''}" @click=${() => this.mobileView = 'browser'}>Browse</button>
        <button class="${this.mobileView === 'player' ? 'active' : ''}" @click=${() => this.mobileView = 'player'}>Player</button>
      </div>
      <div class="panel-container">
        ${this._renderBrowser()}
        <div class="resize-handle ${this.resizing ? 'dragging' : ''}" @mousedown=${this._onResizeStart}></div>
        ${this._renderQueue()}
      </div>
      ${this.toast ? html`<div class="toast ${this.toast.error ? 'error' : ''}">${this.toast.msg}</div>` : ''}
      ${this.volumeOverlay !== null ? html`<div class="volume-overlay">\u{1F50A} ${this.volumeOverlay}%</div>` : ''}
    `;
  }

  _renderBrowser() {
    return html`
      <div class="browser-pane ${this.mobileView === 'player' ? 'hidden' : ''}" style="width:${this.browserWidth}px;flex-shrink:0">
        <div class="tabs">
          <button class="tab ${this.browserTab === 'browse' ? 'active' : ''}" @click=${() => this._switchTab('browse')}>Browse</button>
          <button class="tab ${this.browserTab === 'playlists' ? 'active' : ''}" @click=${() => this._switchTab('playlists')}>Playlists</button>
          <button class="tab ${this.browserTab === 'snapshots' ? 'active' : ''}" @click=${() => this._switchTab('snapshots')}>Snapshots</button>
          <button class="tab ${this.browserTab === 'zones' ? 'active' : ''}" @click=${() => this._switchTab('zones')}>Zones</button>
        </div>
        ${this.browserTab === 'browse' ? this._renderBrowseTab() : ''}
        ${this.browserTab === 'playlists' ? this._renderPlaylistsTab() : ''}
        ${this.browserTab === 'snapshots' ? this._renderSnapshotsTab() : ''}
        ${this.browserTab === 'zones' ? this._renderZonesTab() : ''}
      </div>
    `;
  }

  _renderBrowseTab() {
    const currentFolder = this.browserPath.length > 0 ? this.browserPath[this.browserPath.length - 1] : null;
    const showSearchResults = this.searchResults !== null;
    return html`
      <div class="search-bar">
        <div class="search-input-wrapper">
          <input class="search-input" type="text" placeholder="Search library..."
            .value=${this.searchQuery}
            @input=${e => this._onSearchInput(e)}
            @keydown=${e => this._onSearchKeydown(e)} />
          ${this.searchQuery ? html`
            <button class="search-clear" @click=${() => this._clearSearch()} title="Clear search">\u2715</button>
          ` : ''}
        </div>
        ${this.libraries.length > 1 ? html`
          <select class="search-library-select"
            .value=${this.searchLibraryId || (this.libraries[0] && this.libraries[0].libraryId) || ''}
            @change=${e => this._selectSearchLibrary(e.target.value)}>
            ${this.libraries.map(lib => html`<option value="${lib.libraryId}" ?selected=${lib.libraryId === this.searchLibraryId}>${lib.name}</option>`)}
          </select>
        ` : ''}
        ${showSearchResults ? html`
          <button class="action-btn secondary" @click=${() => this._clearSearch()} title="Clear search">${icons.clear}</button>
        ` : html`
          <button class="action-btn" @click=${() => this._performSearch()} title="Search">${icons.search}</button>
        `}
      </div>
      <div class="search-type-toggle">
        ${['albums', 'songs', 'artists'].map(t => html`
          <button class="${this.searchType === t ? 'active' : ''}"
            @click=${() => this._selectSearchType(t)}>${t.charAt(0).toUpperCase() + t.slice(1)}</button>
        `)}
      </div>
      ${showSearchResults ? this._renderSearchResults() : html`
        <div class="breadcrumbs">
          <button class="breadcrumb" @click=${() => this._navigateBreadcrumb(-1)}>${icons.home}</button>
          ${this.browserPath.map((c, i) => html`<span class="breadcrumb-sep">›</span><button class="breadcrumb" @click=${() => this._navigateBreadcrumb(i)}>${c.title}</button>`)}
        </div>
        ${currentFolder ? html`
          <div class="folder-actions">
            <button class="action-btn" @click=${() => this._addFolderToQueue('replace')} title="Play folder">▶ Play</button>
            <button class="action-btn secondary" @click=${() => this._addFolderToQueue('append')} title="Add folder to queue">+ Add All</button>
          </div>
        ` : ''}
        <div class="browser-with-index">
          <div class="browser-list-wrapper">
            <div class="browser-list">
              ${this.browserLoading ? html`<div class="loading"><div class="spinner"></div></div>` : ''}
              ${!this.browserLoading && !this.browserItems.length ? html`<div class="empty">No items</div>` : ''}
              ${!this.browserLoading ? this.browserItems.map((item, idx) => this._renderBrowserItemWithId(item, idx)) : ''}
              ${!this.browserLoading && this.browserPath.length > 0 && this.browserItems.length < this.browserTotal ? html`
                <div class="browser-item" @click=${() => this._loadMoreBrowserItems()} style="justify-content:center;cursor:pointer">
                  <span style="font-size:11px;color:var(--mu-accent)">Load more (${this.browserItems.length}/${this.browserTotal})</span>
                </div>
              ` : ''}
            </div>
          </div>
          ${this._renderLetterIndex()}
        </div>
      `}
    `;
  }

  _renderSearchResults() {
    const playableCount = this.searchResults ? this.searchResults.filter(i => i.isPlayable).length : 0;
    return html`
      <div class="folder-actions">
        <span style="font-size:12px;color:var(--mu-secondary);flex:1">${this.searchTotal} result${this.searchTotal !== 1 ? 's' : ''}</span>
        ${playableCount > 0 ? html`
          <button class="action-btn" @click=${() => this._addAllSearchResultsToQueue('replace')} title="Play all results">▶ Play All</button>
          <button class="action-btn secondary" @click=${() => this._addAllSearchResultsToQueue('append')} title="Add all to queue">+ Add All</button>
        ` : ''}
      </div>
      <div class="browser-with-index">
        <div class="browser-list-wrapper">
          <div class="browser-list">
            ${this.searchLoading && !this.searchResults.length ? html`<div class="loading"><div class="spinner"></div></div>` : ''}
            ${!this.searchLoading && !this.searchResults.length ? html`<div class="empty">No results</div>` : ''}
            ${this.searchResults.map((item, idx) => this._renderSearchItem(item, idx))}
            ${this.searchResults.length > 0 && this.searchResults.length < this.searchTotal ? html`
              <div class="browser-item" @click=${() => this._loadMoreSearchResults()} style="justify-content:center;cursor:pointer">
                <span style="font-size:11px;color:var(--mu-accent)">${this.searchLoading ? 'Loading...' : `Load more (${this.searchResults.length}/${this.searchTotal})`}</span>
              </div>
            ` : ''}
          </div>
        </div>
      </div>
    `;
  }

  _renderSearchItem(item, idx) {
    const icon = item.isContainer ? icons.folder : icons.music;
    const canNavigate = item.isContainer;
    return html`
      <div class="browser-item" @click=${() => canNavigate ? this._browseFromSearch(item) : null} style="${canNavigate ? '' : 'cursor:default'}">
        ${this._renderArt(item.art_url, 'browser-item-art', icon)}
        <div class="browser-item-info">
          <div class="browser-item-title">${item.title}</div>
          ${item.subtitle ? html`<div class="browser-item-subtitle">${item.subtitle}</div>` : ''}
        </div>
        <div class="browser-item-actions">
          ${item.isPlayable || item.canEnqueue ? html`
            <button class="action-btn" @click=${e => { e.stopPropagation(); this._addSearchResultToQueue(item, 'replace'); }} title="Play">▶</button>
            <button class="action-btn secondary" @click=${e => { e.stopPropagation(); this._addSearchResultToQueue(item, 'next'); }} title="Play next">⏭</button>
            <button class="action-btn secondary" @click=${e => { e.stopPropagation(); this._addSearchResultToQueue(item, 'append'); }} title="Add to queue">+</button>
          ` : ''}
        </div>
      </div>
    `;
  }

  _renderBrowserItem(item) {
    const icon = item.isLibrary ? icons.library : item.isContainer ? icons.folder : icons.music;
    const canNavigate = item.isContainer || item.isLibrary;
    return html`
      <div class="browser-item" @click=${() => canNavigate ? this._browseContainer(item) : null} style="${canNavigate ? '' : 'cursor:default'}">
        ${this._renderArt(item.art_url, 'browser-item-art', icon)}
        <div class="browser-item-info">
          <div class="browser-item-title">${item.title}</div>
          ${item.subtitle ? html`<div class="browser-item-subtitle">${item.subtitle}</div>` : ''}
        </div>
        <div class="browser-item-actions">
          ${item.isPlayable || item.canEnqueue ? html`
            <button class="action-btn" @click=${e => { e.stopPropagation(); this._addToQueue(item, 'replace'); }} title="Play">▶</button>
            <button class="action-btn secondary" @click=${e => { e.stopPropagation(); this._addToQueue(item, 'next'); }} title="Play next">⏭</button>
            <button class="action-btn secondary" @click=${e => { e.stopPropagation(); this._addToQueue(item, 'append'); }} title="Add to queue">+</button>
          ` : ''}
        </div>
      </div>
    `;
  }

  _renderBrowserItemWithId(item, idx) {
    const icon = item.isLibrary ? icons.library : item.isContainer ? icons.folder : icons.music;
    const canNavigate = item.isContainer || item.isLibrary;
    const firstChar = (item.title || '').charAt(0).toUpperCase();
    const letterId = /[A-Z]/.test(firstChar) ? firstChar : '#';
    return html`
      <div class="browser-item" id="browser-item-${letterId}-${idx}" data-letter="${letterId}" @click=${() => canNavigate ? this._browseContainer(item) : null} style="${canNavigate ? '' : 'cursor:default'}">
        ${this._renderArt(item.art_url, 'browser-item-art', icon)}
        <div class="browser-item-info">
          <div class="browser-item-title">${item.title}</div>
          ${item.subtitle ? html`<div class="browser-item-subtitle">${item.subtitle}</div>` : ''}
        </div>
        <div class="browser-item-actions">
          ${item.isPlayable || item.canEnqueue ? html`
            <button class="action-btn" @click=${e => { e.stopPropagation(); this._addToQueue(item, 'replace'); }} title="Play">▶</button>
            <button class="action-btn secondary" @click=${e => { e.stopPropagation(); this._addToQueue(item, 'next'); }} title="Play next">⏭</button>
            <button class="action-btn secondary" @click=${e => { e.stopPropagation(); this._addToQueue(item, 'append'); }} title="Add to queue">+</button>
          ` : ''}
        </div>
      </div>
    `;
  }

  _getAvailableLetters() {
    const letters = new Set();
    for (const item of this.browserItems) {
      const firstChar = (item.title || '').charAt(0).toUpperCase();
      if (/[A-Z]/.test(firstChar)) letters.add(firstChar);
      else if (firstChar) letters.add('#');
    }
    return Array.from(letters).sort((a, b) => {
      if (a === '#') return 1;
      if (b === '#') return -1;
      return a.localeCompare(b);
    });
  }

  async _scrollToLetter(letter) {
    // First, try to find the item in currently loaded items
    let item = this.shadowRoot.querySelector(`[data-letter="${letter}"]`);

    // If not found and we have more items to load, keep loading until we find it or run out
    while (!item && this.browserItems.length < this.browserTotal && !this.browserLoading) {
      await this._loadMoreBrowserItems();
      await this.updateComplete;
      item = this.shadowRoot.querySelector(`[data-letter="${letter}"]`);

      // Check if we've passed this letter alphabetically (items are sorted)
      if (this.browserItems.length > 0) {
        const lastItem = this.browserItems[this.browserItems.length - 1];
        const lastChar = (lastItem.title || '').charAt(0).toUpperCase();
        if (lastChar > letter && letter !== '#') {
          break; // We've passed this letter, no items exist for it
        }
      }
    }

    // Scroll to the item if found
    if (item) {
      item.scrollIntoView({ behavior: 'smooth', block: 'start' });
    } else {
      this._showToast(`No items starting with "${letter}"`);
    }
  }

  _renderLetterIndex() {
    if (this.browserLoading && this.browserItems.length === 0) return '';
    if (this.browserPath.length === 0) return ''; // Only show for container browsing, not library list

    const allLetters = '#ABCDEFGHIJKLMNOPQRSTUVWXYZ'.split('');
    const availableLetters = new Set(this._getAvailableLetters());

    return html`
      <div class="letter-index">
        ${allLetters.map(letter => html`
          <button 
            @click=${() => this._scrollToLetter(letter)}
            style="${availableLetters.has(letter) ? '' : 'opacity: 0.3'}"
          >${letter}</button>
        `)}
      </div>
    `;
  }

  async _deletePlaylist(playlistId) {
    const r = await this._callWS('mu/playlist_delete', { playlist_id: playlistId });
    if (r?.success) {
      this._showToast('Playlist deleted');
      await this._loadPlaylists();
    } else {
      this._showToast('Failed to delete');
    }
  }

  async _loadPlaylist(playlistId, mode = 'replace') {
    if (!this.selectedRenderer) return;
    this.queueLoading = true;
    try {
      const r = await this._callWS('mu/queue_load_playlist', {
        renderer_id: this.selectedRenderer,
        playlist_id: playlistId,
        mode,
      });
      if (!this._wsOk(r)) { this._showToast('Load failed', { error: true }); return; }
      await this._loadQueue();
      this._showToast(mode === 'replace' ? 'Playlist loaded' : 'Playlist added to queue');
    } finally {
      this.queueLoading = false;
    }
  }

  _startRenamePlaylist(playlistId, currentName) {
    this._renamingPlaylistId = playlistId;
    this._renamePlaylistName = currentName;
    this.requestUpdate();
  }

  async _confirmRenamePlaylist() {
    const name = this._renamePlaylistName?.trim();
    if (!name) return;
    const id = this._renamingPlaylistId;
    this._renamingPlaylistId = null;
    await this._callWS('mu/playlist_rename', { playlist_id: id, name });
    await this._loadPlaylists();
    this._showToast('Playlist renamed');
  }

  _cancelRenamePlaylist() {
    this._renamingPlaylistId = null;
    this.requestUpdate();
  }

  _startDelete(type, id) {
    this._pendingDelete = { type, id };
    this.requestUpdate();
    // Auto-cancel after 3 seconds
    setTimeout(() => {
      if (this._pendingDelete?.id === id) {
        this._pendingDelete = null;
        this.requestUpdate();
      }
    }, 3000);
  }

  async _confirmDelete() {
    const { type, id } = this._pendingDelete || {};
    this._pendingDelete = null;
    if (type === 'playlist') {
      await this._deletePlaylist(id);
    } else if (type === 'snapshot') {
      await this._deleteSnapshot(id);
    }
  }

  _renderPlaylistsTab() {
    return html`
      ${this.playlistServers.length > 1 ? html`
        <div class="pane-header">
          <span class="pane-title">Server:</span>
          <select class="renderer-select" @change=${(e) => this._selectPlaylistServer(e)}>
            ${this.playlistServers.map(s => html`<option value="${s.nodeId}" ?selected=${s.nodeId === this.selectedPlaylistServer}>${s.name}</option>`)}
          </select>
          <button class="action-btn" @click=${() => this._createPlaylist()} title="Create new playlist">+ New</button>
        </div>
      ` : html`
        <div class="pane-header">
          <span class="pane-title">Playlists</span>
          <button class="action-btn" @click=${() => this._createPlaylist()} title="Create new playlist">+ New</button>
        </div>
      `}
      ${this._creatingPlaylist ? html`
        <div style="display:flex;gap:6px;padding:8px 10px;border-bottom:1px solid var(--mu-border)">
          <input class="new-playlist-name-input" type="text" placeholder="Playlist name..."
            style="flex:1;background:var(--primary-background-color);color:var(--primary-text-color);border:1px solid var(--mu-border);border-radius:4px;padding:4px 8px;font-size:13px"
            @keydown=${e => e.key === 'Enter' && this._confirmCreatePlaylist()} />
          <button class="action-btn" @click=${() => this._confirmCreatePlaylist()}>Create</button>
          <button class="action-btn secondary" @click=${() => this._cancelCreatePlaylist()}>Cancel</button>
        </div>
      ` : ''}
      <div class="browser-list">
      ${!this.playlists.length ? html`<div class="empty">No playlists</div>` : ''}
      ${this.playlists.map(pl => html`
        <div class="browser-item">
          <div class="browser-item-icon">${icons.playlist || icons.music}</div>
          ${this._renamingPlaylistId === pl.playlistId ? html`
            <input type="text" .value=${this._renamePlaylistName}
              @input=${e => this._renamePlaylistName = e.target.value}
              @keydown=${e => { if(e.key==='Enter') this._confirmRenamePlaylist(); if(e.key==='Escape') this._cancelRenamePlaylist(); }}
              style="background:var(--primary-background-color);color:var(--primary-text-color);border:1px solid var(--mu-border);border-radius:4px;padding:3px 6px;font-size:13px;flex:1"
            />
            <button class="action-btn" @click=${() => this._confirmRenamePlaylist()}>&#10003;</button>
            <button class="action-btn secondary" @click=${() => this._cancelRenamePlaylist()}>&#10005;</button>
          ` : html`
            <div class="browser-item-info" @dblclick=${() => this._startRenamePlaylist(pl.playlistId, pl.name)}>
              <div class="browser-item-title">${pl.name}</div>
              <div class="browser-item-subtitle">${pl.size || 0} track${(pl.size || 0) !== 1 ? 's' : ''}</div>
            </div>
            <div class="browser-item-actions" style="opacity:1">
              <button class="action-btn" @click=${e => { e.stopPropagation(); this._loadPlaylist(pl.playlistId, 'replace'); }} title="Play">&#9654;</button>
              <button class="action-btn secondary" @click=${e => { e.stopPropagation(); this._loadPlaylist(pl.playlistId, 'append'); }} title="Add to queue">+</button>
              ${this._pendingDelete?.id === pl.playlistId ? html`
                <button class="action-btn" style="background:#f44336;color:white" @click=${e => { e.stopPropagation(); this._confirmDelete(); }}>Confirm?</button>
              ` : html`
                <button class="action-btn secondary" @click=${e => { e.stopPropagation(); this._startDelete('playlist', pl.playlistId); }} title="Delete">&#128465;</button>
              `}
            </div>
          `}
        </div>
      `)}
    </div>`;
  }

  _renderSnapshotsTab() {
    return html`
      <div class="pane-header"><span class="pane-title">Snapshots</span>
        ${this._snapshotSaving ? html`
          <input class="snapshot-name-input" type="text" placeholder="Snapshot name..." style="background:var(--primary-background-color);color:var(--primary-text-color);border:1px solid var(--mu-border);border-radius:4px;padding:4px 8px;font-size:13px;width:160px" @keydown=${e => e.key === 'Enter' && this._confirmSaveSnapshot()} />
          <button class="action-btn" @click=${() => this._confirmSaveSnapshot()}>OK</button>
          <button class="action-btn secondary" @click=${() => this._cancelSaveSnapshot()}>Cancel</button>
        ` : html`
          <button class="action-btn" @click=${() => this._saveSnapshot()}>Save</button>
        `}
      </div>
      <div class="browser-list">
        ${!this.snapshots.length ? html`<div class="empty">No snapshots</div>` : ''}
        ${this.snapshots.map(s => html`
          <div class="browser-item">
            <div class="browser-item-icon">${icons.camera}</div>
            <div class="browser-item-info"><div class="browser-item-title">${s.name}</div></div>
            <div class="browser-item-actions" style="opacity:1">
              ${this._createPlaylistSnapshotId === s.snapshotId ? html`
                <input type="text" .value=${this._createPlaylistName}
                  @input=${e => this._createPlaylistName = e.target.value}
                  @keydown=${e => e.key === 'Enter' && this._confirmCreatePlaylistFromSnapshot()}
                  placeholder="Playlist name..." style="background:var(--primary-background-color);color:var(--primary-text-color);border:1px solid var(--mu-border);border-radius:4px;padding:3px 6px;font-size:12px;width:120px" />
                <button class="action-btn" @click=${() => this._confirmCreatePlaylistFromSnapshot()}>OK</button>
                <button class="action-btn secondary" @click=${() => this._cancelCreatePlaylistFromSnapshot()}>&#10005;</button>
              ` : html`
                <button class="action-btn" @click=${e => { e.stopPropagation(); this._loadSnapshotToQueue(s.snapshotId); }} title="Restore to queue">&#9654;</button>
                <button class="action-btn secondary" @click=${e => { e.stopPropagation(); this._startCreatePlaylistFromSnapshot(s.snapshotId, s.name); }} title="Create playlist">&#128203;</button>
                ${this._pendingDelete?.id === s.snapshotId ? html`
                  <button class="action-btn" style="background:#f44336;color:white" @click=${e => { e.stopPropagation(); this._confirmDelete(); }}>Confirm?</button>
                ` : html`
                  <button class="action-btn secondary" @click=${e => { e.stopPropagation(); this._startDelete('snapshot', s.snapshotId); }} title="Delete">&#128465;</button>
                `}
              `}
            </div>
          </div>
        `)}
      </div>
    `;
  }

  async _loadZones() {
    const [controllers, zones] = await Promise.all([
      this._callWS('mu/zone_controllers_list'),
      this._callWS('mu/zones_list'),
    ]);
    this.zoneControllers = controllers || [];
    this.zones = zones || [];
  }

  _getSourceName(sourceId) {
    for (const ctrl of this.zoneControllers) {
      for (const source of ctrl.sources || []) {
        if (source.id === sourceId) return source.name;
      }
    }
    return sourceId?.split(':').pop() || 'Unknown';
  }

  _getAllSources() {
    const sources = [];
    for (const ctrl of this.zoneControllers) {
      for (const source of ctrl.sources || []) {
        sources.push(source);
      }
    }
    return sources;
  }

  _renderZonesTab() {
    if (!this.zones?.length && !this.zoneControllers?.length) {
      return html`<div class="tab-content">
        <div class="pane-header"><span class="pane-title">Zones</span></div>
        <div class="empty" style="flex-direction:column;gap:8px;padding:20px">
          <div>No multi-room zones configured</div>
          <div style="font-size:11px;opacity:0.6">Connect a Snapcast server to enable multi-room audio</div>
        </div>
      </div>`;
    }



    const sources = this._getAllSources();
    const sortedZones = [...this.zones].sort((a, b) => (a.name || '').localeCompare(b.name || ''));

    return html`
      <div class="browser-list" style="padding: 12px;">
        ${sortedZones.map(zone => this._renderZoneItem(zone, sources))}
      </div>
    `;
  }



  _renderZoneItem(zone, sources) {
    const volumePct = Math.round((zone.volume || 0) * 100);
    return html`
      <div class="zone-item">
        <span class="zone-item-name ${zone.connected ? '' : 'disconnected'}">${zone.name}</span>
        <div class="zone-controls">
          <div class="zone-volume-slider" 
            @mousedown=${(e) => this._onZoneVolStart(e, zone)} 
            @touchstart=${(e) => this._onZoneVolStart(e, zone)}
            @click=${(e) => e.preventDefault()}>
            <div class="zone-volume-fill" style="width: ${volumePct}%"></div>
          </div>
          <span style="font-size:12px;color:var(--mu-secondary);min-width:32px">${volumePct}%</span>
          <button class="zone-mute-btn ${zone.mute ? 'muted' : ''}" @click=${() => this._toggleZoneMute(zone)} title="${zone.mute ? 'Unmute' : 'Mute'}">
            ${zone.mute ? html`<svg viewBox="0 0 24 24" fill="currentColor" width="16" height="16"><path d="M16.5 12c0-1.77-1.02-3.29-2.5-4.03v2.21l2.45 2.45c.03-.2.05-.41.05-.63zm2.5 0c0 .94-.2 1.82-.54 2.64l1.51 1.51C20.63 14.91 21 13.5 21 12c0-4.28-2.99-7.86-7-8.77v2.06c2.89.86 5 3.54 5 6.71zM4.27 3L3 4.27 7.73 9H3v6h4l5 5v-6.73l4.25 4.25c-.67.52-1.42.93-2.25 1.18v2.06c1.38-.31 2.63-.95 3.69-1.81L19.73 21 21 19.73l-9-9L4.27 3zM12 4L9.91 6.09 12 8.18V4z"/></svg>` : icons.volume}
          </button>
          <select class="zone-source-select" @change=${(e) => this._setZoneSource(zone, e.target.value)} .value=${zone.sourceId || ''}>
            ${sources.map(s => html`<option value="${s.id}" ?selected=${s.id === zone.sourceId}>${s.name}</option>`)}
          </select>
        </div>
      </div>
    `;
  }

  _handleZoneVolumeClick(e, zone) {
    const rect = e.currentTarget.getBoundingClientRect();
    const pct = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
    this._setZoneVolume(zone, pct);
  }

  _onZoneVolStart(e, zone) {
    this._zoneVolDragging = true;
    this._zoneVolTrack = e.currentTarget;
    this._zoneVolZone = zone;
    this._updateZoneVolFromEvent(e);
    document.addEventListener('mousemove', this._boundZoneVolMove);
    document.addEventListener('mouseup', this._boundZoneVolEnd);
    document.addEventListener('touchmove', this._boundZoneVolMove, { passive: false });
    document.addEventListener('touchend', this._boundZoneVolEnd);
    document.addEventListener('touchcancel', this._boundZoneVolEnd);
    e.preventDefault();
  }

  _onZoneVolMove(e) {
    if (!this._zoneVolDragging) return;
    this._updateZoneVolFromEvent(e);
  }

  _onTouchStart(e, idx) {
    // Only single touch
    if (e.touches.length !== 1) return;

    // Direct start for drag handle
    this._startTouchDrag(idx);
    e.preventDefault();
  }

  _onTouchMove(e) {
    if (this.dragIndex !== null) {
      // We are dragging
      e.preventDefault(); // Stop scrolling
      const touch = e.touches[0];
      const target = this.shadowRoot?.elementFromPoint
        ? this.shadowRoot.elementFromPoint(touch.clientX, touch.clientY)
        : document.elementFromPoint(touch.clientX, touch.clientY);
      const queueItem = target?.closest('.queue-item');
      if (queueItem && queueItem.dataset.index !== undefined) {
        const newIndex = parseInt(queueItem.dataset.index, 10);
        if (!isNaN(newIndex) && newIndex !== this.dragOverIndex) {
          this.dragOverIndex = newIndex;
        }
      }
    }
  }

  _onTouchEnd(e) {
    if (this.dragIndex !== null) {
      e.preventDefault();
      // Drop
      const from = this.dragIndex;
      const to = this.dragOverIndex;
      this.dragIndex = null;
      this.dragOverIndex = null;

      this._removeTouchListeners();

      if (to !== null && from !== to) {
        this._callWS('mu/queue_move', { renderer_id: this.selectedRenderer, from_index: from, to_index: to });
        // Optimistic update
        const item = this.queue[from];
        const newQueue = [...this.queue];
        newQueue.splice(from, 1);
        newQueue.splice(to, 0, item);
        this.queue = newQueue;
      }
    }
  }

  _startTouchDrag(idx) {
    this.dragIndex = idx;
    if (navigator.vibrate) navigator.vibrate(50);

    // Bind global move/up listeners
    this._boundTouchMove = this._onTouchMove.bind(this);
    this._boundTouchEnd = this._onTouchEnd.bind(this);
    document.addEventListener('touchmove', this._boundTouchMove, { passive: false });
    document.addEventListener('touchend', this._boundTouchEnd);
    document.addEventListener('touchcancel', this._boundTouchEnd);
  }

  _removeTouchListeners() {
    if (this._boundTouchMove) {
      document.removeEventListener('touchmove', this._boundTouchMove);
      document.removeEventListener('touchend', this._boundTouchEnd);
      document.removeEventListener('touchcancel', this._boundTouchEnd);
      this._boundTouchMove = null;
      this._boundTouchEnd = null;
    }
  }

  _onZoneVolEnd() {
    if (!this._zoneVolDragging) return;
    this._zoneVolDragging = false;
    document.removeEventListener('mousemove', this._boundZoneVolMove);
    document.removeEventListener('mouseup', this._boundZoneVolEnd);
    document.removeEventListener('touchmove', this._boundZoneVolMove);
    document.removeEventListener('touchend', this._boundZoneVolEnd);
    document.removeEventListener('touchcancel', this._boundZoneVolEnd);
    this._zoneVolZone = null;
  }

  _updateZoneVolFromEvent(e) {
    if (!this._zoneVolTrack || !this._zoneVolZone) return;

    let clientX;
    if (e.touches && e.touches.length > 0) {
      clientX = e.touches[0].clientX;
    } else {
      clientX = e.clientX;
    }

    const rect = this._zoneVolTrack.getBoundingClientRect();
    const pct = Math.max(0, Math.min(1, (clientX - rect.left) / rect.width));
    // Optimistically update UI
    const zone = this._zoneVolZone;
    const idx = this.zones.findIndex(z => z.zoneId === zone.zoneId);
    if (idx >= 0) {
      this.zones = [...this.zones.slice(0, idx), { ...this.zones[idx], volume: pct }, ...this.zones.slice(idx + 1)];
    }
    // Debounce API call
    clearTimeout(this._zoneVolDebounce);
    this._zoneVolDebounce = setTimeout(() => {
      this._callWS('mu/zone_set_volume', { zone_id: zone.zoneId, volume: pct });
    }, 50);
  }

  async _setZoneVolume(zone, volume) {
    await this._callWS('mu/zone_set_volume', { zone_id: zone.zoneId, volume });
    // Optimistically update local state
    const idx = this.zones.findIndex(z => z.zoneId === zone.zoneId);
    if (idx >= 0) {
      this.zones = [...this.zones.slice(0, idx), { ...this.zones[idx], volume }, ...this.zones.slice(idx + 1)];
    }
  }

  async _toggleZoneMute(zone) {
    const newMute = !zone.mute;
    await this._callWS('mu/zone_set_mute', { zone_id: zone.zoneId, mute: newMute });
    // Optimistically update local state
    const idx = this.zones.findIndex(z => z.zoneId === zone.zoneId);
    if (idx >= 0) {
      this.zones = [...this.zones.slice(0, idx), { ...this.zones[idx], mute: newMute }, ...this.zones.slice(idx + 1)];
    }
  }

  async _setZoneSource(zone, sourceId) {
    await this._callWS('mu/zone_select_source', { zone_id: zone.zoneId, source_id: sourceId });
    // Reload zones to reflect new grouping
    await this._loadZones();
  }

  _renderQueue() {
    if (!this.selectedRenderer && !this.renderers.length) {
      return html`<div class="queue-pane ${this.mobileView === 'browser' ? 'hidden' : ''}">
        <div class="pane-header"><span class="pane-title">Media Utopia</span></div>
        <div style="display:flex;flex-direction:column;align-items:center;justify-content:center;flex:1;gap:12px;color:var(--mu-secondary)">
          <svg viewBox="0 0 24 24" style="width:48px;height:48px;opacity:0.3"><path fill="currentColor" d="M12 3v10.55c-.59-.34-1.27-.55-2-.55-2.21 0-4 1.79-4 4s1.79 4 4 4 4-1.79 4-4V7h4V3h-6z"/></svg>
          <div>Waiting for renderers...</div>
          <div style="font-size:12px">Make sure a mu renderer is running and connected to MQTT</div>
        </div>
      </div>`;
    }

    const st = this.rendererState;
    const pb = st?.playback || {};
    const cur = st?.current || {};
    const q = st?.queue || {};
    const sess = st?.session || {};
    const playing = pb.status === 'playing';
    // Use local values when dragging, otherwise use server state
    // ?? not ||: an interpolated position of 0 (start of a new track) is a
    // real value and must not fall back to the previous track's position.
    const pos = this._localSeekPos !== null ? this._localSeekPos : (this._interpolatedPos ?? pb.position_ms ?? 0);
    const dur = pb.duration_ms || 1;
    const vol = this._localVolume !== null ? this._localVolume : (pb.volume ?? 1);
    const shuffle = q.shuffle || false;
    const repeatMode = q.repeatMode || 'off';

    return html`
      <div class="queue-pane ${this.mobileView === 'browser' ? 'hidden' : ''}">
        <div class="pane-header">
          <select class="renderer-select" @change=${this._selectRenderer}>
            ${this.renderers.map(r => html`<option value="${r.rendererId}" ?selected=${r.rendererId === this.selectedRenderer}>${r.name}</option>`)}
          </select>
          ${this.leaseOwned
        ? html`<button class="lease-btn owned" @click=${this._releaseLease}>Release Control</button>`
        : html`<button class="lease-btn" @click=${this._acquireLease}>Take Control</button>`
      }
        </div>

        <div class="now-playing">
          ${this._renderArt(cur.art_url, 'now-playing-art', icons.music)}
          <div class="now-playing-info">
            <div class="now-playing-title">${cur.title || 'Nothing playing'}</div>
            ${cur.artist ? html`<div class="now-playing-artist">${cur.artist}${cur.album ? html` — <span style="opacity:0.7">${cur.album}</span>` : ''}</div>` : ''}
          </div>
        </div>

        <div class="transport">
          <button class="transport-btn ${shuffle ? 'active' : ''}" @click=${this._toggleShuffle} ?disabled=${!this.leaseOwned} title="Shuffle" aria-label="Toggle shuffle">${icons.shuffle}</button>
          <button class="transport-btn" @click=${() => this._transport('prev')} ?disabled=${!this.leaseOwned} aria-label="Previous track">${icons.prev}</button>
          <button class="transport-btn primary" @click=${() => this._transport('toggle')} ?disabled=${!this.leaseOwned} aria-label="${playing ? 'Pause' : 'Play'}">${playing ? icons.pause : icons.play}</button>
          <button class="transport-btn" @click=${() => this._transport('next')} ?disabled=${!this.leaseOwned} aria-label="Next track">${icons.next}</button>
          <button class="transport-btn ${repeatMode !== 'off' ? 'active' : ''}" @click=${this._cycleRepeat} ?disabled=${!this.leaseOwned} title="Repeat: ${repeatMode}" aria-label="Cycle repeat mode">${repeatMode === 'one' ? icons.repeatOne : icons.repeat}</button>
        </div>

        <div class="progress-section">
          <div class="progress-bar-container">
            <span>${formatTime(pos)}</span>
            <div class="progress-track" @mousedown=${this._onSeekStart} @touchstart=${this._onSeekStart} style="touch-action:none">
              <div class="progress-fill" style="width:${(pos / dur) * 100}%"><div class="progress-thumb"></div></div>
            </div>
            <span>${formatTime(dur)}</span>
          </div>
          <div class="volume-row">
            ${icons.volume}
            <div class="volume-track" @mousedown=${this._onVolumeStart} @touchstart=${this._onVolumeStart} style="touch-action:none"><div class="volume-fill" style="width:${vol * 100}%"><div class="volume-thumb"></div></div></div>
            <span>${Math.round(vol * 100)}%</span>
          </div>
        </div>

        <div class="pane-header">
          <span class="pane-title">${this.queue.length > 0 && q.index !== undefined ? `Queue (${q.index + 1}/${this.queue.length})` : `Queue (${this.queue.length})`}</span>
          <button class="icon-btn" @click=${this._shuffleQueue} ?disabled=${!this.leaseOwned || this.queue.length < 2} title="Shuffle">${icons.shuffle}</button>
          <select class="renderer-select" style="max-width:100px" @change=${this._saveQueueToPlaylist} ?disabled=${!this.queue.length || !this.playlists.length}>
            <option value="" selected disabled>Save to...</option>
            ${this.playlists.map(pl => html`<option value="${pl.playlistId}">${pl.name}</option>`)}
          </select>
          <button class="action-btn secondary" @click=${this._queueClear} ?disabled=${!this.leaseOwned || !this.queue.length}>Clear</button>
        </div>

        <div class="queue-list">
          ${this.queueLoading ? html`<div class="loading"><div class="spinner"></div></div>` : ''}
          ${!this.queue.length ? html`<div class="empty">Queue empty</div>` : ''}
          ${this.queue.map((e, i) => this._renderQueueItem(e, i, q.index))}
        </div>
      </div>
    `;
  }

  _renderQueueItem(entry, idx, curIdx) {
    return html`
      <div class="queue-item ${idx === curIdx ? 'playing' : ''} ${this.dragIndex === idx ? 'dragging' : ''} ${this.dragOverIndex === idx ? 'drag-over' : ''}" data-index="${idx}"
        @click=${() => this._queueJump(idx)}
        @dragover=${e => this._onDragOver(e, idx)}
        @dragleave=${this._onDragLeave}
        @drop=${e => this._onDrop(e, idx)}>
        <div class="queue-drag-handle"
           @touchstart=${e => { e.stopPropagation(); this._onTouchStart(e, idx); }}
           @touchmove=${e => this._onTouchMove(e)}
           @touchend=${e => this._onTouchEnd(e)}
           draggable="true"
           data-index="${idx}"
           @dragstart=${e => this._onDragStart(e, idx)}>
           ${icons.drag}
        </div>
        <span class="queue-item-num" style="min-width:20px;text-align:right;font-size:12px;color:var(--mu-secondary);flex-shrink:0">${idx + 1}</span>
        ${this._renderArt(entry.art_url, 'queue-item-art', icons.music)}
        ${idx === curIdx && this.rendererState?.playback?.status === 'playing' ? html`
          <div class="eq-bars">
            <div class="eq-bar"></div>
            <div class="eq-bar"></div>
            <div class="eq-bar"></div>
          </div>
        ` : ''}
        <div class="queue-item-info">
          <div class="queue-item-title">${entry.title || 'Unknown'}</div>
          <div class="queue-item-artist">${entry.artist || ''}</div>
        </div>
        <div class="queue-item-actions">
          <button class="icon-btn" @click=${e => { e.stopPropagation(); this._queueRemove(entry.queueEntryId); }}>${icons.close}</button>
        </div>
      </div>
    `;
  }
}

if (!customElements.get('mu-panel')) {
  customElements.define('mu-panel', MuPanel);
}
