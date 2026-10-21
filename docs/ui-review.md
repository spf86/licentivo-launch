# Desktop interface review

Historical review date: 2026-10-17. Scope: embedded sales-plan administration and system settings, using an isolated test database with test-only accounts, plans, and connections.

## Design requirements

Administration primarily targets desktop browsers: a left navigation column, a right workspace, and complete plan tables. Small-screen compatibility is useful but is not the primary acceptance target.

## Problems addressed

The reported layout compressed navigation, headings, and buttons into single-character vertical columns while leaving unused space.

- Content grid columns use `minmax(0, 1fr)` so the workspace shrinks without breaking labels and actions.
- Desktop layouts retain the sidebar and plan table with name, price, version, publication, and edit/publish/archive controls.
- Oversized data scrolls inside its table container instead of widening the entire page.
- Plan editors retain pricing/term fields and use one required Licentivo policy selector. Device, feature, heartbeat, offline, and floating parameters come from the policy.
- System settings groups email, payments, licensing, export, and backup/restore into independent collapsible sections.
- Plan-policy selection is located in the plan editor rather than system settings.
- Customer management excludes staff. Staff creation, roles, and enable/disable controls live in a separate settings section.
- The keyboard skip link focuses main content without changing the SPA route.

## Recorded checks

| Condition | Result |
| --- | --- |
| 1440x900 plan list | Desktop sidebar, complete table, and actions visible; document width matched viewport |
| Width 1024 plan list | Sidebar retained; table/container widths matched; no page overflow |
| Existing plan editor | Opened and inspected; cancelled without submitting plan changes |
| Widths 390/360 | Section picker/cards used; text and actions no longer became vertical |
| System settings | Independent collapsible sections; no plan-policy mapping form |
| Staff management | Separate role/status/action list and staff-creation form |
| Policy selection | One policy selector; duplicate device/offline/heartbeat fields removed |
| JavaScript | Syntax check passed |

Evidence was captured from real browser rendering with test data and retained locally under ignored `.cache/evidence/`. Historical screenshots are not distributed with this repository.

This review does not cover every page/browser, complete accessibility, or real payment/licensing acceptance.
