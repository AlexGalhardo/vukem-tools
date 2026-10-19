/** Page is one entry of the header's navigation. */
export type Page = {
  href: string;
  label: string;
  /**
   * document asks for a real browser navigation rather than an Inertia visit.
   *
   * It is required for any destination this app does not render itself.
   * Inertia visits by XHR and expects its own JSON back; a plain HTML
   * response makes its client render the page inside a full-screen error
   * iframe, which looks like a white modal that never navigates and cannot be
   * dismissed. Leaving the app is a document link or nothing.
   */
  document?: boolean;
};

/**
 * The site's own pages, in the order the header lists them.
 *
 * The home page is not one of them: the mark and the name to the left of the
 * row are already the link to it, so listing it again spends 44px of a phone
 * header on a destination the header already offers.
 *
 * They live here rather than in the component so the rule above can be tested
 * without a DOM.
 */
export const headerPages: Page[] = [
  // /docs is the Scalar API reference, which vukem/apidocs serves as plain
  // HTML. It is not an Inertia page and must never be visited as one.
  { href: "/docs", label: "API", document: true },
  { href: "/licenses", label: "Licences" },
];
