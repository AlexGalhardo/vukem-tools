import { Head } from "@inertiajs/react";

import Icon from "../components/Icon";

interface Component {
  name: string;
  spdx: string;
  url: string;
  role: string;
  copyleft: boolean;
}

interface Props {
  project: Component;
  components: Component[];
  copyleft: Component[];
}

/**
 * The licence notice.
 *
 * This page exists because of an obligation, not as a courtesy: Ghostscript is
 * AGPL-3.0 and the ffmpeg build is GPL, and the AGPL reaches anyone who makes
 * the software available over a network. So the consequence is stated first,
 * in plain words, before the table — a visitor who runs their own copy has to
 * learn what that means without reading twenty rows.
 */
export default function Licenses({ project, components, copyleft }: Props): React.ReactElement {
  return (
    <>
      <Head title="Licences" />

      <div className="max-w-[70ch]">
        <h1 className="text-3xl font-semibold tracking-tight">Licences</h1>
        <p className="mt-4 text-ink-soft">
          Everything this service runs, and the terms it runs under. Each licence was read from the
          project&rsquo;s own repository rather than assumed; where one could not be confirmed, the
          row says so instead of guessing.
        </p>

        <div className="mt-6 rounded-panel border border-edge bg-raised p-5">
          <p className="text-sm">
            <span className="font-medium">{project.name}</span> itself is{" "}
            <span className="tabular">{project.spdx}</span>:{" "}
            <a href={project.url} className="text-signal hover:underline">
              read the source
            </a>
            .
          </p>
        </div>

        {copyleft.length > 0 && (
          <section className="mt-6 rounded-panel border border-warn/30 bg-warn-soft p-5">
            <h2 className="flex items-center gap-2 font-semibold">
              <Icon name="alert" className="size-4 text-warn" />
              If you run your own copy, read this part
            </h2>
            <p className="mt-2 text-sm text-ink-soft">
              {copyleft.length === 1 ? "One engine" : `${copyleft.length} engines`} below{" "}
              {copyleft.length === 1 ? "carries" : "carry"} a copyleft licence, which places
              obligations on whoever distributes or hosts the software &mdash; not only on us. The
              AGPL in particular applies when you offer it over a network, even without shipping a
              binary.
            </p>
            <ul className="mt-3 space-y-1 text-sm">
              {copyleft.map((c) => (
                <li key={c.name}>
                  <a href={c.url} className="font-medium text-signal hover:underline">
                    {c.name}
                  </a>{" "}
                  <span className="tabular text-ink-faint">{c.spdx}</span> &mdash; {c.role}
                </li>
              ))}
            </ul>
            <p className="mt-3 text-sm text-ink-soft">
              Using this hosted service puts no obligation on you. Self-hosting it does.
            </p>
          </section>
        )}
      </div>

      <section className="mt-10">
        <h2 className="label border-b border-edge pb-2">Everything we run</h2>
        {/* A table, because this is tabular data: three columns a reader
            compares across rows. The licence column is monospaced so the SPDX
            identifiers line up and scan as a column. */}
        <table className="w-full text-sm">
          <thead className="sr-only">
            <tr>
              <th scope="col">Component</th>
              <th scope="col">Licence</th>
              <th scope="col">What it does here</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-edge">
            {components.map((c) => (
              <tr key={c.name} className="align-baseline">
                <th scope="row" className="py-3 pr-4 text-left font-medium">
                  <a href={c.url} className="text-signal hover:underline">
                    {c.name}
                  </a>
                </th>
                <td className="tabular whitespace-nowrap py-3 pr-4 text-ink-soft">
                  {c.spdx}
                  {c.copyleft && (
                    <span className="ml-2 rounded-full border border-warn/30 bg-warn-soft px-1.5 py-0.5 text-[0.625rem] font-semibold uppercase tracking-[0.06em] text-warn">
                      copyleft
                    </span>
                  )}
                </td>
                <td className="py-3 text-ink-soft">{c.role}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
    </>
  );
}
