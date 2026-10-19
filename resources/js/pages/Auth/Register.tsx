import { Head, Link, useForm, usePage } from "@inertiajs/react";
import type { FormEvent } from "react";

import Field from "../../components/Field";
import PasswordField from "../../components/PasswordField";
import SocialSignIn from "../../components/SocialSignIn";
import type { SharedProps } from "../../types";
import PasswordStrength from "../../components/PasswordStrength";

export default function Register() {
	// GET /register is not registered when PASSWORD_AUTH is off, so this branch
	// is defence in depth: if the page is ever reachable, it must not offer a
	// form that posts to a route that does not exist.
	const { passwordAuth } = usePage<SharedProps>().props;
	const passwords = passwordAuth !== false;
	const form = useForm({
		name: "",
		email: "",
		password: "",
		password_confirmation: "",
	});

	const submit = (e: FormEvent) => {
		e.preventDefault();
		form.post("/register", {
			onFinish: () => form.reset("password", "password_confirmation"),
		});
	};

	return (
		<>
			<Head title="Create an account" />
			<h1 className="mb-4 text-2xl font-semibold">Create an account</h1>
			{!passwords && (
				<p className="mb-4 text-ink-soft">
					Sign up with Google to continue.
				</p>
			)}
			{passwords && (
				<form onSubmit={submit} className="space-y-4">
					<Field
						label="Name"
						autoComplete="name"
						required
						value={form.data.name}
						onChange={(e) => form.setData("name", e.target.value)}
						error={form.errors.name}
					/>
					<Field
						label="Email"
						type="email"
						autoComplete="email"
						required
						value={form.data.email}
						onChange={(e) => form.setData("email", e.target.value)}
						error={form.errors.email}
					/>
					<div>
						<PasswordField
							label="Password"
							autoComplete="new-password"
							// No minLength or maxLength: the browser counts UTF-16 code units
							// while the server counts runes, so the attribute would refuse a
							// password the API accepts. PasswordStrength does the counting
							// the server's way, and the server has the final word.
							required
							value={form.data.password}
							onChange={(e) =>
								form.setData("password", e.target.value)
							}
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
						onChange={(e) =>
							form.setData(
								"password_confirmation",
								e.target.value,
							)
						}
						error={form.errors.password_confirmation}
					/>
					<button
						type="submit"
						disabled={form.processing}
						className="rounded-md bg-indigo-600 px-4 py-2 font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
					>
						Create account
					</button>
				</form>
			)}
			<SocialSignIn action="Sign up" />
			<p className="mt-6 text-sm">
				<Link
					href="/login"
					className="text-indigo-600 hover:underline dark:text-indigo-400"
				>
					Already registered?
				</Link>
			</p>
		</>
	);
}
