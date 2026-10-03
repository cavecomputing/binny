/** What the explorer shows, shared by the modules that read or change it. */
export const state = {
    folder: '',          // the folder on screen, or the one a search started from
    search: null,        // the search on screen, "tag -tag @name", or null for a folder
    items: [],           // what's on screen, as /api/list or /api/search sends it
    truncated: false,    // whether the search found more than it sent
    selected: new Set(), // paths of the selected items
};

/** Something changed files on the server: the explorer and the sidebar reload. */
export const filesChanged = () => window.dispatchEvent(new Event('files-changed'));
