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
    padding: 4px 6px;
    font-size: 14px;
    max-width: 140px;
  }

  .lease-btn {
    background: var(--mu-accent);
    border: none;
    color: white;
    cursor: pointer;
    padding: 4px 10px;
    border-radius: 4px;
    font-size: 11px;
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
    border-radius: 4px;
    background: var(--mu-border);
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
    padding: 6px 10px;
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

  .queue-item:hover, .browser-item:hover { background: rgba(255,255,255,0.05); }
  .queue-item.playing { background: rgba(3,169,244,0.12); }

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

  .queue-item-art svg, .browser-item-art svg { width: 16px; height: 16px; opacity: 0.4; }
  .browser-item-art { width: 36px; height: 36px; }

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
    transition: opacity 0.1s;
    flex-shrink: 0;
  }

  .queue-item:hover .queue-item-actions, .browser-item:hover .browser-item-actions { opacity: 1; }

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
    font-size: 9px;
    font-weight: 600;
    text-transform: uppercase;
    flex-shrink: 0;
  }

  .action-btn.secondary { background: rgba(255,255,255,0.12); color: var(--primary-text-color); }

  .browser-with-index { display: flex; flex: 1; overflow: hidden; position: relative; }
  .browser-list-wrapper { flex: 1; overflow-y: auto; }

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
    font-size: 10px;
    font-weight: 500;
    border-radius: 3px;
    min-width: 20px;
  }

  .letter-index button:hover { background: rgba(255,255,255,0.1); color: var(--mu-accent); }

  /* Zone Styles */
  .zone-group {
    border-bottom: 1px solid var(--mu-border);
    padding: 8px;
  }

  .zone-group-header {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 8px;
    font-size: 12px;
    font-weight: 500;
    color: var(--primary-text-color);
  }

  .zone-group-header .source-name {
    flex: 1;
    color: var(--mu-accent);
  }

  .zone-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 0;
    font-size: 11px;
  }

  .zone-item-name {
    flex: 1;
    min-width: 80px;
    color: var(--primary-text-color);
  }

  .zone-item-name.disconnected {
    opacity: 0.5;
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
    padding: 2px 4px;
    font-size: 10px;
    max-width: 100px;
  }

  .breadcrumbs {
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 5px 8px;
    font-size: 10px;
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
    font-size: 10px;
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
    font-size: 10px;
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

  .toast {
    position: fixed;
    bottom: 14px;
    left: 50%;
    transform: translateX(-50%);
    background: var(--mu-card-bg);
    color: var(--primary-text-color);
    padding: 6px 14px;
    border-radius: 4px;
    box-shadow: 0 2px 8px rgba(0,0,0,0.3);
    font-size: 11px;
    z-index: 1000;
  }

  .queue-item.dragging { opacity: 0.4; }
  .queue-item.drag-over { box-shadow: 0 -2px 0 0 var(--mu-accent); }

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
  }

  /* Responsive: Mobile (600px and below) */
  @media (max-width: 600px) {
    .mobile-view-toggle { display: flex; }
    
    .panel-container { flex-direction: column; }
    
    .browser-pane {
      width: 100% !important;
      border-right: none;
      border-bottom: 1px solid var(--mu-border);
      min-width: unset;
    }
    
    .browser-pane.hidden { display: none; }
    .queue-pane.hidden { display: none; }
    
    .queue-pane { min-width: unset; }
    
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
    
    .browser-item, .queue-item { padding: 12px 10px; }
    .browser-item-art, .queue-item-art { width: 48px; height: 48px; }
    .browser-item-title, .queue-item-title { font-size: 14px; }
    .browser-item-subtitle, .queue-item-subtitle { font-size: 12px; }
    
    .action-btn { padding: 8px 12px; font-size: 12px; }
    .icon-btn { width: 32px; height: 32px; }
    .icon-btn svg { width: 16px; height: 16px; }
    
    .tabs { gap: 0; }
    .tab { padding: 12px 8px; font-size: 11px; }
    
    .zone-group { padding: 12px; }
    .zone-item { padding: 12px 0; gap: 10px; }
    .zone-item-name { font-size: 13px; }
    .zone-volume-slider { height: 10px; }
    .zone-mute-btn { width: 40px; height: 40px; }
    .zone-source-select { padding: 6px; font-size: 12px; max-width: none; }
    
    .breadcrumbs { padding: 8px 10px; }
    .breadcrumb { padding: 6px 8px; font-size: 12px; }
    
    .letter-index { padding: 6px 4px; }
    .letter-index button { padding: 8px 10px; font-size: 13px; min-width: 24px; }
    
    .toast { bottom: 60px; padding: 10px 18px; font-size: 13px; }
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
    this._refreshInterval = null;
    this._localSeekPos = null;
    this._localVolume = null;
  }

  connectedCallback() {
    super.connectedCallback();
    this._init();
    this._boundMouseMove = this._onMouseMove.bind(this);
    this._boundMouseUp = this._onMouseUp.bind(this);
    this._boundSeekMove = this._onSeekMove.bind(this);
    this._boundSeekEnd = this._onSeekEnd.bind(this);
    this._boundVolMove = this._onVolMove.bind(this);
    this._boundVolEnd = this._onVolEnd.bind(this);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    if (this._refreshInterval) clearInterval(this._refreshInterval);
    document.removeEventListener('mousemove', this._boundMouseMove);
    document.removeEventListener('mouseup', this._boundMouseUp);
    document.removeEventListener('mousemove', this._boundSeekMove);
    document.removeEventListener('mouseup', this._boundSeekEnd);
    document.removeEventListener('mousemove', this._boundVolMove);
    document.removeEventListener('mouseup', this._boundVolEnd);
  }

  async _init() {
    await this._loadRenderers();
    await this._loadLibraries();
    await this._loadPlaylists();
    await this._loadSnapshots();
    this.loading = false;
    this._refreshInterval = setInterval(() => this._refreshState(), 2000);
  }

  async _callWS(type, data = {}) {
    try { return await this.hass.callWS({ type, ...data }); }
    catch (err) { console.error(`WS ${type}:`, err); return null; }
  }

  _showToast(msg) { this.toast = msg; setTimeout(() => { this.toast = null; }, 2000); }

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
    if (r) { this.renderers = r; if (!this.selectedRenderer && r.length) { this.selectedRenderer = r[0].rendererId; await this._loadRendererState(); await this._loadQueue(); } }
  }

  async _loadRendererState() {
    if (!this.selectedRenderer) return;
    const r = await this._callWS('mu/renderer_state', { renderer_id: this.selectedRenderer });
    if (r) {
      const oldRev = this.rendererState?.queue?.revision;
      this.rendererState = r;
      this.leaseOwned = r.session?.owned || false;
      if (r.queue?.revision !== oldRev) {
        console.log(`Queue revision changed: ${oldRev} -> ${r.queue?.revision}`);
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
    if (r) { this.libraries = r; this.browserPath = []; this.browserItems = r.map(lib => ({ itemId: lib.libraryId, title: lib.name, isContainer: true, isLibrary: true })); }
  }

  async _loadPlaylists() { const r = await this._callWS('mu/playlists_list'); if (r) this.playlists = r; }
  async _loadSnapshots() { const r = await this._callWS('mu/snapshots_list'); if (r) this.snapshots = r; }
  async _refreshState() { await this._loadRendererState(); }

  async _selectRenderer(e) {
    this.selectedRenderer = e.target.value;
    await this._loadRendererState();
    await this._loadQueue();
  }

  async _acquireLease() {
    if (!this.selectedRenderer) return;
    const r = await this._callWS('mu/lease_acquire', { renderer_id: this.selectedRenderer });
    if (r?.success) { this.leaseOwned = true; this._showToast('Acquired'); await this._loadRendererState(); }
  }

  async _releaseLease() {
    if (!this.selectedRenderer) return;
    const r = await this._callWS('mu/lease_release', { renderer_id: this.selectedRenderer });
    if (r?.success) { this.leaseOwned = false; this._showToast('Released'); await this._loadRendererState(); }
  }

  async _transport(action) {
    if (!this.selectedRenderer) return;
    await this._callWS('mu/transport', { renderer_id: this.selectedRenderer, action });
    await this._loadRendererState();
  }

  // Slider drag handling
  _onSeekStart(e) {
    if (!this.leaseOwned || !this.selectedRenderer) return;
    this._seekDragging = true;
    this._seekTrack = e.currentTarget;
    this._updateSeekFromEvent(e);
    document.addEventListener('mousemove', this._boundSeekMove);
    document.addEventListener('mouseup', this._boundSeekEnd);
    e.preventDefault();
  }

  _onSeekMove(e) {
    if (!this._seekDragging) return;
    this._updateSeekFromEvent(e);
  }

  _onSeekEnd() {
    if (!this._seekDragging) return;
    this._seekDragging = false;
    document.removeEventListener('mousemove', this._boundSeekMove);
    document.removeEventListener('mouseup', this._boundSeekEnd);
    // Reset local value after a short delay to let server state catch up
    setTimeout(() => { this._localSeekPos = null; this.requestUpdate(); }, 300);
  }

  _updateSeekFromEvent(e) {
    if (!this._seekTrack) return;
    const rect = this._seekTrack.getBoundingClientRect();
    const pct = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
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
    document.addEventListener('mousemove', this._boundVolMove);
    document.addEventListener('mouseup', this._boundVolEnd);
    e.preventDefault();
  }

  _onVolMove(e) {
    if (!this._volDragging) return;
    this._updateVolumeFromEvent(e);
  }

  _onVolEnd() {
    if (!this._volDragging) return;
    this._volDragging = false;
    document.removeEventListener('mousemove', this._boundVolMove);
    document.removeEventListener('mouseup', this._boundVolEnd);
    // Reset local value after a short delay to let server state catch up
    setTimeout(() => { this._localVolume = null; this.requestUpdate(); }, 300);
  }

  _updateVolumeFromEvent(e) {
    if (!this._volTrack) return;
    const rect = this._volTrack.getBoundingClientRect();
    const vol = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
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
    // Use the bridge's shuffle queue command which physically rearranges queue
    await this._callWS('mu/shuffle', { renderer_id: this.selectedRenderer, shuffle: true });
    await this._loadQueue();
    this._showToast('Shuffled');
  }

  async _queueJump(idx) {
    if (!this.selectedRenderer) return;
    await this._callWS('mu/queue_jump', { renderer_id: this.selectedRenderer, index: idx });
    await this._loadRendererState();
  }

  async _queueRemove(id) {
    if (!this.selectedRenderer) return;
    await this._callWS('mu/queue_remove', { renderer_id: this.selectedRenderer, queue_entry_id: id });
    await this._loadQueue();
  }

  async _queueClear() {
    if (!this.selectedRenderer) return;
    await this._callWS('mu/queue_clear', { renderer_id: this.selectedRenderer });
    await this._loadQueue();
    this._showToast('Cleared');
  }

  async _browseContainer(item) {
    if (item.isLibrary) {
      this.browserPath = [{ id: item.itemId, title: item.title, isLibrary: true }];
      await this._loadBrowserItems(item.itemId, '');
    } else if (item.isContainer) {
      const libId = this.browserPath[0]?.id;
      if (!libId) return;
      this.browserPath = [...this.browserPath, { id: item.itemId, title: item.title }];
      await this._loadBrowserItems(libId, item.itemId);
    }
  }

  async _loadBrowserItems(libId, containerId, append = false) {
    this.browserLoading = true;
    const start = append ? this.browserItems.length : 0;
    const r = await this._callWS('mu/library_browse', { library_id: libId, container_id: containerId, start, count: 100 });
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
      this.browserPath = [];
      this.browserItems = this.libraries.map(lib => ({ itemId: lib.libraryId, title: lib.name, isContainer: true, isLibrary: true }));
    } else {
      const libId = this.browserPath[0]?.id;
      const target = this.browserPath[idx];
      this.browserPath = this.browserPath.slice(0, idx + 1);
      await this._loadBrowserItems(libId, idx === 0 ? '' : target.id);
    }
  }

  async _addToQueue(item, mode) {
    if (!this.selectedRenderer || !item.itemId) return;
    const libId = this.browserPath[0]?.id;
    if (!libId) return;
    await this._callWS('mu/queue_add', { renderer_id: this.selectedRenderer, mode, items: [`lib:${libId}:${item.itemId}`] });
    await this._loadQueue();
    this._showToast(mode === 'replace' ? 'Playing' : mode === 'next' ? 'Next' : 'Added');
  }

  async _switchTab(tab) {
    this.browserTab = tab;
    if (tab === 'playlists') await this._loadPlaylists();
    else if (tab === 'snapshots') await this._loadSnapshots();
    else if (tab === 'zones') await this._loadZones();
  }

  async _loadPlaylistToQueue(id, mode = 'replace') {
    if (!this.selectedRenderer) return;
    await this._callWS('mu/queue_load_playlist', { renderer_id: this.selectedRenderer, playlist_id: id, mode });
    await this._loadQueue();
    this._showToast('Loaded');
  }

  async _loadSnapshotToQueue(id) {
    if (!this.selectedRenderer) return;
    await this._callWS('mu/queue_load_snapshot', { renderer_id: this.selectedRenderer, snapshot_id: id, mode: 'replace' });
    await this._loadQueue();
    this._showToast('Restored');
  }

  async _saveSnapshot() {
    if (!this.selectedRenderer) return;
    const name = prompt('Snapshot name:');
    if (!name?.trim()) return;
    const r = await this._callWS('mu/snapshot_save', { renderer_id: this.selectedRenderer, name: name.trim() });
    if (r?.success) {
      this._showToast('Saved');
      await this._loadSnapshots();
    } else {
      this._showToast('Failed to save snapshot');
    }
  }

  async _deleteSnapshot(snapshotId) {
    if (!confirm('Delete this snapshot?')) return;
    const r = await this._callWS('mu/snapshot_delete', { snapshot_id: snapshotId });
    if (r?.success) {
      this._showToast('Deleted');
      await this._loadSnapshots();
    } else {
      this._showToast('Failed to delete');
    }
  }

  async _createPlaylistFromSnapshot(snapshotId, snapshotName) {
    const name = prompt('Playlist name:', snapshotName);
    if (!name?.trim()) return;
    const r = await this._callWS('mu/playlist_from_snapshot', { snapshot_id: snapshotId, name: name.trim() });
    if (r?.success) {
      this._showToast('Playlist created');
      await this._loadPlaylists();
    } else {
      this._showToast('Failed to create playlist');
    }
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
    if (this.loading) return html`<div class="loading"><div class="spinner"></div></div>`;
    return html`
      <div class="mobile-view-toggle">
        <button class="${this.mobileView === 'browser' ? 'active' : ''}" @click=${() => this.mobileView = 'browser'}>Browse</button>
        <button class="${this.mobileView === 'player' ? 'active' : ''}" @click=${() => this.mobileView = 'player'}>Player</button>
      </div>
      <div class="panel-container">
        ${this._renderBrowser()}
        <div class="resize-handle ${this.resizing ? 'dragging' : ''}" @mousedown=${this._onResizeStart}></div>
        ${this._renderQueue()}
      </div>
      ${this.toast ? html`<div class="toast">${this.toast}</div>` : ''}
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
    return html`
      <div class="breadcrumbs">
        <button class="breadcrumb" @click=${() => this._navigateBreadcrumb(-1)}>${icons.home}</button>
        ${this.browserPath.map((c, i) => html`<span class="breadcrumb-sep">›</span><button class="breadcrumb" @click=${() => this._navigateBreadcrumb(i)}>${c.title}</button>`)}
      </div>
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
    `;
  }

  _renderBrowserItem(item) {
    const icon = item.isLibrary ? icons.library : item.isContainer ? icons.folder : icons.music;
    const canNavigate = item.isContainer || item.isLibrary;
    return html`
      <div class="browser-item" @click=${() => canNavigate ? this._browseContainer(item) : null} style="${canNavigate ? '' : 'cursor:default'}">
        ${item.art_url ? html`<img class="browser-item-art" src="${item.art_url}" @error=${e => e.target.style.display = 'none'}/>` : html`<div class="browser-item-art">${icon}</div>`}
        <div class="browser-item-info">
          <div class="browser-item-title">${item.title}</div>
          ${item.subtitle ? html`<div class="browser-item-subtitle">${item.subtitle}</div>` : ''}
        </div>
        <div class="browser-item-actions">
          ${item.isPlayable || item.canEnqueue ? html`
            <button class="action-btn" @click=${e => { e.stopPropagation(); this._addToQueue(item, 'replace'); }} title="Play">▶</button>
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
        ${item.art_url ? html`<img class="browser-item-art" src="${item.art_url}" @error=${e => e.target.style.display = 'none'}/>` : html`<div class="browser-item-art">${icon}</div>`}
        <div class="browser-item-info">
          <div class="browser-item-title">${item.title}</div>
          ${item.subtitle ? html`<div class="browser-item-subtitle">${item.subtitle}</div>` : ''}
        </div>
        <div class="browser-item-actions">
          ${item.isPlayable || item.canEnqueue ? html`
            <button class="action-btn" @click=${e => { e.stopPropagation(); this._addToQueue(item, 'replace'); }} title="Play">▶</button>
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
    if (!confirm('Delete this playlist?')) return;
    const r = await this._callWS('mu/playlist_delete', { playlist_id: playlistId });
    if (r?.success) {
      this._showToast('Deleted');
      await this._loadPlaylists();
    } else {
      this._showToast('Failed to delete');
    }
  }

  _renderPlaylistsTab() {
    return html`<div class="browser-list">
      ${!this.playlists.length ? html`<div class="empty">No playlists</div>` : ''}
      ${this.playlists.map(pl => html`
        <div class="browser-item">
          <div class="browser-item-art">${icons.playlist}</div>
          <div class="browser-item-info"><div class="browser-item-title">${pl.name}</div><div class="browser-item-subtitle">${pl.size || 0} items</div></div>
          <div class="browser-item-actions" style="opacity:1">
            <button class="action-btn" @click=${() => this._loadPlaylistToQueue(pl.playlistId, 'replace')}>▶</button>
            <button class="action-btn secondary" @click=${() => this._loadPlaylistToQueue(pl.playlistId, 'append')}>+</button>
            <button class="icon-btn" @click=${() => this._deletePlaylist(pl.playlistId)} title="Delete">${icons.close}</button>
          </div>
        </div>
      `)}
    </div>`;
  }

  _renderSnapshotsTab() {
    return html`
      <div class="pane-header"><span class="pane-title">Snapshots</span><button class="action-btn" @click=${() => this._saveSnapshot()}>Save</button></div>
      <div class="browser-list">
        ${!this.snapshots.length ? html`<div class="empty">No snapshots</div>` : ''}
        ${this.snapshots.map(s => html`
          <div class="browser-item">
            <div class="browser-item-art">${icons.camera}</div>
            <div class="browser-item-info"><div class="browser-item-title">${s.name}</div></div>
            <div class="browser-item-actions" style="opacity:1">
              <button class="action-btn" @click=${() => this._loadSnapshotToQueue(s.snapshotId)} title="Restore to queue">▶</button>
              <button class="action-btn secondary" @click=${() => this._createPlaylistFromSnapshot(s.snapshotId, s.name)} title="Create playlist">📋</button>
              <button class="icon-btn" @click=${() => this._deleteSnapshot(s.snapshotId)} title="Delete">${icons.close}</button>
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
    console.log('Zone controllers:', controllers);
    console.log('Zones:', zones);
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
    if (!this.zones.length) {
      return html`<div class="browser-list"><div class="empty">No zones available</div></div>`;
    }

    // Group zones by sourceId
    const groups = new Map();
    for (const zone of this.zones) {
      const sourceId = zone.sourceId || 'unassigned';
      if (!groups.has(sourceId)) {
        groups.set(sourceId, []);
      }
      groups.get(sourceId).push(zone);
    }

    const sources = this._getAllSources();

    return html`
      <div class="browser-list" style="padding: 0;">
        ${Array.from(groups.entries()).map(([sourceId, zones]) =>
      this._renderZoneGroup(sourceId, zones, sources)
    )}
      </div>
    `;
  }

  _renderZoneGroup(sourceId, zones, sources) {
    const sourceName = this._getSourceName(sourceId);
    return html`
      <div class="zone-group">
        <div class="zone-group-header">
          <span class="source-name">🎵 ${sourceName}</span>
        </div>
        ${zones.map(zone => this._renderZoneItem(zone, sources))}
      </div>
    `;
  }

  _renderZoneItem(zone, sources) {
    const volumePct = Math.round((zone.volume || 0) * 100);
    return html`
      <div class="zone-item">
        <span class="zone-item-name ${zone.connected ? '' : 'disconnected'}">${zone.name}</span>
        <div class="zone-volume-slider" @click=${(e) => this._handleZoneVolumeClick(e, zone)}>
          <div class="zone-volume-fill" style="width: ${volumePct}%"></div>
        </div>
        <span style="font-size:10px;color:var(--mu-secondary);min-width:28px">${volumePct}%</span>
        <button class="zone-mute-btn ${zone.mute ? 'muted' : ''}" @click=${() => this._toggleZoneMute(zone)} title="${zone.mute ? 'Unmute' : 'Mute'}">
          ${zone.mute ? html`<svg viewBox="0 0 24 24" fill="currentColor" width="16" height="16"><path d="M16.5 12c0-1.77-1.02-3.29-2.5-4.03v2.21l2.45 2.45c.03-.2.05-.41.05-.63zm2.5 0c0 .94-.2 1.82-.54 2.64l1.51 1.51C20.63 14.91 21 13.5 21 12c0-4.28-2.99-7.86-7-8.77v2.06c2.89.86 5 3.54 5 6.71zM4.27 3L3 4.27 7.73 9H3v6h4l5 5v-6.73l4.25 4.25c-.67.52-1.42.93-2.25 1.18v2.06c1.38-.31 2.63-.95 3.69-1.81L19.73 21 21 19.73l-9-9L4.27 3zM12 4L9.91 6.09 12 8.18V4z"/></svg>` : icons.volume}
        </button>
        <select class="zone-source-select" @change=${(e) => this._setZoneSource(zone, e.target.value)} .value=${zone.sourceId || ''}>
          ${sources.map(s => html`<option value="${s.id}" ?selected=${s.id === zone.sourceId}>${s.name}</option>`)}
        </select>
      </div>
    `;
  }

  _handleZoneVolumeClick(e, zone) {
    const rect = e.currentTarget.getBoundingClientRect();
    const pct = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
    this._setZoneVolume(zone, pct);
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
    const st = this.rendererState;
    const pb = st?.playback || {};
    const cur = st?.current || {};
    const q = st?.queue || {};
    const sess = st?.session || {};
    const playing = pb.status === 'playing';
    // Use local values when dragging, otherwise use server state
    const pos = this._localSeekPos !== null ? this._localSeekPos : (pb.position_ms || 0);
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
          ${cur.art_url ? html`<img class="now-playing-art" src="${cur.art_url}"/>` : html`<div class="now-playing-art">${icons.music}</div>`}
          <div class="now-playing-info">
            <div class="now-playing-title">${cur.title || 'Nothing playing'}</div>
            <div class="now-playing-artist">${cur.artist || ''}</div>
          </div>
        </div>

        <div class="transport">
          <button class="transport-btn ${shuffle ? 'active' : ''}" @click=${this._toggleShuffle} ?disabled=${!this.leaseOwned} title="Shuffle">${icons.shuffle}</button>
          <button class="transport-btn" @click=${() => this._transport('prev')} ?disabled=${!this.leaseOwned}>${icons.prev}</button>
          <button class="transport-btn primary" @click=${() => this._transport('toggle')} ?disabled=${!this.leaseOwned}>${playing ? icons.pause : icons.play}</button>
          <button class="transport-btn" @click=${() => this._transport('next')} ?disabled=${!this.leaseOwned}>${icons.next}</button>
          <button class="transport-btn ${repeatMode !== 'off' ? 'active' : ''}" @click=${this._cycleRepeat} ?disabled=${!this.leaseOwned} title="Repeat">${repeatMode === 'one' ? icons.repeatOne : icons.repeat}</button>
        </div>

        <div class="progress-section">
          <div class="progress-bar-container">
            <span>${formatTime(pos)}</span>
            <div class="progress-track" @mousedown=${this._onSeekStart}>
              <div class="progress-fill" style="width:${(pos / dur) * 100}%"><div class="progress-thumb"></div></div>
            </div>
            <span>${formatTime(dur)}</span>
          </div>
          <div class="volume-row">
            ${icons.volume}
            <div class="volume-track" @mousedown=${this._onVolumeStart}><div class="volume-fill" style="width:${vol * 100}%"><div class="volume-thumb"></div></div></div>
            <span>${Math.round(vol * 100)}%</span>
          </div>
        </div>

        <div class="pane-header">
          <span class="pane-title">Queue (${this.queue.length})</span>
          <button class="icon-btn" @click=${this._shuffleQueue} ?disabled=${!this.leaseOwned || this.queue.length < 2} title="Shuffle">${icons.shuffle}</button>
          <button class="action-btn secondary" @click=${this._queueClear} ?disabled=${!this.leaseOwned || !this.queue.length}>Clear</button>
        </div>

        <div class="queue-list">
          ${!this.queue.length ? html`<div class="empty">Queue empty</div>` : ''}
          ${this.queue.map((e, i) => this._renderQueueItem(e, i, q.index))}
        </div>
      </div>
    `;
  }

  _renderQueueItem(entry, idx, curIdx) {
    return html`
      <div class="queue-item ${idx === curIdx ? 'playing' : ''} ${this.dragIndex === idx ? 'dragging' : ''} ${this.dragOverIndex === idx ? 'drag-over' : ''}"
        draggable="true" @dragstart=${e => this._onDragStart(e, idx)} @dragover=${e => this._onDragOver(e, idx)} @dragleave=${this._onDragLeave} @drop=${e => this._onDrop(e, idx)} @click=${() => this._queueJump(idx)}>
        ${entry.art_url ? html`<img class="queue-item-art" src="${entry.art_url}"/>` : html`<div class="queue-item-art">${icons.music}</div>`}
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

customElements.define('mu-panel', MuPanel);
