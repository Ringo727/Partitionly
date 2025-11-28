// index.js - Landing page logic

document.addEventListener('DOMContentLoaded', () => {
    const joinForm = document.getElementById('join-form');
    const createForm = document.getElementById('create-form');
    const joinError = document.getElementById('join-error');
    const createError = document.getElementById('create-error');
    const joinCodeInput = document.getElementById('join-code');

    // Auto-uppercase and filter join code input
    joinCodeInput.addEventListener('input', (e) => {
        e.target.value = e.target.value.toUpperCase().replace(/[^A-Z0-9]/g, '');
    });

    // Join form submission
    joinForm.addEventListener('submit', async (e) => {
        e.preventDefault();
        joinError.textContent = '';

        const btn = joinForm.querySelector('button[type="submit"]');
        const originalText = btn.textContent;
        btn.disabled = true;
        btn.textContent = 'Joining...';

        try {
            const response = await fetch('/api/round/join', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    code: joinCodeInput.value.trim().toUpperCase(),
                    displayName: document.getElementById('join-name').value.trim()
                })
            });

            const data = await response.json();

            if (data.success) {
                window.location.href = `/round/${data.code}`;
            } else {
                joinError.textContent = data.error || 'Failed to join round';
                btn.disabled = false;
                btn.textContent = originalText;
            }
        } catch (err) {
            joinError.textContent = 'Connection error. Please try again.';
            btn.disabled = false;
            btn.textContent = originalText;
        }
    });

    // Create form submission
    createForm.addEventListener('submit', async (e) => {
        e.preventDefault();
        createError.textContent = '';

        const btn = createForm.querySelector('button[type="submit"]');
        const originalText = btn.textContent;
        btn.disabled = true;
        btn.textContent = 'Creating...';

        try {
            const response = await fetch('/api/round/create', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    name: document.getElementById('round-name').value.trim(),
                    hostName: document.getElementById('host-name').value.trim(),
                    mode: document.querySelector('input[name="mode"]:checked').value,
                    allowGuestDownload: document.getElementById('allow-guest').checked
                })
            });

            const data = await response.json();

            if (data.success) {
                window.location.href = `/round/${data.code}`;
            } else {
                createError.textContent = data.error || 'Failed to create round';
                btn.disabled = false;
                btn.textContent = originalText;
            }
        } catch (err) {
            createError.textContent = 'Connection error. Please try again.';
            btn.disabled = false;
            btn.textContent = originalText;
        }
    });
});