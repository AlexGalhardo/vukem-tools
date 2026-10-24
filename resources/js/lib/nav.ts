/** Page is one entry of the sidebar's navigation. */
export type Page = {
  href: string;
  label: string;
  /** icon names an icon in components/Icon.tsx. */
  icon: string;
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

/** The tools and the account: what somebody comes to the app to use. */
export const mainPages: Page[] = [
  { href: "/", label: "All tools", icon: "grid" },
  { href: "/account", label: "Account", icon: "user" },
];

/**
 * The links at the foot of the sidebar, in order. The licence notice is not
 * one of them: the page footer already carries it.
 *
 * They live here rather than in the component so the rule above can be tested
 * without a DOM.
 */
export const footerPages: Page[] = [
  // /docs is the Scalar API reference, which vukem/apidocs serves as plain
  // HTML. It is not an Inertia page and must never be visited as one.
  { href: "/docs", label: "API reference", icon: "document", document: true },
];

/** Every page the sidebar links to. */
export const headerPages: Page[] = [...mainPages, ...footerPages];
