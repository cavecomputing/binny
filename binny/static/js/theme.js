/** The theme switch. Dark is the default; each device remembers its own choice. */

export function initTheme() {
    document.getElementById('themeBtn').addEventListener('click', () => {
        const next = document.documentElement.dataset.theme === 'light' ? 'dark' : 'light';
        document.documentElement.dataset.theme = next;
        try { localStorage.setItem('binny-theme', next); } catch { /* storage blocked: the choice lasts this visit */ }
    });
}
