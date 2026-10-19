// Shapes of the props the Go controllers send.

export interface User {
  id: number;
  name: string;
  email: string;
  email_verified_at: string | null;
}

export interface SharedProps {
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
