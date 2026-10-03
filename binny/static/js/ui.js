/** Small helpers every module uses: escaping, formatting, the toast and the question dialog. */

export const $ = (id) => document.getElementById(id);

const ESCAPES = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };
export const esc = (text) => String(text).replace(/[&<>"']/g, (c) => ESCAPES[c]);

export const icon = (name) => `<svg class="i"><use href="#i-${name}"/></svg>`;

export const plural = (count, word) => `${count.toLocaleString()} ${word}${count === 1 ? '' : 's'}`;

export function formatSize(bytes) {
    const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
    let i = 0;
    while (bytes >= 1000 && i < units.length - 1) {
        bytes /= 1000;
        i++;
    }
    return `${i ? bytes.toFixed(bytes < 10 ? 1 : 0) : bytes} ${units[i]}`;
}

/** A modified time (seconds) as 2026-10-03. */
export function formatDate(seconds) {
    const date = new Date(seconds * 1000);
    const pad = (n) => String(n).padStart(2, '0');
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

let toastTimer;

/**
 * A short message at the bottom, with an optional { label, run } button such as Undo. It is a
 * popover, so it shows above an open dialog too.
 */
export function toast(html, { error = false, action = null } = {}) {
    const box = $('toast');
    box.innerHTML = html;
    if (action) {
        const button = Object.assign(document.createElement('button'), { type: 'button', className: 'toast__action', textContent: action.label });
        button.addEventListener('click', () => {
            box.hidePopover();
            action.run();
        });
        box.append(button);
    }
    box.classList.toggle('toast--error', error);
    box.hidePopover();
    box.showPopover();
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => box.hidePopover(), error || action ? 6000 : 3000);
}

export const showError = (message) => toast(esc(message), { error: true });

/**
 * Ask a question in the dialog. With a value it asks for text and resolves to it (select: how much
 * of it to preselect); without, it asks for a yes and resolves to true. action(answer), if given,
 * runs on submit; when it throws, the dialog stays open so the answer can be fixed. Cancel resolves
 * to null.
 */
export function ask({ title, iconName, text = '', value = null, select, ok = 'OK', danger = false, action }) {
    const dialog = $('askDialog');
    const input = $('askInput');
    const okButton = $('askOk');
    $('askTitle').textContent = title;
    $('askIcon').setAttribute('href', `#i-${iconName}`);
    $('askText').textContent = text;
    $('askText').hidden = !text;
    input.hidden = value === null;
    input.value = value ?? '';
    okButton.firstElementChild.textContent = ok;
    okButton.className = `cc-btn ${danger ? 'cc-btn--danger' : 'cc-btn--primary'}`;
    dialog.showModal(); // a yes/no question starts on Cancel
    if (value !== null) {
        input.focus();
        input.setSelectionRange(0, select ?? value.length);
    }

    return new Promise((resolve) => {
        let answer = null;
        dialog.querySelector('form').onsubmit = async (event) => {
            event.preventDefault();
            const reply = value === null ? true : input.value;
            okButton.disabled = true;
            try {
                await action?.(reply);
                answer = reply;
                dialog.close();
            } catch { /* api.js showed why; let the answer be fixed */ }
            okButton.disabled = false;
        };
        dialog.onclose = () => resolve(answer);
    });
}

export function initUi() {
    // Cancel buttons, and clicks on the dimmed backdrop, close their dialog.
    document.addEventListener('click', (event) => {
        if (event.target.closest('[data-close]')) event.target.closest('dialog').close();
        else if (event.target.matches('dialog')) event.target.close();
    });
}
