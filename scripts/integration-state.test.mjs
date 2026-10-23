import assert from "node:assert/strict";
import test from "node:test";
import {readFileSync} from "node:fs";
const source = readFileSync(new URL("../internal/httpapi/web/integration-state.js", import.meta.url), "utf8");
const state = await import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);
function configured() {
  state.resetIntegrationDraft();
  state.selectIntegration({productID:"product-a", language:"python", environment:"sandbox"});
  const report = {ready:true, language:"python", config:{launch_product_id:"product-a", environment:"sandbox"}};
  state.rememberIntegrationReport(report);
  assert.equal(state.currentIntegrationReport(), report);
  return report;
}
test("switching product, language or environment never reuses a prior checked report", () => {
  for (const change of [{productID:"product-b"}, {language:"rust"}, {environment:"live"}]) {
    configured(); state.selectIntegration(change); assert.equal(state.currentIntegrationReport(), null);
  }
});
test("profile edits retain matching public configuration but reject stale responses", () => {
  const report = configured();
  state.selectIntegration({projectNotes:"WPF export UI", feature:"pdf.export", platform:"linux", appKind:"web"});
  assert.equal(state.currentIntegrationReport(), report);
  state.selectIntegration({language:"java"});
  state.rememberIntegrationReport(report);
  assert.equal(state.currentIntegrationReport(), null);
});
test("public-key edits can invalidate configuration and account exit clears the draft", () => {
  configured(); state.clearIntegrationReport(); assert.equal(state.currentIntegrationReport(), null);
  state.selectIntegration({projectNotes:"Private project context", feature:"private_feature"});
  state.resetIntegrationDraft();
  assert.equal(state.currentIntegrationReport(), null);
  assert.equal(state.integrationDraft.projectNotes, "");
  assert.equal(state.integrationDraft.productID, "");
  assert.equal(state.integrationDraft.feature, "export");
  assert.doesNotMatch(source, /\b(localStorage|sessionStorage|fetch)\b/);
});
