import { loader } from "fumadocs-core/source"
import { metaSchema, pageSchema } from "fumadocs-core/source/schema"
import { defineDocs } from "fumadocs-mdx/macro"

const docs = defineDocs({
  dir: "content/docs",
  docs: { schema: pageSchema },
  meta: { schema: metaSchema },
})

export const source = loader({
  baseUrl: "/docs",
  source: docs.toFumadocsSource(),
})

// Not /api/search: a static export has no API routes, so the index ships as a file.
export const searchIndexRoute = "/search.json"
