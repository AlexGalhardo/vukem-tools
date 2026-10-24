// Shapes of the props the Go controllers send.

export interface User {
  id: number;
  name: string;
  email: string;
  email_verified_at: string | null;
}

/** ToolGroup is one dropdown of the sidebar: the usable tools of a family. */
export interface ToolGroup {
  name: string;
  tools: { name: string; title: string; icon: string; search: string }[];
}

export interface SharedProps {
  /** toolGroups lists the usable tools, shared with every page. */
  toolGroups?: ToolGroup[];
  appName: string;
  // Only the providers the server has credentials for.
  socialProviders?: string[];
  // False when PASSWORD_AUTH is off: the email and password routes are not
  // registered, so a form posting to them would 404.
  passwordAuth?: boolean;
  errors: Record<string, string>;
  auth?: { user: User | null };
  [key: string]: unknown;
}
