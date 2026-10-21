# Product editor design QA

## Reference pattern

Centered desktop modal with a title/description header, numbered left navigation, one focused form step, and a persistent progress/action footer. The original user-supplied reference image was local development material and is not included in the public repository.

## Implementation reviewed

- Local product administration page, 2026-10-19.
- Desktop viewport: 1664 pixels wide.
- Steps inspected: basic information and platform/installers.
- The current editor contains five steps; the historical visual review preceded the product-documentation step.

## Comparison

| Area | Recorded result |
| --- | --- |
| Modal placement/scale | Centered, near full height, dimmed backdrop |
| Header | Product-specific title, instruction, state badge, and close action |
| Navigation | Numbered steps, active state, fixed desktop column |
| Content | One focused step with internal scrolling |
| Footer | Visible progress, cancel, and save |
| Platforms | Windows, macOS, iOS, Android independently enabled in a two-column grid |
| Responsive layout | Platform grid and step navigation collapse at the existing breakpoint |
| Accessibility | Dialog semantics, labels, keyboard-focusable navigation, required-state checks, visible focus |

## Functional checks

Enabling Windows reveals/enables its version and source fields. Selecting an external HTTPS source replaces the upload chooser with a URL field. Other platforms remain independently disabled until selected. The public download route supported the legacy universal download and separate platform cards after platform data was saved.

The browser reported no local warnings/errors. JavaScript syntax and the existing Go tests passed at the review point.

No P0/P1 blockers or actionable P2 findings were recorded in that limited comparison. This is historical visual evidence, not a complete product or accessibility acceptance certificate.
