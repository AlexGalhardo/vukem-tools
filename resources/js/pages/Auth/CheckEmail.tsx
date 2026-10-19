import { Head, Link } from "@inertiajs/react";

export default function CheckEmail() {
  return (
    <>
      <Head title="Your account is ready" />
      <h1 className="mb-2 text-2xl font-semibold">Your account is ready</h1>
      <p className="mb-4">
        You can sign in now — confirming your address is not required. If that address already had an account, we
        emailed its owner instead, and nothing changed here.
      </p>
      <p className="mb-4 text-ink-soft">
        This page says the same thing either way on purpose, so it never reveals whether an address is registered.
      </p>
      <Link href="/login" className="text-indigo-600 hover:underline dark:text-indigo-400">
        Go to the login page
      </Link>
    </>
  );
}
