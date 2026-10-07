/** The theme switch. Dark is the default; each device remembers its own choice. */

// The browser bar and an installed app's status bar take theme-color; match it to the shell's surface.
function syncThemeColor() {
    document.querySelector('meta[name="theme-color"]').content = getComputedStyle(document.documentElement).getPropertyValue('--bg-surface').trim();
}

function switchTheme() {
    const next = document.documentElement.dataset.theme === 'light' ? 'dark' : 'light';
    document.documentElement.dataset.theme = next;
    try { localStorage.setItem('binny-theme', next); } catch { /* storage blocked: the choice lasts this visit */ }
    syncThemeColor();
}

export function initTheme() {
    syncThemeColor();
    // The top bar's button, and the drawer's on phones.
    for (const button of document.querySelectorAll('#themeBtn, [data-switch-theme]')) button.addEventListener('click', switchTheme);
}
