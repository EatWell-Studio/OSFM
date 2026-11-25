import type { ReactNode } from 'react'

/** The heading of a page. */
export function PageHeading({ children }: { children: ReactNode }) {
  return <h1 className="p-4 text-xl font-semibold">{children}</h1>
}
