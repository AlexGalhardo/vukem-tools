import pkg from "../../package.json" with { type: "json" };

/** The release shown in the sidebar phone header and under the account row. It is package.json. */
export const release: string = pkg.version;
