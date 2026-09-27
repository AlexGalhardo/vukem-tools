"use client";

import { useEffect, useSyncExternalStore } from "react";
import {
	BACKEND_UNREACHABLE,
	formatDeploy,
	getDeploy,
	recordDeploy,
	subscribeDeploy,
} from "@/lib/deploy-info";
import { cn } from "@/lib/utils";

/** Mostra qual deploy (frontend + API) respondeu a última chamada. */
export function DeployBadge() {
	const deploy = useSyncExternalStore(subscribeDeploy, getDeploy, () => null);

	useEffect(() => {
		fetch("/api/deploy", { cache: "no-store" })
			.then((res) => recordDeploy(res.headers))
			.catch(() => {});
	}, []);

	if (!deploy) return null;
	const down = deploy.backend === BACKEND_UNREACHABLE;
	const label = formatDeploy(deploy);

	return (
		<span
			title={`Respondido por ${label}`}
			className={cn(
				"hidden max-w-64 truncate rounded-md border px-2 py-0.5 text-[11px] md:inline",
				down
					? "border-destructive/40 text-destructive"
					: "border-border/60 text-muted-foreground",
			)}
		>
			{deploy.frontend} → {deploy.backend ?? "?"}
		</span>
	);
}
