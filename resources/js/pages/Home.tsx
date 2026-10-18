import { Head } from "@inertiajs/react";

export default function Home() {
  return (
    <>
      <Head title="Welcome" />
      <section className="py-12 text-center">
        <h1 className="text-3xl font-semibold tracking-tight">Vukem Tools</h1>
        <p className="mt-3 text-zinc-600 dark:text-zinc-400">
          Convert, compress and optimise your files. The tool pages and the search land in phase
          S1.10 of the roadmap.
        </p>
      </section>
    </>
  );
}
