import {integrationTargets, buildIntegrationPrompt} from "/assets/integration-prompts.js";
import {integrationDraft as draft, selectIntegration, currentIntegrationReport, rememberIntegrationReport, clearIntegrationReport, integrationNavigation} from "/assets/integration-state.js";

const esc = value => String(value ?? "").replace(/[&<>"']/g, c => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
const options = (entries, selected) => Object.entries(entries).map(([key, label]) => `<option value="${esc(key)}" ${key === selected ? "selected" : ""}>${esc(label)}</option>`).join("");

export async function integrationAssistant(root, api) {
  const catalog = await api("/admin/catalog");
  if (!(catalog.products || []).some(product => product.id === draft.productID)) selectIntegration({productID:catalog.default_product_id || (catalog.products?.length === 1 ? catalog.products[0].id : "")});
  const products = `<option value="">暂不关联产品</option>${(catalog.products || []).map(product => `<option value="${esc(product.id)}" ${product.id === draft.productID ? "selected" : ""}>${esc(product.name)}</option>`).join("")}`;
  const languages = Object.fromEntries(Object.entries(integrationTargets).map(([key, target]) => [key, target.label]));
  root.innerHTML = `<div class="section-head"><div><p class="eyebrow">AI assisted integration</p><h1>AI 快速接入</h1><p class="hint">选择语言和要保护的功能，生成适合现有项目的英文提示词，交给你使用的 AI 编程工具。</p></div></div>${integrationNavigation("ai")}
    <ol class="integration-steps"><li><strong>描述项目</strong><span>选择语言、平台与功能</span></li><li><strong>准备材料</strong><span>现有工程、接入包与初始化参数</span></li><li><strong>交给 AI</strong><span>复制提示词，再按清单验收</span></li></ol>
    <div class="integration-ai-layout"><section class="card"><h2>你的项目</h2><form id="integration-ai-form">
      <label>开发语言<select name="language">${options(languages, draft.language)}</select></label>
      <div class="form-row"><label>程序类型<select name="app_kind">${options({desktop:"桌面程序", web:"Web 应用（后端接入）", service:"后台服务 / CLI"}, draft.appKind)}</select></label><label>运行平台<select name="platform">${options({windows:"Windows", macos:"macOS", linux:"Linux", "cross-platform":"跨平台"}, draft.platform)}</select></label></div>
      <label>受保护功能名<input name="feature" value="${esc(draft.feature)}" required maxlength="64" pattern="[A-Za-z0-9_.\\-]+" spellcheck="false" aria-describedby="integration-feature-help"></label><p class="hint" id="integration-feature-help">使用 Licentivo 策略中的真实功能名；export 只是示例。支持字母、数字、点、下划线和连字符，最多 64 字符。</p>
      <label>项目说明（选填）<textarea name="project_notes" rows="3" maxlength="600" placeholder="Existing WPF app. Require sign-in before exporting PDFs and preserve the current UI." aria-describedby="integration-notes-help">${esc(draft.projectNotes)}</textarea></label><p class="hint" id="integration-notes-help">请用英文填写框架、登录方式和接入位置，最多 600 字符。不要填写密码、管理密钥或客户授权。</p>
      <details class="integration-context" open><summary>选择现有软件与环境</summary><label>Launch 软件产品<select name="product_id">${products}</select></label><label>订单与授权环境<select name="environment">${options({sandbox:"测试 sandbox", live:"正式 live"}, draft.environment)}</select></label><p class="hint">选择后生成时自动读取该软件已有的授权映射和公钥。无需输入产品 ID 或创建配置文件。</p></details>
      <p class="form-error" role="alert" tabindex="-1"></p><button type="submit">生成英文接入提示词</button>
    </form><div class="integration-ai-config" data-config-status></div></section>
    <section class="card integration-prompt"><div class="section-head"><h2>交给 AI 的提示词</h2><span class="pill orange" data-prompt-state>待生成</span></div><p class="hint" data-prompt-description>可以先生成草稿；选择软件后，自动填入当前网址与产品信息。</p>
      <div class="actions"><button type="button" data-copy-prompt disabled>复制提示词</button><button type="button" class="secondary" data-download-prompt disabled>下载提示词.txt</button></div><p class="hint" data-copy-status role="status" aria-live="polite"></p>
      <label for="integration-ai-prompt">AI 接入提示词</label><textarea id="integration-ai-prompt" readonly rows="20" spellcheck="false" placeholder="选择项目设置后，点击“生成英文接入提示词”。"></textarea>
      <details><summary>怎样交给 AI 才能快速开始？</summary><ol><li>在 AI 编程工具中打开现有程序工程，并提供 Licentivo Launch 接入包。</li><li>粘贴提示词。配置已就绪时，提示词会填入当前站点地址、产品 ID 与公钥，AI 直接传给 Launch 接入包，无需生成配置文件。</li><li>让 AI 读取对应语言的 README 和示例，再接入现有界面和业务流程。</li><li>核对受保护操作确实经过 SDK 校验，完成提示词中的本地测试。支付与实际激活另做测试环境验收。</li></ol></details><p class="hint">提示词在当前浏览器生成；复制或下载由你操作，本页面不会向 AI 服务发送内容。</p>
    </section></div>`;
  const form = root.querySelector("#integration-ai-form"), output = root.querySelector("#integration-ai-prompt");
  const copy = root.querySelector("[data-copy-prompt]"), save = root.querySelector("[data-download-prompt]"), status = root.querySelector("[data-copy-status]");
  const updateConfigStatus = () => {
    root.querySelector("[data-config-status]").innerHTML = currentIntegrationReport()?.ready
      ? '<strong>初始化参数已读取</strong><p class="hint">提示词会包含当前站点地址与对应产品的初始化参数。配置检查不代表支付、激活或功能验收已通过。</p><a href="#/admin/integration">查看或重新检查配置</a>'
      : '<strong>可以先生成接入草稿</strong><p class="hint">请选择已有软件；生成时将自动读取站点地址、授权映射和公钥。缺少设置时仅生成标明待完成项的草稿。</p><a href="#/admin/integration">前往接入检查</a>';
  };
  const readValues = () => {
    selectIntegration({language:form.elements.language.value, productID:form.elements.product_id.value, environment:form.elements.environment.value, appKind:form.elements.app_kind.value, platform:form.elements.platform.value, feature:form.elements.feature.value, projectNotes:form.elements.project_notes.value});
    updateConfigStatus();
  };
  const markChanged = () => {
    generation++; readValues(); output.value = ""; copy.disabled = true; save.disabled = true; status.textContent = "";
    root.querySelector("[data-prompt-state]").textContent = "待生成";
    root.querySelector("[data-prompt-state]").className = "pill orange";
    root.querySelector("[data-prompt-description]").textContent = "设置已变化，请重新生成与当前项目一致的提示词。";
  };
  form.addEventListener("input", markChanged); form.addEventListener("change", markChanged);
  let generation = 0;
  const generate = async () => {
    const error = form.querySelector(".form-error"); error.textContent = ""; readValues();
    const current = ++generation; output.value = ""; copy.disabled = true; save.disabled = true;
    const selected = {productID:draft.productID,language:draft.language,environment:draft.environment};
    try {
      const [context, report] = await Promise.all([api("/admin/integration/context"), selected.productID ? api("/admin/integration/check", "POST", {product_id:selected.productID,language:selected.language,environment:selected.environment}) : Promise.resolve(null)]);
      if (current !== generation || !root.isConnected) return;
      clearIntegrationReport(); if (report) rememberIntegrationReport(report); updateConfigStatus();
      const prompt = buildIntegrationPrompt({language:selected.language, appKind:draft.appKind, platform:draft.platform, feature:draft.feature, projectNotes:draft.projectNotes, report:currentIntegrationReport(), merchantOrigin:context.merchant_origin, environment:selected.environment});
      const issues = report?.checks?.filter(check => check.status !== 'ready') || [];
      if (issues.length) error.textContent = issues.map(check => check.message).join('；');
      output.value = prompt.text; copy.disabled = false; save.disabled = false; status.textContent = "";
      root.querySelector("[data-prompt-state]").textContent = prompt.ready ? "参数已自动填写" : "待配置草稿";
      root.querySelector("[data-prompt-state]").className = `pill ${prompt.ready ? "" : "orange"}`;
      root.querySelector("[data-prompt-description]").textContent = prompt.title;
    } catch (reason) {
      if (current !== generation || !root.isConnected) return;
      output.value = ""; copy.disabled = true; save.disabled = true; error.textContent = reason.message || "请检查项目设置后重试。"; error.focus();
    }
  };
  form.addEventListener("submit", event => { event.preventDefault(); generate(); });
  copy.addEventListener("click", async () => {
    if (copy.disabled || !output.value) return;
    try { await navigator.clipboard.writeText(output.value); status.textContent = "已复制。请在 AI 编程工具中粘贴，并提供现有工程和接入包。"; }
    catch { output.focus(); output.select(); status.textContent = "浏览器未允许自动复制，已选中文本。请按 Ctrl+C（Mac 为 ⌘C）复制。"; }
  });
  save.addEventListener("click", () => {
    if (save.disabled || !output.value) return;
    const url = URL.createObjectURL(new Blob([output.value], {type:"text/plain;charset=utf-8"}));
    const link = document.createElement("a"); link.href = url; link.download = `Licentivo-AI-${draft.language}-integration-prompt.txt`; link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  });
  updateConfigStatus(); generate();
}
