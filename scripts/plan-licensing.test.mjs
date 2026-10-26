import assert from "node:assert/strict";
import test from "node:test";
import {
  licensingContext,
  planLicenseStatus,
  planPricingMode,
  savePlanWithLicensing,
} from "../internal/httpapi/web/plan-licensing.js";

const product = { id: "product-1", name: "Test product" };
const timedPolicy = {
  id: "policy-timed",
  product_id: product.id,
  name: "Timed policy",
  mode: "timed",
  version: 1,
};
const perpetualPolicy = {
  ...timedPolicy,
  id: "policy-perpetual",
  name: "Perpetual policy",
  mode: "perpetual",
};

function connected({ products = [product], policies = [timedPolicy] } = {}) {
  return licensingContext({
    licensing: [{
      id: "connection-1",
      catalog: {
        products: { items: products },
        policies: { items: policies },
      },
    }],
    mappings: [],
  });
}

test("licensing context handles an installation without a connection", () => {
  const context = licensingContext(null);
  assert.equal(context.licensing, null);
  assert.deepEqual(context.products, []);
  assert.deepEqual(context.policies, []);
  assert.deepEqual(context.selectablePolicies, []);
  assert.deepEqual(context.mappings, []);
});

test("licensing context excludes policies whose products are unavailable", () => {
  const orphan = { ...timedPolicy, id: "orphan", product_id: "deleted-product" };
  const context = connected({ policies: [orphan, timedPolicy] });
  assert.deepEqual(context.policies, [orphan, timedPolicy]);
  assert.deepEqual(context.selectablePolicies, [timedPolicy]);
  assert.equal(context.productName(product.id), product.name);
});

test("custom licensing suppresses saved Licentivo policies and mappings", () => {
  const context = licensingContext({
    licensing_provider: "custom_http",
    licensing: [{catalog: {products: {items: [product]}, policies: {items: [timedPolicy]}}}],
    mappings: [{version_id: "saved", policy_id: timedPolicy.id}],
    external: {default_provider: "custom_http", connections: []},
  });
  assert.equal(context.licensing, null);
  assert.deepEqual(context.selectablePolicies, []);
  assert.deepEqual(context.mappings, []);
  assert.equal(planLicenseStatus(context).kind, "custom");
});

test("licensing status distinguishes disconnected, failed, empty and ready states", async t => {
  const cases = [
    { name: "not connected", context: licensingContext(null), kind: "disconnected" },
    { name: "no connection takes precedence over a refresh error", context: licensingContext({ licensing: [] }), error: "Refresh failed", kind: "disconnected" },
    { name: "refresh failed with cached policies", context: connected(), error: "Refresh failed", kind: "sync-error" },
    { name: "refresh failure takes precedence over an empty catalog", context: connected({ products: [], policies: [] }), error: "Refresh failed", kind: "sync-error" },
    { name: "no remote products", context: connected({ products: [], policies: [] }), kind: "no-products" },
    { name: "products exist without policies", context: connected({ policies: [] }), kind: "no-policies" },
    { name: "only orphaned policies exist", context: connected({ policies: [{ ...timedPolicy, product_id: "deleted-product" }] }), kind: "no-policies" },
    { name: "a usable policy exists", context: connected(), kind: "ready" },
  ];
  for (const { name, context, error, kind } of cases) {
    await t.test(name, () => {
      const status = planLicenseStatus(context, error);
      assert.equal(status.kind, kind);
      assert.equal(typeof status.title, "string");
      assert.ok(status.title.trim(), "every state needs a visible title");
      assert.equal(typeof status.message, "string");
      assert.ok(status.message.trim(), "every state needs actionable explanatory text");
    });
  }
});

test("missing policies preserve an existing draft's pricing type", () => {
  assert.equal(planPricingMode(undefined), "fixed_term");
  assert.equal(planPricingMode(null, "fixed_term"), "fixed_term");
  assert.equal(planPricingMode(null, "perpetual"), "perpetual");
});

test("a selected policy determines fixed-term or perpetual pricing", () => {
  assert.equal(planPricingMode(timedPolicy), "fixed_term");
  assert.equal(planPricingMode(perpetualPolicy), "perpetual");
  assert.equal(planPricingMode(timedPolicy, "perpetual"), "fixed_term");
  assert.equal(planPricingMode(perpetualPolicy, "fixed_term"), "perpetual");
});

test("a disconnected plan can be saved as a draft without calling the mapping API", async () => {
  const payload = { name: "Draft", purchase_type: "fixed_term", amount_minor: 1200 };
  const saved = { ...payload, id: "plan-1", version_id: "version-1" };
  const calls = [];
  const api = async (...args) => {
    calls.push(args);
    assert.equal(args[0], "/admin/plans", "an unconnected draft must not request a mapping");
    return saved;
  };

  const result = await savePlanWithLicensing(api, payload, null);

  assert.deepEqual(calls, [["/admin/plans", "POST", payload]]);
  assert.deepEqual(result.plan, saved);
  assert.equal(result.mapped, false);
  assert.ok(!result.mappingError);
});

test("a selected policy is mapped to the newly saved version after saving", async () => {
  const payload = { id: "plan-1", version: 2, purchase_type: "perpetual" };
  const saved = { ...payload, version: 3, version_id: "new-version-3" };
  const calls = [];
  const api = async (...args) => {
    calls.push(args);
    if (args[0] === "/admin/plans") return saved;
    assert.deepEqual(calls[0], ["/admin/plans", "POST", payload]);
    return { saved: true };
  };

  const result = await savePlanWithLicensing(api, payload, perpetualPolicy);

  assert.deepEqual(calls, [
    ["/admin/plans", "POST", payload],
    ["/admin/connections/mappings", "POST", { version_id: "new-version-3", policy_id: perpetualPolicy.id }],
  ]);
  assert.deepEqual(result.plan, saved);
  assert.equal(result.mapped, true);
  assert.ok(!result.mappingError);
});

test("a failed mapping returns the saved draft and error without saving a second version", async () => {
  const payload = { name: "Draft", purchase_type: "fixed_term" };
  const saved = { ...payload, id: "plan-1", version_id: "version-1" };
  const mappingError = new Error("Licentivo policy has changed; refresh the catalog");
  const calls = [];
  const api = async (...args) => {
    calls.push(args);
    if (args[0] === "/admin/plans") return saved;
    throw mappingError;
  };

  const result = await savePlanWithLicensing(api, payload, timedPolicy);

  assert.deepEqual(result.plan, saved);
  assert.equal(result.mapped, false);
  assert.equal(result.mappingError, mappingError.message);
  assert.deepEqual(calls, [
    ["/admin/plans", "POST", payload],
    ["/admin/connections/mappings", "POST", { version_id: saved.version_id, policy_id: timedPolicy.id }],
  ]);
});

test("a failed draft save propagates the original error and never attempts a mapping", async () => {
  const payload = { name: "Draft" };
  const saveError = new Error("Version conflict");
  const calls = [];
  const api = async (...args) => {
    calls.push(args);
    throw saveError;
  };

  await assert.rejects(savePlanWithLicensing(api, payload, timedPolicy), error => error === saveError);
  assert.deepEqual(calls, [["/admin/plans", "POST", payload]]);
});
