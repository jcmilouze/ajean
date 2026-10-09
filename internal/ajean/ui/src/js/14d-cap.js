// ===== Cap : trois suites possibles à la fin d'une tâche (voir cap.go) =====
// Le serveur publie {suggestions:[…]} dans le journal de la conversation quand un tour
// avec outils se termine et que l'interrupteur est allumé. On les montre en pastilles ;
// un clic envoie la demande. Elles disparaissent dès qu'un nouveau tour commence, qu'on
// tape quelque chose, ou qu'on clique « Masquer ». Au rechargement le journal est rejoué :
// des suggestions suivies d'un autre message sont masquées par ce message, donc seules
// celles du DERNIER tour restent affichées.
let CAP_ON = false;

function showSuggestions(list){
  const bar = document.getElementById('cap-bar');
  if(!bar || !Array.isArray(list) || !list.length) return;
  // Le champ contient déjà un brouillon : l'utilisateur a pris la main, on ne l'encombre pas.
  if(typeof composerHasContent === 'function' && composerHasContent()) return;
  bar.textContent = '';
  for(const text of list.slice(0, 3)){
    const b = document.createElement('button');
    b.type = 'button';
    b.textContent = text;
    b.onclick = ()=>pickSuggestion(text);
    bar.appendChild(b);
  }
  const other = document.createElement('button');
  other.type = 'button'; other.className = 'cap-more';
  other.textContent = t('cap.other');
  other.onclick = ()=>{ hideSuggestions(); const ta = document.getElementById('input'); if(ta) ta.focus(); };
  bar.appendChild(other);
  const hide = document.createElement('button');
  hide.type = 'button'; hide.className = 'cap-more';
  hide.textContent = t('cap.hide');
  hide.onclick = hideSuggestions;
  bar.appendChild(hide);
  bar.classList.add('show');
}

function hideSuggestions(){
  const bar = document.getElementById('cap-bar');
  if(!bar) return;
  bar.classList.remove('show');
  bar.textContent = '';
}

function pickSuggestion(text){
  hideSuggestions();
  const ta = document.getElementById('input');
  ta.value = text;
  autoGrow(ta); syncSendBtn();
  send();
}

async function loadCap(){
  try{
    const r = await jfetch('/api/cap');
    const j = await r.json().catch(()=>({}));
    if(j.ok) CAP_ON = !!j.on;
  }catch(e){ /* interrupteur indisponible : il reste coupé */ }
}

async function toggleCap(){
  const want = !CAP_ON;
  try{
    const r = await jfetch('/api/cap', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({on:want})});
    const j = await r.json().catch(()=>({}));
    if(!r.ok || !j.ok){ toast(j.error || t('common.error_prefix')); return; }
    CAP_ON = !!j.on;
    if(!CAP_ON) hideSuggestions();
    toast(t(CAP_ON ? 'cap.on' : 'cap.off'));
  }catch(e){ toast(t('common.error_prefix') + (e.message || e)); }
}

loadCap();
