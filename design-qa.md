# Interface design QA

Review date: 2026-10-22. Result: **passed** for the local checks below.

## Reference and conditions

The user selected generated design direction 1: a pale neutral sidebar, white workspace, green active states, horizontal settings categories, and a provider list beside a focused configuration form. The 1487 by 1058 reference is local design material.

The reference and final desktop capture were inspected together, followed by focused comparison of the sidebar, category navigation, provider rows, labels, and actions. Desktop viewport: 1487 by 1058 CSS pixels; the screenshot surface excludes its scrollbar/chrome inset, producing 1472 by 1047 pixels. Mobile viewport: 390 by 844 CSS pixels, with a 375-pixel client area when the scrollbar is present.

The isolated preview had one sample product, a test administrator, and unconfigured payment providers. The mail screenshot shows an unsaved Resend selection to demonstrate its options, not a verified mail connection.

## Comparison

| Area | Final observation |
| --- | --- |
| Layout | The 272-pixel sidebar and two-column payment workspace preserve the selected direction. |
| Typography | Clear heading hierarchy, visible labels, consistent form rows, readable supporting text. |
| Colors | Neutral surfaces, restrained borders, green actions and selection, amber refund explanation. |
| Assets | Current official provider icons replace generated approximations; licensed Feather navigation icons. |
| Copy | Actual provider identifiers replace illustrative mockup values; mail behavior is explained beside its controls. |
| Mobile | Page selector, two-column provider grid, single-column form, internally scrolling category row, reachable save/help actions. |

Separate product/plan navigation and additional language, security, personnel, and data settings preserve existing workflows. These are intentional adaptations of the selected direction.

## Findings resolved

| Priority | Finding | Resolution and evidence |
| --- | --- | --- |
| P1 | Main content overflowed beside the sidebar. | Corrected width and margin; final payment form fits. |
| P1 | Missing shared helpers broke empty lists and product pages. | Restored helpers; affected routes render normally. |
| P2 | Crowded labels/provider controls. | Improved labels, official icons, simplified mobile cards. |
| P2 | Shortcuts opened the default settings category. | Links target the appropriate category; disabled optional mail has an explicit status. |
| P2 | Reverting a form still prompted about changes. | Compare effective form values. Disabled to Resend to Disabled then navigation passed without a prompt. |
| P2 | Footer margins caused mobile horizontal overflow. | Width accounts for margins; client and document scroll width both measured 375 pixels. |

No unresolved P0, P1, or P2 findings were observed in this review scope.

## Interactions checked

- Admin overview, products, plans, orders/invoices, refunds, customers, and settings rendered, including empty states.
- Mobile product editor opened and closed; step navigation and persistent actions remained usable.
- Storefront, product detail, document empty state, and download page rendered. The fixture download destination was not treated as a real installer.
- Customer software, orders, refunds, and security tabs rendered.
- Payment-provider selection and mobile page selection worked. The full mobile Paddle form, save action, refund note, and help were inspected.
- Licentivo's API-key connection form remains under Authorization connection. No real credentials were entered.
- Resend/generic SMTP selection and independent verification/notification options were inspected. Reverting mail settings no longer blocks navigation.
- The final browser error log was empty. Rendered content was also checked because caught errors may not reach the console.
- JavaScript syntax, Go tests, isolated MySQL integration, and Go vet passed; see the verification record for scope.

Local evidence is in the ignored artifacts directory: settings-desktop.png, settings-mobile.png, settings-mobile-full.png, mail-settings.png, product-editor-mobile.png, and storefront captures. These development screenshots are not shipped.

This review does not certify every locale, assistive technology, or device. Real provider checkout, refund settlement, and inbox delivery require external acceptance credentials.

## Policy settings and account menu — 2026-10-27

final result: passed

### Visual truth and evidence

- Approved source: `C:/Users/spf86/.codex/generated_images/01a17769-98d3-7623-8846-83abe958faf5/exec-11238916-f7ec-488c-8c7b-ddc707c82b10.png` (1486 × 1059 pixels).
- Rendered implementation: `D:/GoProject/Licentivo Launch/artifacts/store-policies/desktop-final.png` (1472 × 1105 pixels, full page).
- Full comparison: `D:/GoProject/Licentivo Launch/artifacts/store-policies/comparison-final.png`. Source and implementation were opened together in this image and visually inspected.
- Focused account menu, document selector, labels, and toolbar comparison: `D:/GoProject/Licentivo Launch/artifacts/store-policies/comparison-details.png`.
- Before fixes: `D:/GoProject/Licentivo Launch/artifacts/store-policies/comparison-before.png` and `desktop-before.png` in the same directory.
- Mobile: `mobile.png` (375 × 1349 full page), `mobile-account.png` (375 × 812 viewport surface).
- Actual local service, with empty initial policy bodies: `D:/GoProject/Licentivo Launch/artifacts/store-policies/live-settings.png` (1680 × 915 pixels).
- Published customer page: `public-terms-final.png` in the same directory.

Desktop comparison used a 1487 × 1058 CSS viewport at devicePixelRatio 1. The screenshot surface excludes its scrollbar inset; the full-page capture also includes bottom workspace padding. The comparison uses the first 1059 pixels and pads the excluded inset with white, without stretching either image. Mobile used a 390 × 844 CSS viewport at density 1, then reset the override. The initial mobile full-page capture includes the sticky header's scroll offset; the viewport account capture confirms the actual top layout at scrollY 0. Client width and document scroll width were equal, with no horizontal page overflow.

Both comparison views show the service-terms document, a published version with newer draft changes, two published documents, one privacy draft, and an open account menu. Sample legal text exists only in the isolated QA database. Publication dates reflect the test run. The actual local store has three empty global drafts and no newly published global policy.

### Findings and comparison history

| Priority | Earlier finding | Fix and final evidence |
| --- | --- | --- |
| P1 | Document selector grid stretched its three rows to the editor's height. | Added `align-content:start`; final comparison shows compact rows and the divider beside the editor. |
| P2 | Selector contents and logout were centered by shared button styles. | Explicit start alignment and full-width menu actions; focused comparison confirms consistent icon/text alignment. |
| P2 | Editor text was smaller and action spacing looser than the approved design. | Set 16px editor copy, tightened heading margins, removed inherited action top margin, and gave save/publish consistent 124px minimum widths. Final full comparison keeps actions and footer entry preview in the intended sequence. |
| P2 | Converting headings to lists could retain Markdown punctuation after saving. | Normalize selected list blocks to paragraphs and serialize headings inside list items as item text. Browser round trips now produce clean `ul/li` and `ol/li` for both items. |
| P2 | Customer reading view repeated the document title when the body began with the same heading. | Suppress only a matching leading title in reading/preview views; the public page has one service-terms title and retains the published body. |

The first comparison was blocked by the alignment, type, and spacing findings. The revised capture was compared again against the source, including the focused regions. No actionable P0/P1/P2 visual findings remain in this scope.

### Required fidelity surfaces

| Surface | Assessment |
| --- | --- |
| Fonts and typography | Existing Inter/Segoe UI/Microsoft YaHei stack retained. Page/editor hierarchy, 16px body, readable labels, and control weights preserve the approved direction. The existing app's 14px menu labels are an intentional shared UI constraint. |
| Spacing and layout rhythm | Existing 272px sidebar and workspace padding retained, accounting for the modest horizontal shift from the generated mock. The two-column policy region, compact selector, divider, toolbar, actions, and footer match the intended structure. Mobile uses three horizontal document choices and reachable stacked actions. |
| Colors and tokens | Shared forest-green action and selection tokens, pale green sidebar/selection, neutral borders, amber draft marker, and red logout text. No new theme introduced. |
| Image quality and assets | Existing brand mark and licensed official Feather SVGs remain sharp. There is no photo or illustration in this screen; standard interface icons use the established icon family. No raster mock is embedded as functioning UI. |
| Copy and content | Three policy names, separate draft/public states, publication action, preview explanation, and footer entry explanation are present. Live initial bodies are empty. Example text and timestamps in QA captures are fixture content. |

### Functional checks

- Saved a draft, reloaded it, previewed unsaved changes, and published from the UI.
- Published content remained separate from subsequent drafts; public privacy access returned the unpublished state and the footer omitted its draft link.
- Verified heading formatting and both list formats survive saving; inserted a valid link and rejected a JavaScript URL; preview rendered the current content safely.
- Account menu opened, closed on Escape, and linked to the existing shop and account center. Logout returned the QA browser to the login page. The actual local administrator session was preserved.
- Existing unsaved-change guards are used for document switching, navigation, and logout. IAB auto-handled the native confirmation during automation, so its separate accept/cancel branches were not independently certified here.
- Mobile viewport had no horizontal document overflow; format controls, save/publish, and account menu remained available.
- IAB application console warnings/errors were empty after the flows. The existing Chrome tab contained older extension message-channel errors under its previous orders route; the policy UI loaded and no policy exception was observed.
- 50 frontend tests passed before the final serializer correction; the six policy tests passed again after that correction. JavaScript syntax checks, Go vet for app/httpapi, and OpenAPI schema reference validation passed.
- In isolated MySQL, all non-HTTP packages passed the full Go run; the HTTP run's only failure was a test fixture using a customer session already revoked by commerce tests. Policy checks were moved before that revocation and `TestApplicationFlows` passed on a freshly reset task test database. Other HTTP tests had passed in the full run. Logs: `.cache/store-policies-tests.log` and `.cache/store-policies-http-tests.log`.
- Updated the actual local binary with its existing configuration, checked readiness, retained both original product IDs and brand, and verified zero global policies were published. Task-owned preview and MySQL processes were stopped afterward.

### Implementation checklist

- [x] Compare approved design and rendered screen together, then inspect focused regions.
- [x] Resolve the reported visual and formatting issues and compare revised captures.
- [x] Verify draft/public separation, permissions, validation, restart persistence, and concurrent edit conflicts.
- [x] Check desktop, mobile, account actions, console output, and existing local store state.
- [x] Preserve prior QA history and unrelated workspace changes.

P3 follow-up only: a font-specific optical pass could match generated glyph weight more closely; the current implementation follows the app's existing font and icon system. No remaining issue blocks the approved layout or policy workflow.
