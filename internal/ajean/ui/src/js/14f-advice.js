// ===== Conseil de preset d'un hook UserPromptSubmit (voir hooks_prompt.go) =====
// Un hook synchrone peut conseiller un autre preset pour la demande qui vient de
// partir. Le conseil s'affiche dans la bande des suites possibles (#cap-bar). Un clic
// confirme, arrête le tour en cours, bascule, puis remet la demande dans le champ :
// elle repart d'un Entrée quand le modèle a rechargé. Rien ne bascule sans ce clic.
let ADVICE_PROMPT = ''; // dernière demande envoyée (renseignée par handleDelta)

function showAdvice(a){
  const bar = document.getElementById('cap-bar');
  if(!bar || !a || !a.preset) return;
  // Déjà sur le preset conseillé (bascule faite depuis, ou conseil rejoué) : rien à dire.
  if(a.preset === ACTIVE_PRESET_ID) return;
  bar.textContent = '';
  const go = document.createElement('button');
  go.type = 'button';
  go.textContent = t('advice.switch_to') + (a.label || a.preset);
  if(a.reason) go.title = a.reason;
  go.onclick = ()=>applyAdvice(a);
  bar.appendChild(go);
  const skip = document.createElement('button');
  skip.type = 'button'; skip.className = 'cap-more';
  skip.textContent = t('advice.ignore');
  skip.onclick = hideSuggestions;
  bar.appendChild(skip);
  bar.classList.add('show');
}

async function applyAdvice(a){
  hideSuggestions();
  let list = [];
  try{ list = await jget('/api/presets'); }catch(e){}
  const i = list.findIndex(p=>p.id === a.preset);
  if(i < 0){ toast(t('advice.unknown')); return; }
  const name = list[i].name;
  // On confirme AVANT d'arrêter : un refus ne doit pas coûter le tour en cours.
  const ok = await askConfirm(t('advice.confirm_prefix') + name + t('advice.confirm_suffix'),
    {title:t('advice.title'), okText:t('advice.ok')});
  if(!ok) return;
  if(busy){
    stopGen();
    await new Promise(r=>setTimeout(r, 800)); // laisse l'arrêt se poser avant de relancer le service
  }
  await switchTo(i+1, name, list[i].id, true);
  const ta = document.getElementById('input');
  if(ta && ADVICE_PROMPT && !ta.value.trim()){
    ta.value = ADVICE_PROMPT;
    autoGrow(ta); syncSendBtn(); ta.focus();
    toast(t('advice.restored'));
  }
}
