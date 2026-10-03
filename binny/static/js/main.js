/** Entry point: wires up each module. */
import { initDrop } from './drop.js';
import { initExplorer } from './explorer.js';
import { initMove } from './move.js';
import { initShortcuts } from './shortcuts.js';
import { initSidebar } from './sidebar.js';
import { initTheme } from './theme.js';
import { initUi } from './ui.js';
import { initUpload } from './upload.js';

initTheme();
initUi();
initSidebar();
initExplorer();
initUpload();
initMove();
initDrop();
initShortcuts();
