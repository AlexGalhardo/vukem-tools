// Shapes of the props the Go controllers send.

export interface User {
  id: number;
  name: string;
  email: string;
  email_verified_at: string | null;
}

export interface SharedProps {
  appName: string;
  errors: Record<string, string>;
  auth?: { user: User | null };
  [key: string]: unknown;
}
