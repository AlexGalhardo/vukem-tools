import { Head, Link, useForm, usePage } from "@inertiajs/react";
import type { FormEvent } from "react";

import Field from "../../components/Field";
import PasswordField from "../../components/PasswordField";
import SocialSignIn from "../../components/SocialSignIn";
import type { SharedProps } from "../../types";

export default function Login() {
	// PASSWORD_AUTH is off until a real sending domain is verified: password
	// reset needs mail that reaches a stranger's inbox, and the sandbox
	// delivers nothing. The routes are not registered either, so this hides a
	// form that would 404 rather than merely tidying the page.
	const { passwordAuth } = usePage<SharedProps>().props;
	const passwords = passwordAuth !== false;
	const form = useForm({ email: "", password: "", remember: false });

	const submit = (e: FormEvent) => {
		e.preventDefault();
		form.post("/login", { onFinish: () => form.reset("password") });
	};

	return (
		<>
			<Head title="Log in" />
			<h1 className="mb-4 text-2xl font-semibold">Log in</h1>
			{!passwords && (
				<p className="mb-4 text-ink-soft">
					Sign in with Google to continue.
				</p>
			)}
			{passwords && (
				<form onSubmit={submit} className="space-y-4">
					<Field
						label="Email"
						type="email"
						autoComplete="username"
						required
						value={form.data.email}
						onChange={(e) => form.setData("email", e.target.value)}
						error={form.errors.email}
					/>
					<PasswordField
						label="Password"
						autoComplete="current-password"
						required
						value={form.data.password}
						onChange={(e) =>
							form.setData("password", e.target.value)
						}
						error={form.errors.password}
					/>
					<label className="flex items-center gap-2">
						<input
							type="checkbox"
							checked={form.data.remember}
							onChange={(e) =>
								form.setData("remember", e.target.checked)
							}
						/>
						Remember me
					</label>
					<button
						type="submit"
						disabled={form.processing}
						className="rounded-md bg-indigo-600 px-4 py-2 font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
					>
						Log in
					</button>
				</form>
			)}
			<SocialSignIn action="Log in" />
			{passwords && (
				<p className="mt-6 flex gap-4 text-sm">
					<Link
						href="/forgot-password"
						className="text-indigo-600 hover:underline dark:text-indigo-400"
					>
						Forgot your password?
					</Link>
					<Link
						href="/register"
						className="text-indigo-600 hover:underline dark:text-indigo-400"
					>
						Create an account
					</Link>
				</p>
			)}
		</>
	);
}
