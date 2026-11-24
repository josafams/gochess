'use strict';
{
  // ================= Comunicação com a engine =================
  // "http": servidor local (gochess gui). "wasm": engine em WebAssembly num Worker.
  function httpBackend() {
    return {
      ready: Promise.resolve(),
      call: req => fetch('api', { method: 'POST', body: JSON.stringify(req) }).then(r => r.json())
    };
  }
  function wasmBackend() {
    const worker = new Worker('worker.js');
    const pending = new Map();
    let nextId = 1, ok, fail;
    const ready = new Promise((a, b) => { ok = a; fail = b; });
    worker.onmessage = e => {
      const d = e.data;
      if (d.type === 'ready') ok();
      else if (d.type === 'error') fail(new Error(d.message));
      else if (d.type === 'resp') { pending.get(d.id)(d.resp); pending.delete(d.id); }
    };
    worker.onerror = e => fail(new Error(e.message || 'falha no worker'));
    return {
      ready,
      call: req => ready.then(() => new Promise(res => {
        const id = nextId++;
        pending.set(id, res);
        worker.postMessage({ id, req });
      }))
    };
  }
  const engine = window.GOCHESS_BACKEND === 'http' ? httpBackend() : wasmBackend();

  // ================= Peças em SVG (desenho próprio) =================
  const SHAPES = {
    p: '<circle cx="22.5" cy="13" r="5.5"/><path d="M18.5 18.5h8l3 14h-14z"/>',
    r: '<path d="M12 9h4.5v3.5h3V9h6v3.5h3V9H33v8H12z"/><path d="M14.5 17h16v15h-16z"/>',
    n: '<path d="M14 33c0-8 3.5-11.5 6.5-15-3 .5-6.5 2.5-9 .5-1.5-3 3.5-7.5 7-9.5l.8-3.3 3 2.4C30 8 33.5 14.5 33.5 23v10z"/><circle class="d" cx="20" cy="12.5" r="1.3"/>',
    b: '<circle cx="22.5" cy="6.5" r="2.3"/><path d="M22.5 9c4.8 3.8 7.2 8.6 5.6 12.8-1.2 2.6-10 2.6-11.2 0C15.3 17.6 17.7 12.8 22.5 9z"/><path class="d" d="M22.5 13.5v5M20 16h5" fill="none"/><path d="M18.5 23h8l1.5 9.5h-11z"/>',
    q: '<path d="M11.5 31 9 14l7 8 2.3-13 4.2 12.5L26.7 9l2.3 13 7-8-2.5 17z"/><circle cx="9" cy="13" r="2"/><circle cx="18.3" cy="8.5" r="2"/><circle cx="26.7" cy="8.5" r="2"/><circle cx="36" cy="13" r="2"/>',
    k: '<path d="M21.4 3h2.2v4h3.4v2.2h-3.4V13h-2.2V9.2H18V7h3.4z"/><path d="M13 31c-2.5-9 3-15.5 9.5-15.5S34.5 22 32 31z"/>'
  };
  const BASE = '<path d="M11 32.5h23a2 2 0 0 1 2 2V37a2 2 0 0 1-2 2H11a2 2 0 0 1-2-2v-2.5a2 2 0 0 1 2-2z"/>';
  function pieceSVG(color, type) {
    const fill = color === 'w' ? '#fbf8f1' : '#2a2620';
    const line = color === 'w' ? '#1d1a15' : '#0b0a08';
    const detail = color === 'w' ? '#1d1a15' : '#d9cfbd';
    return `<svg class="piece" viewBox="0 0 45 45" aria-hidden="true"><g fill="${fill}" stroke="${line}" stroke-width="1.5" stroke-linejoin="round">${SHAPES[type]}${BASE}</g>` +
      `<style>.d{stroke:${detail};fill:${detail}}</style></svg>`;
  }

  // ================= Som (sintetizado) =================
  let audio = null;
  function beep(kind) {
    if (!$('sound').checked) return;
    try {
      audio = audio || new (window.AudioContext || window.webkitAudioContext)();
      const t = audio.currentTime;
      const tone = (freq, start, dur, type, vol) => {
        const o = audio.createOscillator(), g = audio.createGain();
        o.type = type; o.frequency.value = freq;
        g.gain.setValueAtTime(vol, t + start);
        g.gain.exponentialRampToValueAtTime(0.0001, t + start + dur);
        o.connect(g).connect(audio.destination);
        o.start(t + start); o.stop(t + start + dur + 0.02);
      };
      if (kind === 'capture') { tone(180, 0, 0.09, 'square', 0.12); tone(120, 0.02, 0.08, 'triangle', 0.15); }
      else if (kind === 'check') { tone(660, 0, 0.08, 'triangle', 0.15); tone(880, 0.09, 0.1, 'triangle', 0.15); }
      else if (kind === 'end') { tone(523, 0, 0.15, 'sine', 0.15); tone(392, 0.16, 0.25, 'sine', 0.15); }
      else tone(320, 0, 0.06, 'triangle', 0.18);
    } catch (e) { /* sem áudio */ }
  }

  // ================= Estado =================
  const FILES = 'abcdefgh';
  const STORE = 'gochess-game-v2';
  const $ = id => document.getElementById(id);
  const boardEl = $('board');
  const sqName = sq => FILES[sq % 8] + (Math.floor(sq / 8) + 1);
  const sqIndex = name => (name.charCodeAt(1) - 49) * 8 + (name.charCodeAt(0) - 97);
  const colorOf = ch => (ch === ch.toUpperCase() ? 'w' : 'b');
  const other = c => (c === 'w' ? 'b' : 'w');

  const st = {
    startFen: '', moves: [], view: 0, shown: null, live: null,
    mode: 'play', human: 'w', flipped: false, level: 4, tc: '',
    clock: { w: 0, b: 0, running: null, since: 0 }, flag: null,
    selected: null, hint: null, thinking: false, gen: 0, info: null, analysis: null,
    showResult: true, anim: null, cache: new Map()
  };
  const startSide = () => (st.startFen ? st.startFen.split(' ')[1] : 'w');
  const turnAt = n => (n % 2 === 0 ? startSide() : other(startSide()));

  function save() {
    try {
      localStorage.setItem(STORE, JSON.stringify({
        startFen: st.startFen, moves: st.moves, human: st.human, level: st.level,
        flipped: st.flipped, mode: st.mode, tc: st.tc, clock: { w: st.clock.w, b: st.clock.b }
      }));
    } catch (e) {}
  }
  function load() {
    try {
      const s = JSON.parse(localStorage.getItem(STORE) || 'null');
      if (!s || !Array.isArray(s.moves)) return;
      Object.assign(st, {
        startFen: s.startFen || '', moves: s.moves, human: s.human || 'w', level: s.level || 4,
        flipped: !!s.flipped, mode: s.mode === 'analysis' ? 'analysis' : 'play', tc: s.tc || ''
      });
      if (s.clock) { st.clock.w = s.clock.w; st.clock.b = s.clock.b; }
    } catch (e) {}
  }

  // Estado (via API) de um prefixo de lances, com cache.
  async function stateAt(n) {
    const moves = st.moves.slice(0, n);
    const key = st.startFen + '|' + moves.join(' ');
    if (st.cache.has(key)) return st.cache.get(key);
    const g = await engine.call({ action: 'state', fen: st.startFen || undefined, moves });
    if (!g.error) {
      if (st.cache.size > 400) st.cache.clear();
      st.cache.set(key, g);
    }
    return g;
  }

  const isLive = () => st.view === st.moves.length;

  // ================= Fluxo =================
  async function refresh() {
    const gen = st.gen;
    const live = await stateAt(st.moves.length);
    if (gen !== st.gen) return;
    if (live.error) { st.moves = []; st.startFen = ''; st.view = 0; return refresh(); }
    st.live = live;
    st.shown = isLive() ? live : await stateAt(st.view);
    if (gen !== st.gen) return;
    save();
    render();
    if (live.status !== 'playing' && isLive()) stopClock();
    if (st.mode === 'analysis') analyze();
    else if (live.status === 'playing' && !st.flag && live.turn !== st.human && !st.thinking) engineMove();
  }

  function soundFor(g, prevBoard, move) {
    if (!g || !move) return;
    if (g.status !== 'playing') beep('end');
    else if (g.check) beep('check');
    else if (prevBoard && prevBoard[sqIndex(move.slice(2, 4))] !== '.' || (move && g.san.length && g.san[g.san.length - 1].includes('x'))) beep('capture');
    else beep('move');
  }

  async function engineMove() {
    const gen = st.gen;
    st.thinking = true;
    startClock(st.live.turn);
    render();
    const req = { action: 'think', fen: st.startFen || undefined, moves: st.moves.slice(), level: st.level, book: true };
    if (st.tc) {
      const inc = +st.tc.split('+')[1] * 1000;
      req.wtime = Math.max(1, Math.round(currentClock('w')));
      req.btime = Math.max(1, Math.round(currentClock('b')));
      req.inc = inc;
    }
    const r = await engine.call(req);
    st.thinking = false;
    if (gen !== st.gen) { render(); return; }
    if (r.error || !r.best) { render(); return; }
    if (st.flag) { render(); return; }
    if (!r.fromBook) st.info = r;
    applyMove(r.best, true);
  }

  function applyMove(move, animate) {
    const prev = st.shown;
    const mover = turnAt(st.moves.length);
    if (st.mode === 'analysis' && !isLive()) st.moves = st.moves.slice(0, st.view);
    st.moves.push(move);
    st.view = st.moves.length;
    st.selected = null;
    st.hint = null;
    st.analysis = null;
    st.showResult = true;
    st.anim = animate ? { from: sqIndex(move.slice(0, 2)), to: sqIndex(move.slice(2, 4)) } : null;
    if (st.mode === 'play') switchClock(mover);
    st.gen++;
    refresh().then(() => soundFor(st.live, prev && prev.board, move));
  }

  function newGame() {
    const c = $('color').value;
    st.human = c === 'r' ? (Math.random() < 0.5 ? 'w' : 'b') : c;
    st.flipped = st.human === 'b';
    st.level = +$('level').value;
    st.tc = $('tc').value;
    st.startFen = '';
    st.moves = [];
    st.view = 0;
    st.selected = st.hint = st.info = st.analysis = null;
    st.showResult = true;
    st.flag = null;
    resetClock();
    st.gen++;
    engine.call({ action: 'new', moves: [] }).then(refresh);
  }

  function undo() {
    if (st.thinking || !st.moves.length) return;
    if (st.mode === 'analysis') {
      st.moves.pop();
    } else {
      do { st.moves.pop(); } while (st.moves.length && turnAt(st.moves.length) !== st.human);
    }
    st.view = st.moves.length;
    st.selected = st.hint = st.analysis = null;
    st.showResult = true;
    st.flag = null;
    st.gen++;
    refresh();
  }

  async function hint() {
    const g = st.shown;
    if (!g || st.thinking || g.status !== 'playing' || !isLive()) return;
    const gen = st.gen;
    st.thinking = true;
    render();
    const r = await engine.call({ action: 'think', fen: st.startFen || undefined, moves: st.moves.slice(), level: 4 });
    st.thinking = false;
    if (gen === st.gen && r.best) { st.hint = r.best; st.info = r; }
    render();
  }

  // Análise contínua: buscas cada vez mais longas da posição exibida.
  async function analyze() {
    const gen = st.gen;
    const g = st.shown;
    if (!g || g.status !== 'playing') { renderInfo(); return; }
    // Pedaços curtos: a engine fica livre logo para outros pedidos; a tabela de
    // transposição guarda o trabalho, então a profundidade continua subindo.
    for (const t of [250, 600, 1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000]) {
      const r = await engine.call({ action: 'analyze', fen: st.startFen || undefined, moves: st.moves.slice(0, st.view), multipv: 3, timeMs: t });
      if (gen !== st.gen || st.mode !== 'analysis') return;
      if (r.error) return;
      st.analysis = r;
      renderInfo();
    }
  }

  function setMode(mode) {
    st.mode = mode;
    st.selected = st.hint = st.analysis = null;
    stopClock();
    st.gen++;
    render();
    refresh();
  }

  // ================= Relógio =================
  function resetClock() {
    const base = st.tc ? +st.tc.split('+')[0] * 1000 : 0;
    st.clock = { w: base, b: base, running: null, since: 0 };
  }
  function currentClock(c) {
    let v = st.clock[c];
    if (st.clock.running === c) v -= performance.now() - st.clock.since;
    return v;
  }
  function startClock(c) {
    if (!st.tc || st.mode !== 'play' || st.flag || st.clock.running === c) return;
    stopClock();
    st.clock.running = c;
    st.clock.since = performance.now();
  }
  function stopClock() {
    const c = st.clock.running;
    if (c) st.clock[c] -= performance.now() - st.clock.since;
    st.clock.running = null;
  }
  function switchClock(mover) {
    if (!st.tc) return;
    stopClock();
    if (st.moves.length > 1) st.clock[mover] += +st.tc.split('+')[1] * 1000;
  }
  function fmtClock(ms) {
    ms = Math.max(0, ms);
    const s = Math.floor(ms / 1000);
    if (ms < 10000) return `${s}.${Math.floor((ms % 1000) / 100)}`;
    return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
  }
  setInterval(() => {
    if (!st.tc || st.mode !== 'play') return;
    const g = st.live;
    if (g && g.status === 'playing' && !st.flag && isLive()) {
      if (st.clock.running !== g.turn && (st.moves.length > 0 || g.turn !== st.human || st.thinking)) startClock(g.turn);
      for (const c of ['w', 'b']) {
        if (currentClock(c) <= 0 && !st.flag) {
          stopClock();
          st.clock[c] = 0;
          st.flag = c;
          st.gen++;
          st.thinking = false;
          beep('end');
          save();
          render();
        }
      }
    }
    renderClocks();
  }, 100);

  // ================= Interação =================
  function canMove() {
    const g = st.shown;
    if (!g || g.status !== 'playing' || st.flag) return false;
    if (st.mode === 'analysis') return true;
    return isLive() && g.turn === st.human && !st.thinking;
  }

  function tryMove(from, to) {
    const g = st.shown;
    const cands = g.legal.filter(m => m.slice(0, 2) === sqName(from) && m.slice(2, 4) === sqName(to));
    if (cands.length === 1) { applyMove(cands[0], false); return true; }
    if (cands.length > 1) { showPromo(cands); return true; }
    return false;
  }

  function onSquare(sq) {
    if (!canMove()) return;
    const g = st.shown;
    if (st.selected !== null && tryMove(st.selected, sq)) return;
    const pc = g.board[sq];
    const own = pc !== '.' && colorOf(pc) === g.turn && g.legal.some(m => m.slice(0, 2) === sqName(sq));
    st.selected = own && st.selected !== sq ? sq : null;
    render();
  }

  function showPromo(cands) {
    const c = st.shown.turn;
    $('promo-choices').innerHTML = ['q', 'r', 'b', 'n'].map(p =>
      `<button type="button" data-m="${cands[0].slice(0, 4) + p}" aria-label="${{ q: 'Dama', r: 'Torre', b: 'Bispo', n: 'Cavalo' }[p]}">${pieceSVG(c, p)}</button>`
    ).join('');
    $('promo').classList.remove('hidden');
  }

  // Arrastar peças (mouse e toque).
  let drag = null;
  boardEl.addEventListener('pointerdown', e => {
    const b = e.target.closest('.sq');
    if (!b || e.button > 0) return;
    const sq = +b.dataset.sq;
    const pc = st.shown ? st.shown.board[sq] : '.';
    const canDrag = canMove() && pc !== '.' && colorOf(pc) === st.shown.turn;
    drag = { sq, x: e.clientX, y: e.clientY, moved: false, canDrag };
    boardEl.setPointerCapture(e.pointerId);
  });
  boardEl.addEventListener('pointermove', e => {
    if (!drag || !drag.canDrag) return;
    if (!drag.moved && Math.hypot(e.clientX - drag.x, e.clientY - drag.y) < 6) return;
    if (!drag.moved) {
      drag.moved = true;
      st.selected = drag.sq;
      render();
      const pc = st.shown.board[drag.sq];
      $('drag').innerHTML = pieceSVG(colorOf(pc), pc.toLowerCase());
      $('drag').classList.remove('hidden');
      boardEl.querySelector(`[data-sq="${drag.sq}"]`).classList.add('dragging');
    }
    $('drag').style.left = e.clientX + 'px';
    $('drag').style.top = e.clientY + 'px';
  });
  boardEl.addEventListener('pointerup', e => {
    if (!drag) return;
    const d = drag;
    drag = null;
    $('drag').classList.add('hidden');
    if (!d.moved) { onSquare(d.sq); return; }
    const el = document.elementFromPoint(e.clientX, e.clientY);
    const target = el && el.closest && el.closest('.sq');
    if (target && +target.dataset.sq !== d.sq && tryMove(d.sq, +target.dataset.sq)) return;
    st.selected = null;
    render();
  });
  boardEl.addEventListener('pointercancel', () => { drag = null; $('drag').classList.add('hidden'); render(); });
  boardEl.addEventListener('click', e => {
    // Teclado (Enter/Espaço em um botão de casa) gera click sem pointerdown.
    if (e.detail === 0) { const b = e.target.closest('.sq'); if (b) onSquare(+b.dataset.sq); }
  });

  function goTo(n) {
    n = Math.max(0, Math.min(st.moves.length, n));
    if (n === st.view) return;
    st.view = n;
    st.selected = st.hint = st.analysis = null;
    st.anim = null;
    st.gen++;
    refresh();
  }

  // ================= Desenho =================
  const STATUS_TEXT = {
    checkmate: g => (st.mode === 'play' ? (g.winner === st.human ? ['Você venceu!', 'Xeque-mate.'] : ['A engine venceu', 'Xeque-mate.'])
      : [g.winner === 'w' ? 'Brancas vencem' : 'Pretas vencem', 'Xeque-mate.']),
    stalemate: () => ['Empate', 'Afogamento: quem joga não tem lance legal.'],
    fifty: () => ['Empate', 'Regra dos 50 lances.'],
    repetition: () => ['Empate', 'Repetição tripla da posição.'],
    material: () => ['Empate', 'Material insuficiente para dar mate.'],
    time: () => (st.flag === st.human ? ['Seu tempo acabou', 'A engine venceu no relógio.'] : ['Tempo da engine acabou', 'Você venceu no relógio.'])
  };

  function render() {
    const g = st.shown;
    if (!g) return;
    document.body.classList.toggle('analysis', st.mode === 'analysis');
    $('mode-play').classList.toggle('on', st.mode === 'play');
    $('mode-analysis').classList.toggle('on', st.mode === 'analysis');
    document.querySelector('.table').classList.toggle('viewing', !isLive());

    const last = st.view ? st.moves[st.view - 1] : null;
    const lastSq = last ? [sqIndex(last.slice(0, 2)), sqIndex(last.slice(2, 4))] : [];
    const hintSq = st.hint ? [sqIndex(st.hint.slice(0, 2)), sqIndex(st.hint.slice(2, 4))] : [];
    const targets = new Set();
    if (st.selected !== null) {
      const from = sqName(st.selected);
      for (const m of g.legal) if (m.slice(0, 2) === from) targets.add(sqIndex(m.slice(2, 4)));
    }
    const kingSq = g.check ? g.board.indexOf(g.turn === 'w' ? 'K' : 'k') : -1;

    let html = '';
    for (let row = 0; row < 8; row++) {
      for (let col = 0; col < 8; col++) {
        const rank = st.flipped ? row : 7 - row;
        const file = st.flipped ? 7 - col : col;
        const sq = rank * 8 + file;
        const pc = g.board[sq];
        const cls = ['sq'];
        if ((rank + file) % 2 === 0) cls.push('dark');
        if (lastSq.includes(sq)) cls.push('last');
        if (hintSq.includes(sq)) cls.push('hint');
        if (st.selected === sq) cls.push('sel');
        if (sq === kingSq) cls.push('check');
        let inner = '';
        if (col === 0) inner += `<span class="coord rank">${rank + 1}</span>`;
        if (row === 7) inner += `<span class="coord file">${FILES[file]}</span>`;
        if (pc !== '.') inner += pieceSVG(colorOf(pc), pc.toLowerCase());
        if (targets.has(sq)) inner += pc !== '.' ? '<span class="ring"></span>' : '<span class="dot"></span>';
        const label = sqName(sq) + (pc !== '.' ? ' ' + pc : '');
        html += `<button type="button" class="${cls.join(' ')}" data-sq="${sq}" aria-label="${label}">${inner}</button>`;
      }
    }
    boardEl.innerHTML = html;
    document.querySelector('.table').classList.toggle('flipped', st.flipped);
    animate();

    // Status
    const status = $('status');
    status.classList.remove('alert', 'busy');
    const over = st.flag ? 'time' : (st.live && st.live.status !== 'playing' ? st.live.status : null);
    if (!isLive()) {
      status.textContent = `Vendo o lance ${st.view} de ${st.moves.length}`;
      $('result').classList.add('hidden');
    } else if (over) {
      const [title, sub] = STATUS_TEXT[over](st.live);
      status.textContent = `${title.replace(/[.!]$/, '')} · ${sub}`;
      $('result-title').textContent = title;
      $('result-sub').textContent = sub;
      $('result').classList.toggle('hidden', !st.showResult);
    } else {
      $('result').classList.add('hidden');
      if (st.mode === 'analysis') status.textContent = `Análise · vez das ${g.turn === 'w' ? 'brancas' : 'pretas'}`;
      else if (st.thinking && g.turn !== st.human) { status.textContent = 'A engine está pensando'; status.classList.add('busy'); }
      else if (st.thinking) { status.textContent = 'Calculando dica'; status.classList.add('busy'); }
      else if (g.turn === st.human) {
        status.textContent = g.check ? 'Xeque! Sua vez' : 'Sua vez';
        if (g.check) status.classList.add('alert');
      } else status.textContent = 'Vez da engine';
    }

    // Controles
    $('undo').disabled = st.thinking || !st.moves.length;
    $('hint').disabled = !canMove() || st.mode === 'analysis';
    $('nav-first').disabled = $('nav-prev').disabled = st.view === 0;
    $('nav-next').disabled = $('nav-last').disabled = isLive();
    if ($('color').value !== 'r') $('color').value = st.human;
    $('level').value = String(st.level);
    $('tc').value = st.tc;
    for (const id of ['color', 'level', 'tc', 'hint']) $(id).closest('label, button').classList.toggle('play-only', true);

    // Súmula: cada lance é um botão que leva àquela posição.
    const san = st.live ? st.live.san : [];
    const offset = startSide() === 'b' ? 1 : 0;
    let mv = '';
    const cell = i => i < 0 || i >= san.length ? '<span></span>'
      : `<button type="button" data-ply="${i + 1}"${i + 1 === st.view ? ' class="cur"' : ''}>${san[i]}</button>`;
    for (let i = -offset; i < san.length; i += 2) {
      mv += `<li><span class="n">${(i + offset) / 2 + 1}.</span>${cell(i)}${cell(i + 1)}</li>`;
    }
    const list = $('moves');
    list.innerHTML = mv;
    const cur = list.querySelector('.cur');
    if (cur) cur.scrollIntoView({ block: 'nearest' });
    else list.scrollTop = list.scrollHeight;

    renderClocks();
    renderInfo();
  }

  function animate() {
    const a = st.anim;
    st.anim = null;
    if (!a || matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    const fromEl = boardEl.querySelector(`[data-sq="${a.from}"]`);
    const toEl = boardEl.querySelector(`[data-sq="${a.to}"]`);
    const piece = toEl && toEl.querySelector('.piece');
    if (!fromEl || !piece) return;
    const f = fromEl.getBoundingClientRect(), t = toEl.getBoundingClientRect();
    piece.style.transform = `translate(${f.left - t.left}px, ${f.top - t.top}px)`;
    piece.style.zIndex = 5;
    requestAnimationFrame(() => requestAnimationFrame(() => {
      piece.classList.add('anim');
      piece.style.transform = '';
    }));
  }

  function renderClocks() {
    const bottom = st.flipped ? 'b' : 'w', top = other(bottom);
    const name = c => (st.mode === 'analysis' ? (c === 'w' ? 'Brancas' : 'Pretas') : (c === st.human ? 'Você' : 'Engine'));
    $('top-name').textContent = name(top);
    $('bottom-name').textContent = name(bottom);
    for (const [id, c] of [['clock-top', top], ['clock-bottom', bottom]]) {
      const el = $(id);
      if (!st.tc || st.mode !== 'play') { el.textContent = '—'; el.className = 'clock off'; continue; }
      const v = currentClock(c);
      el.textContent = fmtClock(v);
      el.className = 'clock' + (st.clock.running === c ? ' running' : '') + (v < 20000 ? ' low' : '');
    }
  }

  function fmtEval(cp, mate) {
    if (mate) return (mate > 0 ? '+M' : '-M') + Math.abs(mate);
    return (cp > 0 ? '+' : '') + (cp / 100).toFixed(2);
  }

  function renderInfo() {
    const a = st.mode === 'analysis' ? st.analysis : st.info;
    const lines = $('lines');
    const pv = $('engine-pv');
    $('engine-title').textContent = st.mode === 'analysis' ? 'Análise' : 'Engine';
    let pct = 50, text = '0.0';
    if (a && a.depth) {
      if (a.mateIn) { pct = a.mateIn > 0 ? 100 : 0; text = 'M' + Math.abs(a.mateIn); }
      else { pct = 100 / (1 + Math.exp(-a.evalCp / 250)); text = (a.evalCp > 0 ? '+' : '') + (a.evalCp / 100).toFixed(1); }
      const nodes = a.nodes >= 1e6 ? (a.nodes / 1e6).toFixed(1) + ' mi' : Math.round(a.nodes / 1e3) + ' mil';
      $('engine-stats').textContent = `prof. ${a.depth} · ${nodes} nós · ${(a.timeMs / 1000).toFixed(1)} s`;
    } else {
      $('engine-stats').textContent = st.mode === 'analysis' ? 'calculando…' : '—';
    }
    if (st.mode === 'analysis' && a && a.lines) {
      pv.textContent = '';
      lines.innerHTML = a.lines.map((l, i) => {
        const moveNo = Math.floor((st.view + (startSide() === 'b' ? 1 : 0)) / 2) + 1;
        const prefix = st.shown.turn === 'w' ? `${moveNo}.` : `${moveNo}…`;
        return `<li><button type="button" data-line="${i}"><span class="ev">${fmtEval(l.evalCp, l.mateIn)}</span>` +
          `<span class="ln"><b>${prefix} ${l.pv[0] || l.san}</b> ${l.pv.slice(1).join(' ')}</span></button></li>`;
      }).join('');
    } else {
      lines.innerHTML = '';
      if (st.mode === 'analysis') pv.textContent = st.shown && st.shown.status !== 'playing' ? 'Fim de jogo.' : 'Calculando as melhores linhas…';
      else if (a && a.depth) {
        pv.textContent = '';
        const b = document.createElement('b');
        b.textContent = (a.mateIn ? `Mate em ${Math.abs(a.mateIn)}` : text) + ' ';
        pv.append(b, (a.pv || []).join(' '));
      }
    }
    const g = st.live;
    if (isLive() && g && g.status === 'checkmate') { pct = g.winner === 'w' ? 100 : 0; text = g.winner === 'w' ? '1-0' : '0-1'; }
    else if (isLive() && g && g.status !== 'playing') { pct = 50; text = '½'; }
    $('evalfill').style.height = pct + '%';
    $('evaltext').textContent = text;
  }

  function pgn() {
    const g = st.live;
    if (!g) return '';
    const res = st.flag ? (st.flag === 'w' ? '0-1' : '1-0')
      : g.status === 'checkmate' ? (g.winner === 'w' ? '1-0' : '0-1') : g.status === 'playing' ? '*' : '1/2-1/2';
    const names = c => (st.mode === 'analysis' ? '?' : (st.human === c ? 'Humano' : 'GoChess'));
    let tags = `[Event "GoChess"]\n[White "${names('w')}"]\n[Black "${names('b')}"]\n[Result "${res}"]\n`;
    if (st.startFen) tags += `[SetUp "1"]\n[FEN "${st.startFen}"]\n`;
    let body = '';
    const offset = startSide() === 'b' ? 1 : 0;
    g.san.forEach((s, i) => {
      const k = i + offset;
      if (k % 2 === 0) body += `${k / 2 + 1}. `;
      else if (i === 0) body += `${(k - 1) / 2 + 1}... `;
      body += s + ' ';
    });
    return tags + '\n' + body + res;
  }

  async function copyText(btn, text, label) {
    try { await navigator.clipboard.writeText(text); btn.textContent = 'Copiado!'; }
    catch (e) {
      // Sem acesso à área de transferência: mostra o texto para copiar à mão.
      $('pos-text').value = text;
      $('pos-error').textContent = 'Selecione e copie o texto acima.';
      $('posdlg').classList.remove('hidden');
    }
    setTimeout(() => { btn.textContent = label; }, 1500);
  }

  async function loadPosition() {
    const text = $('pos-text').value.trim();
    if (!text) return;
    const err = $('pos-error');
    err.textContent = 'Carregando…';
    const looksFen = /^[pnbrqkPNBRQK1-8]+(\/[pnbrqkPNBRQK1-8]+){7}\s+[wb]\b/.test(text);
    let r;
    if (looksFen) r = await engine.call({ action: 'state', fen: text, moves: [] });
    else r = await engine.call({ action: 'pgn', pgn: text, moves: [] });
    if (r.error) { err.textContent = r.error.replace(/^FEN inválida.*/, 'FEN inválido.'); return; }
    err.textContent = '';
    const START = 'rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1';
    st.startFen = looksFen ? r.fen : (r.startFen && r.startFen !== START ? r.startFen : '');
    st.moves = looksFen ? [] : (r.moves || []);
    st.view = st.moves.length;
    st.selected = st.hint = st.info = st.analysis = null;
    st.flag = null;
    st.showResult = false;
    resetClock();
    $('posdlg').classList.add('hidden');
    if (st.mode === 'play') {
      // Numa posição carregada, você joga com quem tem a vez.
      st.human = r.turn;
      st.flipped = r.turn === 'b';
    }
    st.gen++;
    refresh();
  }

  // ================= Eventos =================
  $('promo-choices').addEventListener('click', e => {
    const b = e.target.closest('button');
    if (!b) return;
    $('promo').classList.add('hidden');
    applyMove(b.dataset.m, false);
  });
  $('promo-cancel').addEventListener('click', () => { $('promo').classList.add('hidden'); st.selected = null; render(); });
  $('new').addEventListener('click', newGame);
  $('result-new').addEventListener('click', newGame);
  $('result-close').addEventListener('click', () => { st.showResult = false; render(); });
  $('undo').addEventListener('click', undo);
  $('hint').addEventListener('click', hint);
  $('flip').addEventListener('click', () => { st.flipped = !st.flipped; save(); render(); });
  $('level').addEventListener('change', () => { st.level = +$('level').value; save(); });
  $('tc').addEventListener('change', () => { if (!st.moves.length) { st.tc = $('tc').value; resetClock(); render(); } });
  $('mode-play').addEventListener('click', () => setMode('play'));
  $('mode-analysis').addEventListener('click', () => setMode('analysis'));
  $('nav-first').addEventListener('click', () => goTo(0));
  $('nav-prev').addEventListener('click', () => goTo(st.view - 1));
  $('nav-next').addEventListener('click', () => goTo(st.view + 1));
  $('nav-last').addEventListener('click', () => goTo(st.moves.length));
  $('moves').addEventListener('click', e => { const b = e.target.closest('button[data-ply]'); if (b) goTo(+b.dataset.ply); });
  $('lines').addEventListener('click', e => {
    const b = e.target.closest('button[data-line]');
    if (b && st.analysis && st.analysis.lines) applyMove(st.analysis.lines[+b.dataset.line].move, true);
  });
  $('copy').addEventListener('click', () => copyText($('copy'), pgn(), 'Copiar PGN'));
  $('copy-fen').addEventListener('click', () => copyText($('copy-fen'), st.shown ? st.shown.fen : '', 'Copiar FEN'));
  $('open-pos').addEventListener('click', () => { $('pos-text').value = ''; $('pos-error').textContent = ''; $('posdlg').classList.remove('hidden'); $('pos-text').focus(); });
  $('pos-cancel').addEventListener('click', () => $('posdlg').classList.add('hidden'));
  $('pos-load').addEventListener('click', loadPosition);
  document.addEventListener('keydown', e => {
    if (e.target.tagName === 'TEXTAREA' || e.target.tagName === 'SELECT') return;
    if (e.key === 'Escape') { st.selected = null; $('promo').classList.add('hidden'); $('posdlg').classList.add('hidden'); render(); }
    if (e.key === 'ArrowLeft') { goTo(st.view - 1); e.preventDefault(); }
    if (e.key === 'ArrowRight') { goTo(st.view + 1); e.preventDefault(); }
    if (e.key === 'Home') goTo(0);
    if (e.key === 'End') goTo(st.moves.length);
  });

  // ================= Início =================
  load();
  if (!st.tc) resetClock();
  st.view = st.moves.length;
  $('level').value = String(st.level);
  $('color').value = st.human;
  $('tc').value = st.tc;
  engine.ready
    .then(refresh)
    .catch(err => {
      const s = $('status');
      s.textContent = 'Não foi possível iniciar a engine: ' + err.message;
      s.classList.add('alert');
    });
}
