# Interface languages

The storefront, purchase, customer account and application integration screens
offer fourteen interface languages: Simplified Chinese, English, German, Italian,
Traditional Chinese, Hindi, Spanish, Arabic, French, Bengali, Portuguese,
Indonesian, Urdu and Japanese. Arabic and Urdu use right-to-left page layout;
generated code and English AI integration instructions retain left-to-right text.

Fixed interface labels come from bundled JSON catalogs under
`internal/httpapi/web/interface-locales/`. Simplified Chinese uses the source
labels. These catalogs load locally; they do not call a translation service.
Administration sections outside these functional screens may still contain
source-language labels.

Merchant-authored product content uses the existing content-language settings
and DeepL integration. Selecting an interface language does not create a content
translation or change the merchant's product names, account details, identifiers
or code. When the server has no matching content translation, the interface
retains its selected language and marks merchant content as original-language
content.

The interface choice persists across refreshes. Languages with existing public
URL routes retain those routes; other interface languages use the public hash
route and an explicit `ui_locale` query parameter. Sandbox preview selection is
preserved when switching languages.

Run `node --test scripts/interface-locales.test.mjs` to check required labels,
placeholder preservation, dynamic policy-check messages, merchant-value
preservation and public language navigation.
