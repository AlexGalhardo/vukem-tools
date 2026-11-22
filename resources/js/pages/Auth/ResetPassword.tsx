import { Head, useForm } from "@inertiajs/react";
import type { FormEvent } from "react";

import Field from "../../components/Field";
import PasswordField from "../../components/PasswordField";
import PasswordStrength from "../../components/PasswordStrength";
import { authSubmitClass } from "./submitClass";

export default function ResetPassword({
  token,
  email,
  codeFile,
}: {
  token: string;
  email: string;
  codeFile?: string;
}) {
  const form = useForm({ token, email, password: "", password_confirmation: "" });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    form.post("/reset-password", { onFinish: () => form.reset("password", "password_confirmation") });
  };

  return (
    <>
      <Head title="Choose a new password" />
      <h1 className="mb-4 text-2xl font-semibold">Choose a new password</h1>
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
        {codeFile ? (
          <>
            <p className="text-zinc-600 dark:text-zinc-400">
              This computer sends no email. If the address has an account, the reset code was saved
              in this file; open it and paste the code below:
              <br />
              <code className="break-all">{codeFile}</code>
            </p>
            <Field
              label="Reset code"
              autoComplete="off"
              required
              value={form.data.token}
              onChange={(e) => form.setData("token", e.target.value)}
              error={form.errors.token}
            />
          </>
        ) : null}
        <div>
          <PasswordField
            label="New password"
            autoComplete="new-password"
            // No minLength or maxLength: the browser counts UTF-16 code units
            // while the server counts runes, so the attribute would refuse a
            // password the API accepts. PasswordStrength does the counting
            // the server's way, and the server has the final word.
            required
            value={form.data.password}
            onChange={(e) => form.setData("password", e.target.value)}
            error={form.errors.password}
          />
          <PasswordStrength
            password={form.data.password}
            confirmation={form.data.password_confirmation}
          />
        </div>
        <PasswordField
          label="Confirm password"
          autoComplete="new-password"
          required
          value={form.data.password_confirmation}
          onChange={(e) => form.setData("password_confirmation", e.target.value)}
          error={form.errors.password_confirmation}
        />
        <button
          type="submit"
          disabled={form.processing}
          className={authSubmitClass}
        >
          Reset password
        </button>
      </form>
    </>
  );
}
