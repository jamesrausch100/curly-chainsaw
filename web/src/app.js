// Curly Chainsaw — Web App Client
//
// Lightweight JS module that powers the web dashboard SPA.
// Talks to the Go backend via the REST API defined in internal/server/server.go.

const API_BASE = window.location.origin;

const api = {
    async get(path) {
        const res = await fetch(`${API_BASE}${path}`);
        if (!res.ok) throw new Error(`GET ${path}: ${res.status}`);
        return res.json();
    },

    async post(path, body) {
        const res = await fetch(`${API_BASE}${path}`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });
        if (!res.ok) throw new Error(`POST ${path}: ${res.status}`);
        return res.json();
    },

    health()     { return this.get('/api/health'); },
    stats()      { return this.get('/api/stats'); },
    profile()    { return this.get('/api/profile'); },
    matches()    { return this.get('/api/jobs/matches'); },
    apps()       { return this.get('/api/applications'); },

    search(q)    { return this.get(`/api/jobs/search?q=${encodeURIComponent(q)}`); },
    discover()   { return this.post('/api/jobs/discover', {}); },
    apply(ids)   { return this.post('/api/apply', { job_ids: ids }); },
    saveProfile(p) { return this.post('/api/profile', p); },
};

// ─── State ───
let state = {
    view: 'dashboard',
    stats: { jobs_discovered: 0, matches_found: 0, applications_sent: 0 },
    matches: [],
    searchResults: [],
    profile: null,
};

// ─── Rendering ───
function renderJobCard(job, score) {
    const badges = [];
    if (job.remote) badges.push('<span class="badge badge-remote">REMOTE</span>');
    if (job.platform) badges.push(`<span class="badge badge-platform">${job.platform}</span>`);
    if (score) badges.push(`<span class="badge badge-score">${Math.round(score * 100)}% match</span>`);

    return `
        <div class="job-card" data-id="${job.id}">
            <div class="job-header">
                <div>
                    <div class="job-title">${job.title}</div>
                    <div class="job-company">${job.company}</div>
                </div>
                ${score ? `<div class="match-score">${Math.round(score * 100)}%</div>` : ''}
            </div>
            <div class="job-meta">
                ${badges.join('')}
                ${job.location ? `<span class="badge">${job.location}</span>` : ''}
            </div>
        </div>
    `;
}

function renderStats(stats) {
    const el = (id) => document.getElementById(id);
    if (el('statDiscovered')) el('statDiscovered').textContent = stats.jobs_discovered || 0;
    if (el('statMatches'))    el('statMatches').textContent = stats.matches_found || 0;
    if (el('statApplied'))    el('statApplied').textContent = stats.applications_sent || 0;
}

// ─── Init ───
async function init() {
    try {
        const [stats, profile] = await Promise.all([
            api.stats().catch(() => state.stats),
            api.profile().catch(() => null),
        ]);
        state.stats = stats;
        state.profile = profile;
        renderStats(stats);
    } catch (e) {
        console.log('API not available yet:', e.message);
    }
}

// Auto-init when DOM is ready.
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
} else {
    init();
}

// Export for use in HTML.
window.CurlyApp = { api, state, renderJobCard, renderStats, init };
