import { Head, useForm, usePage } from "@inertiajs/react";
import { useState } from "react";

import PasswordField from "../../components/PasswordField";
import PasswordStrength from "../../components/PasswordStrength";
import type { SharedProps } from "../../types";

interface Profile {
	name: string;
	email: string;
	verified: boolean;
	joined_at: string;
}

interface Key {
	id: string;
	name: string;
	abilities: string[];
	last_used_at: string | null;
	expires_at: string | null;
	revoked_at: string | null;
}

interface Usage {
	spent_seconds: number;
	budget_seconds: number;
	resets_at: string;
	exempt: boolean;
}

interface Props {
	profile: Profile;
	keys: Key[];
	abilities: string[];
	// Absent in a process that did not build the conversion pipeline, so the
	// section is omitted rather than shown wrong.
	usage?: Usage;
}

/**
 * The account area.
 *
 * One page with four sections rather than four pages: these are short forms,
 * and somebody who came to rotate a key should not have to go looking for it.
 * Each section is a <section> with its own heading, so a screen reader's
 * landmark list and a keyboard both move between them in one step.
 */
export default function AccountIndex({
	profile,
	keys,
	abilities,
	usage,
}: Props): React.ReactElement {
	const page = usePage<SharedProps>();
	const status = page.props.status as string | undefined;
	// The plaintext key, flashed by the server exactly once. It is not a prop
	// of the page, because a prop survives in the history entry and the back
	// button would put the secret back on screen.
	const freshKey = page.props.key as string | undefined;

	return (
		<>
			<Head title="Account" />

			<div className="max-w-[70ch]">
				<h1 className="text-3xl font-semibold tracking-tight">
					Account
				</h1>

				{status && (
					<p
						role="status"
						className="mt-4 rounded-panel border border-edge bg-raised px-4 py-3 text-sm"
					>
						{status}
					</p>
				)}

				{freshKey && <NewKey value={freshKey} />}

				<ProfileSection profile={profile} />
				{/* A Google-only account has no password it set and no way to reset
            one, so the form would ask for a current password that does not
            exist. */}
				{page.props.passwordAuth !== false && <PasswordSection />}
				<KeysSection keys={keys} abilities={abilities} />
				{usage && <UsageSection usage={usage} />}
			</div>
		</>
	);
}

/** The key, shown once, with a copy button. */
function NewKey({ value }: { value: string }): React.ReactElement {
	const [copied, setCopied] = useState(false);

	const copy = async (): Promise<void> => {
		try {
			await navigator.clipboard.writeText(value);
			setCopied(true);
		} catch {
			// Clipboard access can be denied; the value is selectable either way,
			// which is why it sits in a readonly input rather than in a <code>.
			setCopied(false);
		}
	};

	return (
		<div className="mt-4 rounded-panel border border-signal bg-raised p-5">
			<h2 className="font-medium">Your new key</h2>
			<p className="mt-1 text-sm text-ink-soft">
				This is the only time it is shown. Store it somewhere safe; if
				you lose it, revoke it and make another.
			</p>
			<div className="mt-3 flex gap-2">
				<input
					readOnly
					value={value}
					aria-label="Your new API key"
					onFocus={(e) => e.currentTarget.select()}
					className="tabular min-w-0 flex-1 rounded-field border border-edge bg-sunken px-3 py-2 text-sm"
				/>
				<button
					type="button"
					onClick={copy}
					className="rounded-field border border-edge px-3 py-2 text-sm font-medium transition-colors duration-100 hover:bg-sunken"
				>
					{copied ? "Copied" : "Copy"}
				</button>
			</div>
		</div>
	);
}

function Section({
	title,
	description,
	children,
}: {
	title: string;
	description: string;
	children: React.ReactNode;
}): React.ReactElement {
	return (
		<section className="mt-8 rounded-panel border border-edge bg-raised p-5">
			<h2 className="text-lg font-medium">{title}</h2>
			<p className="mt-1 text-sm text-ink-soft">{description}</p>
			<div className="mt-4">{children}</div>
		</section>
	);
}

function ProfileSection({ profile }: { profile: Profile }): React.ReactElement {
	const form = useForm({ name: profile.name });

	return (
		<Section
			title="Profile"
			description="Your name is what appears in the interface. Your email address is how you sign in."
		>
			<form
				onSubmit={(e) => {
					e.preventDefault();
					form.patch("/account/profile", { preserveScroll: true });
				}}
			>
				<label className="block">
					<span className="label">Name</span>
					<input
						value={form.data.name}
						onChange={(e) => form.setData("name", e.target.value)}
						required
						maxLength={100}
						autoComplete="name"
						className="mt-1 block w-full rounded-field border border-edge bg-sunken px-3 py-2"
					/>
				</label>
				{form.errors.name && (
					<p className="mt-1 text-sm text-bad" role="alert">
						{form.errors.name}
					</p>
				)}

				<div className="mt-4 flex items-baseline justify-between gap-4">
					<p className="text-sm text-ink-soft">
						<span className="font-medium text-ink">
							{profile.email}
						</span>
						{profile.verified
							? " · verified"
							: " · not verified yet"}
					</p>
					<button
						type="submit"
						disabled={
							form.processing || form.data.name === profile.name
						}
						className="rounded-field bg-signal px-4 py-2 text-sm font-medium text-raised transition-opacity duration-100 hover:opacity-90 disabled:opacity-50 dark:text-surface"
					>
						{form.processing ? "Saving…" : "Save"}
					</button>
				</div>

				{/* Said rather than left as a missing field: somebody looking for the
            input should learn why it is not here. */}
				<p className="mt-3 text-sm text-ink-soft">
					To change your email address, write to us. Changing it means
					verifying the new one and keeping the old one working until
					you do, which is not something this form can do safely.
				</p>
			</form>
		</Section>
	);
}

function PasswordSection(): React.ReactElement {
	const form = useForm({
		current_password: "",
		password: "",
		password_confirmation: "",
	});

	return (
		<Section
			title="Password"
			description="Changing your password signs out every other session. This one stays signed in."
		>
			<form
				onSubmit={(e) => {
					e.preventDefault();
					form.put("/account/password", {
						preserveScroll: true,
						onSuccess: () => form.reset(),
					});
				}}
				className="space-y-4"
			>
				<PasswordField
					label="Current password"
					value={form.data.current_password}
					onChange={(e) =>
						form.setData("current_password", e.target.value)
					}
					error={form.errors.current_password}
					autoComplete="current-password"
					required
				/>

				<div>
					<PasswordField
						label="New password"
						value={form.data.password}
						onChange={(e) =>
							form.setData("password", e.target.value)
						}
						error={form.errors.password}
						autoComplete="new-password"
						required
					/>
					<PasswordStrength
						password={form.data.password}
						confirmation={form.data.password_confirmation}
					/>
				</div>

				<PasswordField
					label="Confirm new password"
					value={form.data.password_confirmation}
					onChange={(e) =>
						form.setData("password_confirmation", e.target.value)
					}
					error={form.errors.password_confirmation}
					autoComplete="new-password"
					required
				/>

				<button
					type="submit"
					disabled={form.processing}
					className="rounded-field bg-signal px-4 py-2 text-sm font-medium text-raised transition-opacity duration-100 hover:opacity-90 disabled:opacity-50 dark:text-surface"
				>
					{form.processing ? "Changing…" : "Change password"}
				</button>
			</form>
		</Section>
	);
}

function KeysSection({
	keys,
	abilities,
}: {
	keys: Key[];
	abilities: string[];
}): React.ReactElement {
	const form = useForm<{ device_name: string; abilities: string[] }>({
		device_name: "",
		abilities: [],
	});

	const toggle = (ability: string): void => {
		form.setData(
			"abilities",
			form.data.abilities.includes(ability)
				? form.data.abilities.filter((a) => a !== ability)
				: [...form.data.abilities, ability],
		);
	};

	const live = keys.filter((k) => !k.revoked_at);
	const revoked = keys.filter((k) => k.revoked_at);

	return (
		<Section
			title="API keys"
			description="A key lets a script use the API without your password. Give each one only what it needs: a key that can do everything is a key you cannot safely share."
		>
			{live.length > 0 && (
				<ul className="mb-6 divide-y divide-edge">
					{live.map((k) => (
						<KeyRow key={k.id} entry={k} />
					))}
				</ul>
			)}

			<form
				onSubmit={(e) => {
					e.preventDefault();
					form.post("/account/keys", {
						preserveScroll: true,
						onSuccess: () => form.reset(),
					});
				}}
			>
				<label className="block">
					<span className="label">What is this key for?</span>
					<input
						value={form.data.device_name}
						onChange={(e) =>
							form.setData("device_name", e.target.value)
						}
						placeholder="My laptop, the build server, …"
						required
						maxLength={100}
						className="mt-1 block w-full rounded-field border border-edge bg-sunken px-3 py-2"
					/>
				</label>
				{form.errors.device_name && (
					<p className="mt-1 text-sm text-bad" role="alert">
						{form.errors.device_name}
					</p>
				)}

				<fieldset className="mt-4">
					<legend className="label">What may it do?</legend>
					<div className="mt-2 grid gap-2 sm:grid-cols-2">
						{abilities.map((ability) => (
							<label
								key={ability}
								className="flex items-center gap-2 text-sm"
							>
								<input
									type="checkbox"
									checked={form.data.abilities.includes(
										ability,
									)}
									onChange={() => toggle(ability)}
									className="size-4 rounded-sm border-edge accent-signal"
								/>
								<span className="tabular">{ability}</span>
							</label>
						))}
					</div>
				</fieldset>
				{form.errors.abilities && (
					<p className="mt-1 text-sm text-bad" role="alert">
						{form.errors.abilities}
					</p>
				)}

				<button
					type="submit"
					disabled={
						form.processing || form.data.abilities.length === 0
					}
					className="mt-4 rounded-field bg-signal px-4 py-2 text-sm font-medium text-raised transition-opacity duration-100 hover:opacity-90 disabled:opacity-50 dark:text-surface"
				>
					{form.processing ? "Creating…" : "Create key"}
				</button>
			</form>

			{revoked.length > 0 && (
				<details className="mt-6">
					<summary className="cursor-pointer text-sm text-ink-soft">
						{revoked.length} revoked{" "}
						{revoked.length === 1 ? "key" : "keys"}
					</summary>
					<ul className="mt-2 divide-y divide-edge">
						{revoked.map((k) => (
							<KeyRow key={k.id} entry={k} />
						))}
					</ul>
				</details>
			)}
		</Section>
	);
}

function KeyRow({ entry }: { entry: Key }): React.ReactElement {
	const form = useForm({});
	const revoked = Boolean(entry.revoked_at);

	return (
		<li className="flex items-start justify-between gap-4 py-3">
			<div className="min-w-0">
				<p className="truncate font-medium">{entry.name}</p>
				<p className="mt-0.5 text-sm text-ink-soft">
					{entry.abilities.join(", ")}
				</p>
				<p className="mt-0.5 text-sm text-ink-soft">
					{revoked
						? `Revoked ${formatDate(entry.revoked_at)}`
						: entry.last_used_at
							? `Last used ${formatDate(entry.last_used_at)}`
							: "Never used"}
					{!revoked &&
						entry.expires_at &&
						` · expires ${formatDate(entry.expires_at)}`}
				</p>
			</div>

			{!revoked && (
				<button
					type="button"
					onClick={() =>
						form.delete(`/account/keys/${entry.id}`, {
							preserveScroll: true,
						})
					}
					disabled={form.processing}
					className="shrink-0 rounded-md border border-edge px-3 py-1.5 text-sm hover:border-danger hover:text-bad disabled:opacity-50"
				>
					Revoke
				</button>
			)}
		</li>
	);
}

function UsageSection({ usage }: { usage: Usage }): React.ReactElement {
	const spent = usage.spent_seconds;
	const budget = usage.budget_seconds;
	const unlimited = usage.exempt || budget <= 0;
	// Clamped: a conversion that overran its estimate can push spent past the
	// budget, and a bar wider than its track reads as a rendering bug.
	const share = unlimited
		? 0
		: Math.min(100, Math.round((spent / budget) * 100));

	return (
		<Section
			title="Usage today"
			description="Processing time is counted per day and resets at midnight UTC."
		>
			<p className="tabular text-2xl font-semibold">
				{formatDuration(spent)}
				{!unlimited && (
					<span className="text-base font-normal text-ink-soft">
						{" "}
						of {formatDuration(budget)}
					</span>
				)}
			</p>

			{unlimited ? (
				<p className="mt-2 text-sm text-ink-soft">
					This account has no daily limit.
				</p>
			) : (
				<>
					<div
						role="progressbar"
						aria-valuenow={share}
						aria-valuemin={0}
						aria-valuemax={100}
						aria-label="Processing time used today"
						className="mt-3 h-2 overflow-hidden rounded-full bg-sunken"
					>
						<div
							className={`h-full rounded-full ${share >= 100 ? "bg-bad" : "bg-signal"}`}
							style={{ width: `${share}%` }}
						/>
					</div>
					<p className="mt-2 text-sm text-ink-soft">
						{share >= 100 ? "Spent for today. Resets" : "Resets"}{" "}
						{formatDate(usage.resets_at)}.
					</p>
				</>
			)}
		</Section>
	);
}

/** formatDuration renders seconds the way a person reads a budget. */
function formatDuration(seconds: number): string {
	if (seconds < 60) {
		return `${seconds}s`;
	}

	const minutes = Math.floor(seconds / 60);
	if (minutes < 60) {
		return `${minutes} min`;
	}

	const hours = Math.floor(minutes / 60);
	const rest = minutes % 60;

	return rest === 0 ? `${hours} h` : `${hours} h ${rest} min`;
}

/** formatDate renders a timestamp in the reader's own locale and zone. */
function formatDate(iso: string | null): string {
	if (!iso) {
		return "never";
	}

	const at = new Date(iso);
	if (Number.isNaN(at.getTime())) {
		return "unknown";
	}

	return at.toLocaleString(undefined, {
		dateStyle: "medium",
		timeStyle: "short",
	});
}
