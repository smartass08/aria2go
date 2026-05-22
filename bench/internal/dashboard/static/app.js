const MAX_POINTS = 120;

const charts = {};
const current = { aria2c: [], aria2go: [] };

function initChart(id, label, color1, color2, yFormat) {
  const ctx = document.getElementById(id).getContext('2d');
  charts[id] = new Chart(ctx, {
    type: 'line',
    data: {
      labels: [],
      datasets: [
        { label: 'aria2c',  data: [], borderColor: color1, backgroundColor: color1 + '33', tension: 0.2, pointRadius: 0, borderWidth: 1.5 },
        { label: 'aria2go', data: [], borderColor: color2, backgroundColor: color2 + '33', tension: 0.2, pointRadius: 0, borderWidth: 1.5 },
      ],
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      animation: { duration: 0 },
      scales: {
        x: { display: false },
        y: {
          ticks: { color: '#8b949e', font: { size: 10 }, callback: yFormat || ((v) => v) },
          grid: { color: '#21262d' },
        },
      },
      plugins: { legend: { labels: { color: '#e6edf3', boxWidth: 12, font: { size: 11 } } } },
    },
  });
}

function fmtBytes(b) {
  if (b < 1024) return b + ' B';
  if (b < 1024*1024) return (b/1024).toFixed(1) + ' KiB';
  if (b < 1024*1024*1024) return (b/1048576).toFixed(1) + ' MiB';
  return (b/1073741824).toFixed(2) + ' GiB';
}

function init() {
  initChart('chart-cpu',     'CPU %',    '#f97583', '#79c0ff', (v) => v.toFixed(0) + '%');
  initChart('chart-rss',     'RSS',      '#f97583', '#79c0ff', fmtBytes);
  initChart('chart-threads', 'Threads',  '#f97583', '#79c0ff');
  initChart('chart-fds',     'FDs',      '#f97583', '#79c0ff');

  const src = new EventSource('/events');
  src.onmessage = (e) => {
    try { handle(JSON.parse(e.data)); } catch (err) { console.error(err); }
  };
  src.onerror = () => {
    document.getElementById('status').className = 'status idling';
    document.getElementById('status').textContent = 'disconnected';
  };
}

function handle(ev) {
  if (ev.type === 'meta') {
    document.getElementById('ver-aria2c').textContent = ev.aria2c_version || '-';
    document.getElementById('ver-aria2go').textContent = ev.aria2go_version || '-';
    document.getElementById('host').textContent = ev.host || '-';
  } else if (ev.type === 'scenario_start') {
    document.getElementById('status').className = 'status running';
    document.getElementById('status').textContent = 'running';
    document.getElementById('sc-kind').textContent = ev.kind;
    const tag = document.getElementById('sc-binary');
    tag.textContent = ev.binary;
    tag.className = 'tag ' + ev.binary;
    document.getElementById('sc-progress').textContent = 'started';
    current.aria2c = [];
    current.aria2go = [];
    startTimer(Date.now());
  } else if (ev.type === 'sample') {
    const bucket = ev.binary === 'aria2c' ? current.aria2c : current.aria2go;
    bucket.push(ev);
    if (bucket.length > MAX_POINTS) bucket.shift();
    updateCharts();
  } else if (ev.type === 'scenario_end') {
    document.getElementById('sc-progress').textContent = 'done';
    appendSummary(ev);
  } else if (ev.type === 'finished') {
    document.getElementById('status').className = 'status done';
    document.getElementById('status').textContent = 'done';
    stopTimer();
  }
}

function updateCharts() {
  const labels = [];
  const len = Math.max(current.aria2c.length, current.aria2go.length);
  for (let i = 0; i < len; i++) labels.push(i);

  ['chart-cpu', 'chart-rss', 'chart-threads', 'chart-fds'].forEach((key, idx) => {
    const fields = [
      (s) => s.cpu_user_pct,
      (s) => s.rss_bytes,
      (s) => s.threads,
      (s) => s.open_fds,
    ];
    charts[key].data.labels = labels;
    charts[key].data.datasets[0].data = current.aria2c.map(fields[idx]);
    charts[key].data.datasets[1].data = current.aria2go.map(fields[idx]);
    charts[key].update('none');
  });
}

function appendSummary(ev) {
  const s = ev.summary;
  const tbody = document.querySelector('#summary-table tbody');
  const tr = document.createElement('tr');
  tr.innerHTML = `
    <td>${ev.kind}</td>
    <td>${ev.binary}</td>
    <td>${(ev.duration_ns / 1e9).toFixed(1)}s</td>
    <td>${s.cpu_user_pct.mean.toFixed(1)}%</td>
    <td>${s.cpu_sys_pct.mean.toFixed(1)}%</td>
    <td>${fmtBytes(s.rss_bytes.max)}</td>
    <td>${s.throughput_mbps.toFixed(1)} MB/s</td>
  `;
  tbody.appendChild(tr);
}

let timerStart = null, timerTimer = null;
function startTimer(startMs) {
  timerStart = startMs;
  if (timerTimer) clearInterval(timerTimer);
  timerTimer = setInterval(() => {
    const d = (Date.now() - timerStart) / 1000;
    const mm = Math.floor(d / 60).toString().padStart(2, '0');
    const ss = Math.floor(d % 60).toString().padStart(2, '0');
    document.getElementById('elapsed').textContent = `${mm}:${ss}`;
  }, 500);
}
function stopTimer() { if (timerTimer) clearInterval(timerTimer); }

init();
