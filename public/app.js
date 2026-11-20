const $ = selector => document.querySelector(selector);
const $$ = selector => [...document.querySelectorAll(selector)];
const state = {
  me:null, friends:[], requests:{incoming:[],outgoing:[]}, onlineFriendIds:new Set(), presenceLoaded:false,
  onlineCount:0, memberCount:0, activeType:'global', dmFriendId:null,
  friendTab:'friends', messages:[], hasMore:false, loadingMessages:false, messageLoadId:0,
  pendingFile:null, pendingUrl:null, eventSource:null, unread:{}, authMode:'login',
  searchTimer:null, searchQuery:'', searchResults:[],
  profileAvatarFile:null, profileAvatarUrl:null, updateAvailableVersion:null,
  serverMode:'local', serverURL:'', searchError:null
};
const escapeHTML = value => String(value ?? '').replace(/[&<>"']/g, ch => ({ '&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;' }[ch]));
const timeFormat = new Intl.DateTimeFormat('pt-BR',{hour:'2-digit',minute:'2-digit'});
const dateFormat = new Intl.DateTimeFormat('pt-BR',{day:'numeric',month:'long',year:'numeric'});
const timeOf = stamp => timeFormat.format(new Date(stamp));
const avatarClass = nick => {
  let hash=0;
  for(const c of String(nick ?? '')) hash=(hash*31+c.charCodeAt(0))|0;
  return `c${Math.abs(hash)%6}`;
};
const avatarHtml = (nick, {medium=false,large=false,online=null,imageId=null}={}) => {
  const letter = String(nick || '?').slice(0,1).toUpperCase();
  const sizeClass = large?'avatar--large':medium?'avatar--medium':'';
  const image = imageId ? `<img src="/api/images/${encodeURIComponent(imageId)}" alt="Foto de perfil de ${escapeHTML(nick)}" />` : escapeHTML(letter);
  return `<span class="avatar ${avatarClass(nick)} ${sizeClass}">${image}${online===null?'':`<span class="avatar-status ${online?'':'offline'}"></span>`}</span>`;
};
async function api(url, {method='GET', data, file}={}) {
  const config={method,credentials:'same-origin',headers:{}};
  if(data!==undefined){config.headers['Content-Type']='application/json';config.body=JSON.stringify(data);}
  if(file){config.headers['Content-Type']=file.type || 'application/octet-stream';config.body=file;}
  let response;
  try {response=await fetch(url,config);} catch { throw new Error('Sem conexão com o servidor. Confira a internet.'); }
  let body;
  try {body=await response.json();} catch {throw new Error('Resposta inesperada do servidor.');}
  if(!response.ok){const e=new Error(body.error || 'Algo deu errado.');e.status=response.status;throw e;}
  return body;
}
function toast(message,kind='success') {
  const box=document.createElement('div');box.className=`toast ${kind==='error'?'error':''}`;
  box.innerHTML=`<span>${kind==='error'?'!':'✳'}</span><div>${escapeHTML(message)}</div>`;
  $('#toast-region').prepend(box);
  setTimeout(()=>box.remove(),4700);
  while($('#toast-region').children.length>4) $('#toast-region').lastElementChild.remove();
}
function showAuth() {
  state.eventSource?.close(); state.eventSource=null;state.me=null;state.presenceLoaded=false;state.onlineFriendIds=new Set();
  clearProfileAvatarSelection();
  $('#blocking-update-screen').classList.add('hidden');
  $('#loading-screen').classList.add('hidden');$('#app-screen').classList.add('hidden');$('#auth-screen').classList.remove('hidden');
  $('#auth-error').classList.add('hidden');$('#auth-password').value='';
  $('#recovery-panel').classList.add('hidden');$('#auth-form').classList.remove('hidden');
  $('#forgot-password').classList.remove('hidden');$('.auth-tabs').classList.remove('hidden');
}

function clearProfileAvatarSelection(){
  if(state.profileAvatarUrl) URL.revokeObjectURL(state.profileAvatarUrl);
  state.profileAvatarFile=null;
  state.profileAvatarUrl=null;
  const input=$('#profile-avatar-input');
  if(input) input.value='';
}
function renderMyProfileChip(){
  $('#my-nick').textContent='@'+state.me.nickname;
  const avatar=$('#my-avatar');
  const temp=document.createElement('div');
  temp.innerHTML=avatarHtml(state.me.nickname,{medium:true,imageId:state.me.avatarImageId});
  const replacement=temp.firstElementChild;
  replacement.id='my-avatar';
  avatar.replaceWith(replacement);
}
function selectAuthMode(mode) {
  state.authMode=mode;
  const register=mode==='register';
  $('#auth-title').innerHTML=register?'Crie seu próprio espaço <span class="waving">✳</span>':'Bom ter você de volta <span class="waving">✳</span>';
  $('#auth-description').textContent=register?'Escolha um nick, crie sua senha e encontre sua galera.':'Entre na sua conta e continue de onde parou.';
  $('#auth-submit').innerHTML=`${register?'Criar minha conta':'Entrar no DDD'} <span aria-hidden="true">↗</span>`;
  $('#register-help').classList.toggle('hidden',!register);
  $('#register-email-field').classList.toggle('hidden',!register);
  $('#auth-email').required=register;
  $('#forgot-password').classList.toggle('hidden',register);
  $('#recovery-panel').classList.add('hidden');$('#auth-form').classList.remove('hidden');$('.auth-tabs').classList.remove('hidden');
  $('#auth-password').autocomplete=register?'new-password':'current-password';
  $('#auth-error').classList.add('hidden');
  for(const [tab,name] of [['#tab-login','login'],['#tab-register','register']]){
    $(tab).classList.toggle('selected',mode===name);$(tab).setAttribute('aria-selected',String(mode===name));
  }
}
function showRecovery(){
  $('.auth-tabs').classList.add('hidden');
  $('#auth-form').classList.add('hidden');
  $('#forgot-password').classList.add('hidden');
  $('#recovery-panel').classList.remove('hidden');
  $('#recovery-message').textContent='';
  $('#auth-title').textContent='Recuperar minha conta';
  $('#auth-description').textContent='Vamos enviar um código para o e-mail cadastrado na sua conta.';
  $('#recovery-email').value=$('#auth-email').value || '';
  $('#recovery-request-form').classList.remove('hidden');
  $('#recovery-confirm-form').classList.add('hidden');
}
function emailSectionHTML(){
  const u=state.me||{};
  const current=u.email?escapeHTML(u.email):'Nenhum e-mail cadastrado';
  return `<section class="profile-email-settings"><h4>✉ E-mail de recuperação</h4><p class="mail-note">${current}</p>
    <form id="profile-email-request" class="profile-details-form">
      <label for="profile-email-address">${u.email?'Alterar e-mail':'Adicionar e-mail de recuperação'}</label>
      <input type="email" id="profile-email-address" placeholder="voce@exemplo.com" maxlength="254" value="${escapeHTML(u.email||'')}" required />
      <label for="profile-email-password">Senha atual (segurança)</label>
      <input type="password" id="profile-email-password" autocomplete="current-password" minlength="8" maxlength="128" required placeholder="Digite sua senha atual" />
      <button class="primary-button" type="submit">Salvar e-mail</button>
    </form>
    <p class="mail-note">Não precisa confirmar o e-mail. Se esquecer sua senha, enviaremos um código para esse endereço quando o envio estiver disponível. O e-mail não aparece no perfil público.</p>
  </section>`;
}
function setAuthError(text) {const err=$('#auth-error');err.textContent=text;err.classList.remove('hidden');}
async function initApp() {
  $('#loading-screen').classList.add('hidden');$('#auth-screen').classList.add('hidden');$('#app-screen').classList.remove('hidden');
  renderMyProfileChip();
  await Promise.allSettled([refreshFriends(),refreshRequests(),refreshStats()]);
  await openGlobal();
  connectEvents();
}
async function refreshStats(){
  try{const d=await api('/api/stats');state.memberCount=d.registered;state.onlineCount=d.onlineCount;renderStats();}catch{}
}
function renderStats(){ $('#online-stat').textContent=state.onlineCount;$('#member-stat').textContent=state.memberCount; }
async function refreshFriends(){
  const d=await api('/api/friends');state.friends=d.friends;
  renderSidebarFriends();renderOnlineFriends();
  if(!$('#friends-view').classList.contains('hidden')&&state.friendTab==='friends') renderFriendPanel();
}
async function refreshRequests(){
  const d=await api('/api/requests');state.requests=d;
  const n=d.incoming.length;
  $('#requests-badge').textContent=n;$('#requests-badge').classList.toggle('hidden',n===0);
  $('#tab-request-count').textContent=n?String(n):'';
  if(!$('#friends-view').classList.contains('hidden')&&state.friendTab==='requests') renderFriendPanel();
}
function isOnline(id){return state.presenceLoaded ? state.onlineFriendIds.has(id) : state.friends.some(f=>f.id===id&&f.online);}
function renderSidebarFriends(){
  const list=$('#friends-sidebar');
  if(!state.friends.length){list.innerHTML='<div class="placeholder-sidebar">Seus amigos aparecerão aqui ✨</div>';return;}
  list.innerHTML=state.friends.map(f=>`<button type="button" class="friend-side-item ${state.activeType==='dm'&&state.dmFriendId===f.id?'selected':''}" data-dm="${f.id}">${avatarHtml(f.nickname,{online:isOnline(f.id),imageId:f.avatarImageId})}<span>${escapeHTML(f.nickname)}</span>${state.unread[f.id]?`<span class="nav-badge">${Math.min(99,state.unread[f.id])}</span>`:''}</button>`).join('');
}
function renderOnlineFriends(){
  const online=state.friends.filter(f=>isOnline(f.id));
  $('#online-friends-qty').textContent=online.length;
  $('#online-friends').innerHTML=online.length?online.slice(0,9).map(f=>`<button class="right-friend" type="button" data-dm="${f.id}">${avatarHtml(f.nickname,{online:true,imageId:f.avatarImageId})}<span><strong>${escapeHTML(f.nickname)}</strong><small>Disponível para conversar</small></span></button>`).join(''):'<p class="muted small">Seus amigos online vão aparecer aqui.</p>';
}
function setNav(type){
  const isChat=type==='chat';
  $('#chat-view').classList.toggle('hidden',!isChat);
  $('#friends-view').classList.toggle('hidden',isChat);
  $('#nav-global').classList.toggle('active',isChat&&state.activeType==='global');
  $('#nav-friends').classList.toggle('active',!isChat);
  renderSidebarFriends();
  closeMobileSidebar();
}
async function openGlobal(){await openChat('global');}
async function openDM(id){if(!state.friends.some(f=>f.id===id)){toast('Essa pessoa ainda não está na sua lista de amigos.','error');return;}await openChat('dm',id);}
async function openChat(type,id=null){
  state.activeType=type;state.dmFriendId=type==='dm'?id:null;
  if(type==='dm')state.unread[id]=0;
  const friend=state.friends.find(f=>f.id===id);
  const name=type==='global'?'chat-global':friend?.nickname || 'Conversa privada';
  $('#chat-title').textContent=name;
  $('#chat-subtitle').textContent=type==='global'?(state.serverMode==='local'?'Modo local · apenas contas deste computador':'Um lugar para todo mundo se encontrar'):'Conversa privada · só vocês dois';
  $('#chat-header-icon').innerHTML=type==='global'?'#':avatarHtml(name,{imageId:friend?.avatarImageId});
  $('#chat-header-icon').classList.toggle('friends-icon',type==='dm');
  $('#message-input').placeholder=type==='global'?'Mensagem para # chat-global':`Mensagem para @${name}`;
  $('#chat-intro').classList.toggle('hidden',type!=='global');
  $('#chat-intro p').textContent=state.serverMode==='local'?'Você está no modo local: só é possível conversar com as contas criadas neste computador. Para conversar com amigos em outros PCs, conecte-se a um servidor online.':'O chat global conecta todos que estão no mesmo servidor DDD. Diga um oi e entre na conversa!';
  $('#message-list').innerHTML='<div class="message-skeleton">Carregando conversa…</div>';
  $('#load-more').classList.add('hidden');
  state.messages=[]; state.hasMore=false;
  setNav('chat');
  const loadId=++state.messageLoadId;
  try {
    const route=type==='global'?'/api/messages?channel=global':`/api/messages?channel=dm&with=${encodeURIComponent(id)}`;
    const data=await api(route);
    if(loadId!==state.messageLoadId)return;
    state.messages=data.messages;state.hasMore=data.hasMore;
    renderMessages();
  } catch(e) {if(loadId===state.messageLoadId) {$('#message-list').innerHTML='';toast(e.message,'error');}}
  renderSidebarFriends();
}
function renderMessages({scrollToBottom=true}={}){
  const list=$('#message-list');
  if(!state.messages.length){
    list.innerHTML=state.activeType==='global'?'':'<div class="empty-panel"><div class="empty-panel__icon">✉</div><strong>Comece a conversa!</strong><p>Um oi já é um ótimo começo. Suas mensagens ficam visíveis só para vocês dois.</p></div>';
  } else {
    let lastDate='';
    list.innerHTML=state.messages.map(m=>{
      const day=dateFormat.format(new Date(m.createdAt));
      const dateDivider=day===lastDate?'':`<div class="date-divider"><span>${escapeHTML(day)}</span></div>`;
      lastDate=day;
      const mine=m.senderId===state.me.id;
      const text=m.body?`<div class="message-text">${escapeHTML(m.body).replace(/\n/g,'<br>')}</div>`:'';
      const image=m.imageId?`<button type="button" class="message-image-button" data-view-image="${encodeURIComponent(m.imageId)}" data-image-sender="${escapeHTML(m.senderNick)}" title="Ampliar imagem no DDD"><img class="message-image" loading="lazy" src="/api/images/${encodeURIComponent(m.imageId)}" alt="Imagem enviada por ${escapeHTML(m.senderNick)}" /></button>`:'';
      return `${dateDivider}<article class="message-item ${mine?'message-mine':''}">${avatarHtml(m.senderNick,{imageId:m.senderAvatarImageId})}<div class="message-body"><div class="message-topline"><button type="button" class="message-author" data-profile="${m.senderId}">${escapeHTML(m.senderNick)}</button><time class="message-time" datetime="${new Date(m.createdAt).toISOString()}">${timeOf(m.createdAt)}</time></div>${text}${image}</div></article>`;
    }).join('');
  }
  $('#load-more').classList.toggle('hidden',!state.hasMore);
  if(scrollToBottom) requestAnimationFrame(()=>{$('#chat-scroll').scrollTop=$('#chat-scroll').scrollHeight;});
}
function appendMessage(m){
  if(state.messages.some(x=>x.id===m.id))return;
  const current=state.activeType==='global'?m.channel==='global':m.channel==='dm'&&(
    (m.senderId===state.me.id&&m.recipientId===state.dmFriendId)||(m.senderId===state.dmFriendId&&m.recipientId===state.me.id));
  if(current){
    const scroller=$('#chat-scroll');const nearBottom=scroller.scrollHeight-scroller.scrollTop-scroller.clientHeight<130;
    state.messages.push(m);if(state.messages.length>210)state.messages.shift();
    renderMessages({scrollToBottom:nearBottom||m.senderId===state.me.id});
  }else if(m.channel==='dm'&&m.senderId!==state.me.id){
    state.unread[m.senderId]=(state.unread[m.senderId]||0)+1;
    renderSidebarFriends();
    if(document.visibilityState==='visible')toast(`Nova mensagem de @${m.senderNick}`);
  }
}
async function loadMoreMessages(){
  if(!state.hasMore||state.loadingMessages||!state.messages.length)return;
  state.loadingMessages=true;$('#load-more').disabled=true;
  const s=$('#chat-scroll');const oldHeight=s.scrollHeight;
  const base=state.activeType==='global'?'/api/messages?channel=global':`/api/messages?channel=dm&with=${encodeURIComponent(state.dmFriendId)}`;
  const id=state.messageLoadId;
  try{
    const data=await api(`${base}&before=${state.messages[0].createdAt}`);
    if(id!==state.messageLoadId)return;
    state.messages=[...data.messages,...state.messages];state.hasMore=data.hasMore;
    renderMessages({scrollToBottom:false});
    requestAnimationFrame(()=>{s.scrollTop=s.scrollHeight-oldHeight;});
  }catch(e){toast(e.message,'error');}finally{state.loadingMessages=false;$('#load-more').disabled=false;}
}
function openFriends(tab='friends'){
  state.friendTab=tab;setNav('friends');
  $$('.friend-tab').forEach(el=>el.classList.toggle('selected',el.dataset.friendTab===tab));
  renderFriendPanel();
  if(tab==='search')setTimeout(()=>$('#friend-search-input')?.focus(),40);
}
function userRow(nick,id,subtitle,actions,online=null,avatarImageId=null){
  return `<div class="user-row">${avatarHtml(nick,{medium:true,online,imageId:avatarImageId})}<span class="person-info"><strong>${escapeHTML(nick)}</strong><small>${escapeHTML(subtitle)}</small></span><div class="user-actions">${actions}</div></div>`;
}
const actionButton = (label,action,id,style='')=>`<button type="button" class="action-button ${style}" data-action="${action}" data-user-id="${id}">${label}</button>`;
function renderFriendPanel(){
  const panel=$('#friend-panel');
  if(state.friendTab==='friends'){
    panel.innerHTML=state.friends.length?`<div class="section-heading">TODOS OS AMIGOS — ${state.friends.length}</div>${state.friends.map(f=>userRow(f.nickname,f.id,isOnline(f.id)?'Online agora':'Offline',actionButton('Mensagem','dm',f.id,'positive'),isOnline(f.id),f.avatarImageId)).join('')}`:emptyPanel('♧','Seu círculo começa aqui.','Encontre pessoas pelo nick ou converse no chat global para fazer amizades.','Encontrar pessoas');
  }else if(state.friendTab==='requests'){
    const incoming=state.requests.incoming;
    const outgoing=state.requests.outgoing;
    panel.innerHTML=(!incoming.length&&!outgoing.length)?emptyPanel('✉','Nenhum pedido por enquanto.','Quando alguém quiser ser seu amigo, o convite aparecerá aqui.','Encontrar pessoas'):
      `${incoming.length?`<div class="section-heading">PEDIDOS RECEBIDOS — ${incoming.length}</div>${incoming.map(r=>userRow(r.nickname,r.userId,'Quer fazer amizade com você',actionButton('Aceitar','accept',r.id,'positive')+actionButton('Recusar','decline',r.id,'decline'),null,r.avatarImageId)).join('')}`:''}${outgoing.length?`<div class="section-heading section-heading--spaced">PEDIDOS ENVIADOS — ${outgoing.length}</div>${outgoing.map(r=>userRow(r.nickname,r.userId,'Aguardando resposta','<span class="muted small">Pendente</span>',null,r.avatarImageId)).join('')}`:''}`;
  }else{
    const localWarning=state.serverMode==='local'?'<div class="server-search-notice"><strong>Você está no modo local.</strong> A busca só encontra contas deste computador. <button type="button" id="connect-server-hint">Conectar ao servidor online</button></div>':'';
    panel.innerHTML=`${localWarning}<div class="friend-search-wrap"><span>⌕</span><input id="friend-search-input" type="search" maxlength="30" placeholder="Buscar por nick, com ou sem @" value="${escapeHTML(state.searchQuery)}" autocomplete="off" aria-label="Buscar pessoas pelo nick" /></div><p class="friend-help">Busque pelo menos 2 caracteres. A sua própria conta não aparece nos resultados.</p><div id="friend-search-results">${renderSearchResults()}</div>`;
  }
}
function emptyPanel(icon,title,detail,action){return `<div class="empty-panel"><div class="empty-panel__icon">${icon}</div><strong>${escapeHTML(title)}</strong><p>${escapeHTML(detail)}</p><button type="button" class="outline-action" data-action="switch-search">${escapeHTML(action)} ↗</button></div>`;}
function renderSearchResults(){
  if(state.searchQuery.trim().replace(/^@+/, '').length<2)return '<div class="empty-panel"><div class="empty-panel__icon">⌕</div><strong>Quem você quer encontrar?</strong><p>Digite um nick para começar.</p></div>';
  if(state.searchError)return `<div class="empty-panel"><div class="empty-panel__icon">!</div><strong>Não foi possível pesquisar.</strong><p>${escapeHTML(state.searchError)}</p></div>`;
  if(!state.searchResults.length)return `<div class="empty-panel"><div class="empty-panel__icon">✧</div><strong>Ninguém encontrado.</strong><p>${state.serverMode==='local'?'A busca só funciona entre contas deste computador. Conecte-se a um servidor central para encontrar amigos em outros PCs.':'Tente outro nick ou confira a digitação.'}</p></div>`;
  return `<div class="result-heading">RESULTADOS DA BUSCA</div>${state.searchResults.map(u=>{
    const status=u.status;
    const actions=status==='friend'?actionButton('Conversar','dm',u.id,'positive'):
      status==='outgoing'?'<span class="muted small">Pedido enviado ✓</span>':
      status==='incoming'?'<span class="muted small">Enviou convite para você</span>':actionButton('+ Adicionar','add',u.id);
    return userRow(u.nickname,u.id,status==='friend'?'Já são amigos':u.online?'Online agora':'Membro do DDD',actions,u.online,u.avatarImageId);
  }).join('')}`;
}
async function searchUsers(q){
  state.searchQuery=q;
  state.searchError=null;
  const clean=q.trim().replace(/^@+/, '').trim();
  if(clean.length<2){state.searchResults=[];if($('#friend-search-results'))$('#friend-search-results').innerHTML=renderSearchResults();return;}
  try {
    const data=await api(`/api/users?q=${encodeURIComponent(clean)}`);
    if(state.searchQuery!==q)return;
    state.searchResults=data.users;
    if($('#friend-search-results'))$('#friend-search-results').innerHTML=renderSearchResults();
  }catch(e){
    if(state.searchQuery!==q)return;
    state.searchError=e.message;
    state.searchResults=[];
    if($('#friend-search-results'))$('#friend-search-results').innerHTML=renderSearchResults();
  }
}
async function friendAction(action,id){
  try{
    if(action==='dm'){await openDM(id);return;}
    if(action==='add'){
      await api('/api/requests',{method:'POST',data:{userId:id}});
      toast('Pedido de amizade enviado! ✨');
    }
    if(action==='accept'||action==='decline'){
      await api(`/api/requests/${encodeURIComponent(id)}/${action}`,{method:'POST'});
      toast(action==='accept'?'Amizade aceita! Agora vocês podem conversar.':'Pedido recusado.');
    }
    await Promise.allSettled([refreshFriends(),refreshRequests()]);
    if(state.friendTab==='search'&&state.searchQuery.length>=2)await searchUsers(state.searchQuery);
    else if(!$('#friends-view').classList.contains('hidden'))renderFriendPanel();
    if($('#profile-dialog').open)$('#profile-dialog').close();
  }catch(e){toast(e.message,'error');}
}
async function openProfile(id){
  if(!id)return;
  const isSelf=id===state.me.id;
  const dialog=$('#profile-dialog');
  if(!dialog.open)dialog.showModal();
  $('#profile-content').innerHTML='<div class="profile-loading">Carregando perfil…</div>';
  try{
    const data=await api(`/api/profile/${encodeURIComponent(id)}`);
    if(!dialog.open)return;
    const person=data.user;
    const name=person.nickname;
    const friend=state.friends.some(x=>x.id===id);
    const incoming=state.requests.incoming.some(x=>x.userId===id);
    const outgoing=state.requests.outgoing.some(x=>x.userId===id);
    const action=isSelf?'':friend?actionButton('Enviar mensagem','dm',id,'positive'):incoming?'Esta pessoa já enviou um pedido. Veja em Amizades.':outgoing?'Pedido de amizade enviado ✓':actionButton('+ Enviar pedido de amizade','add',id);
    const bio=person.bio?escapeHTML(person.bio):isSelf?'Escreva algo sobre você.':'Nenhuma bio ainda.';
    const status=person.status?escapeHTML(person.status):isSelf?'Defina como você está hoje.':'Sem status definido';
    $('#profile-content').innerHTML=`<div class="profile-main">${avatarHtml(name,{large:true,imageId:person.avatarImageId})}<h3>@${escapeHTML(name)}</h3><div class="profile-info"><strong>Status</strong><p class="profile-status">${status}</p><strong>Sobre mim</strong><p class="profile-bio">${bio}</p></div><div class="profile-actions">${action}</div>${isSelf?`<form id="profile-details-form" class="profile-details-form"><label for="profile-status">Seu status <small>(até 60 caracteres)</small></label><input id="profile-status" name="status" maxlength="60" placeholder="Ex.: Jogando e conversando 🎮" value="${escapeHTML(person.status||'')}" /><label for="profile-bio">Sua bio <small>(até 280 caracteres)</small></label><textarea id="profile-bio" name="bio" maxlength="280" rows="3" placeholder="Conte um pouco sobre você…">${escapeHTML(person.bio||'')}</textarea><button class="primary-button" type="submit">Salvar bio e status</button></form><div class="profile-avatar-tools"><input id="profile-avatar-input" type="file" accept="image/png,image/jpeg,image/webp,image/gif" hidden /><div id="profile-avatar-preview" class="image-preview hidden"></div><div class="profile-avatar-buttons"><button type="button" id="choose-avatar" class="outline-action">Escolher foto</button><button type="button" id="save-avatar" class="primary-button" disabled>Salvar foto</button><button type="button" id="remove-avatar" class="icon-btn text-button">Remover foto</button></div><p class="muted small">PNG, JPG, WEBP ou GIF, até 5 MB.</p></div>${emailSectionHTML()}`:''}</div>`;
  }catch(e){if(dialog.open)$('#profile-content').innerHTML=`<div class="profile-loading">${escapeHTML(e.message)}</div>`;}
}
async function saveProfileDetails(event){
  event.preventDefault();
  const form=event.target;
  const button=form.querySelector('button[type="submit"]');
  button.disabled=true;
  try{
    const data=await api('/api/profile/details',{method:'POST',data:{bio:form.querySelector('#profile-bio').value,status:form.querySelector('#profile-status').value}});
    state.me={...state.me,...data.user};
    toast('Bio e status atualizados!');
    await openProfile(state.me.id);
  }catch(e){toast(e.message,'error');button.disabled=false;}
}
function showImageViewer(id,nick){
  if(!/^[0-9a-f-]{36}$/i.test(id))return;
  const modal=$('#image-viewer');
  $('#image-viewer-photo').src=`/api/images/${encodeURIComponent(id)}`;
  $('#image-viewer-caption').textContent=`Imagem enviada por @${nick}`;
  if(!modal.open)modal.showModal();
}
function closeImageViewer(){
  const modal=$('#image-viewer');
  if(modal.open)modal.close();
  $('#image-viewer-photo').removeAttribute('src');
}
function connectEvents(){
  state.eventSource?.close();
  const es=new EventSource('/api/events');state.eventSource=es;
  es.addEventListener('message:new',evt=>{try{appendMessage(JSON.parse(evt.data));}catch(e){console.warn(e);}});
  es.addEventListener('friends:changed',async()=>{
    await Promise.allSettled([refreshFriends(),refreshRequests()]);
    if(state.friendTab==='search'&&state.searchQuery.length>=2)searchUsers(state.searchQuery);
  });
  es.addEventListener('presence',evt=>{
    const data=JSON.parse(evt.data);state.onlineCount=data.onlineCount;state.onlineFriendIds=new Set(data.friendIds);state.presenceLoaded=true;
    renderStats();renderSidebarFriends();renderOnlineFriends();
    if(state.friendTab==='friends'&&!$('#friends-view').classList.contains('hidden'))renderFriendPanel();
  });
  es.addEventListener('profile:changed',async()=>{
    await Promise.allSettled([refreshFriends(),refreshRequests()]);
    if(state.friendTab==='search'&&state.searchQuery.length>=2)searchUsers(state.searchQuery);
  });
  es.addEventListener('notification',evt=>{const data=JSON.parse(evt.data);toast(data.text||'Você tem uma notificação!');refreshRequests().catch(()=>{});});
  es.addEventListener('ready',()=>refreshStats().catch(()=>{}));
  es.onerror=()=>{ /* EventSource reconecta automaticamente */ };
}
function clearPendingFile(){
  if(state.pendingUrl)URL.revokeObjectURL(state.pendingUrl);
  state.pendingFile=null;state.pendingUrl=null;$('#file-input').value='';$('#image-preview').classList.add('hidden');$('#image-preview').innerHTML='';
}
function selectFile(file){
  if(!file)return;
  if(!['image/png','image/jpeg','image/webp','image/gif'].includes(file.type)){toast('Escolha um PNG, JPG, WEBP ou GIF.','error');return;}
  if(file.size>5*1024*1024){toast('A imagem deve ter até 5 MB.','error');return;}
  clearPendingFile();
  state.pendingFile=file;state.pendingUrl=URL.createObjectURL(file);
  $('#image-preview').innerHTML=`<img src="${state.pendingUrl}" alt="Pré-visualização da imagem" /><span>${escapeHTML(file.name)}</span><button type="button" id="remove-image">Remover ×</button>`;
  $('#image-preview').classList.remove('hidden');
}
async function sendMessage(){
  const input=$('#message-input'),text=input.value.trim();
  if(!text&&!state.pendingFile)return;
  const type=state.activeType,to=state.dmFriendId;
  const send=$('#send-button');send.disabled=true;
  try{
    let imageId;
    if(state.pendingFile){const upload=await api('/api/images',{method:'POST',file:state.pendingFile});imageId=upload.imageId;}
    const result=await api('/api/messages',{method:'POST',data:{channel:type,recipientId:to,text,imageId}});
    input.value='';input.style.height='35px';clearPendingFile();
    appendMessage(result.message);
  }catch(e){toast(e.message,'error');}finally{send.disabled=false;input.focus();}
}
async function openUpdates(){
  closeMobileSidebar();
  $('#update-dialog').showModal();
  $('#update-status').textContent='Verificando configuração local…';
  try{
    const data=await api('/api/update/status');
    $('#update-repo').value=data.repo||'';
    $('#update-form').classList.toggle('hidden',!!data.managed);
    $('#update-installed').textContent=data.version;
    $('#update-status').textContent=data.enabled?'Atualizações ativadas. Quando uma versão assinada for publicada neste repositório, ela será instalada ao abrir o DDD ou quando você clicar em Atualizar agora.':'Ainda não conectado a um repositório de atualizações.';
  }catch(e){$('#update-status').textContent=e.message;}
}
async function saveUpdateRepo(e){
  e.preventDefault();
  const button=$('#update-form button[type="submit"]');button.disabled=true;
  try{
    const data=await api('/api/update/config',{method:'POST',data:{repo:$('#update-repo').value.trim()}});
    $('#update-status').textContent=`Fonte salva: ${data.repo}. As próximas versões publicadas nesse repositório serão instaladas automaticamente ao abrir o DDD.`;
    toast('Configuração das atualizações salva!');
  }catch(e){$('#update-status').textContent=e.message;toast(e.message,'error');}
  finally{button.disabled=false;}
}
async function checkUpdates(){
  const button=$('#check-update');button.disabled=true;
  $('#update-status').textContent='Consultando as versões publicadas…';
  try{
    const data=await api('/api/update/check',{method:'POST'});
    state.updateAvailableVersion=data.available?data.latestVersion:null;
    $('#update-now').classList.toggle('hidden',!data.available);
    $('#update-status').textContent=data.available?`Existe uma nova versão (${data.latestVersion}). Você pode atualizar agora sem baixar outro arquivo.`:`Você está usando a versão mais recente (${data.version}).`;
  }catch(e){$('#update-status').textContent=e.message;}
  finally{button.disabled=false;}
}

async function startUpdateNow(){
  const button=$('#update-now');
  button.disabled=true;
  try{
    const data=await api('/api/update/apply',{method:'POST'});
    if(!data.updated){
      $('#update-status').textContent=data.message || 'Você já está na versão mais recente.';
      $('#update-now').classList.add('hidden');
      return;
    }
    $('#update-dialog').close();
    $('#blocking-update-screen').classList.remove('hidden');
    $('#blocking-update-version').textContent=data.latestVersion || state.updateAvailableVersion || 'nova versão';
    $('#blocking-update-text').textContent='Atualizando o DDD. Durante essa etapa ninguém pode usar o app. Aguarde sem fechar a janela.';
  }catch(e){
    $('#update-status').textContent=e.message;
    toast(e.message,'error');
  } finally {
    button.disabled=false;
  }
}

async function chooseProfileAvatar(file){
  if(!file)return;
  if(!['image/png','image/jpeg','image/webp','image/gif'].includes(file.type)){toast('Escolha um PNG, JPG, WEBP ou GIF para a foto de perfil.','error');return;}
  if(file.size>5*1024*1024){toast('A foto de perfil deve ter até 5 MB.','error');return;}
  clearProfileAvatarSelection();
  state.profileAvatarFile=file;
  state.profileAvatarUrl=URL.createObjectURL(file);
  const preview=$('#profile-avatar-preview');
  preview.innerHTML=`<img src="${state.profileAvatarUrl}" alt="Pré-visualização da nova foto de perfil" /><span>${escapeHTML(file.name)}</span>`;
  preview.classList.remove('hidden');
  $('#save-avatar').disabled=false;
}
async function saveProfileAvatar(){
  if(!state.profileAvatarFile)return;
  const btn=$('#save-avatar');btn.disabled=true;
  try{
    const upload=await api('/api/images',{method:'POST',file:state.profileAvatarFile});
    const data=await api('/api/profile/avatar',{method:'POST',data:{imageId:upload.imageId}});
    state.me={...state.me,...data.user};
    renderMyProfileChip();
    await Promise.allSettled([refreshFriends(),refreshRequests()]);
    renderMessages({scrollToBottom:false});
    openProfile(state.me.id);
    clearProfileAvatarSelection();
    toast('Foto de perfil atualizada!');
  }catch(e){toast(e.message,'error');} finally{btn.disabled=false;}
}
async function removeProfileAvatar(){
  const btn=$('#remove-avatar');btn.disabled=true;
  try{
    const data=await api('/api/profile/avatar',{method:'POST',data:{imageId:''}});
    state.me={...state.me,...data.user};
    renderMyProfileChip();
    await Promise.allSettled([refreshFriends(),refreshRequests()]);
    renderMessages({scrollToBottom:false});
    openProfile(state.me.id);
    clearProfileAvatarSelection();
    toast('Foto de perfil removida.');
  }catch(e){toast(e.message,'error');} finally{btn.disabled=false;}
}
async function refreshServerStatus(){
  const data=await api('/api/server/status');
  state.serverMode=data.mode||'local';
  state.serverURL=data.url||'';
  $('#server-mode-indicator').textContent=state.serverMode==='local'?'LOCAL':'ONLINE';
  $('#server-mode-indicator').classList.toggle('online',state.serverMode!=='local');
  $('#nav-server').classList.toggle('hidden',state.serverMode==='public' || !!data.managed);
  $('#nav-updates').classList.toggle('hidden',state.serverMode==='public');
}
function openServerSettings(){
  closeMobileSidebar();
  $('#server-dialog').showModal();
  $('#server-url').value=state.serverURL;
  $('#server-current-mode').textContent=state.serverMode==='local'?'Modo local: apenas contas deste computador':`Conectado a ${state.serverURL||'um servidor online'}`;
  $('#server-settings-status').textContent=state.serverMode==='local'?'Para conversar com outros computadores, conecte-se a um servidor DDD que já esteja publicado.':'Se mudar de servidor, talvez seja necessário criar uma conta nele. As contas locais não são apagadas.';
  $('#server-local-mode').classList.toggle('hidden',state.serverMode==='local');
}
async function saveServerSettings(e){
  e.preventDefault();
  const btn=$('#server-form button[type="submit"]');btn.disabled=true;
  try{
    await api('/api/server/config',{method:'POST',data:{url:$('#server-url').value.trim()}});
    location.reload();
  }catch(e){$('#server-settings-status').textContent=e.message;btn.disabled=false;}
}
async function useLocalServer(){
  const btn=$('#server-local-mode');btn.disabled=true;
  try{
    await api('/api/server/config',{method:'POST',data:{url:''}});
    location.reload();
  }catch(e){$('#server-settings-status').textContent=e.message;btn.disabled=false;}
}
function openMobileSidebar(){$('#sidebar').classList.add('open');$('#mobile-backdrop').classList.remove('hidden');}
function closeMobileSidebar(){$('#sidebar').classList.remove('open');$('#mobile-backdrop').classList.add('hidden');}
function wireEvents(){
  $('#tab-login').addEventListener('click',()=>selectAuthMode('login'));
  $('#tab-register').addEventListener('click',()=>selectAuthMode('register'));
  $('#forgot-password').addEventListener('click',showRecovery);
  $('#recovery-back').addEventListener('click',()=>selectAuthMode('login'));
  $('#recovery-restart').addEventListener('click',()=>{
    $('#recovery-confirm-form').classList.add('hidden');
    $('#recovery-request-form').classList.remove('hidden');
    $('#recovery-message').textContent='';
  });
  $('#recovery-request-form').addEventListener('submit',async e=>{
    e.preventDefault();
    const btn=e.target.querySelector('button[type=submit]');btn.disabled=true;
    try{
      const data=await api('/api/auth/forgot',{method:'POST',data:{email:$('#recovery-email').value.trim()}});
      $('#recovery-request-form').classList.add('hidden');
      $('#recovery-confirm-form').classList.remove('hidden');
      $('#recovery-message').textContent=data.message;
    }catch(err){$('#recovery-message').textContent=err.message;}finally{btn.disabled=false;}
  });
  $('#recovery-confirm-form').addEventListener('submit',async e=>{
    e.preventDefault();
    const btn=e.target.querySelector('button[type=submit]');btn.disabled=true;
    try{
      const data=await api('/api/auth/reset',{method:'POST',data:{
        email:$('#recovery-email').value.trim(),code:$('#recovery-code').value.trim(),
        password:$('#recovery-password').value
      }});
      selectAuthMode('login');
      $('#auth-description').textContent=data.message;
      toast(data.message);
    }catch(err){$('#recovery-message').textContent=err.message;}finally{btn.disabled=false;}
  });
  $('#toggle-password').addEventListener('click',()=>{
    const input=$('#auth-password');input.type=input.type==='password'?'text':'password';
    $('#toggle-password').setAttribute('aria-label',input.type==='password'?'Mostrar senha':'Ocultar senha');
  });
  $('#auth-form').addEventListener('submit',async e=>{
    e.preventDefault();$('#auth-error').classList.add('hidden');
    const button=$('#auth-submit');button.disabled=true;
    try{
      const nickname=$('#auth-nick').value.trim();const password=$('#auth-password').value;
      const data={nickname,password};
      if(state.authMode==='register')data.email=$('#auth-email').value.trim();
      const d=await api(`/api/auth/${state.authMode==='register'?'register':'login'}`,{method:'POST',data});
      state.me=d.user;await initApp();
      if(state.authMode==='register')toast('Conta criada! E-mail de recuperação cadastrado.');
    }catch(err){setAuthError(err.message);}finally{button.disabled=false;}
  });
  $('.profile-chip').addEventListener('click',e=>{ if(e.target.closest('#logout-btn')) return; openProfile(state.me.id); });
  $('#logout-btn').addEventListener('click',async()=>{
    try{await api('/api/auth/logout',{method:'POST'});}catch(e){toast(e.message,'error');}
    showAuth();
  });
  $('#nav-global').addEventListener('click',()=>openGlobal());
  $('#nav-friends').addEventListener('click',()=>openFriends('friends'));
  $('#nav-updates').addEventListener('click',openUpdates);
  $('#nav-server').addEventListener('click',openServerSettings);
  $('#close-server').addEventListener('click',()=>$('#server-dialog').close());
  $('#server-form').addEventListener('submit',saveServerSettings);
  $('#server-local-mode').addEventListener('click',useLocalServer);
  $('#close-update').addEventListener('click',()=>$('#update-dialog').close());
  $('#update-form').addEventListener('submit',saveUpdateRepo);
  $('#check-update').addEventListener('click',checkUpdates);
  $('#update-now').addEventListener('click',startUpdateNow);
  $('#find-friends-shortcut').addEventListener('click',()=>openFriends('search'));
  $$('.friend-tab').forEach(tab=>tab.addEventListener('click',()=>openFriends(tab.dataset.friendTab)));
  $('#friends-sidebar').addEventListener('click',e=>{const btn=e.target.closest('[data-dm]');if(btn)openDM(btn.dataset.dm);});
  $('#online-friends').addEventListener('click',e=>{const btn=e.target.closest('[data-dm]');if(btn)openDM(btn.dataset.dm);});
  $('#friend-panel').addEventListener('click',async e=>{
    if(e.target.closest('#connect-server-hint')){openServerSettings();return;}
    const button=e.target.closest('[data-action]');if(!button)return;
    if(button.dataset.action==='switch-search'){openFriends('search');return;}
    button.disabled=true;await friendAction(button.dataset.action,button.dataset.userId);button.disabled=false;
  });
  $('#friend-panel').addEventListener('input',e=>{
    if(e.target.id!=='friend-search-input')return;
    clearTimeout(state.searchTimer);const value=e.target.value;
    state.searchQuery=value;
    state.searchTimer=setTimeout(()=>searchUsers(value),350);
  });
  $('#message-list').addEventListener('click',e=>{
    const img=e.target.closest('[data-view-image]');
    if(img){showImageViewer(img.dataset.viewImage,img.dataset.imageSender);return;}
    const p=e.target.closest('[data-profile]');if(p)openProfile(p.dataset.profile);
  });
  $('#close-image-viewer').addEventListener('click',closeImageViewer);
  $('#image-viewer').addEventListener('click',e=>{if(e.target===e.currentTarget)closeImageViewer();});
  $('#image-viewer').addEventListener('close',()=>$('#image-viewer-photo').removeAttribute('src'));
  $('#profile-content').addEventListener('submit',async e=>{
    if(e.target.id==='profile-details-form'){saveProfileDetails(e);return;}
    if(e.target.id==='profile-email-request'){
      e.preventDefault();
      const btn=e.target.querySelector('button[type=submit]');btn.disabled=true;
      try{
        const data=await api('/api/auth/email/update',{method:'POST',data:{
          email:$('#profile-email-address').value.trim(),password:$('#profile-email-password').value
        }});
        toast(data.message);
        const me=await api('/api/me');state.me=me.user;
        await openProfile(state.me.id);
      }catch(err){toast(err.message,'error');btn.disabled=false;}
      return;
    }

  });
  $('#profile-content').addEventListener('click',e=>{
    const b=e.target.closest('[data-action]');if(b)friendAction(b.dataset.action,b.dataset.userId);
    if(e.target.closest('#choose-avatar')) $('#profile-avatar-input')?.click();
    if(e.target.closest('#save-avatar')) saveProfileAvatar();
    if(e.target.closest('#remove-avatar')) removeProfileAvatar();
  });
  $('#profile-content').addEventListener('change',e=>{ if(e.target.id==='profile-avatar-input') chooseProfileAvatar(e.target.files[0]); });
  $('#close-profile').addEventListener('click',()=>$('#profile-dialog').close());
  $('#close-info').addEventListener('click',()=>$('#info-dialog').close());
  $('#chat-info-btn').addEventListener('click',()=>$('#info-dialog').showModal());
  $('#load-more').addEventListener('click',loadMoreMessages);
  $('#attach-button').addEventListener('click',()=>$('#file-input').click());
  $('#file-input').addEventListener('change',e=>selectFile(e.target.files[0]));
  $('#image-preview').addEventListener('click',e=>{if(e.target.closest('#remove-image'))clearPendingFile();});
  $('#message-form').addEventListener('submit',e=>{e.preventDefault();sendMessage();});
  $('#message-input').addEventListener('keydown',e=>{
    if(e.key==='Enter'&&!e.shiftKey){e.preventDefault();$('#message-form').requestSubmit();}
  });
  $('#message-input').addEventListener('input',e=>{
    e.target.style.height='35px';e.target.style.height=Math.min(e.target.scrollHeight,120)+'px';
  });
  $('#chat-scroll').addEventListener('dragover',e=>{if(e.dataTransfer.types.includes('Files'))e.preventDefault();});
  $('#chat-scroll').addEventListener('drop',e=>{e.preventDefault();const f=[...e.dataTransfer.files].find(f=>f.type.startsWith('image/'));if(f)selectFile(f);});
  $('#mobile-menu').addEventListener('click',openMobileSidebar);
  $('#mobile-menu-friends').addEventListener('click',openMobileSidebar);
  $('#mobile-backdrop').addEventListener('click',closeMobileSidebar);
  document.addEventListener('keydown',e=>{if(e.key==='Escape')closeMobileSidebar();});
}
async function bootstrap(){
  wireEvents();
  try{await refreshServerStatus();}catch(e){toast('Não foi possível verificar a conexão com o servidor.','error');}
  try{
    const d=await api('/api/me');
    if(d.user){state.me=d.user;await initApp();}
    else showAuth();
  }catch(e){showAuth();toast('Não foi possível conectar ao servidor.','error');}
}
bootstrap();
