/** Entry point: wires up each module. */
import { initDrop } from './drop.js';
import { initExplorer } from './explorer.js';
import { initMove } from './move.js';
import { initSearch } from './search.js';
import { initShortcuts } from './shortcuts.js';
import { initSidebar } from './sidebar.js';
import { initTagger } from './tagger.js';
import { initTags } from './tags.js';
import { initTheme } from './theme.js';
import { initTrash } from './trash.js';
import { initUi } from './ui.js';
import { initUpload } from './upload.js';

initTheme();
initUi();
initSidebar();
initTags();
initSearch();
initExplorer();
initTrash();
initTagger();
initUpload();
initMove();
initDrop();
initShortcuts();
