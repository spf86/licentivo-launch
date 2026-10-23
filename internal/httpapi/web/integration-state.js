// Page drafts live only in memory. No project notes or public imports are persisted.
const defaults = Object.freeze({productID:"", language:"go", environment:"sandbox", appKind:"desktop", platform:"windows", feature:"export", projectNotes:""});
export const integrationDraft = {...defaults};
let report = null;
export function resetIntegrationDraft() { Object.assign(integrationDraft, defaults); report = null; }
export function clearIntegrationReport() { report = null; }
export function selectIntegration(values) {
  if (["productID", "language", "environment"].some(key => key in values && values[key] !== integrationDraft[key])) clearIntegrationReport();
  Object.assign(integrationDraft, values);
}
export function rememberIntegrationReport(value) { report = value; }
export function currentIntegrationReport() {
  return report?.language === integrationDraft.language && report.config?.launch_product_id === integrationDraft.productID && report.config?.environment === integrationDraft.environment ? report : null;
}
export function integrationNavigation(active) {
  return `<nav class="integration-nav" aria-label="程序接入方式"><a href="#/admin/integration" ${active === "config" ? 'aria-current="page"' : ""}>接入检查与代码</a><a href="#/admin/integration-ai" ${active === "ai" ? 'aria-current="page"' : ""}>AI 快速接入</a></nav>`;
}
