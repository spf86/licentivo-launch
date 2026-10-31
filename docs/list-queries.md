# Complete customer lists

The website uses bounded pages and an explicit total for full history. The original array endpoints remain capped at 100 for compatibility; they must not be used to compute a complete count.

| Endpoint | Access | Filters |
|---|---|---|
| `GET /api/v1/me/orders/search` | Authenticated customer; both rows and total are scoped to that user | `q` (order ID, product or plan), `status`, `environment` |
| `GET /api/v1/admin/users/search` | Admin, operations or support; website staff excluded | `q` (literal email substring), `status` |

Both return `{items, total, limit, offset}`. `limit` defaults to 25 and accepts 1–100. `offset` accepts 0–1,000,000. Search text is at most 254 UTF-8 bytes; `%` and `_` are literal text rather than SQL wildcards. Results sort by creation time descending, then ID descending, so equal timestamps paginate consistently. New writes can move offset boundaries between separate requests; the API does not claim a snapshot across pages.

Customer order status accepts pending, closed, paid, refunding, refunded and reversed. Environment accepts sandbox or live. Customer status accepts active, disabled or closed. Invalid pagination or filters return 400. Public customer management views never include password hashes, session tokens or full license secrets.
