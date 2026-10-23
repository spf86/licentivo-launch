import {buildIntegrationPrompt, integrationTargets} from "/assets/integration-prompts.js";
import {integrationDraft, selectIntegration, clearIntegrationReport, rememberIntegrationReport, integrationNavigation} from "/assets/integration-state.js";
const esc = value => String(value ?? "").replace(/[&<>"']/g, c => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));

export async function integrationWizard(root, api) {
  const [catalog, deployment] = await Promise.all([api("/admin/catalog"), api("/admin/integration/context")]);
  const products = catalog.products || [];
  if (!products.some(product => product.id === integrationDraft.productID)) selectIntegration({productID: catalog.default_product_id || (products.length === 1 ? products[0].id : "")});
  root.innerHTML = `<div class="section-head"><div><p class="eyebrow">Developer setup</p><h1>程序接入</h1><p class="hint">选择现有软件，自动读取当前站点地址、授权产品映射和签名公钥。支持 9 种语言直接传参初始化。</p></div><a class="button secondary" href="#/admin/plans">管理销售套餐</a></div>${integrationNavigation("config")}
  <div class="integration-layout"><section class="card"><h2>选择接入目标</h2><form id="integration-form">
  <label>软件产品<select name="product_id" required><option value="">请选择软件</option>${products.map(product => `<option value="${esc(product.id)}" ${product.id === integrationDraft.productID ? "selected" : ""}>${esc(product.name)}</option>`).join("")}</select></label>
  <div class="form-row"><label>开发语言<select name="language">${Object.entries(integrationTargets).map(([key, target]) => `<option value="${key}" ${key === integrationDraft.language ? "selected" : ""}>${esc(target.label)}</option>`).join("")}</select></label>
  <label>环境<select name="environment"><option value="sandbox" ${integrationDraft.environment === "sandbox" ? "selected" : ""}>测试 sandbox</option><option value="live" ${integrationDraft.environment === "live" ? "selected" : ""}>正式 live</option></select></label></div>
  <p class="hint">只安装对应语言的 Launch 接入包。无需手填产品 ID、公钥，也无需生成或下载 JSON 配置文件。</p>
  <dl class="integration-identities"><dt>当前商店地址</dt><dd data-origin><code>${esc(deployment.merchant_origin)}</code></dd><dt>官方授权服务</dt><dd><code>${esc(deployment.licensing_origin)}</code></dd></dl>
  <p class="form-error" role="alert" tabindex="-1"></p><button type="submit" ${products.length ? "" : "disabled"}>读取产品信息并生成接入代码</button></form>
  <p><a href="#/admin/integration-ai">使用 AI 快速接入</a>，或按开发文档手动编写代码。</p></section>
  <section class="card integration-result" aria-live="polite"><h2>初始化代码</h2><p class="hint">Launch 会根据所选软件自动填写参数，代码通过 Connect/connect 直接初始化。缺少套餐映射或公钥时会显示待处理事项。</p><p>初始化不等于已授权；真实登录、激活和功能校验仍需验收。</p></section></div>`;
  const form = root.querySelector("#integration-form"), result = root.querySelector(".integration-result");
  let generation = 0;
  const changed = () => { const button=form.querySelector("button[type=submit]"); button.disabled=!products.length; button.textContent="读取产品信息并生成接入代码"; generation++; selectIntegration({productID:form.elements.product_id.value,language:form.elements.language.value,environment:form.elements.environment.value}); clearIntegrationReport(); result.innerHTML = '<h2>初始化代码</h2><p class="hint">选择已变化，请重新读取产品信息。</p>'; };
  form.addEventListener("change", changed);
  form.addEventListener("submit", async event => {
    event.preventDefault(); changed(); const current = ++generation;
    const error = form.querySelector(".form-error"), button = form.querySelector("button[type=submit]"); error.textContent = ""; button.disabled = true; button.textContent = "正在读取并验证…";
    try {
      const report = await api("/admin/integration/check", "POST", {product_id:integrationDraft.productID,language:integrationDraft.language,environment:integrationDraft.environment});
      if (current !== generation || !root.isConnected) return;
      rememberIntegrationReport(report);
      root.querySelector("[data-origin]").innerHTML = `<code>${esc(report.config.merchant_origin)}</code>`;
      const prompt = buildIntegrationPrompt({language:report.language,environment:report.config.environment,merchantOrigin:report.config.merchant_origin,report});
      result.innerHTML = `<h2>${prompt.ready ? "可直接使用的初始化代码" : "需要先完成以下设置"}</h2><ul class="integration-checks">${report.checks.map(check => `<li class="${check.status === "ready" ? "ready" : "failed"}"><strong>${check.status === "ready" ? "✓" : "!"} ${esc(check.message)}</strong></li>`).join("")}</ul>${prompt.ready ? `<p class="hint">地址、产品和公钥已自动填写。将此连接函数加入程序，再接入登录与受保护功能；不要另建配置文件。</p><pre><code>${esc(prompt.code.content)}</code></pre><div class="actions"><button type="button" data-copy-code>复制初始化代码</button><button type="button" class="secondary" data-save-code>保存代码</button><a class="button secondary" href="#/admin/integration-ai">生成英文 AI 提示词</a></div><p data-copy-status role="status"></p><p>完整手动开发流程见接入包内 <code>${esc(integrationTargets[report.language].wrapperPath)}/README.md</code> 和 <code>docs/manual-integration.md</code>。</p>` : '<p><a href="#/admin/plans">检查套餐映射</a> · <a href="#/admin/settings/licensing">检查 Licentivo 连接</a></p>'}`;
      if (!prompt.ready) return;
      result.querySelector("[data-copy-code]").addEventListener("click", async () => {
        const state = result.querySelector("[data-copy-status]");
        try { await navigator.clipboard.writeText(prompt.code.content); state.textContent = "初始化代码已复制。"; }
        catch { state.textContent = "浏览器未允许自动复制，请选中上方代码手动复制。"; }
      });
      result.querySelector("[data-save-code]").addEventListener("click", () => {
        const url = URL.createObjectURL(new Blob([prompt.code.content], {type:"text/plain;charset=utf-8"})); const link = document.createElement("a"); link.href = url; link.download = prompt.code.filename; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000);
      });
    } catch (reason) { if (current === generation && root.isConnected) { error.textContent = reason.message || "读取失败，请稍后重试"; error.focus(); } }
    finally { if (current === generation && root.isConnected) { button.disabled = false; button.textContent = "读取产品信息并生成接入代码"; } }
  });
}
