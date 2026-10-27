const escape=value=>String(value??"").replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));

export const policyTypes=[
 {key:"terms",title:"服务条款",field:"terms_of_service"},
 {key:"refunds",title:"退款政策",field:"refund_policy"},
 {key:"privacy",title:"隐私告知",field:"privacy_policy"},
];

export function policyURL(value){
 const href=String(value||"").trim();
 if(!href||/[\u0000-\u0020\u007f]/.test(href))return "";
 try{const url=new URL(href,"https://policy.invalid");return ["http:","https:"].includes(url.protocol)&&!url.username&&!url.password?href:""}catch{return ""}
}

function inline(value,depth=0){
 if(depth>3)return escape(value);
 const tokens=/\[([^\]\n]+)\]\(([^\s)]+)\)|\*\*([^\n]+?)\*\*|\*([^*\n]+?)\*/g;
 let html="",position=0;
 for(const match of value.matchAll(tokens)){
  html+=escape(value.slice(position,match.index));
  if(match[1]!==undefined){const href=policyURL(match[2]);html+=href?`<a href="${escape(href)}" target="_blank" rel="noopener noreferrer">${escape(match[1])}</a>`:escape(match[0])}
  else if(match[3]!==undefined)html+=`<strong>${inline(match[3],depth+1)}</strong>`;
  else html+=`<em>${escape(match[4])}</em>`;
  position=match.index+match[0].length;
 }
 return html+escape(value.slice(position));
}

// Policy content is stored as a small Markdown subset. Raw HTML is always text.
export function renderPolicyBody(body,title=""){
 const lines=String(body||"").replace(/\r\n?/g,"\n").split("\n");
 if(title&&lines[0]?.replace(/^#\s+/,"").trim()===title.trim()&&/^#\s+/.test(lines[0]))lines.shift();
 const output=[],paragraph=[],list=[];let listType="";
 const flushParagraph=()=>{if(paragraph.length){output.push(`<p>${paragraph.map(line=>inline(line)).join("<br>")}</p>`);paragraph.length=0}};
 const flushList=()=>{if(list.length){output.push(`<${listType}>${list.map(line=>`<li>${inline(line)}</li>`).join("")}</${listType}>`);list.length=0;listType=""}};
 for(const raw of lines){
  const line=raw.trim(),heading=line.match(/^(#{1,3})\s+(.+)$/),item=line.match(/^([-*]|\d+\.)\s+(.+)$/);
  if(!line){flushParagraph();flushList();continue}
  if(heading){flushParagraph();flushList();output.push(`<h${heading[1].length}>${inline(heading[2])}</h${heading[1].length}>`);continue}
  if(item){flushParagraph();const type=/\d/.test(item[1])?"ol":"ul";if(listType&&listType!==type)flushList();listType=type;list.push(item[2]);continue}
  flushList();paragraph.push(raw);
 }
 flushParagraph();flushList();return output.join("");
}

export function policyEditorValue(root){
 const content=(node,inList=false)=>{
  if(node.nodeType===3)return node.textContent.replace(/\u00a0/g," ");
  if(node.nodeType!==1)return "";
  const tag=node.tagName.toLowerCase();
  if(["script","style","iframe","object","img"].includes(tag))return "";
  if(tag==="br")return "\n";
  const text=[...node.childNodes].map(child=>content(child,inList)).join("");
  if(tag==="b"||tag==="strong")return text.trim()?`**${text}**`:text;
  if(tag==="i"||tag==="em")return text.trim()?`*${text}*`:text;
  if(tag==="a"){const href=policyURL(node.getAttribute("href"));return href?`[${text.replace(/[\[\]]/g,"")}](${href})`:text}
  if(/^h[1-3]$/.test(tag))return inList?text.trim():`${"#".repeat(Number(tag[1]))} ${text.trim()}\n\n`;
  if(tag==="ul"||tag==="ol")return [...node.children].map((li,index)=>`${tag==="ol"?index+1+".":"-"} ${[...li.childNodes].map(child=>content(child,true)).join("").trim()}`).join("\n")+"\n\n";
  if(tag==="p"||tag==="div")return text+(inList?"\n":"\n\n");
  return text;
 };
 return [...root.childNodes].map(node=>content(node)).join("").replace(/\n{3,}/g,"\n\n").trim();
}

export function policyLinks(site,product=null){
 return policyTypes.flatMap(type=>{
  const published=site?.policies?.find(policy=>policy.key===type.key);
  if(published)return [{key:type.key,title:published.title,href:`#/policies/${type.key}`}];
  if(product?.slug&&String(product.content?.[type.field]||"").trim())return [{key:type.key,title:type.title,href:`#/docs/${encodeURIComponent(product.slug)}/${type.key}`}];
  return [];
 });
}

export function policyNotice(site,product,label="继续支付即表示你已阅读"){
 const links=policyLinks(site,product);
 return links.length?`<p class="checkout-policy-note">${escape(label)} ${links.map(link=>`<a href="${escape(link.href)}">${escape(link.title)}</a>`).join("、")}。</p>`:"";
}
