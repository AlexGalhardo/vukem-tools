import { Head, useForm } from "@inertiajs/react";
import type { FormEvent } from "react";

import Field from "../../components/Field";
import { authSubmitClass } from "./submitClass";

export default function ForgotPassword({ local = false }: { local?: boolean }) {
  const form = useForm({ email: "" });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    form.post("/forgot-password");
  };

  return (
    <>
      <Head title="Forgot your password?" />
      <h1 className="mb-2 text-2xl font-semibold">Forgot your password?</h1>
      <p className="mb-4 text-zinc-600 dark:text-zinc-400">
        {local
          ? "This computer sends no email. We will save a reset code to a file on it, and you enter the code on the next page."
          : "We will email you a link to choose a new one."}
      </p>
      <form onSubmit={submit} className="space-y-4">
        <Field
          label="Email"
          type="email"
          autoComplete="email"
          required
          value={form.data.email}
          onChange={(e) => form.setData("email", e.target.value)}
          error={form.errors.email}
        />
        <button
          type="submit"
          disabled={form.processing}
          className={authSubmitClass}
        >
          {local ? "Save a reset code" : "Email me a reset link"}
        </button>
      </form>
    </>
  );
}
