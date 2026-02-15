package server

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>curly-chainsaw | Job Aggregator</title>
<style>
  :root {
    --bg: #0f0f0f;
    --surface: #1a1a1a;
    --surface2: #252525;
    --border: #333;
    --text: #e0e0e0;
    --text-dim: #888;
    --accent: #4ade80;
    --accent2: #22d3ee;
    --danger: #f87171;
    --warning: #fbbf24;
  }

  * { margin: 0; padding: 0; box-sizing: border-box; }

  body {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif;
    background: var(--bg);
    color: var(--text);
    min-height: 100vh;
  }

  .header {
    background: var(--surface);
    border-bottom: 1px solid var(--border);
    padding: 1rem 2rem;
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .header h1 {
    font-size: 1.5rem;
    font-weight: 700;
    background: linear-gradient(135deg, var(--accent), var(--accent2));
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
  }

  .header .subtitle {
    color: var(--text-dim);
    font-size: 0.85rem;
  }

  .stats-bar {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
    gap: 1rem;
    padding: 1.5rem 2rem;
  }

  .stat-card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 1.25rem;
  }

  .stat-card .label {
    color: var(--text-dim);
    font-size: 0.8rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .stat-card .value {
    font-size: 2rem;
    font-weight: 700;
    color: var(--accent);
    margin-top: 0.25rem;
  }

  .main {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 1.5rem;
    padding: 0 2rem 2rem;
  }

  @media (max-width: 900px) {
    .main { grid-template-columns: 1fr; }
  }

  .panel {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    overflow: hidden;
  }

  .panel-header {
    padding: 1rem 1.25rem;
    border-bottom: 1px solid var(--border);
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .panel-header h2 {
    font-size: 1rem;
    font-weight: 600;
  }

  .panel-body {
    padding: 1rem 1.25rem;
    max-height: 500px;
    overflow-y: auto;
  }

  .search-box {
    width: 100%;
    grid-column: 1 / -1;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 1.25rem;
    display: flex;
    gap: 0.75rem;
  }

  .search-box input {
    flex: 1;
    background: var(--surface2);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 0.75rem 1rem;
    color: var(--text);
    font-size: 1rem;
    outline: none;
  }

  .search-box input:focus {
    border-color: var(--accent);
  }

  .search-box select {
    background: var(--surface2);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 0.75rem;
    color: var(--text);
    font-size: 0.9rem;
    outline: none;
  }

  .btn {
    background: var(--accent);
    color: #000;
    border: none;
    border-radius: 8px;
    padding: 0.75rem 1.5rem;
    font-weight: 600;
    font-size: 0.9rem;
    cursor: pointer;
    transition: opacity 0.15s;
  }

  .btn:hover { opacity: 0.85; }
  .btn:disabled { opacity: 0.4; cursor: not-allowed; }

  .btn-sm {
    padding: 0.4rem 0.8rem;
    font-size: 0.8rem;
    border-radius: 6px;
  }

  .btn-outline {
    background: transparent;
    border: 1px solid var(--accent);
    color: var(--accent);
  }

  .job-card {
    background: var(--surface2);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 1rem;
    margin-bottom: 0.75rem;
    transition: border-color 0.15s;
  }

  .job-card:hover { border-color: var(--accent); }

  .job-card .title {
    font-weight: 600;
    font-size: 0.95rem;
    margin-bottom: 0.25rem;
  }

  .job-card .company {
    color: var(--accent2);
    font-size: 0.85rem;
  }

  .job-card .meta {
    color: var(--text-dim);
    font-size: 0.8rem;
    margin-top: 0.5rem;
    display: flex;
    gap: 1rem;
    flex-wrap: wrap;
  }

  .job-card .score {
    display: inline-block;
    background: var(--accent);
    color: #000;
    font-size: 0.75rem;
    font-weight: 700;
    padding: 0.15rem 0.5rem;
    border-radius: 4px;
  }

  .job-card .actions {
    margin-top: 0.75rem;
    display: flex;
    gap: 0.5rem;
  }

  .badge {
    display: inline-block;
    font-size: 0.7rem;
    padding: 0.2rem 0.5rem;
    border-radius: 4px;
    font-weight: 600;
  }

  .badge-remote { background: #065f46; color: #6ee7b7; }
  .badge-ashby { background: #312e81; color: #a5b4fc; }
  .badge-greenhouse { background: #064e3b; color: #6ee7b7; }
  .badge-workday { background: #7c2d12; color: #fdba74; }
  .badge-applied { background: #065f46; color: #6ee7b7; }
  .badge-failed { background: #7f1d1d; color: #fca5a5; }

  .empty {
    color: var(--text-dim);
    text-align: center;
    padding: 2rem;
    font-size: 0.9rem;
  }

  .loading {
    text-align: center;
    padding: 2rem;
    color: var(--text-dim);
  }

  .profile-form {
    display: grid;
    gap: 0.75rem;
  }

  .profile-form label {
    font-size: 0.8rem;
    color: var(--text-dim);
  }

  .profile-form input, .profile-form textarea {
    width: 100%;
    background: var(--surface2);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 0.6rem 0.75rem;
    color: var(--text);
    font-size: 0.9rem;
    outline: none;
  }

  .profile-form input:focus, .profile-form textarea:focus {
    border-color: var(--accent);
  }

  .toast {
    position: fixed;
    bottom: 2rem;
    right: 2rem;
    background: var(--accent);
    color: #000;
    padding: 0.75rem 1.25rem;
    border-radius: 8px;
    font-weight: 600;
    font-size: 0.9rem;
    transform: translateY(100px);
    opacity: 0;
    transition: all 0.3s;
    z-index: 1000;
  }

  .toast.show {
    transform: translateY(0);
    opacity: 1;
  }
</style>
</head>
<body>
  <div class="header">
    <div>
      <h1>curly-chainsaw</h1>
      <div class="subtitle">a job for you. a job for me. a job for everyone.</div>
    </div>
    <div style="display: flex; gap: 0.75rem;">
      <button class="btn btn-sm btn-outline" onclick="showPanel('profile')">Profile</button>
      <button class="btn btn-sm btn-outline" onclick="showPanel('recruiter')">Recruiter View</button>
    </div>
  </div>

  <div class="stats-bar">
    <div class="stat-card">
      <div class="label">Jobs Discovered</div>
      <div class="value" id="stat-jobs">--</div>
    </div>
    <div class="stat-card">
      <div class="label">Matches Found</div>
      <div class="value" id="stat-matches">--</div>
    </div>
    <div class="stat-card">
      <div class="label">Applications Sent</div>
      <div class="value" id="stat-apps">--</div>
    </div>
    <div class="stat-card">
      <div class="label">Total Users</div>
      <div class="value" id="stat-users">--</div>
    </div>
  </div>

  <div class="main">
    <div class="search-box">
      <input type="text" id="search-input" placeholder="Search jobs... (e.g. software engineer, backend, Go)" onkeydown="if(event.key==='Enter')searchJobs()">
      <select id="platform-filter">
        <option value="">All Platforms</option>
        <option value="ashby">Ashby</option>
        <option value="greenhouse">Greenhouse</option>
        <option value="workday">Workday</option>
      </select>
      <button class="btn" onclick="searchJobs()">Search</button>
      <button class="btn btn-outline" onclick="discoverJobs()">Auto-Discover</button>
    </div>

    <div class="panel" id="panel-results">
      <div class="panel-header">
        <h2>Search Results</h2>
        <span id="result-count" style="color: var(--text-dim); font-size: 0.85rem;"></span>
      </div>
      <div class="panel-body" id="results-body">
        <div class="empty">Search for jobs to get started</div>
      </div>
    </div>

    <div class="panel" id="panel-matches">
      <div class="panel-header">
        <h2>Your Matches</h2>
        <span id="match-count" style="color: var(--text-dim); font-size: 0.85rem;"></span>
      </div>
      <div class="panel-body" id="matches-body">
        <div class="empty">Run auto-discover to find matches</div>
      </div>
    </div>
  </div>

  <div class="toast" id="toast"></div>

  <script>
    const API = '';
    let currentUser = localStorage.getItem('curly_user_id') || 'default';

    async function fetchAPI(path, opts = {}) {
      const resp = await fetch(API + path, {
        headers: { 'Content-Type': 'application/json', ...opts.headers },
        ...opts,
      });
      return resp.json();
    }

    function toast(msg) {
      const el = document.getElementById('toast');
      el.textContent = msg;
      el.classList.add('show');
      setTimeout(() => el.classList.remove('show'), 3000);
    }

    function platformBadge(p) {
      return '<span class="badge badge-' + p + '">' + p + '</span>';
    }

    function renderJob(job, score) {
      let html = '<div class="job-card">';
      html += '<div class="title">' + esc(job.title) + '</div>';
      html += '<div class="company">' + esc(job.company) + '</div>';
      html += '<div class="meta">';
      html += '<span>' + esc(job.location || 'Location N/A') + '</span>';
      if (job.remote) html += '<span class="badge badge-remote">Remote</span>';
      html += platformBadge(job.platform);
      if (score !== undefined) html += '<span class="score">' + Math.round(score * 100) + '% match</span>';
      html += '</div>';
      html += '<div class="actions">';
      html += '<a href="' + esc(job.url) + '" target="_blank" class="btn btn-sm btn-outline">View</a>';
      html += '<button class="btn btn-sm" onclick="applyToJob(\'' + esc(job.id) + '\')">Apply</button>';
      html += '</div>';
      html += '</div>';
      return html;
    }

    async function searchJobs() {
      const q = document.getElementById('search-input').value.trim();
      if (!q) return;
      const platform = document.getElementById('platform-filter').value;

      document.getElementById('results-body').innerHTML = '<div class="loading">Searching across platforms...</div>';

      let url = '/api/jobs/search?q=' + encodeURIComponent(q);
      if (platform) url += '&platform=' + platform;

      const data = await fetchAPI(url);
      const body = document.getElementById('results-body');
      document.getElementById('result-count').textContent = (data.total || 0) + ' jobs found';

      if (!data.jobs || data.jobs.length === 0) {
        body.innerHTML = '<div class="empty">No jobs found. Try different keywords.</div>';
        return;
      }

      body.innerHTML = data.jobs.map(j => renderJob(j)).join('');
      toast('Found ' + data.total + ' jobs in ' + data.duration);
      loadStats();
    }

    async function discoverJobs() {
      document.getElementById('matches-body').innerHTML = '<div class="loading">Discovering and matching jobs...</div>';

      const keywords = document.getElementById('search-input').value.trim();
      const data = await fetchAPI('/api/jobs/discover', {
        method: 'POST',
        body: JSON.stringify({
          user_id: currentUser,
          keywords: keywords ? keywords.split(',').map(k => k.trim()) : [],
        }),
      });

      const body = document.getElementById('matches-body');
      document.getElementById('match-count').textContent = (data.matched || 0) + ' matches';

      if (!data.top_matches || data.top_matches.length === 0) {
        body.innerHTML = '<div class="empty">No matches found. Update your profile.</div>';
        return;
      }

      body.innerHTML = data.top_matches.map(m => renderJob(m.job, m.score)).join('');
      toast('Discovered ' + data.discovered + ' jobs, ' + data.matched + ' matches!');
      loadStats();
    }

    async function applyToJob(jobId) {
      const data = await fetchAPI('/api/apply', {
        method: 'POST',
        body: JSON.stringify({ user_id: currentUser, job_ids: [jobId] }),
      });
      if (data.applied > 0) {
        toast('Application submitted!');
      } else {
        toast('Application failed. Check the job details.');
      }
      loadStats();
    }

    async function loadStats() {
      const data = await fetchAPI('/api/stats');
      document.getElementById('stat-jobs').textContent = data.total_jobs || 0;
      document.getElementById('stat-apps').textContent = data.total_applications || 0;
      document.getElementById('stat-users').textContent = data.total_users || 0;
    }

    function showPanel(name) {
      if (name === 'profile') {
        toast('Profile editor coming soon - use POST /api/profile for now');
      } else if (name === 'recruiter') {
        toast('Recruiter search: GET /api/recruiter/search?skills=Go,Python');
      }
    }

    function esc(str) {
      if (!str) return '';
      const div = document.createElement('div');
      div.textContent = str;
      return div.innerHTML;
    }

    // Load stats on page load.
    loadStats();
  </script>
</body>
</html>`
