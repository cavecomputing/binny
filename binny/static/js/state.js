/** What the explorer shows, shared by the modules that read or change it. */
export const state = {
    folder: '',          // the folder on screen
    items: [],           // its listing, as /api/list sends it
    selected: new Set(), // paths of the selected items
};

/** Something changed files on the server: the explorer and the sidebar reload. */
export const filesChanged = () => window.dispatchEvent(new Event('files-changed'));
