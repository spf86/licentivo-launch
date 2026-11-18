export const escapeHTML=value=>String(value??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
export function documentURL(raw){
 const value=String(raw||'');if(!value||/[\s\\\u0000-\u001f]/.test(value)||value.startsWith('//'))return '';
 if(/^\/api\/v1\/product-document-images\/[a-zA-Z0-9-]+$/.test(value))return value;
 try{const u=new URL(value);return ['http:','https:'].includes(u.protocol)&&!u.username&&!u.password?value:''}catch{return ''}
}
function inline(value){
 const tokens=/!\[([^\]\n]*)\]\(([^\s)]+)\)|\[([^\]\n]+)\]\(([^\s)]+)\)|\*\*([^\n]+?)\*\*|`([^`\n]+)`|\*([^*\n]+)\*/g;let out='',at=0;
 for(const m of value.matchAll(tokens)){out+=escapeHTML(value.slice(at,m.index));if(m[1]!==undefined){const url=documentURL(m[2]);out+=url?`<img loading="lazy" src="${escapeHTML(url)}" alt="${escapeHTML(m[1])}">`:escapeHTML(m[0])}else if(m[3]!==undefined){const url=documentURL(m[4]);out+=url?`<a href="${escapeHTML(url)}" rel="noopener noreferrer">${escapeHTML(m[3])}</a>`:escapeHTML(m[0])}else if(m[5]!==undefined)out+=`<strong>${escapeHTML(m[5])}</strong>`;else if(m[6]!==undefined)out+=`<code>${escapeHTML(m[6])}</code>`;else out+=`<em>${escapeHTML(m[7])}</em>`;at=m.index+m[0].length}return out+escapeHTML(value.slice(at));
}
export function documentHeadings(body){let code=false;const out=[];for(const line of String(body||'').split(/\r?\n/)){if(line.trim().startsWith('```')){code=!code;continue}const m=line.trim().match(/^(#{1,3})\s+(.+)$/);if(!code&&m)out.push({id:`section-${out.length+1}`,title:m[2]})}return out}
export function renderDocument(body){
 const lines=String(body||'').replace(/\r\n?/g,'\n').split('\n');let out='',i=0,heading=0;
 const cells=line=>line.trim().replace(/^\||\|$/g,'').split('|');
 while(i<lines.length){const line=lines[i].trim();if(!line){i++;continue}
  if(line.startsWith('```')){const code=[];i++;while(i<lines.length&&!lines[i].trim().startsWith('```'))code.push(lines[i++]);if(i<lines.length)i++;out+=`<pre><code>${escapeHTML(code.join('\n'))}</code></pre>`;continue}
  const h=line.match(/^(#{1,3})\s+(.+)$/);if(h){heading++;out+=`<h${h[1].length} id="section-${heading}">${inline(h[2])}</h${h[1].length}>`;i++;continue}
  if(i+1<lines.length&&line.includes('|')&&/^\|?[ :\-]+\|[ |:\-]*$/.test(lines[i+1].trim())){out+=`<div class="document-table-wrap"><table><thead><tr>${cells(line).map(c=>`<th>${inline(c.trim())}</th>`).join('')}</tr></thead><tbody>`;i+=2;while(i<lines.length&&lines[i].includes('|')&&lines[i].trim()){out+=`<tr>${cells(lines[i++]).map(c=>`<td>${inline(c.trim())}</td>`).join('')}</tr>`}out+='</tbody></table></div>';continue}
  const list=line.match(/^([-*]|\d+\.)\s+(.+)$/);if(list){const tag=/\d/.test(list[1])?'ol':'ul';out+=`<${tag}>`;while(i<lines.length){const m=lines[i].trim().match(/^([-*]|\d+\.)\s+(.+)$/);if(!m||(/\d/.test(m[1])?'ol':'ul')!==tag)break;out+=`<li>${inline(m[2])}</li>`;i++}out+=`</${tag}>`;continue}
  if(line.startsWith('> ')){out+=`<blockquote>${inline(line.slice(2))}</blockquote>`;i++;continue}
  out+=`<p>${inline(line)}</p>`;i++;
 }return out;
}
// Serialize only our editor vocabulary. Pasted HTML is inserted as text, and
// neither serialization nor public rendering accepts executable markup.
export function editorMarkdown(root){
 const text=node=>{
  if(node.nodeType===3)return node.textContent.replace(/\u00a0/g,' ');if(node.nodeType!==1)return '';
  const tag=node.tagName.toLowerCase(),children=()=>[...node.childNodes].map(text).join('');
  if(['script','style','iframe','object'].includes(tag))return '';
  if(tag==='br')return '\n';if(tag==='img'){const url=documentURL(node.getAttribute('src'));return url?`\n![${(node.getAttribute('alt')||'').replace(/[\[\]\n]/g,'')}](${url})\n`:''}
  if(tag==='pre')return '\n```\n'+node.textContent+'\n```\n\n';
  if(tag==='code')return '`'+node.textContent+'`';
  if(tag==='b'||tag==='strong')return '**'+children()+'**';if(tag==='i'||tag==='em')return '*'+children()+'*';
  if(tag==='a'){const url=documentURL(node.getAttribute('href'));return url?`[${children().replace(/[\[\]]/g,'')}](${url})`:children()}
  if(/^h[1-3]$/.test(tag))return '#'.repeat(Number(tag[1]))+' '+children().trim()+'\n\n';
  if(tag==='ul'||tag==='ol')return [...node.children].map((li,i)=>(tag==='ol'?`${i+1}. `:'- ')+[...li.childNodes].map(text).join('').trim()).join('\n')+'\n\n';
  if(tag==='blockquote')return '> '+children().trim()+'\n\n';
  if(tag==='table'){const rows=[...node.querySelectorAll('tr')].map(row=>[...row.children].map(c=>c.textContent.replace(/\|/g,'/').replace(/\n/g,' ')));if(!rows.length)return '';return rows.map((row,i)=>'| '+row.join(' | ')+' |'+(i===0?'\n| '+row.map(()=>'---').join(' | ')+' |':'')).join('\n')+'\n\n'}
  return children()+(['p','div'].includes(tag)?'\n\n':'');
 };return [...root.childNodes].map(text).join('').replace(/\n{3,}/g,'\n\n').trim();
}
export const publishedArticles=product=>(product?.document_groups||[]).flatMap(group=>group.articles.map(article=>({...article,group:group.title})));
