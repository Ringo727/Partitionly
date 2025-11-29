// round.js - Round page functionality

document.addEventListener('DOMContentLoaded', () => {
    // Read data from data attributes
    const dataEl = document.getElementById('round-data');
    const code = dataEl.dataset.code;
    const state = dataEl.dataset.state;
    const mode = dataEl.dataset.mode;
    const isHost = dataEl.dataset.isHost === 'true';
    const isParticipant = dataEl.dataset.isParticipant === 'true';
    const participantId = dataEl.dataset.participantId;
    const hasSample = dataEl.dataset.hasSample === 'true';

    // Store in window for potential later use
    window.ROUND_DATA = { code, state, mode, isHost, isParticipant, participantId, hasSample };

    // Elements
    const toast = document.getElementById('toast');
    const roundCode = document.getElementById('round-code');
    const roundState = document.getElementById('round-state');

    // === Toast Notifications ===
    function showToast(message, type = 'success') {
        toast.textContent = message;
        toast.className = `toast ${type} show`;
        setTimeout(() => {
            toast.classList.remove('show');
        }, 7000);
    }

    // === Copy Round Code ===
    if (roundCode) {
        roundCode.addEventListener('click', async () => {
            try {
                await navigator.clipboard.writeText(code);
                showToast('Code copied to clipboard!');
            } catch (err) {
                // Fallback for older browsers
                const textArea = document.createElement('textarea');
                textArea.value = code;
                document.body.appendChild(textArea);
                textArea.select();
                document.execCommand('copy');
                document.body.removeChild(textArea);
                showToast('Code copied to clipboard!');
            }
        });
    }

    // === Back to Home (also leaves round) ===
    const backLink = document.querySelector('.back-link');
    if (backLink && isParticipant) {
        backLink.addEventListener('click', async (e) => {
            e.preventDefault();

            if (!confirm('Leave this round and return home?')) {
                return;
            }

            try {
                const response = await fetch(`/api/round/${code}/leave`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' }
                });

                // Redirect home regardless of response
                window.location.href = '/';
            } catch (err) {
                // Still redirect home even if the leave request fails
                window.location.href = '/';
            }
        });
    }

    // === File Upload Helper ===
    function setupUploadArea(areaId, inputId, progressId, statusId, endpoint, fieldName, onSuccess) {
        const area = document.getElementById(areaId);
        const input = document.getElementById(inputId);
        const progressBar = document.getElementById(progressId);
        const statusEl = document.getElementById(statusId);

        if (!area || !input) return;

        // Click to open file dialog
        area.addEventListener('click', () => {
            if (!area.classList.contains('disabled')) {
                input.click();
            }
        });

        // Drag and drop
        area.addEventListener('dragover', (e) => {
            e.preventDefault();
            if (!area.classList.contains('disabled')) {
                area.classList.add('dragover');
            }
        });

        area.addEventListener('dragleave', () => {
            area.classList.remove('dragover');
        });

        area.addEventListener('drop', (e) => {
            e.preventDefault();
            area.classList.remove('dragover');
            if (!area.classList.contains('disabled') && e.dataTransfer.files.length) {
                input.files = e.dataTransfer.files;
                handleFileUpload(e.dataTransfer.files[0]);
            }
        });

        // File selection
        input.addEventListener('change', () => {
            if (input.files.length) {
                handleFileUpload(input.files[0]);
            }
        });

        async function handleFileUpload(file) {
            // Validate file type
            const validTypes = ['.mp3', '.wav', '.m4a', '.flac', '.ogg', '.aac'];
            const ext = '.' + file.name.split('.').pop().toLowerCase();
            if (!validTypes.includes(ext)) {
                showToast('Invalid file type. Please upload an audio file.', 'error');
                return;
            }

            // Validate file size (32MB)
            if (file.size > 32 * 1024 * 1024) {
                showToast('File too large. Maximum size is 32MB.', 'error');
                return;
            }

            // Show progress
            if (progressBar) {
                progressBar.classList.remove('hidden');
                progressBar.querySelector('.progress-fill').style.width = '0%';
            }
            if (statusEl) {
                statusEl.textContent = 'Uploading...';
                statusEl.className = 'upload-status';
            }

            // Create form data
            const formData = new FormData();
            formData.append(fieldName, file);

            try {
                const xhr = new XMLHttpRequest();

                // Progress tracking
                xhr.upload.addEventListener('progress', (e) => {
                    if (e.lengthComputable && progressBar) {
                        const percent = (e.loaded / e.total) * 100;
                        progressBar.querySelector('.progress-fill').style.width = percent + '%';
                    }
                });

                // Wrap XHR in promise
                const response = await new Promise((resolve, reject) => {
                    xhr.onload = () => {
                        if (xhr.status >= 200 && xhr.status < 300) {
                            resolve(JSON.parse(xhr.responseText));
                        } else {
                            reject(new Error(xhr.responseText));
                        }
                    };
                    xhr.onerror = () => reject(new Error('Network error'));
                    xhr.open('POST', endpoint);
                    xhr.send(formData);
                });

                if (response.success) {
                    showToast(response.message || 'Upload successful!');
                    if (statusEl) {
                        statusEl.innerHTML = '<i data-lucide="check" class="icon-inline icon-success"></i> ' + (response.originalName || file.name);
                        statusEl.className = 'upload-status success-message';
                        lucide.createIcons();
                    }
                    if (onSuccess) onSuccess(response);
                } else {
                    showToast(response.error || 'Upload failed', 'error');
                    if (statusEl) {
                        statusEl.textContent = response.error || 'Upload failed';
                        statusEl.className = 'upload-status error-message';
                    }
                }
            } catch (err) {
                console.error('Upload error:', err);
                showToast('Upload failed. Please try again.', 'error');
                if (statusEl) {
                    statusEl.textContent = 'Upload failed';
                    statusEl.className = 'upload-status error-message';
                }
            } finally {
                // Hide progress after delay
                setTimeout(() => {
                    if (progressBar) progressBar.classList.add('hidden');
                }, 1000);
            }
        }
    }

    // === Participant Upload ===
    if (isParticipant) {
        setupUploadArea(
            'upload-area',
            'file-input',
            'upload-progress',
            'upload-status',
            `/api/round/${code}/upload`,
            'audio',
            (response) => {
                // Update submission status
                const statusDiv = document.getElementById('submission-status');
                if (statusDiv) {
                    statusDiv.innerHTML = `<span><i data-lucide="check" class="icon-inline"></i> Submitted: ${response.originalName}</span>`;
                    lucide.createIcons();
                } else {
                    const uploadSection = document.getElementById('upload-section');
                    const newStatus = document.createElement('div');
                    newStatus.id = 'submission-status';
                    newStatus.className = 'file-status success';
                    newStatus.innerHTML = `<span><i data-lucide="check" class="icon-inline"></i> Submitted: ${response.originalName}</span>`;
                    uploadSection.insertBefore(newStatus, uploadSection.querySelector('.upload-area'));
                    lucide.createIcons();
                }
                // Update participant list
                refreshParticipants();
            }
        );
    }

    // === Host: Sample Upload ===
    if (isHost && mode === 'sample') {
        const sampleArea = document.getElementById('sample-upload-area');
        const replaceSampleBtn = document.getElementById('replace-sample-btn');

        setupUploadArea(
            'sample-upload-area',
            'sample-file-input',
            'sample-progress',
            'sample-upload-status',
            `/api/round/${code}/upload-sample`,
            'sample',
            (response) => {
                // Show success state
                const section = document.getElementById('sample-upload-section');
                let statusDiv = section.querySelector('.file-status');
                if (!statusDiv) {
                    statusDiv = document.createElement('div');
                    statusDiv.className = 'file-status success';
                    section.insertBefore(statusDiv, sampleArea);
                }
                statusDiv.innerHTML = `
                    <span><i data-lucide="check" class="icon-inline"></i> Sample uploaded</span>
                    <button class="btn btn-sm btn-outline" id="replace-sample-btn">Replace</button>
                `;
                sampleArea.classList.add('hidden');
                lucide.createIcons();

                // Re-attach replace button listener
                document.getElementById('replace-sample-btn').addEventListener('click', () => {
                    sampleArea.classList.remove('hidden');
                });
            }
        );

        // Replace sample button
        if (replaceSampleBtn) {
            replaceSampleBtn.addEventListener('click', () => {
                sampleArea.classList.remove('hidden');
            });
        }
    }

    // === Host: State Controls ===
    const startBtn = document.getElementById('start-round-btn');
    const closeBtn = document.getElementById('close-round-btn');

    if (startBtn) {
        startBtn.addEventListener('click', () => updateRoundState('active'));
    }

    if (closeBtn) {
        closeBtn.addEventListener('click', () => updateRoundState('closed'));
    }

    // === Leave Round ===
    const leaveBtn = document.getElementById('leave-round-btn');
    if (leaveBtn) {
        leaveBtn.addEventListener('click', async () => {
            if (!confirm('Are you sure you want to leave this round?')) {
                return;
            }

            leaveBtn.disabled = true;
            leaveBtn.textContent = 'Leaving...';

            try {
                const response = await fetch(`/api/round/${code}/leave`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' }
                });

                const data = await response.json();

                if (data.success) {
                    showToast('You have left the round');
                    setTimeout(() => {
                        window.location.href = '/';
                    }, 1000);
                } else {
                    showToast(data.error || 'Failed to leave round', 'error');
                    leaveBtn.disabled = false;
                    leaveBtn.textContent = 'Leave Round';
                }
            } catch (err) {
                console.error('Leave error:', err);
                showToast('Failed to leave round', 'error');
                leaveBtn.disabled = false;
                leaveBtn.textContent = 'Leave Round';
            }
        });
    }

    async function updateRoundState(newState) {
        const btn = newState === 'active' ? startBtn : closeBtn;
        if (btn) {
            btn.disabled = true;
            btn.textContent = 'Updating...';
        }

        try {
            const response = await fetch(`/api/round/${code}/state`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ state: newState })
            });

            const data = await response.json();

            if (data.success) {
                showToast(`Round is now ${newState}`);
                // Reload to update UI
                setTimeout(() => window.location.reload(), 500);
            } else {
                showToast(data.error || 'Failed to update state', 'error');
                if (btn) {
                    btn.disabled = false;
                    btn.textContent = newState === 'active' ? 'Start Round' : 'Close Round';
                }
            }
        } catch (err) {
            console.error('State update error:', err);
            showToast('Failed to update round state', 'error');
            if (btn) {
                btn.disabled = false;
                btn.textContent = newState === 'active' ? 'Start Round' : 'Close Round';
            }
        }
    }

    // === Polling for Updates ===
    let pollInterval = null;

    async function refreshParticipants() {
        try {
            const response = await fetch(`/api/round/${code}/info`);
            const round = await response.json();

            // Update participant count
            const countEl = document.getElementById('participant-count');
            if (countEl) {
                countEl.textContent = Object.keys(round.participants).length;
            }

            // Update participant list
            const list = document.getElementById('participants-list');
            if (list) {
                list.innerHTML = Object.values(round.participants).map(p => {
                    const hasSubmitted = round.submissions && round.submissions[p.id];
                    return `
                    <li class="participant-item ${hasSubmitted ? 'submitted' : 'not-submitted'}" data-id="${p.id}">
                        <div class="participant-info">
                            <span class="participant-name">${escapeHtml(p.displayName)}</span>
                            ${p.isHost ? '<span class="badge badge-host">Host</span>' : ''}
                        </div>
                        ${hasSubmitted
                            ? '<span class="participant-status"><i data-lucide="check" class="icon-inline"></i></span>'
                            : '<span class="participant-status pending"><i data-lucide="clock" class="icon-inline"></i></span>'}
                    </li>
                `}).join('');
                lucide.createIcons();
            }

            // Update state badge if changed
            if (round.state !== window.ROUND_DATA.state) {
                window.ROUND_DATA.state = round.state;
                if (roundState) {
                    roundState.textContent = round.state;
                    roundState.className = `badge badge-${round.state}`;
                }
                // Reload to update UI controls
                window.location.reload();
            }

        } catch (err) {
            console.error('Refresh error:', err);
        }
    }

    function escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    // Start polling (every 5 seconds)
    function startPolling() {
        if (pollInterval) return;
        pollInterval = setInterval(refreshParticipants, 5000);
    }

    function stopPolling() {
        if (pollInterval) {
            clearInterval(pollInterval);
            pollInterval = null;
        }
    }

    // Poll when page is visible
    document.addEventListener('visibilitychange', () => {
        if (document.hidden) {
            stopPolling();
        } else {
            startPolling();
        }
    });

    // Start polling on load
    startPolling();
});