/** The theme switch. Dark is the default; each device remembers its own choice. */

function switchTheme() {
    const next = document.documentElement.dataset.theme === 'light' ? 'dark' : 'light';
    document.documentElement.dataset.theme = next;
    try { localStorage.setItem('binny-theme', next); } catch { /* storage blocked: the choice lasts this visit */ }
}

export function initTheme() {
    // The top bar's button, and the drawer's on phones.
    for (const button of document.querySelectorAll('#themeBtn, [data-switch-theme]')) button.addEventListener('click', switchTheme);
}
