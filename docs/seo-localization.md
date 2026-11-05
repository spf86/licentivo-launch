# SEO for each language and market

Open **System settings → SEO** or a product's **Search optimization** screen. Choose the content language before editing. Each language has separate homepage/product keywords, titles, descriptions, product title templates, fallback descriptions, and default keywords. The keyword field provides examples in that language; enter phrases used by the customers and countries you actually target. A language can serve multiple countries, so the application does not assume every English or Spanish visitor belongs to one country.

The public HTML now reads the current product/page's keywords first, followed by defaults for that language. Legacy defaults remain available to the original source language; other languages do not inherit its keywords, fallback copy, or title template. Saving one language preserves the other language records. Product content translations and SEO overrides remain independent. Previews use published translated product/brand content when available and check whether the primary phrase appears naturally in the title and description.

Localized titles, descriptions, HTML content, canonical URLs, and the existing `hreflang` links are rendered on the server. Incomplete translated content stays `noindex` and points to the source version until translation is ready. [Google's multilingual-site guidance](https://developers.google.com/search/docs/specialty/international/managing-multi-regional-sites) explains the role of separate language URLs and language annotations.

Keywords support planning and compatibility metadata. They should inform natural page copy rather than being repeated mechanically. The `meta keywords` tag does not affect Google ranking, according to [Google's supported metadata documentation](https://developers.google.com/search/docs/crawling-indexing/special-tags). The application does not claim keyword search volume or ranking improvements.

The system settings categories now live in an expandable sidebar group alongside **Operations tools**. Mobile screens use a category selector. This keeps every category reachable without a horizontal navigation scrollbar.
