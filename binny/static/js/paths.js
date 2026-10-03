/** Paths are relative to data/files/ with "/" between folders; '' is the top folder. */

export const nameOf = (path) => path.slice(path.lastIndexOf('/') + 1);

export const parentOf = (path) => path.slice(0, Math.max(path.lastIndexOf('/'), 0));

/** The path and each folder above it, top first: 'a/b' -> ['a', 'a/b']. */
export function ancestors(path) {
    const parts = path ? path.split('/') : [];
    return parts.map((_, i) => parts.slice(0, i + 1).join('/'));
}

const encode = (path) => path.split('/').map(encodeURIComponent).join('/');

/** The app's address for a folder, e.g. "#/photos/2026". */
export const folderHash = (path) => `#/${encode(path)}`;

/** The folder the address bar names; the top folder for any other address. */
export function currentFolder() {
    if (!location.hash.startsWith('#/')) return '';
    try {
        return location.hash.slice(2).split('/').filter(Boolean).map(decodeURIComponent).join('/');
    } catch {
        return ''; // a malformed %-escape
    }
}

/** Whether the address bar shows the trash (#trash) rather than a folder. */
export const isTrash = () => location.hash === '#trash';

/** Where the server sends a stored file, or a folder as a zip. */
export const fileUrl = (path, download = false) => `/files/${encode(path)}${download ? '?download' : ''}`;

export const byName = (a, b) => a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' });
