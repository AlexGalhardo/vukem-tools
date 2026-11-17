import pkg from "../../package.json" with { type: "json" };

/** The release shown at the bottom left of every page. It is package.json. */
export const release: string = pkg.version;
